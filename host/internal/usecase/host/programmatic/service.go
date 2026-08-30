package programmatic

import (
	"context"
	"errors"
	"fmt"
	"os"

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
	// coordinator prepares and executes Agent Core runs.
	coordinator Coordinator
	// modelCatalog owns configured models and the active selection.
	modelCatalog ModelCatalog
	// stateSnapshot returns the current Agent Core state.
	stateSnapshot func() run.State
	// historySnapshot returns canonical public conversation history.
	historySnapshot func() []agent.HistoryEntry
	// sessionControl owns active-session lifecycle operations.
	sessionControl SessionControl
	// delivery correlates run events with accepted requests.
	delivery *Delivery
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
		controller.CommandGetSessionStats, controller.CommandGetSessionTree, controller.CommandNavigateSessionTree:
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
	case controller.CommandGetSessionTree:
		return s.sessionTree(command), true
	case controller.CommandNavigateSessionTree:
		return s.navigateSessionTree(ctx, command), true
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
		return s.rejection(command, controller.RejectionInternal, fmt.Sprintf("model selection failed: %v", err))
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

// navigateSessionTree commits no-summary navigation or returns one classified terminal result.
func (s *Service) navigateSessionTree(ctx context.Context, command controller.Command) controller.Response {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return s.rejection(command, controller.RejectionInvalidArgument, "target entry ID is required")
	}
	if command.SummaryMode != controller.SummaryModeNoSummary {
		return s.rejection(command, controller.RejectionInvalidArgument, "summary mode is not available")
	}
	result, err := s.sessionControl.Navigate(ctx, targetID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			response := emptyResponse(command.CorrelationID, controller.ResponseSessionTreeNavigation)
			response.TreeNavigation = mo.Some(controller.TreeNavigationResult{
				Status: controller.TreeNavigationStatusCanceled, Committed: mo.None[controller.TreeNavigationCommitted](),
			})
			return response
		}
		return s.sessionRejection(command, err)
	}
	committed, mapErr := mapTreeNavigationCommitted(result)
	if mapErr != nil {
		return s.rejection(command, controller.RejectionInternal, fmt.Sprintf("Session tree is unavailable: %v", mapErr))
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseSessionTreeNavigation)
	response.TreeNavigation = mo.Some(controller.TreeNavigationResult{
		Status: controller.TreeNavigationStatusCommitted, Committed: mo.Some(committed),
	})
	return response
}

// sessionEntries returns the current active-session entries without taking the replacement gate.
func (s *Service) sessionEntries(command controller.Command) controller.Response {
	entries, err := mapSessionEntries(s.sessionControl.Entries())
	if err != nil {
		return s.rejection(command, controller.RejectionInternal, fmt.Sprintf("Session entries are unavailable: %v", err))
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseSessionEntries)
	response.SessionEntries = entries
	return response
}

// sessionRejection maps domain failures to stable rejection codes while retaining error details.
func (s *Service) sessionRejection(command controller.Command, err error) controller.Response {
	switch {
	case errors.Is(err, session.ErrBusy):
		return s.rejection(command, controller.RejectionBusy, "another operation is active")
	case errors.Is(err, session.ErrInvalidName):
		return s.rejection(command, controller.RejectionInvalidArgument, "session name is required")
	case errors.Is(err, session.ErrEntryNotFound):
		return s.rejection(command, controller.RejectionNotFound, "session tree entry was not found")
	case errors.Is(err, session.ErrPersistenceUnavailable):
		return s.rejection(command, controller.RejectionPersistenceUnavailable, err.Error())
	case errors.Is(err, session.ErrUnavailable):
		return s.rejection(command, controller.RejectionSessionUnavailable, err.Error())
	case errors.Is(err, os.ErrNotExist):
		return s.rejection(command, controller.RejectionNotFound, "session was not found")
	default:
		return s.rejection(command, controller.RejectionInternal, fmt.Sprintf("session operation failed: %v", err))
	}
}

// sessionTree returns the complete active-session tree without private extension payload bytes.
func (s *Service) sessionTree(command controller.Command) controller.Response {
	tree, err := mapSessionTree(s.sessionControl.Tree())
	if err != nil {
		return s.rejection(command, controller.RejectionInternal, fmt.Sprintf("Session tree is unavailable: %v", err))
	}
	response := emptyResponse(command.CorrelationID, controller.ResponseSessionTree)
	response.SessionTree = mo.Some(tree)
	return response
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
		SessionTree:       mo.None[controller.SessionTree](),
		TreeNavigation:    mo.None[controller.TreeNavigationResult](),
		Rejection:         mo.None[controller.Rejection](),
	}
}

func (s *Service) preflight(
	command controller.Command,
) (*activeRun, *controller.Response, error) {
	if command.CorrelationID == "" {
		return nil, nil, ErrCorrelationRequired
	}
	if !command.Valid() {
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
		SessionTree:       mo.None[controller.SessionTree](),
		TreeNavigation:    mo.None[controller.TreeNavigationResult](),
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
		command, controller.RejectionInternal, fmt.Sprintf("Host run ID allocation failed: %v", prepareErr),
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
