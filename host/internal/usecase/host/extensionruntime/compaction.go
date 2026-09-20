package extensionruntime

import (
	"context"
	"fmt"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// appendCompactionHandler adds one accepted capability to its registration-order group.
func appendCompactionHandler(
	set *contextcompaction.HandlerSet,
	handler contextcompaction.Handler,
	kind startup.RawHandlerKind,
) {
	switch kind {
	case startup.RawHandlerKindCompactionRequest:
		set.Requests = append(set.Requests, handler)
	case startup.RawHandlerKindCompactionGenerate:
		set.Generators = append(set.Generators, handler)
	case startup.RawHandlerKindCompactionResult:
		set.Results = append(set.Results, handler)
	case startup.RawHandlerKindCompactionSuccess:
		set.Successes = append(set.Successes, handler)
	case startup.RawHandlerKindCompactionFailure:
		set.Failures = append(set.Failures, handler)
	case startup.RawHandlerKindUnspecified, startup.RawHandlerKindSessionBeforeTreeRequest,
		startup.RawHandlerKindSessionBeforeTreeResult, startup.RawHandlerKindSessionTree,
		startup.RawHandlerKindAgentStart, startup.RawHandlerKindAgentEnd, startup.RawHandlerKindAgentSettled,
		startup.RawHandlerKindTurnStart, startup.RawHandlerKindTurnEnd, startup.RawHandlerKindMessageStart,
		startup.RawHandlerKindMessageUpdate, startup.RawHandlerKindMessageEnd,
		startup.RawHandlerKindToolExecutionStart, startup.RawHandlerKindToolExecutionUpdate,
		startup.RawHandlerKindToolExecutionEnd, startup.RawHandlerKindModelSelection,
		startup.RawHandlerKindReasoningSelection, startup.RawHandlerKindModelSelectionObserver,
		startup.RawHandlerKindReasoningSelectionObserver, startup.RawHandlerKindRetry:
	}
}

// SnapshotCompactionHandlers captures accepted registration membership and runtime generations.
func (s *Service) SnapshotCompactionHandlers() contextcompaction.HandlerSet {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return contextcompaction.HandlerSet{
		Requests:   slices.Clone(s.compactionHandlers.Requests),
		Generators: slices.Clone(s.compactionHandlers.Generators),
		Results:    slices.Clone(s.compactionHandlers.Results),
		Successes:  slices.Clone(s.compactionHandlers.Successes),
		Failures:   slices.Clone(s.compactionHandlers.Failures),
	}
}

// HandleCompactionRequest invokes one request handler from the captured runtime generation.
func (s *Service) HandleCompactionRequest(
	ctx context.Context,
	handler contextcompaction.Handler,
	binding extension.Context,
	invocation contextcompaction.RequestInvocation,
) (contextcompaction.RequestAction, error) {
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return contextcompaction.RequestAction{}, err
	}
	result, invokeErr := owner.state.runtime.HandleCompactionRequest(ctx, handler.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return result, invokeErr
}

// GenerateCompaction invokes one generator from the captured runtime generation.
func (s *Service) GenerateCompaction(
	ctx context.Context,
	handler contextcompaction.Handler,
	binding extension.Context,
	invocation contextcompaction.RequestInvocation,
) (contextcompaction.Result, error) {
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return contextcompaction.Result{}, err
	}
	result, invokeErr := owner.state.runtime.GenerateCompaction(ctx, handler.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return result, invokeErr
}

// HandleCompactionResult invokes one result handler from the captured runtime generation.
func (s *Service) HandleCompactionResult(
	ctx context.Context,
	handler contextcompaction.Handler,
	binding extension.Context,
	invocation contextcompaction.ResultInvocation,
) (contextcompaction.ResultAction, error) {
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return contextcompaction.ResultAction{}, err
	}
	result, invokeErr := owner.state.runtime.HandleCompactionResult(ctx, handler.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return result, invokeErr
}

// ObserveCompactionSuccess invokes one success observer from the captured runtime generation.
func (s *Service) ObserveCompactionSuccess(
	ctx context.Context,
	handler contextcompaction.Handler,
	binding extension.Context,
	invocation contextcompaction.OutcomeInvocation,
) error {
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return err
	}
	invokeErr := owner.state.runtime.ObserveCompactionSuccess(ctx, handler.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return invokeErr
}

// ObserveCompactionFailure invokes one failure observer from the captured runtime generation.
func (s *Service) ObserveCompactionFailure(
	ctx context.Context,
	handler contextcompaction.Handler,
	binding extension.Context,
	invocation contextcompaction.OutcomeInvocation,
) error {
	owner, err := s.compactionOwner(handler)
	if err != nil {
		return err
	}
	invokeErr := owner.state.runtime.ObserveCompactionFailure(ctx, handler.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, invokeErr)
	return invokeErr
}

// compactionOwner reserves one exact captured runtime generation.
func (s *Service) compactionOwner(handler contextcompaction.Handler) (operationOwner, error) {
	owner, available := s.beginOperation(handler.ExtensionID, handler.RuntimeID)
	if !available {
		return operationOwner{}, fmt.Errorf(
			"%w: extension handler %q is unavailable", ErrExtensionUnavailable, handler.HandlerID,
		)
	}
	return owner, nil
}
