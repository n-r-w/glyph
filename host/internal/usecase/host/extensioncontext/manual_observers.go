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
	// outcome contains the acquired policy values and independent persistence receipt.
	outcome := contextcompaction.OutcomeInvocation{
		State: state, ParentCompactionID: "", Committed: result.Committed,
		Canceled: result.Canceled, Error: mo.None[string](),
	}
	if cause != nil {
		outcome.Error = mo.Some(cause.Error())
	}
	// failure selects the immutable terminal observation route.
	failure := cause != nil || result.Committed.IsNone()
	// observers contains the captured terminal notification targets.
	observers := handlers.Successes
	if failure {
		observers = handlers.Failures
	}
	// acquiredCauses retains handled work from earlier observers.
	var acquiredCauses error
	// handler identifies the current captured terminal observer.
	for _, handler := range observers {
		// binding supplies ordinary session access for the captured observer.
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
		// parent owns cancellation for this observer and its children.
		parent, cancel := context.WithCancel(ctx)
		// operation tracks observer-owned work through terminal teardown.
		operation := &compactionOperation{
			authorityID: "",
			lifecycleID: lifetimeID,
			owner: PolicyOwner{
				ExtensionID: handler.ExtensionID,
				RuntimeID:   handler.RuntimeID,
				HandlerID:   handler.HandlerID,
			},
			snapshot:             CompactionSnapshot{},
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
		outcome.ParentCompactionID = lifetimeID
		outcome.State = state.MapValue(contextcompaction.PolicyState.Clone)
		outcome.Committed = result.Committed.MapValue(session.Entry.Clone)
		if failure {
			err = s.policyRuntime.ObserveCompactionFailure(parent, handler, binding, outcome)
		} else {
			err = s.policyRuntime.ObserveCompactionSuccess(parent, handler, binding, outcome)
		}
		// acquiredErr and terminalErr contain acquired child failures and terminal teardown failures.
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
