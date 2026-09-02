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
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/internal/operation"
)

// ErrOperationIDRequired reports an operation without an identifier.
var ErrOperationIDRequired = errors.New("operation ID is required")

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
	// delivery routes run progress and settlement to active operations.
	delivery *Delivery
}

var _ controller.HostSession = (*Service)(nil)

// New creates one Programmatic Control session over the agent-run delivery router.
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

// handle executes one prepared transport-independent operation.
func (s *Service) handle(
	ctx context.Context,
	command controller.Command,
) (controller.Response, *activeRun, error) {
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
		return s.rejection(command, controller.RejectionInvalidArgument, errors.New("user text is required")), nil, nil
	}
	runContext, cancel := context.WithCancel(ctx)
	preparedRun := &activeRun{
		delivery:      s.delivery,
		operationID:   command.OperationID,
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
	if !s.delivery.reserve(preparedRun) {
		// Delivery did not accept ownership, so this path must release the prepared run reservation.
		s.coordinator.CancelPrepared(runID)
		cancel()
		close(preparedRun.events)
		close(preparedRun.streamDone)
		close(preparedRun.done)
		return s.rejection(command, controller.RejectionBusy, errors.New("a run is active")), nil, nil
	}

	return emptyResponse(command.OperationID, controller.ResponseUserRequestCompleted), preparedRun, nil
}

// handleImmediate dispatches commands that do not own an agent-run event stream.
func (s *Service) handleImmediate(
	ctx context.Context,
	command controller.Command,
	current *activeRun,
) (controller.Response, bool, error) {
	if response, handled, sessionErr := s.handleSessionImmediate(ctx, command); handled {
		return response, true, sessionErr
	}
	switch command.Kind {
	case controller.CommandGetRunState:
		return s.runState(command.OperationID, current), true, nil
	case controller.CommandGetMessages:
		response, err := s.messages(command.OperationID)
		return response, true, err
	case controller.CommandGetModels:
		return s.models(command.OperationID), true, nil
	case controller.CommandSelectModel:
		response, err := s.selectModel(ctx, command)
		return response, true, err
	case controller.CommandSelectReasoningChoice:
		return s.selectReasoningChoice(command), true, nil
	case controller.CommandUnspecified, controller.CommandCancel:
		return s.rejection(
			command,
			controller.RejectionInvalidArgument,
			errors.New("invalid command payload"),
		), true, nil
	case controller.CommandUserRequest:
		return controller.Response{}, false, nil
	case controller.CommandCreateSession, controller.CommandListSessions, controller.CommandResumeSession,
		controller.CommandSetSessionName, controller.CommandGetSessionInfo, controller.CommandGetSessionEntries,
		controller.CommandGetSessionStats, controller.CommandGetSessionTree, controller.CommandNavigateSessionTree,
		controller.CommandForkSession, controller.CommandCloneSession, controller.CommandSetEntryLabel:
		return controller.Response{}, false, nil
	default:
		return s.rejection(
			command,
			controller.RejectionInvalidArgument,
			errors.New("invalid command payload"),
		), true, nil
	}
}

// handleSessionImmediate routes session commands that do not require run coordination.
func (s *Service) handleSessionImmediate(
	ctx context.Context,
	command controller.Command,
) (controller.Response, bool, error) {
	switch command.Kind {
	case controller.CommandCreateSession:
		response, err := s.createSession(ctx, command)
		return response, true, err
	case controller.CommandListSessions:
		response, err := s.listSessions(ctx, command)
		return response, true, err
	case controller.CommandResumeSession:
		response, err := s.resumeSession(ctx, command)
		return response, true, err
	case controller.CommandSetSessionName:
		response, err := s.setSessionName(ctx, command)
		return response, true, err
	case controller.CommandGetSessionInfo:
		return sessionInfoResponse(command.OperationID, s.sessionControl.Info()), true, nil
	case controller.CommandGetSessionEntries:
		return s.sessionEntries(command), true, nil
	case controller.CommandGetSessionStats:
		return sessionStatisticsResponse(command.OperationID, s.sessionControl.Statistics()), true, nil
	case controller.CommandGetSessionTree:
		return s.sessionTree(command), true, nil
	case controller.CommandNavigateSessionTree:
		response, err := s.navigateSessionTree(ctx, command)
		return response, true, err
	case controller.CommandForkSession:
		response, err := s.forkSession(ctx, command)
		return response, true, err
	case controller.CommandCloneSession:
		response, err := s.cloneSession(ctx, command)
		return response, true, err
	case controller.CommandSetEntryLabel:
		response, err := s.setEntryLabel(ctx, command)
		return response, true, err
	case controller.CommandUnspecified, controller.CommandUserRequest, controller.CommandCancel,
		controller.CommandGetRunState, controller.CommandGetMessages, controller.CommandGetModels,
		controller.CommandSelectModel, controller.CommandSelectReasoningChoice:
		return controller.Response{}, false, nil
	default:
		return controller.Response{}, false, nil
	}
}

// runState returns the public run state for one operation query.
func (s *Service) runState(operationID string, active *activeRun) controller.Response {
	state := s.stateSnapshot()
	publicState := controller.RunStateIdle
	if active != nil || state.Status == run.StatusRunning || state.Status == run.StatusAwaitingSettlement {
		publicState = controller.RunStateRunning
	}
	activeOperationID := mo.None[string]()
	if publicState == controller.RunStateRunning && active != nil {
		activeOperationID = mo.Some(active.operationID)
	}
	response := emptyResponse(operationID, controller.ResponseRunState)
	response.State = mo.Some(controller.RunStateResult{
		State:             publicState,
		ActiveOperationID: activeOperationID,
	})
	return response
}

// messages returns a public history snapshot for one operation query.
func (s *Service) messages(operationID string) (controller.Response, error) {
	response := emptyResponse(operationID, controller.ResponseMessages)
	messages, err := mapHistory(s.historySnapshot())
	if err != nil {
		return controller.Response{}, err
	}
	response.Messages = messages
	return response, nil
}

// models returns configured models and the active selection.
func (s *Service) models(operationID string) controller.Response {
	response := emptyResponse(operationID, controller.ResponseModels)
	response.Models = mo.Some(controller.ModelsResult{
		Models:          s.modelCatalog.Models(),
		ActiveSelection: mo.Some(s.modelCatalog.ActiveSelection()),
	})
	return response
}

// selectModel validates and commits a provider model selection.
func (s *Service) selectModel(ctx context.Context, command controller.Command) (controller.Response, error) {
	providerID, hasProvider := command.ProviderID.Get()
	modelID, hasModel := command.ModelID.Get()
	if !hasProvider || !hasModel {
		return s.rejection(
			command,
			controller.RejectionInvalidArgument,
			errors.New("provider and model are required"),
		), nil
	}
	selection, err := s.modelCatalog.SelectModel(ctx, providerID, modelID)
	if err != nil {
		if isOperationCancellation(ctx, err) {
			return controller.Response{}, err
		}
		return s.selectionRejected(command, err), nil
	}
	response := emptyResponse(command.OperationID, controller.ResponseModelSelection)
	response.Selection = mo.Some(selection)
	return response, nil
}

// selectReasoningChoice validates and commits a reasoning selection.
func (s *Service) selectReasoningChoice(command controller.Command) controller.Response {
	reasoningChoice, present := command.ReasoningChoice.Get()
	if !present {
		return s.rejection(command, controller.RejectionInvalidArgument, errors.New("reasoning choice is required"))
	}
	selection, err := s.modelCatalog.SelectReasoningChoice(reasoningChoice)
	if err != nil {
		return s.selectionRejected(command, err)
	}
	response := emptyResponse(command.OperationID, controller.ResponseModelSelection)
	response.Selection = mo.Some(selection)
	return response
}

// selectionRejected maps a model-selection failure to an operation rejection.
func (s *Service) selectionRejected(command controller.Command, err error) controller.Response {
	var selectionFailure SelectionFailure
	if !errors.As(err, &selectionFailure) {
		return s.rejection(command, controller.RejectionInternal, fmt.Errorf("model selection failed: %w", err))
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
	return s.rejection(command, code, err)
}

// createSession returns replacement information only after the shared gate and active state commit succeed.
func (s *Service) createSession(ctx context.Context, command controller.Command) (controller.Response, error) {
	replacement, err := s.sessionControl.Create(ctx)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	return sessionInfoResponse(command.OperationID, replacement.Info), nil
}

// listSessions maps the ordered persisted-session view without changing active state.
func (s *Service) listSessions(ctx context.Context, command controller.Command) (controller.Response, error) {
	listed, err := s.sessionControl.List(ctx)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	response := emptyResponse(command.OperationID, controller.ResponseSessions)
	response.Sessions = listed
	return response, nil
}

// resumeSession preserves the previous active session when load or replacement fails.
func (s *Service) resumeSession(ctx context.Context, command controller.Command) (controller.Response, error) {
	id, present := command.SessionID.Get()
	if !present || id == "" {
		return s.rejection(command, controller.RejectionInvalidArgument, errors.New("session ID is required")), nil
	}
	replacement, err := s.sessionControl.Resume(ctx, id)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	return sessionInfoResponse(command.OperationID, replacement.Info), nil
}

// setSessionName returns the information snapshot produced by the durable name append.
func (s *Service) setSessionName(ctx context.Context, command controller.Command) (controller.Response, error) {
	name, present := command.SessionName.Get()
	if !present {
		return s.rejection(
			command,
			controller.RejectionInvalidArgument,
			errors.New("session name is required"),
		), nil
	}
	info, err := s.sessionControl.SetName(ctx, name)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	return sessionInfoResponse(command.OperationID, info), nil
}

// forkSession returns a replacement only after its snapshot is durable.
func (s *Service) forkSession(ctx context.Context, command controller.Command) (controller.Response, error) {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return s.rejection(command, controller.RejectionInvalidArgument, errors.New("target entry ID is required")), nil
	}
	replacement, nextInput, err := s.sessionControl.Fork(ctx, targetID)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	entries, mapErr := mapSessionEntries(replacement.Entries)
	if mapErr != nil {
		return s.rejection(
			command,
			controller.RejectionInternal,
			fmt.Errorf("session entries are unavailable: %w", mapErr),
		), nil
	}
	response := emptyResponse(command.OperationID, controller.ResponseForkSession)
	response.Replacement = mo.Some(controller.SessionReplacement{
		Info: replacement.Info, ActiveBranch: entries, NextInput: mo.Some(nextInput),
	})
	return response, nil
}

// cloneSession returns a replacement only after its snapshot is durable.
func (s *Service) cloneSession(ctx context.Context, command controller.Command) (controller.Response, error) {
	replacement, err := s.sessionControl.Clone(ctx)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	entries, mapErr := mapSessionEntries(replacement.Entries)
	if mapErr != nil {
		return s.rejection(
			command,
			controller.RejectionInternal,
			fmt.Errorf("session entries are unavailable: %w", mapErr),
		), nil
	}
	response := emptyResponse(command.OperationID, controller.ResponseCloneSession)
	response.Replacement = mo.Some(controller.SessionReplacement{
		Info: replacement.Info, ActiveBranch: entries, NextInput: mo.None[string](),
	})
	return response, nil
}

// setEntryLabel returns the complete committed tree after one durable mutation.
func (s *Service) setEntryLabel(ctx context.Context, command controller.Command) (controller.Response, error) {
	targetID, targetPresent := command.TargetEntryID.Get()
	label, labelPresent := command.EntryLabel.Get()
	if !targetPresent || targetID == "" || !labelPresent {
		return s.rejection(
			command,
			controller.RejectionInvalidArgument,
			errors.New("target entry ID and label are required"),
		), nil
	}
	tree, err := s.sessionControl.SetLabel(ctx, targetID, label)
	if err != nil {
		return s.sessionOperationError(ctx, command, err)
	}
	mapped, err := mapSessionTree(tree)
	if err != nil {
		return s.rejection(
			command,
			controller.RejectionInternal,
			fmt.Errorf("session tree is unavailable: %w", err),
		), nil
	}
	response := emptyResponse(command.OperationID, controller.ResponseSetEntryLabel)
	response.SessionTree = mo.Some(mapped)
	return response, nil
}

// navigateSessionTree commits requested navigation or returns one classified terminal result.
func (s *Service) navigateSessionTree(ctx context.Context, command controller.Command) (controller.Response, error) {
	targetID, present := command.TargetEntryID.Get()
	if !present || targetID == "" {
		return s.rejection(command, controller.RejectionInvalidArgument, errors.New("target entry ID is required")), nil
	}
	mode, validMode := summaryModeFromProgrammatic(command.SummaryMode)
	focus := strings.TrimSpace(command.CustomFocus.OrEmpty())
	invalidFocus := mode == sessionnavigation.SummaryModeSummarizeWithCustomPrompt && focus == "" ||
		mode != sessionnavigation.SummaryModeSummarizeWithCustomPrompt && focus != ""
	if !validMode || invalidFocus {
		return s.rejection(
			command,
			controller.RejectionInvalidArgument,
			errors.New("invalid summary mode or custom focus"),
		), nil
	}
	result, err := s.sessionControl.Navigate(ctx, sessionnavigation.Request{
		TargetEntryID: targetID, SummaryMode: mode, CustomFocus: command.CustomFocus,
	})
	if err != nil {
		if isOperationCancellation(ctx, err) {
			return controller.Response{}, err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			response := emptyResponse(command.OperationID, controller.ResponseSessionTreeNavigation)
			response.TreeNavigation = mo.Some(controller.TreeNavigationResult{
				Status:    controller.TreeNavigationStatusCanceled,
				Committed: mo.None[controller.TreeNavigationCommitted](), Issues: nil,
			})
			return response, nil
		}
		return s.sessionRejection(command, err), nil
	}
	if result.Canceled {
		response := emptyResponse(command.OperationID, controller.ResponseSessionTreeNavigation)
		response.TreeNavigation = mo.Some(controller.TreeNavigationResult{
			Status:    controller.TreeNavigationStatusCanceled,
			Committed: mo.None[controller.TreeNavigationCommitted](), Issues: mapOperationIssues(result.Issues),
		})
		return response, nil
	}
	committed, mapErr := mapTreeNavigationCommitted(result)
	if mapErr != nil {
		return s.rejection(
			command,
			controller.RejectionInternal,
			fmt.Errorf("session tree is unavailable: %w", mapErr),
		), nil
	}
	response := emptyResponse(command.OperationID, controller.ResponseSessionTreeNavigation)
	response.TreeNavigation = mo.Some(controller.TreeNavigationResult{
		Status: controller.TreeNavigationStatusCommitted, Committed: mo.Some(committed),
		Issues: mapOperationIssues(result.Issues),
	})
	return response, nil
}

// sessionEntries returns the current active-session entries without taking the replacement gate.
func (s *Service) sessionEntries(command controller.Command) controller.Response {
	entries, err := mapSessionEntries(s.sessionControl.Entries())
	if err != nil {
		return s.rejection(
			command,
			controller.RejectionInternal,
			fmt.Errorf("session entries are unavailable: %w", err),
		)
	}
	response := emptyResponse(command.OperationID, controller.ResponseSessionEntries)
	response.SessionEntries = entries
	return response
}

// sessionOperationError preserves pure owner cancellation and maps every independent failure to a response.
func (s *Service) sessionOperationError(
	ctx context.Context,
	command controller.Command,
	err error,
) (controller.Response, error) {
	if isOperationCancellation(ctx, err) {
		return controller.Response{}, err
	}
	return s.sessionRejection(command, err), nil
}

// sessionRejection maps domain failures to stable rejection codes while retaining error details.
func (s *Service) sessionRejection(command controller.Command, err error) controller.Response {
	switch {
	case errors.Is(err, session.ErrBusy):
		return s.rejection(command, controller.RejectionBusy, err)
	case errors.Is(err, session.ErrInvalidName):
		return s.rejection(command, controller.RejectionInvalidArgument, err)
	case errors.Is(err, session.ErrInvalidForkTarget):
		return s.rejection(command, controller.RejectionInvalidArgument, err)
	case errors.Is(err, session.ErrEntryNotFound):
		return s.rejection(command, controller.RejectionNotFound, err)
	case errors.Is(err, sessionnavigation.ErrModelUnavailable):
		return s.rejection(command, controller.RejectionModelUnavailable, err)
	case errors.Is(err, sessionnavigation.ErrCredentialUnavailable):
		return s.rejection(command, controller.RejectionCredentialUnavailable, err)
	case errors.Is(err, sessionnavigation.ErrModelFailed):
		return s.rejection(command, controller.RejectionModelFailed, err)
	case errors.Is(err, sessionnavigation.ErrExtensionInvalidResult):
		return s.rejection(command, controller.RejectionExtensionInvalidResult, err)
	case errors.Is(err, sessionnavigation.ErrExtensionUnavailable):
		return s.rejection(command, controller.RejectionExtensionUnavailable, err)
	case errors.Is(err, session.ErrPersistenceUnavailable):
		return s.rejection(command, controller.RejectionPersistenceUnavailable, err)
	case errors.Is(err, session.ErrUnavailable):
		return s.rejection(command, controller.RejectionSessionUnavailable, err)
	case errors.Is(err, os.ErrNotExist):
		return s.rejection(command, controller.RejectionNotFound, fmt.Errorf("session was not found: %w", err))
	default:
		return s.rejection(command, controller.RejectionInternal, fmt.Errorf("session operation failed: %w", err))
	}
}

// sessionTree returns the complete active-session tree without private extension payload bytes.
func (s *Service) sessionTree(command controller.Command) controller.Response {
	tree, err := mapSessionTree(s.sessionControl.Tree())
	if err != nil {
		return s.rejection(command, controller.RejectionInternal, fmt.Errorf("session tree is unavailable: %w", err))
	}
	response := emptyResponse(command.OperationID, controller.ResponseSessionTree)
	response.SessionTree = mo.Some(tree)
	return response
}

// sessionInfoResponse initializes only the session-information response variant.
func sessionInfoResponse(operationID string, info session.Info) controller.Response {
	response := emptyResponse(operationID, controller.ResponseSessionInfo)
	response.SessionInfo = mo.Some(info)
	return response
}

// sessionStatisticsResponse initializes the complete statistics response variant.
func sessionStatisticsResponse(operationID string, statistics session.Statistics) controller.Response {
	return controller.Response{
		SessionEntries:    nil,
		OperationID:       operationID,
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
		Replacement:       mo.None[controller.SessionReplacement](),
		CancelTargetState: mo.None[operation.TerminalState](),
	}
}

// preflight validates operation identity, payload, and run admission.
func (s *Service) preflight(
	command controller.Command,
) (*activeRun, *controller.Response, error) {
	if command.OperationID == "" {
		return nil, nil, ErrOperationIDRequired
	}
	if !command.Valid() {
		response := s.rejection(command, controller.RejectionInvalidArgument, errors.New("invalid command payload"))
		return nil, &response, nil
	}
	active := s.delivery.activeSnapshot()
	if active != nil && active.operationID == command.OperationID {
		response := s.rejection(command, controller.RejectionOperationIDInUse, errors.New("operation ID is active"))
		return active, &response, nil
	}
	if command.Kind == controller.CommandUserRequest && active != nil {
		response := s.rejection(command, controller.RejectionBusy, errors.New("a run is active"))
		return active, &response, nil
	}
	return active, nil, nil
}

// filterRunError removes expected cancellation while preserving independent run failures.
func filterRunError(outcome agent.RunOutcome, runErr error) error {
	if outcome != agent.RunOutcomeAborted {
		return runErr
	}
	return removeCancellation(runErr)
}

// isOperationCancellation reports when owner cancellation is the only reason accepted work stopped.
func isOperationCancellation(ctx context.Context, err error) bool {
	return errors.Is(ctx.Err(), context.Canceled) && removeCancellation(err) == nil
}

// removeCancellation recursively removes cancellation leaves from joined errors.
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

// emptyResponse creates a response with only operation identity and kind set.
func emptyResponse(operationID string, kind controller.ResponseKind) controller.Response {
	return controller.Response{
		SessionEntries:    nil,
		OperationID:       operationID,
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
		Replacement:       mo.None[controller.SessionReplacement](),
		Rejection:         mo.None[controller.Rejection](),
		CancelTargetState: mo.None[operation.TerminalState](),
	}
}

// runPreparationRejected keeps run ID allocation failure inside the command response.
func (s *Service) runPreparationRejected(
	command controller.Command,
	prepareErr error,
) (controller.Response, *activeRun, error) {
	if errors.Is(prepareErr, session.ErrBusy) {
		return s.rejection(command, controller.RejectionBusy, prepareErr), nil, nil
	}
	return controller.Response{}, nil, fmt.Errorf("prepare Host run: %w", prepareErr)
}

// rejection creates a typed rejection for one operation.
func (s *Service) rejection(
	command controller.Command,
	code controller.RejectionCode,
	cause error,
) controller.Response {
	response := emptyResponse(command.OperationID, controller.ResponseRejected)
	response.Rejection = mo.Some(controller.Rejection{Command: command.Kind, Code: code, Cause: cause})
	return response
}
