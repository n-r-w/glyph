package programmatic

import (
	"context"
	"errors"
	"slices"
	"sync"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
)

// Prepare validates and admits one Programmatic operation without domain work.
func (s *Service) Prepare(
	ctx context.Context,
	command controller.Command,
) (operation.Prepared[controller.AgentEvent, controller.Response], error) {
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
		return &runPrepared{active: active, release: sync.Once{}}, nil
	}
	release := func() {}
	if isSessionMutation(command.Kind) {
		reservation, acquired := s.sessionControl.TryAcquire()
		if !acquired {
			return nil, controller.Reject(controller.RejectionCodeBusy)
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

var _ operation.Prepared[controller.AgentEvent, controller.Response] = (*commandPrepared)(nil)

// Run executes one admitted non-agent operation.
func (p *commandPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[controller.AgentEvent],
) operation.Outcome[controller.Response] {
	response, active, err := p.service.handle(ctx, p.command)
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
			errors.New(rejection.Message),
		)
	}
	if isCanceledNavigation(response) && errors.Is(ctx.Err(), context.Canceled) {
		return operation.Canceled[controller.Response]()
	}
	return operation.Completed(response)
}

// Release frees the session-mutation reservation when present.
func (p *commandPrepared) Release() {
	p.release()
}

// runPrepared adapts the existing Agent Core event and settlement ownership to the shared runtime.
type runPrepared struct {
	// active owns the prepared Agent Core run and its event stream.
	active *activeRun
	// release joins or frees the run reservation once.
	release sync.Once
}

var _ operation.Prepared[controller.AgentEvent, controller.Response] = (*runPrepared)(nil)

// Run starts Agent Core after Running and reports progress until settlement finishes.
func (p *runPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[controller.AgentEvent],
) operation.Outcome[controller.Response] {
	stop := context.AfterFunc(ctx, p.active.cancel)
	defer stop()
	p.active.Start()
	for event := range p.active.Events() {
		if err := reporter.Report(event); err != nil {
			return operation.Failed[controller.Response](controller.FailureCodeInternal, err)
		}
	}
	if p.active.err != nil {
		return operation.Failed[controller.Response](failureCode(p.active.err), p.active.err)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return operation.Canceled[controller.Response]()
	}
	return operation.Completed(emptyResponse(p.active.operationID, controller.ResponseUserRequestCompleted))
}

// Release frees an unstarted run reservation or joins completed run cleanup.
func (p *runPrepared) Release() {
	p.release.Do(func() {
		_ = p.active.delivery.cancelAndWait(p.active)
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
			return controller.Reject(controller.RejectionCodeInvalidArgument)
		}
		descriptors := s.modelCatalog.Models()
		for index := range descriptors {
			descriptor := &descriptors[index]
			if descriptor.Provider == provider && descriptor.Model == modelID {
				return nil
			}
		}
		return controller.Reject(controller.RejectionCodeNotFound)
	}
	if command.Kind == controller.CommandSelectReasoningChoice {
		choice, present := command.ReasoningChoice.Get()
		if !present {
			return controller.Reject(controller.RejectionCodeInvalidArgument)
		}
		selection := s.modelCatalog.ActiveSelection()
		descriptors := s.modelCatalog.Models()
		for index := range descriptors {
			descriptor := &descriptors[index]
			if descriptor.Provider == selection.Provider && descriptor.Model == selection.Model {
				if slices.Contains(descriptor.ReasoningCapabilities.Choices, choice) {
					return nil
				}
				return controller.Reject(controller.RejectionCodeReasoningUnsupported)
			}
		}
		return controller.Reject(controller.RejectionCodeNotFound)
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
		return controller.Reject(controller.RejectionCodeInvalidArgument)
	}
	switch rejection.Code {
	case controller.RejectionBusy:
		return controller.Reject(controller.RejectionCodeBusy)
	case controller.RejectionNotFound:
		return controller.Reject(controller.RejectionCodeNotFound)
	case controller.RejectionReasoningUnsupported:
		return controller.Reject(controller.RejectionCodeReasoningUnsupported)
	case controller.RejectionInvalidArgument, controller.RejectionOperationIDInUse,
		controller.RejectionUnspecified,
		controller.RejectionInternal, controller.RejectionCredentialUnavailable,
		controller.RejectionSessionUnavailable, controller.RejectionPersistenceUnavailable,
		controller.RejectionModelUnavailable, controller.RejectionModelFailed,
		controller.RejectionExtensionInvalidResult, controller.RejectionExtensionUnavailable:
		return controller.Reject(controller.RejectionCodeInvalidArgument)
	default:
		return controller.Reject(controller.RejectionCodeInvalidArgument)
	}
}

// failureCode maps unclassified Host errors to the common machine code.
func failureCode(error) string {
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
