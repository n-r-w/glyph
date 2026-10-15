package extensionruntime

import (
	"context"
	"fmt"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// SnapshotCompactionHandlers captures accepted registration membership and runtime generations.
func (s *Service) SnapshotCompactionHandlers() extensioncontext.HandlerSet {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return extensioncontext.HandlerSet{
		Requests:   slices.Clone(s.compactionHandlers.Requests),
		Generators: slices.Clone(s.compactionHandlers.Generators),
		Results:    slices.Clone(s.compactionHandlers.Results),
		Successes:  slices.Clone(s.compactionHandlers.Successes),
		Failures:   slices.Clone(s.compactionHandlers.Failures),
	}
}

// ObserveCompactionSuccess invokes one success observer from the captured runtime generation.
func (s *Service) ObserveCompactionSuccess(
	ctx context.Context,
	handler extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.OutcomeInvocation,
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
	handler extensioncontext.Participant,
	binding extension.Context,
	invocation extensioncontext.OutcomeInvocation,
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
func (s *Service) compactionOwner(handler extensioncontext.Participant) (operationOwner, error) {
	owner, available := s.beginOperation(handler.ExtensionID, handler.RuntimeID)
	if !available {
		return operationOwner{}, &extensioncontext.PolicyOperationError{
			Category: modelexecution.FailureExtensionFailed,
			Cause:    fmt.Errorf("%w: extension handler %q is unavailable", ErrExtensionUnavailable, handler.HandlerID),
		}
	}
	return owner, nil
}
