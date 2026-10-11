// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"fmt"
	"sync"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/errtree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
)

// compactionOperation owns one snapshot, mutation receipt, and nested-work lifetime.
type compactionOperation struct {
	// authorityID identifies the policy's mutation capability.
	authorityID string
	// lifecycleID identifies non-authorizing nested-work correlation.
	lifecycleID string
	// owner identifies the captured runtime and handler that own this lifetime.
	owner PolicyOwner
	// snapshot contains the immutable session input.
	snapshot CompactionSnapshot
	// requests contains the captured request capabilities in registration order.
	requests []RequestParticipant
	// context is intentionally retained parent lifetime: a temporary decision and technical debt.
	// It supplies synchronous cancellation checks before persistence and owned child cancellation/joins.
	// TODO: Remove this field when an explicit-context parent-owner handoff preserves synchronous checks,
	// complete causes, admitted-child release (including skipped Run), and joins.
	context context.Context
	// cancel ends the parent lifetime.
	cancel context.CancelFunc
	// mutex protects mutation state and child admission.
	mutex sync.Mutex
	// children tracks admitted nested work.
	children sync.WaitGroup
	// closed seals child admission during teardown.
	closed bool
	// replacementRequested prevents repeated mutation requests.
	replacementRequested bool
	// receipt contains the actual durable entry.
	receipt mo.Option[session.Entry]
	// causes retains acquired nested and publication failures.
	causes error
	// terminalError retains irreversible commit and independent teardown failures.
	terminalError error
	// state retains only diagnostic values received from the policy.
	state mo.Option[contextcompaction.PolicyState]
}

// recordReceipt retains persistence independently of policy transport completion.
func (o *compactionOperation) recordReceipt(entry session.Entry) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	o.receipt = mo.Some(entry.Clone())
}

// finish ends child admission and joins the parent's nested work.
func (o *compactionOperation) finish() (mo.Option[session.Entry], error, error) {
	o.mutex.Lock()
	o.closed = true
	o.mutex.Unlock()
	o.cancel()
	o.children.Wait()
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.receipt, o.causes, o.terminalError
}

// isOwningCancellation distinguishes expected child shutdown from independent failure.
func (o *compactionOperation) isOwningCancellation(cause error) bool {
	return errtree.AllLeavesMatch(cause, func(leaf error) bool {
		return errors.Is(leaf, o.context.Err()) || errors.Is(context.Cause(o.context), leaf)
	})
}

// recordState retains acquired composition values independently of persistence.
func (o *compactionOperation) recordState(state contextcompaction.PolicyState) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	o.state = mo.Some(state.Clone())
}

// diagnosticState supplies detached acquired values for terminal notification.
func (o *compactionOperation) diagnosticState() mo.Option[contextcompaction.PolicyState] {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.state.MapValue(contextcompaction.PolicyState.Clone)
}

// findRequestParticipant resolves one captured request registration under live parent authority.
func (o *compactionOperation) findRequestParticipant(
	target extensioncontroller.CompactionRequestParticipant,
) (RequestParticipant, error) {
	o.mutex.Lock()
	if o.closed || o.context.Err() != nil {
		o.mutex.Unlock()
		return RequestParticipant{}, errors.Join(
			errors.New("compaction operation has ended"),
			o.context.Err(),
			context.Cause(o.context),
		)
	}
	// Membership includes the captured registration position and runtime generation.
	if target.RegistrationOrder < 0 || target.RegistrationOrder >= int64(len(o.requests)) {
		o.mutex.Unlock()
		return RequestParticipant{}, errors.New(
			"request participant is not captured by the compaction operation",
		)
	}
	participant := o.requests[target.RegistrationOrder]
	o.mutex.Unlock()
	if participant.ExtensionID != target.ExtensionID || participant.RuntimeID != target.RuntimeID ||
		participant.HandlerID != target.HandlerID {
		return RequestParticipant{}, errors.New(
			"request participant identity does not match the captured registration",
		)
	}
	return participant, nil
}

// findCompactionOperation validates the captured policy's mutation authority.
func (s *Service) findCompactionOperation(extensionID, runtimeID, operationID string) (*compactionOperation, error) {
	s.mutex.Lock()
	operation, exists := s.compactionOperations[operationID]
	s.mutex.Unlock()
	if !exists {
		return nil, errors.New("compaction operation is unavailable")
	}
	if operation.authorityID != operationID {
		return nil, errors.New("compaction mutation authority is unavailable")
	}
	if operation.owner.ExtensionID != extensionID || operation.owner.RuntimeID != runtimeID {
		return nil, fmt.Errorf(
			"compaction operation does not belong to extension %q generation %q",
			extensionID,
			runtimeID,
		)
	}
	return operation, nil
}

// BeginCompactionChild attaches nested work to its parent without admission reacquisition.
func (s *Service) BeginCompactionChild(
	extensionID, runtimeID, operationID string,
) (func(error), error) {
	s.mutex.Lock()
	operation, exists := s.compactionOperations[operationID]
	s.mutex.Unlock()
	if !exists || operation.lifecycleID != operationID {
		return nil, errors.New("compaction lifetime binding is unavailable")
	}
	if operation.owner.ExtensionID != extensionID || operation.owner.RuntimeID != runtimeID {
		return nil, fmt.Errorf(
			"compaction lifetime binding does not belong to extension %q generation %q",
			extensionID,
			runtimeID,
		)
	}
	operation.mutex.Lock()
	if operation.closed || operation.context.Err() != nil {
		operation.mutex.Unlock()
		return nil, errors.Join(
			errors.New("compaction operation has ended"),
			operation.context.Err(),
			context.Cause(operation.context),
		)
	}
	operation.children.Add(1)
	operation.mutex.Unlock()
	var releaseOnce sync.Once
	return func(cause error) {
		releaseOnce.Do(func() {
			operation.mutex.Lock()
			operation.causes = errors.Join(operation.causes, cause)
			if operation.closed && cause != nil && !operation.isOwningCancellation(cause) {
				operation.terminalError = errors.Join(operation.terminalError, cause)
			}
			operation.mutex.Unlock()
			operation.children.Done()
		})
	}, nil
}

// LinkCompactionChild links admitted work to the parent lifetime with explicit Run context.
// The caller retains child admission ownership until linkage cleanup finishes.
func (s *Service) LinkCompactionChild(
	ctx context.Context,
	lifecycleID string,
) (childContext context.Context, unlink func()) {
	s.mutex.Lock()
	operation := s.compactionOperations[lifecycleID]
	s.mutex.Unlock()
	// Preserve Run values while propagating parent cancellation.
	child, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(operation.context, func() { cancel(context.Cause(operation.context)) })
	if cause := context.Cause(operation.context); cause != nil {
		cancel(cause)
	}
	return child, func() { stop(); cancel(nil) }
}
