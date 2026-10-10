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

// BindManualPolicy supplies runtime invocation and durable replacement services.
func (s *Service) BindManualPolicy(runtime PolicyRuntime, sessions CompactionSessions) {
	s.policyRuntime = runtime
	s.compactionSessions = sessions
}

// CompactManualPolicy invokes the registered owner for an admitted manual replacement.
func (s *Service) CompactManualPolicy(
	ctx context.Context,
	input modelexecution.ProviderRequest,
	instructions mo.Option[string],
) (result modelexecution.ManualCompactionResult, terminalCause error) {
	result = modelexecution.ManualCompactionResult{Committed: mo.None[session.Entry](), Canceled: false}
	// handlers fixes outcome delivery to registered generations at admission.
	handlers := s.policyRuntime.SnapshotCompactionHandlers()
	// state contains diagnostic values received from the policy.
	state := mo.None[contextcompaction.PolicyState]()
	// acquiredCauses retains handled child failures for a later terminal failure.
	var acquiredCauses error
	defer func() {
		terminalCause = s.finishManualOutcome(
			ctx,
			handlers,
			state,
			result,
			terminalCause,
			acquiredCauses,
		)
	}()
	// owners contains the policy generations captured for admission.
	owners := s.policyRuntime.SnapshotCompactionPolicies()
	if len(owners) == 0 {
		return result, errors.New("no compaction policy owner is registered")
	}
	if len(owners) != 1 {
		return result, errors.New("multiple compaction policy owners are registered")
	}
	// owner identifies the sole captured policy generation.
	owner := owners[0]
	// snapshot contains the immutable session input for this operation.
	snapshot := s.compactionSessions.CaptureCompaction()
	// bound contains the issued policy context; err contains any issuance failure.
	bound, err := s.IssueContext(owner.ExtensionID)
	if err != nil {
		return result, fmt.Errorf("issue captured policy context: %w", err)
	}
	s.mutex.Lock()
	// incarnation identifies the session lifetime of the issued binding.
	incarnation := s.bindings[owner.ExtensionID].incarnation
	s.mutex.Unlock()
	if bound.RuntimeInstanceID != owner.RuntimeID || bound.SessionID != snapshot.Identity.ID ||
		incarnation != snapshot.Identity.Incarnation {
		return result, errors.New("captured compaction policy runtime or session changed before invocation")
	}
	input.History = snapshot.History
	// operationID identifies the policy mutation authority.
	operationID := rand.Text()
	// lifecycleID identifies the nested-work lifetime.
	lifecycleID := rand.Text()
	// parentContext and cancel control the policy and nested-work lifetime.
	parentContext, cancel := context.WithCancel(ctx)
	// operation owns mutation state, acquired causes, and the durable receipt.
	operation := &compactionOperation{
		authorityID:          operationID,
		lifecycleID:          lifecycleID,
		owner:                owner,
		snapshot:             snapshot,
		context:              parentContext,
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
	s.compactionOperations[operationID] = operation
	s.compactionOperations[lifecycleID] = operation
	s.mutex.Unlock()
	// invocation contains captured input and scoped policy authority.
	invocation := PolicyInvocation{
		OperationID:  operationID,
		LifecycleID:  lifecycleID,
		Snapshot:     snapshot,
		Input:        input,
		Instructions: instructions, RecordState: operation.recordState,
	}
	// canceled and policyErr contain the policy cancellation and invocation outcomes.
	canceled, policyErr := s.policyRuntime.InvokeCompactionPolicy(parentContext, owner, bound, invocation)
	// committed contains the receipt; acquiredErr and terminalErr contain child and teardown failures.
	committed, acquiredErr, terminalErr := operation.finish()
	acquiredCauses = acquiredErr
	state = operation.diagnosticState()
	s.mutex.Lock()
	delete(s.compactionOperations, operationID)
	delete(s.compactionOperations, lifecycleID)
	s.mutex.Unlock()
	result.Committed = committed
	// hasReceipt selects receipt-dependent settlement once.
	_, hasReceipt := committed.Get()
	result.Canceled = canceled && !hasReceipt
	if policyErr != nil {
		policyErr = fmt.Errorf(
			"invoke compaction policy %q generation %q: %w",
			owner.ExtensionID,
			owner.RuntimeID,
			policyErr,
		)
	}
	terminalErr = errors.Join(policyErr, terminalErr, ctx.Err(), context.Cause(ctx))
	if canceled && hasReceipt {
		terminalErr = errors.Join(terminalErr, errors.New("compaction policy canceled after durable replacement"))
	}
	if !hasReceipt && !canceled && terminalErr == nil {
		terminalErr = errors.New("compaction policy finished without replacement or cancellation")
	}
	if terminalErr != nil {
		return result, terminalErr
	}
	return result, nil
}
