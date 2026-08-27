package programmatic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// ErrCorrelationRequired reports a command that cannot receive a correlated response.
var ErrCorrelationRequired = errors.New("correlation ID is required")

// Service coordinates one Programmatic Control connection.
type Service struct {
	coordinator     Coordinator
	modelCatalog    ModelCatalog
	stateSnapshot   func() run.State
	historySnapshot func() []agent.HistoryEntry
	sessionControl  SessionControl
	delivery        *Delivery
}

var _ controller.HostSession = (*Service)(nil)

// New creates one Programmatic Control session over a synchronous delivery router.
func New(
	coordinator Coordinator,
	modelCatalog ModelCatalog,
	stateSnapshot func() run.State,
	historySnapshot func() []agent.HistoryEntry,
	sessionControl SessionControl,
	delivery *Delivery,
) *Service {
	return &Service{
		coordinator: coordinator, modelCatalog: modelCatalog, stateSnapshot: stateSnapshot,
		historySnapshot: historySnapshot,
		sessionControl:  sessionControl, delivery: delivery,
	}
}

// Handle executes one transport-independent command and returns its single response.
func (s *Service) Handle(
	ctx context.Context,
	command controller.Command,
) (controller.Response, controller.Operation, error) {
	current, rejection, err := s.preflight(command)
	if err != nil {
		return controller.Response{}, nil, err
	}
	if rejection != nil {
		return *rejection, nil, nil
	}

	if response, handled, handleErr := s.handleImmediate(ctx, command, current); handled {
		return response, nil, handleErr
	}

	runID, err := s.coordinator.PrepareRun()
	if err != nil {
		return s.runPreparationRejected(command, err)
	}

	userText, present := command.UserText.Get()
	if !present {
		return s.rejection(command, controller.RejectionInvalidArgument, "user text is required"), nil, nil
	}
	runContext, cancel := context.WithCancel(ctx)
	operation := &activeRun{
		delivery:      s.delivery,
		correlationID: command.CorrelationID,
		runID:         runID,
		coordinator:   s.coordinator,
		userText:      userText,
		runContext:    runContext,
		cancel:        cancel,
		events:        make(chan controller.AgentEvent),
		streamDone:    make(chan struct{}),
		done:          make(chan struct{}),
		state:         operationAccepted,
		streamStopped: false,
		err:           nil,
	}
	if !s.delivery.reserve(operation) {
		// Delivery did not accept ownership, so this path must release the prepared run reservation.
		s.coordinator.CancelPrepared(runID)
		cancel()
		close(operation.events)
		close(operation.streamDone)
		close(operation.done)
		return s.rejection(command, controller.RejectionBusy, "a run is active"), nil, nil
	}

	return emptyResponse(command.CorrelationID, controller.ResponseUserRequestAccepted), operation, nil
}

// handleImmediate dispatches commands that do not transfer ownership to an asynchronous run.
func (s *Service) handleImmediate(
	ctx context.Context,
	command controller.Command,
	current *activeRun,
) (controller.Response, bool, error) {
	if response, handled := s.handleSessionImmediate(ctx, command); handled {
		return response, true, nil
	}
	switch command.Kind {
	case controller.CommandAbort:
		response, err := s.abort(command.CorrelationID, current)
		return response, true, err
	case controller.CommandGetRunState:
		return s.runState(command.CorrelationID, current), true, nil
	case controller.CommandGetMessages:
		response, err := s.messages(command.CorrelationID)
		return response, true, err
	case controller.CommandGetModels:
		return s.models(command.CorrelationID), true, nil
	case controller.CommandSelectModel:
		return s.selectModel(ctx, command), true, nil
	case controller.CommandSelectReasoningChoice:
		return s.selectReasoningChoice(command), true, nil
	case controller.CommandUnspecified:
		return s.rejection(command, controller.RejectionInvalidArgument, "invalid command payload"), true, nil
	case controller.CommandUserRequest:
		return controller.Response{}, false, nil
	case controller.CommandCreateSession, controller.CommandListSessions, controller.CommandResumeSession,
		controller.CommandSetSessionName, controller.CommandGetSessionInfo, controller.CommandGetSessionEntries,
		controller.CommandGetSessionStats:
		return controller.Response{}, false, nil
	default:
		return s.rejection(command, controller.RejectionInvalidArgument, "invalid command payload"), true, nil
	}
}

// handleSessionImmediate routes session commands that do not require run coordination.
func (s *Service) handleSessionImmediate(ctx context.Context, command controller.Command) (controller.Response, bool) {
	switch command.Kind {
	case controller.CommandCreateSession:
		return s.createSession(ctx, command), true
	case controller.CommandListSessions:
		return s.listSessions(ctx, command), true
	case controller.CommandResumeSession:
		return s.resumeSession(ctx, command), true
	case controller.CommandSetSessionName:
		return s.setSessionName(ctx, command), true
	case controller.CommandGetSessionInfo:
		return sessionInfoResponse(command.CorrelationID, s.sessionControl.Info()), true
	case controller.CommandGetSessionEntries:
		return s.sessionEntries(command), true
	case controller.CommandGetSessionStats:
		return sessionStatisticsResponse(command.CorrelationID, s.sessionControl.Statistics()), true
	case controller.CommandUnspecified, controller.CommandUserRequest, controller.CommandAbort,
		controller.CommandGetRunState, controller.CommandGetMessages, controller.CommandGetModels,
		controller.CommandSelectModel, controller.CommandSelectReasoningChoice:
		return controller.Response{}, false
	default:
		return controller.Response{}, false
	}
}

// CancelAndWait cancels and joins work owned by the controller connection.
func (s *Service) CancelAndWait(context.Context) error {
	return s.delivery.cancelAndWaitAll()
}

func (s *Service) abort(correlationID string, active *activeRun) (controller.Response, error) {
	if err := s.delivery.cancelAndWait(active); err != nil {
		return controller.Response{}, fmt.Errorf("abort Programmatic Control run: %w", err)
	}
	return emptyResponse(correlationID, controller.ResponseAbortCompleted), nil
}

func (s *Service) runState(correlationID string, active *activeRun) controller.Response {
	state := s.stateSnapshot()
	publicState := controller.RunStateIdle
	if active != nil || state.Status == run.StatusRunning || state.Status == run.StatusAwaitingSettlement {
		publicState = controller.RunStateRunning
	}
	activeCorrelationID := mo.None[string]()
	if publicState == controller.RunStateRunning && active != nil {
		activeCorrelationID = mo.Some(active.correlationID)
	}
	response := emptyResponse(correlationID, controller.ResponseRunState)
	response.State = mo.Some(controller.RunStateResult{
		State:               publicState,
		ActiveCorrelationID: activeCorrelationID,
	})
	return response
}

func (s *Service) messages(correlationID string) (controller.Response, error) {
	response := emptyResponse(correlationID, controller.ResponseMessages)
	messages, err := mapHistory(s.historySnapshot())
	if err != nil {
		return controller.Response{}, err
	}
	response.Messages = messages
	return response, nil
}

func (s *Service) models(correlationID string) controller.Response {
	response := emptyResponse(correlationID, controller.ResponseModels)
	response.Models = mo.Some(controller.ModelsResult{
		Models:          s.modelCatalog.Models(),
		ActiveSelection: mo.Some(s.modelCatalog.Selection()),
	})
	return response
}

func (s *Service) selectModel(ctx context.Context, command controller.Command) controller.Response {
	providerID, hasProvider := command.ProviderID.Get()
	modelID, hasModel := command.ModelID.Get()
	if !hasProvider || !hasModel {
		return s.rejection(command, controller.RejectionInvalidArgument, "provider and model are required")
	}
	selection, err := s.modelCatalog.SelectModel(ctx, providerID, modelID)
	if err != nil {
		return s.selectionRejected(command, err)
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseModelSelection)
	response.Selection = mo.Some(selection)
	return response
}

func (s *Service) selectReasoningChoice(command controller.Command) controller.Response {
	reasoningChoice, present := command.ReasoningChoice.Get()
	if !present {
		return s.rejection(command, controller.RejectionInvalidArgument, "reasoning choice is required")
	}
	selection, err := s.modelCatalog.SelectReasoningChoice(reasoningChoice)
	if err != nil {
		return s.selectionRejected(command, err)
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseModelSelection)
	response.Selection = mo.Some(selection)
	return response
}

func (s *Service) selectionRejected(command controller.Command, err error) controller.Response {
	var selectionFailure SelectionFailure
	if !errors.As(err, &selectionFailure) {
		return s.rejection(command, controller.RejectionInternal, "model selection failed")
	}
	code := controller.RejectionInternal
	switch SelectionCode(selectionFailure.SelectionCode()) {
	case SelectionNotFound:
		code = controller.RejectionNotFound
	case SelectionReasoningUnsupported:
		code = controller.RejectionReasoningUnsupported
	case SelectionCredentialUnavailable:
		code = controller.RejectionCredentialUnavailable
	default:
	}
	return s.rejection(command, code, selectionFailure.Error())
}

// createSession returns replacement information only after the shared gate and active state commit succeed.
func (s *Service) createSession(ctx context.Context, command controller.Command) controller.Response {
	replacement, err := s.sessionControl.Create(ctx)
	if err != nil {
		return s.sessionRejection(command, err)
	}
	return sessionInfoResponse(command.CorrelationID, replacement.Info)
}

// listSessions maps the ordered persisted-session view without changing active state.
func (s *Service) listSessions(ctx context.Context, command controller.Command) controller.Response {
	listed, err := s.sessionControl.List(ctx)
	if err != nil {
		return s.sessionRejection(command, err)
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseSessions)
	response.Sessions = listed
	return response
}

// resumeSession preserves the previous active session when load or replacement fails.
func (s *Service) resumeSession(ctx context.Context, command controller.Command) controller.Response {
	id, present := command.SessionID.Get()
	if !present || id == "" {
		return s.rejection(command, controller.RejectionInvalidArgument, "session ID is required")
	}
	replacement, err := s.sessionControl.Resume(ctx, id)
	if err != nil {
		return s.sessionRejection(command, err)
	}
	return sessionInfoResponse(command.CorrelationID, replacement.Info)
}

// setSessionName returns the information snapshot produced by the durable name append.
func (s *Service) setSessionName(ctx context.Context, command controller.Command) controller.Response {
	name, present := command.SessionName.Get()
	if !present {
		return s.rejection(command, controller.RejectionInvalidArgument, "session name is required")
	}
	info, err := s.sessionControl.SetName(ctx, name)
	if err != nil {
		return s.sessionRejection(command, err)
	}
	return sessionInfoResponse(command.CorrelationID, info)
}

// sessionInfo returns the current active-session snapshot without taking the replacement gate.
func (s *Service) sessionEntries(command controller.Command) controller.Response {
	entries, err := mapSessionEntries(s.sessionControl.Entries())
	if err != nil {
		return s.rejection(command, controller.RejectionInternal, "Session entries are unavailable.")
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseSessionEntries)
	response.SessionEntries = entries
	return response
}

// sessionRejection maps domain failures to stable client-safe rejection codes and messages.
func (s *Service) sessionRejection(command controller.Command, err error) controller.Response {
	switch {
	case errors.Is(err, session.ErrBusy):
		return s.rejection(command, controller.RejectionBusy, "another operation is active")
	case errors.Is(err, session.ErrInvalidName):
		return s.rejection(command, controller.RejectionInvalidArgument, "session name is required")
	case errors.Is(err, session.ErrUnavailable):
		return s.rejection(command, controller.RejectionSessionUnavailable, "session is unavailable")
	case errors.Is(err, os.ErrNotExist):
		return s.rejection(command, controller.RejectionNotFound, "session was not found")
	default:
		return s.rejection(command, controller.RejectionInternal, "session operation failed")
	}
}

// sessionInfoResponse initializes only the session-information response variant.
func sessionInfoResponse(correlationID string, info session.Info) controller.Response {
	response := emptyResponse(correlationID, controller.ResponseSessionInfo)
	response.SessionInfo = mo.Some(info)
	return response
}

// sessionStatisticsResponse initializes the complete statistics response variant.
func sessionStatisticsResponse(correlationID string, statistics session.Statistics) controller.Response {
	return controller.Response{
		SessionEntries:    nil,
		CorrelationID:     correlationID,
		Kind:              controller.ResponseSessionStats,
		State:             mo.None[controller.RunStateResult](),
		Messages:          nil,
		Models:            mo.None[controller.ModelsResult](),
		Selection:         mo.None[model.Selection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.Some(statistics),
		Rejection:         mo.None[controller.Rejection](),
	}
}

func (s *Service) preflight(
	command controller.Command,
) (*activeRun, *controller.Response, error) {
	if command.CorrelationID == "" {
		return nil, nil, ErrCorrelationRequired
	}
	if invalidCommand(command) {
		response := s.rejection(command, controller.RejectionInvalidArgument, "invalid command payload")
		return nil, &response, nil
	}
	active := s.delivery.activeSnapshot()
	if active != nil && active.correlationID == command.CorrelationID {
		response := s.rejection(command, controller.RejectionCorrelationInUse, "correlation ID is active")
		return active, &response, nil
	}
	if command.Kind == controller.CommandUserRequest && active != nil {
		response := s.rejection(command, controller.RejectionBusy, "a run is active")
		return active, &response, nil
	}
	if command.Kind == controller.CommandAbort && active == nil {
		response := s.rejection(command, controller.RejectionNoActiveRun, "no run is active")
		return nil, &response, nil
	}
	return active, nil, nil
}

func invalidCommand(command controller.Command) bool {
	if invalid, handled := invalidSessionCommand(command); handled {
		return invalid
	}
	switch command.Kind {
	case controller.CommandUserRequest:
		return invalidUserRequest(command)
	case controller.CommandAbort, controller.CommandGetRunState,
		controller.CommandGetMessages, controller.CommandGetModels:
		return command.UserText.IsSome() || hasModelArguments(command) || hasSessionArguments(command)
	case controller.CommandSelectModel:
		return invalidModelSelection(command)
	case controller.CommandSelectReasoningChoice:
		return invalidReasoningSelection(command)
	case controller.CommandCreateSession, controller.CommandListSessions,
		controller.CommandResumeSession, controller.CommandSetSessionName,
		controller.CommandGetSessionInfo, controller.CommandGetSessionEntries, controller.CommandGetSessionStats:
		return true
	case controller.CommandUnspecified:
		return true
	}
	return true
}

// invalidSessionCommand validates exact argument presence for lifecycle commands.
func invalidSessionCommand(command controller.Command) (invalid, handled bool) {
	switch command.Kind {
	case controller.CommandCreateSession, controller.CommandListSessions,
		controller.CommandGetSessionInfo, controller.CommandGetSessionEntries, controller.CommandGetSessionStats:
		return command.UserText.IsSome() || hasModelArguments(command) || hasSessionArguments(command), true
	case controller.CommandResumeSession:
		id, present := command.SessionID.Get()
		isInvalid := !present || id == "" || command.SessionName.IsSome() ||
			command.UserText.IsSome() || hasModelArguments(command)
		return isInvalid, true
	case controller.CommandSetSessionName:
		isInvalid := command.SessionName.IsNone() || command.SessionID.IsSome() ||
			command.UserText.IsSome() || hasModelArguments(command)
		return isInvalid, true
	case controller.CommandUnspecified, controller.CommandUserRequest, controller.CommandAbort,
		controller.CommandGetRunState, controller.CommandGetMessages, controller.CommandGetModels,
		controller.CommandSelectModel, controller.CommandSelectReasoningChoice:
		return false, false
	default:
		return false, false
	}
}

// invalidUserRequest validates the payload selected by a user-request discriminator.
func invalidUserRequest(command controller.Command) bool {
	userText, ok := command.UserText.Get()
	return !ok || strings.TrimSpace(userText) == "" || hasModelArguments(command) || hasSessionArguments(command)
}

// invalidModelSelection validates the payload selected by a model-selection discriminator.
func invalidModelSelection(command controller.Command) bool {
	providerID, providerPresent := command.ProviderID.Get()
	modelID, modelPresent := command.ModelID.Get()
	return command.UserText.IsSome() || !providerPresent || providerID == "" ||
		!modelPresent || modelID == "" || command.ReasoningChoice.IsSome() || hasSessionArguments(command)
}

// invalidReasoningSelection validates the payload selected by a reasoning-selection discriminator.
func invalidReasoningSelection(command controller.Command) bool {
	reasoningChoice, reasoningPresent := command.ReasoningChoice.Get()
	return command.UserText.IsSome() || command.ProviderID.IsSome() || command.ModelID.IsSome() ||
		!reasoningPresent || !validReasoningChoice(reasoningChoice) || hasSessionArguments(command)
}

func hasModelArguments(command controller.Command) bool {
	return command.ProviderID.IsSome() || command.ModelID.IsSome() || command.ReasoningChoice.IsSome()
}

// hasSessionArguments reports arguments that are invalid on non-session commands.
func hasSessionArguments(command controller.Command) bool {
	return command.SessionID.IsSome() || command.SessionName.IsSome()
}

func validReasoningChoice(level model.ReasoningChoice) bool {
	switch level {
	case model.ReasoningChoiceOff, model.ReasoningChoiceOn, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
		model.ReasoningChoiceMedium, model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh,
		model.ReasoningChoiceMax:
		return true
	default:
		return false
	}
}

func filterRunError(outcome agent.RunOutcome, runErr error) error {
	if outcome != agent.RunOutcomeAborted {
		return runErr
	}
	return removeCancellation(runErr)
}

func removeCancellation(err error) error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		filtered := lo.FilterMap(joined.Unwrap(), func(child error, _ int) (error, bool) {
			childErr := removeCancellation(child)
			return childErr, childErr != nil
		})
		return errors.Join(filtered...)
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		child := wrapped.Unwrap()
		filtered := removeCancellation(child)
		if filtered == nil {
			return nil
		}
		if errors.Is(err, context.Canceled) {
			return filtered
		}
		return err
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func emptyResponse(correlationID string, kind controller.ResponseKind) controller.Response {
	return controller.Response{
		SessionEntries:    nil,
		CorrelationID:     correlationID,
		Kind:              kind,
		State:             mo.None[controller.RunStateResult](),
		Messages:          nil,
		Models:            mo.None[controller.ModelsResult](),
		Selection:         mo.None[model.Selection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](),
		Rejection:         mo.None[controller.Rejection](),
	}
}

// runPreparationRejected keeps run ID allocation failure inside the command response.
func (s *Service) runPreparationRejected(
	command controller.Command,
	prepareErr error,
) (controller.Response, controller.Operation, error) {
	if errors.Is(prepareErr, session.ErrBusy) {
		return s.rejection(command, controller.RejectionBusy, "another operation is active"), nil, nil
	}
	return s.rejection(
		command, controller.RejectionInternal, "Host run ID allocation failed",
	), nil, nil
}

func (s *Service) rejection(
	command controller.Command,
	code controller.RejectionCode,
	message string,
) controller.Response {
	response := emptyResponse(command.CorrelationID, controller.ResponseRejected)
	response.Rejection = mo.Some(controller.Rejection{Command: command.Kind, Code: code, Message: message})
	return response
}
