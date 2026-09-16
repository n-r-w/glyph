package programmatic

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/internal/operation"
)

// Prepare validates and admits one Programmatic operation without domain work.
func (s *Service) Prepare(
	ctx context.Context,
	command controller.Command,
) (operation.Prepared[controller.OperationProgress, controller.Response], error) {
	_, rejection, err := s.preflight(command)
	if err != nil {
		return nil, err
	}
	if rejection != nil {
		return nil, mapPreparationRejection(*rejection)
	}
	selection, selectionPresent, selectionErr := s.prepareSelection(command)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if command.Kind == controller.CommandUserRequest {
		response, active, handleErr := s.handle(ctx, command)
		if handleErr != nil {
			return nil, handleErr
		}
		if rejected := response.Rejection; rejected.IsPresent() {
			return nil, mapPreparationRejection(response)
		}
		return active, nil
	}
	release := func() {}
	if selectionPresent {
		release = selection.Release
	}
	if isSessionMutation(command.Kind) {
		reservation, acquired := s.gate.TryAcquire()
		if !acquired {
			return nil, controller.Reject(
				controller.RejectionCodeBusy,
				errors.New("another Programmatic session mutation is active"),
			)
		}
		release = reservation
	}
	return &commandPrepared{
		service: s, command: command, selection: selection, release: sync.OnceFunc(release),
	}, nil
}

// commandPrepared defers all query, selection, and session work until Running.
type commandPrepared struct {
	// service owns the deferred Host operation.
	service *Service
	// command contains validated operation input.
	command controller.Command
	// selection executes an admitted selection operation when present.
	selection PreparedModelSelection
	// release frees optional mutation admission once.
	release func()
}

var _ operation.Prepared[controller.OperationProgress, controller.Response] = (*commandPrepared)(nil)

// Run executes one admitted non-agent operation.
func (p *commandPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[controller.OperationProgress],
) operation.Outcome[controller.Response] {
	if p.selection != nil {
		return p.runSelection(ctx)
	}
	var response controller.Response
	var active *runPrepared
	var err error
	if p.command.Kind == controller.CommandNavigateSessionTree {
		response, err = p.service.navigateSessionTree(
			ctx,
			p.command,
			programmaticNavigationCallback(reporter),
		)
	} else {
		response, active, err = p.service.handle(ctx, p.command)
	}
	if err != nil {
		if isOperationCancellation(ctx, err) {
			return operation.Canceled[controller.Response]()
		}
		return operation.Failed[controller.Response](failureCode(err), err)
	}
	if active != nil {
		return operation.Failed[controller.Response](
			controller.FailureCodeInternal,
			errors.New("unexpected agent operation"),
		)
	}
	if rejection, present := response.Rejection.Get(); present {
		return operation.Failed[controller.Response](
			failureCodeForRejection(rejection.Code),
			rejection.Cause,
		)
	}
	if isCanceledNavigation(response) && errors.Is(ctx.Err(), context.Canceled) {
		return operation.Canceled[controller.Response]()
	}
	// Navigation issues are declared diagnostics even when navigation itself completed.
	var sources []error
	if navigation, present := response.TreeNavigation.Get(); present {
		for _, issue := range navigation.Issues {
			sources = append(sources, errors.New(issue.Message))
		}
	}
	return operation.CompletedWithSource(response, errors.Join(sources...))
}

// runSelection executes one admitted selection and preserves the commit boundary in its terminal outcome.
func (p *commandPrepared) runSelection(ctx context.Context) operation.Outcome[controller.Response] {
	result := p.selection.Run(ctx)
	if result.Committed {
		response := emptyResponse(p.command.OperationID, controller.ResponseModelSelection)
		response.Selection = mo.Some(result.Selection)
		response.SelectionIssues = projectModelSelectionIssues(result.Issues)
		return operation.CompletedWithSource(response, result.Source)
	}
	if result.Source == nil {
		return operation.Failed[controller.Response](
			controller.FailureCodeInternal, errors.New("selection completed without a commit or failure"),
		)
	}
	if isOperationCancellation(ctx, result.Source) {
		return operation.Canceled[controller.Response]()
	}
	rejected := p.service.selectionRejected(p.command, result.Source)
	rejection := rejected.Rejection.MustGet()
	return operation.Failed[controller.Response](failureCodeForRejection(rejection.Code), rejection.Cause)
}

// Release frees the session-mutation reservation when present.
func (p *commandPrepared) Release() {
	p.release()
}

// runPrepared owns execution and cleanup for one admitted Core run.
type runPrepared struct {
	// coordinator executes Core and orders settlement.
	coordinator Coordinator
	// output binds progress without owning application work.
	output RunOutput
	// operationID identifies the client operation.
	operationID string
	// runID identifies the reserved Core run.
	runID string
	// userText contains the admitted input.
	userText string
	// started distinguishes execution from canceled preparation.
	started bool
	// release frees prepared resources once after operation work finishes.
	release sync.Once
}

var _ operation.Prepared[controller.OperationProgress, controller.Response] = (*runPrepared)(nil)

// Run executes Core on the operation worker after acceptance acknowledgement.
func (p *runPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[controller.OperationProgress],
) operation.Outcome[controller.Response] {
	p.started = true
	unbind := p.output.BindProgress(p.runID, reporter)
	defer unbind()
	outcome, runErr := p.coordinator.RunPrepared(ctx, p.runID, p.userText)
	if err := filterRunError(outcome, runErr); err != nil {
		return operation.Failed[controller.Response](failureCode(err), err)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return operation.Canceled[controller.Response]()
	}
	return operation.Completed(emptyResponse(p.operationID, controller.ResponseUserRequestCompleted))
}

// Release frees unstarted reservations after Run has returned or acceptance has failed.
func (p *runPrepared) Release() {
	p.release.Do(func() {
		if !p.started {
			p.coordinator.CancelPrepared(p.runID)
			p.output.CancelPrepared(p.runID)
		}
	})
}

// isCanceledNavigation reports a domain-canceled navigation result without treating other completed results as
// cancellation.
func isCanceledNavigation(response controller.Response) bool {
	result, present := response.TreeNavigation.Get()
	return present && result.Status == controller.TreeNavigationStatusCanceled
}

// prepareSelection validates request shape and delegates shared admission for selection commands.
func (s *Service) prepareSelection(command controller.Command) (PreparedModelSelection, bool, error) {
	if command.Kind == controller.CommandSelectModel {
		provider, providerPresent := command.ProviderID.Get()
		modelID, modelPresent := command.ModelID.Get()
		if !providerPresent || !modelPresent || provider == "" || modelID == "" {
			return nil, false, controller.Reject(
				controller.RejectionCodeInvalidArgument,
				errors.New("programmatic model selection is incomplete"),
			)
		}
		prepared, err := s.modelSelection.PrepareProgrammaticSelection(ModelSelectionCommand{
			Kind: ModelSelectionCommandModel, Provider: provider, Model: modelID, ReasoningChoice: "",
		})
		return prepared, true, mapSelectionPreparationError(err)
	}
	if command.Kind == controller.CommandSelectReasoningChoice {
		choice, present := command.ReasoningChoice.Get()
		if !present {
			return nil, false, controller.Reject(
				controller.RejectionCodeInvalidArgument,
				errors.New("programmatic reasoning choice is required"),
			)
		}
		prepared, err := s.modelSelection.PrepareProgrammaticSelection(ModelSelectionCommand{
			Kind: ModelSelectionCommandReasoning, Provider: "", Model: "", ReasoningChoice: choice,
		})
		return prepared, true, mapSelectionPreparationError(err)
	}
	return nil, false, nil
}

// mapSelectionPreparationError projects shared admission and starting-target failures.
func mapSelectionPreparationError(err error) error {
	if err == nil {
		return nil
	}
	failure, ok := errors.AsType[SelectionFailure](err)
	if !ok {
		return err
	}
	switch SelectionCode(failure.ModelSelectionCode()) {
	case SelectionBusy:
		return controller.Reject(controller.RejectionCodeBusy, err)
	case SelectionNotFound:
		return controller.Reject(controller.RejectionCodeNotFound, err)
	case SelectionReasoningUnsupported:
		return controller.Reject(controller.RejectionCodeReasoningUnsupported, err)
	case SelectionCredentialUnavailable, SelectionModelUnavailable,
		SelectionExtensionRejected, SelectionExtensionUnavailable:
		return err
	default:
		return err
	}
}

// isSessionMutation reports operation kinds that reserve the shared session gate.
func isSessionMutation(kind controller.CommandKind) bool {
	switch kind {
	case controller.CommandCreateSession, controller.CommandResumeSession, controller.CommandSetSessionName,
		controller.CommandNavigateSessionTree, controller.CommandForkSession, controller.CommandCloneSession,
		controller.CommandSetEntryLabel:
		return true
	case controller.CommandUnspecified, controller.CommandUserRequest, controller.CommandCancel,
		controller.CommandGetRunState, controller.CommandGetMessages, controller.CommandGetModels,
		controller.CommandSelectModel, controller.CommandSelectReasoningChoice, controller.CommandListSessions,
		controller.CommandGetSessionInfo, controller.CommandGetSessionEntries, controller.CommandGetSessionStats,
		controller.CommandGetSessionTree:
		return false
	default:
		return false
	}
}

// mapPreparationRejection converts the old domain classification into lifecycle rejection codes.
func mapPreparationRejection(response controller.Response) error {
	rejection, present := response.Rejection.Get()
	if !present {
		return controller.Reject(
			controller.RejectionCodeInvalidArgument,
			errors.New("programmatic preparation rejection is absent"),
		)
	}
	cause := rejection.Cause
	switch rejection.Code {
	case controller.RejectionBusy:
		return controller.Reject(controller.RejectionCodeBusy, cause)
	case controller.RejectionNotFound:
		return controller.Reject(controller.RejectionCodeNotFound, cause)
	case controller.RejectionReasoningUnsupported:
		return controller.Reject(controller.RejectionCodeReasoningUnsupported, cause)
	case controller.RejectionInvalidArgument, controller.RejectionOperationIDInUse,
		controller.RejectionUnspecified,
		controller.RejectionInternal, controller.RejectionCredentialUnavailable,
		controller.RejectionSessionUnavailable, controller.RejectionPersistenceUnavailable,
		controller.RejectionModelUnavailable, controller.RejectionModelFailed,
		controller.RejectionExtensionInvalidResult, controller.RejectionExtensionUnavailable,
		controller.RejectionExtensionRejected:
		return controller.Reject(controller.RejectionCodeInvalidArgument, cause)
	default:
		return controller.Reject(controller.RejectionCodeInvalidArgument, cause)
	}
}

// failureCode distinguishes source-classified history persistence from other Host errors.
func failureCode(err error) string {
	if errors.Is(err, agent.ErrPersistenceUnavailable) {
		return controller.FailureCodePersistenceUnavailable
	}
	return controller.FailureCodeInternal
}

// failureCodeForRejection converts domain-work failures into operation failure codes.
func failureCodeForRejection(code controller.RejectionCode) string {
	switch code {
	case controller.RejectionCredentialUnavailable:
		return controller.FailureCodeCredentialUnavailable
	case controller.RejectionSessionUnavailable:
		return controller.FailureCodeSessionUnavailable
	case controller.RejectionPersistenceUnavailable:
		return controller.FailureCodePersistenceUnavailable
	case controller.RejectionModelUnavailable:
		return controller.FailureCodeModelUnavailable
	case controller.RejectionModelFailed:
		return controller.FailureCodeModelFailed
	case controller.RejectionExtensionInvalidResult:
		return controller.FailureCodeExtensionInvalidResult
	case controller.RejectionExtensionUnavailable:
		return controller.FailureCodeExtensionUnavailable
	case controller.RejectionExtensionRejected:
		return controller.FailureCodeExtensionRejected
	case controller.RejectionUnspecified, controller.RejectionInvalidArgument, controller.RejectionBusy,
		controller.RejectionOperationIDInUse, controller.RejectionInternal, controller.RejectionNotFound,
		controller.RejectionReasoningUnsupported:
		return controller.FailureCodeInternal
	default:
		return controller.FailureCodeInternal
	}
}
