// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// finishPolicyOutcome publishes captured compaction outcomes and retains acquired failure causes.
func (s *Service) finishPolicyOutcome(
	ctx context.Context,
	handlers HandlerSet,
	state mo.Option[PolicyState],
	result modelexecution.ManualCompactionResult,
	cause, policyCauses error,
) error {
	terminalSource := selectPolicyOperationCategory(cause)
	if cause != nil {
		cause = errors.Join(cause, policyCauses)
		policyCauses = nil
	}
	outcome := OutcomeInvocation{
		State: state, ParentCompactionID: mo.None[string](), Committed: result.Committed,
		Canceled: result.Canceled, Error: mo.None[string](),
	}
	if cause != nil {
		outcome.Error = mo.Some(cause.Error())
	}
	failure := cause != nil || result.Committed.IsNone()
	observers := handlers.Successes
	if failure {
		observers = handlers.Failures
	}
	// Keep handled failures from earlier observers for terminal reporting.
	var acquiredCauses error
	for _, handler := range observers {
		binding, err := s.IssueContext(handler.ExtensionID)
		if err != nil {
			err = NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, err)
			combined := fmt.Errorf(
				"issue compaction observer %q context: %w",
				handler.HandlerID,
				errors.Join(cause, policyCauses, err, acquiredCauses),
			)
			return retainPolicyOutcomeSource(terminalSource, err, combined)
		}
		if binding.RuntimeInstanceID != handler.RuntimeID {
			unavailable := NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, fmt.Errorf(
				"captured compaction observer %q generation %q is unavailable",
				handler.HandlerID,
				handler.RuntimeID,
			))
			return retainPolicyOutcomeSource(
				terminalSource,
				unavailable,
				errors.Join(cause, policyCauses, unavailable, acquiredCauses),
			)
		}
		operation := s.newLifetime(ctx, PolicyOwner(handler))
		outcome.ParentCompactionID = mo.Some(operation.lifecycleID)
		outcome.State = state.MapValue(PolicyState.Clone)
		outcome.Committed = result.Committed.MapValue(session.Entry.Clone)
		if failure {
			err = s.policyRuntime.ObserveCompactionFailure(operation.context, handler, binding, outcome)
		} else {
			err = s.policyRuntime.ObserveCompactionSuccess(operation.context, handler, binding, outcome)
		}
		_, acquiredErr, terminalErr := operation.finish()
		acquiredCauses = errors.Join(acquiredCauses, acquiredErr)
		s.mutex.Lock()
		delete(s.compactionOperations, operation.lifecycleID)
		s.mutex.Unlock()
		err = NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, errors.Join(err, terminalErr))
		if err != nil {
			combined := errors.Join(
				cause,
				policyCauses,
				fmt.Errorf("compaction observer %q: %w", handler.HandlerID, errors.Join(err, acquiredCauses)),
			)
			return retainPolicyOutcomeSource(terminalSource, err, combined)
		}
	}
	return cause
}

// retainPolicyOutcomeSource keeps terminal selection separate from acquired outcome diagnostics.
func retainPolicyOutcomeSource(
	preceding mo.Option[modelexecution.FailureCategory],
	current, combined error,
) error {
	category, present := preceding.Get()
	if !present {
		category, present = selectPolicyOperationCategory(current).Get()
	}
	if !present {
		return combined
	}
	return &PolicyOperationError{Category: category, Cause: combined}
}
