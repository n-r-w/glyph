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

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// BindPolicy supplies runtime invocation and durable replacement services.
func (s *Service) BindPolicy(
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
	notified := false
	defer func() {
		if notified {
			return
		}
		terminalCause = s.finishPolicyOutcome(
			ctx,
			handlers,
			mo.None[contextcompaction.PolicyState](),
			result,
			terminalCause,
			nil,
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
	notified = true
	result, _, terminalCause = s.runPolicy(
		ctx,
		owner,
		bound,
		snapshot,
		handlers,
		PolicyPurposeManual,
		input,
		instructions,
	)
	return result, terminalCause
}

// runPolicy owns one captured composition invocation and its outcome delivery.
func (s *Service) runPolicy(
	ctx context.Context,
	owner PolicyOwner,
	bound extension.Context,
	snapshot CompactionSnapshot,
	handlers contextcompaction.HandlerSet,
	purpose PolicyPurpose,
	input modelexecution.ProviderRequest,
	instructions mo.Option[string],
) (result modelexecution.ManualCompactionResult, disposition PolicyDisposition, terminalCause error) {
	result = modelexecution.ManualCompactionResult{Committed: mo.None[session.Entry](), Canceled: false}
	requests := newParticipants(handlers.Requests)
	generators := newParticipants(handlers.Generators)
	results := newParticipants(handlers.Results)
	state := mo.None[contextcompaction.PolicyState]()
	var acquiredCauses error
	defer func() {
		if purpose == PolicyPurposeAutomatic && disposition == PolicyPreserve && result.Committed.IsNone() &&
			terminalCause == nil {
			return
		}
		terminalCause = s.finishPolicyOutcome(ctx, handlers, state, result, terminalCause, acquiredCauses)
		if purpose == PolicyPurposeAutomatic && result.Canceled && terminalCause == nil {
			terminalCause = errors.Join(errors.New("compaction policy canceled automatic preparation"), acquiredCauses)
		}
	}()
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
		generators:           generators,
		results:              results,
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
		Purpose: purpose, OperationID: operationID,
		LifecycleID:  lifecycleID,
		Snapshot:     snapshot,
		Requests:     requests,
		Generators:   generators,
		Results:      results,
		Input:        input,
		Instructions: instructions, RecordState: operation.recordState,
	}
	var policyErr error
	disposition, policyErr = s.policyRuntime.InvokeCompactionPolicy(parentContext, owner, bound, invocation)
	committed, acquiredErr, terminalErr := operation.finish()
	acquiredCauses = acquiredErr
	state = operation.diagnosticState()
	s.mutex.Lock()
	delete(s.compactionOperations, operationID)
	delete(s.compactionOperations, lifecycleID)
	s.mutex.Unlock()
	result.Committed = committed
	_, hasReceipt := committed.Get()
	result.Canceled = disposition == PolicyCanceled && !hasReceipt
	if policyErr != nil {
		policyErr = fmt.Errorf(
			"invoke compaction policy %q generation %q: %w",
			owner.ExtensionID,
			owner.RuntimeID,
			policyErr,
		)
	}
	terminalErr = errors.Join(policyErr, terminalErr, ctx.Err(), context.Cause(ctx))
	terminalErr = errors.Join(terminalErr, disposition.validateCompletion(purpose, hasReceipt, terminalErr))
	if terminalErr != nil {
		return result, disposition, terminalErr
	}
	return result, disposition, nil
}
