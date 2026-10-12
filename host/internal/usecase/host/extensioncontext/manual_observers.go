// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// finishManualOutcome publishes terminal state and retains all acquired failure causes.
func (s *Service) finishManualOutcome(
	ctx context.Context,
	handlers contextcompaction.HandlerSet,
	state mo.Option[contextcompaction.PolicyState],
	result modelexecution.ManualCompactionResult,
	cause, policyCauses error,
) error {
	if cause != nil {
		cause = errors.Join(cause, policyCauses)
		policyCauses = nil
	}
	outcome := contextcompaction.OutcomeInvocation{
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
			return fmt.Errorf(
				"issue compaction observer %q context: %w",
				handler.HandlerID,
				errors.Join(cause, policyCauses, err, acquiredCauses),
			)
		}
		if binding.RuntimeInstanceID != handler.RuntimeID {
			return errors.Join(fmt.Errorf(
				"captured compaction observer %q generation %q is unavailable",
				handler.HandlerID,
				handler.RuntimeID,
			), cause, policyCauses, acquiredCauses)
		}
		// lifetimeID correlates nested observer work without mutation authority.
		lifetimeID := rand.Text()
		parent, cancel := context.WithCancel(ctx)
		operation := &compactionOperation{
			authorityID: "",
			lifecycleID: lifetimeID,
			owner: PolicyOwner{
				ExtensionID: handler.ExtensionID,
				RuntimeID:   handler.RuntimeID,
				HandlerID:   handler.HandlerID,
			},
			snapshot:             CompactionSnapshot{},
			requests:             nil,
			generators:           nil,
			results:              nil,
			context:              parent,
			cancel:               cancel,
			mutex:                sync.Mutex{},
			children:             sync.WaitGroup{},
			closed:               false,
			replacementRequested: false,
			receipt:              mo.None[session.Entry](),
			causes:               nil,
			terminalError:        nil,
			state:                mo.None[contextcompaction.PolicyState](),
		}
		s.mutex.Lock()
		s.compactionOperations[lifetimeID] = operation
		s.mutex.Unlock()
		outcome.ParentCompactionID = mo.Some(lifetimeID)
		outcome.State = state.MapValue(contextcompaction.PolicyState.Clone)
		outcome.Committed = result.Committed.MapValue(session.Entry.Clone)
		if failure {
			err = s.policyRuntime.ObserveCompactionFailure(parent, handler, binding, outcome)
		} else {
			err = s.policyRuntime.ObserveCompactionSuccess(parent, handler, binding, outcome)
		}
		_, acquiredErr, terminalErr := operation.finish()
		acquiredCauses = errors.Join(acquiredCauses, acquiredErr)
		s.mutex.Lock()
		delete(s.compactionOperations, lifetimeID)
		s.mutex.Unlock()
		err = errors.Join(err, terminalErr)
		if err != nil {
			return errors.Join(
				cause,
				policyCauses,
				fmt.Errorf("compaction observer %q: %w", handler.HandlerID, errors.Join(err, acquiredCauses)),
			)
		}
	}
	return cause
}
