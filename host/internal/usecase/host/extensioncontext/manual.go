// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// BindManualPolicy supplies runtime invocation and durable replacement services.
func (s *Service) BindManualPolicy(
	runtime PolicyRuntime,
	sessions CompactionSessions,
	participantTimeout time.Duration,
) {
	s.policyRuntime = runtime
	s.compactionSessions = sessions
	s.participantTimeout = participantTimeout
}

// CompactManualPolicy invokes the registered owner for an admitted manual replacement.
func (s *Service) CompactManualPolicy(
	ctx context.Context,
	input modelexecution.ProviderRequest,
	instructions mo.Option[string],
) (result modelexecution.ManualCompactionResult, terminalCause error) {
	result = modelexecution.ManualCompactionResult{Committed: mo.None[session.Entry](), Canceled: false}
	// Use the observer generations captured at admission for terminal delivery.
	handlers := s.policyRuntime.SnapshotCompactionHandlers()
	requests := newRequestParticipants(handlers.Requests)
	state := mo.None[contextcompaction.PolicyState]()
	// Keep handled child failures for a later terminal failure.
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
	owners := s.policyRuntime.SnapshotCompactionPolicies()
	if len(owners) == 0 {
		return result, errors.New("no compaction policy owner is registered")
	}
	if len(owners) != 1 {
		return result, errors.New("multiple compaction policy owners are registered")
	}
	owner := owners[0]
	snapshot := s.compactionSessions.CaptureCompaction()
	bound, err := s.IssueContext(owner.ExtensionID)
	if err != nil {
		return result, fmt.Errorf("issue captured policy context: %w", err)
	}
	s.mutex.Lock()
	incarnation := s.bindings[owner.ExtensionID].incarnation
	s.mutex.Unlock()
	if bound.RuntimeInstanceID != owner.RuntimeID || bound.SessionID != snapshot.Identity.ID ||
		incarnation != snapshot.Identity.Incarnation {
		return result, errors.New("captured compaction policy runtime or session changed before invocation")
	}
	// operationID identifies the policy mutation authority.
	operationID := rand.Text()
	// lifecycleID identifies the nested-work lifetime.
	lifecycleID := rand.Text()
	parentContext, cancel := context.WithCancel(ctx)
	operation := &compactionOperation{
		authorityID:          operationID,
		lifecycleID:          lifecycleID,
		owner:                owner,
		snapshot:             snapshot,
		requests:             requests,
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
	invocation := PolicyInvocation{
		OperationID:  operationID,
		LifecycleID:  lifecycleID,
		Snapshot:     snapshot,
		Requests:     requests,
		Input:        input,
		Instructions: instructions, RecordState: operation.recordState,
	}
	canceled, policyErr := s.policyRuntime.InvokeCompactionPolicy(parentContext, owner, bound, invocation)
	committed, acquiredErr, terminalErr := operation.finish()
	acquiredCauses = acquiredErr
	state = operation.diagnosticState()
	s.mutex.Lock()
	delete(s.compactionOperations, operationID)
	delete(s.compactionOperations, lifecycleID)
	s.mutex.Unlock()
	result.Committed = committed
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
