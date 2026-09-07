package programmatic

import (
	"context"
	"errors"
	"slices"
	"sync"

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
	if validationErr := s.validateSelection(command); validationErr != nil {
		return nil, validationErr
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
	return &commandPrepared{service: s, command: command, release: sync.OnceFunc(release)}, nil
}

// commandPrepared defers all query, selection, and session work until Running.
type commandPrepared struct {
	// service owns the deferred Host operation.
	service *Service
	// command contains validated operation input.
	command controller.Command
	// release frees optional mutation admission once.
	release func()
}

var _ operation.Prepared[controller.OperationProgress, controller.Response] = (*commandPrepared)(nil)

// Run executes one admitted non-agent operation.
func (p *commandPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[controller.OperationProgress],
) operation.Outcome[controller.Response] {
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

// validateSelection rejects unavailable in-memory model choices before operation creation.
func (s *Service) validateSelection(command controller.Command) error {
	if command.Kind == controller.CommandSelectModel {
		provider, providerPresent := command.ProviderID.Get()
		modelID, modelPresent := command.ModelID.Get()
		if !providerPresent || !modelPresent {
			return controller.Reject(
				controller.RejectionCodeInvalidArgument,
				errors.New("programmatic model selection is incomplete"),
			)
		}
		descriptors := s.modelCatalog.Models()
		for index := range descriptors {
			descriptor := &descriptors[index]
			if descriptor.Provider == provider && descriptor.Model == modelID {
				return nil
			}
		}
		return controller.Reject(
			controller.RejectionCodeNotFound,
			errors.New("programmatic model selection was not found"),
		)
	}
	if command.Kind == controller.CommandSelectReasoningChoice {
		choice, present := command.ReasoningChoice.Get()
		if !present {
			return controller.Reject(
				controller.RejectionCodeInvalidArgument,
				errors.New("programmatic reasoning choice is required"),
			)
		}
		selection := s.modelCatalog.ActiveSelection()
		descriptors := s.modelCatalog.Models()
		for index := range descriptors {
			descriptor := &descriptors[index]
			if descriptor.Provider == selection.Provider && descriptor.Model == selection.Model {
				if slices.Contains(descriptor.ReasoningCapabilities.Choices, choice) {
					return nil
				}
				return controller.Reject(
					controller.RejectionCodeReasoningUnsupported,
					errors.New("programmatic reasoning choice is not supported"),
				)
			}
		}
		return controller.Reject(
			controller.RejectionCodeNotFound,
			errors.New("active Programmatic model selection was not found"),
		)
	}
	return nil
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
		controller.RejectionExtensionInvalidResult, controller.RejectionExtensionUnavailable:
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
	case controller.RejectionUnspecified, controller.RejectionInvalidArgument, controller.RejectionBusy,
		controller.RejectionOperationIDInUse, controller.RejectionInternal, controller.RejectionNotFound,
		controller.RejectionReasoningUnsupported:
		return controller.FailureCodeInternal
	default:
		return controller.FailureCodeInternal
	}
}
