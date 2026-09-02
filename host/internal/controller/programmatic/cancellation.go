package programmatic

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// cancellationTarget records terminal delivery state for one owned operation.
type cancellationTarget struct {
	// done closes after target terminal delivery order is established.
	done chan struct{}
	// state records the target terminal state.
	state operation.TerminalState
	// kind identifies the target request for payload validation.
	kind CommandKind
}

// targetRegistry owns cancellation target observations for one Programmatic connection.
type targetRegistry struct {
	// mutex protects target lifecycle state.
	mutex sync.Mutex
	// targets contains connection-owned operations before terminal delivery.
	targets map[string]*cancellationTarget
}

// newTargetRegistry creates an empty connection-owned target registry.
func newTargetRegistry() *targetRegistry {
	return &targetRegistry{mutex: sync.Mutex{}, targets: make(map[string]*cancellationTarget)}
}

// add registers terminal observation before Accepted is queued.
func (r *targetRegistry) add(id string, kind CommandKind) *cancellationTarget {
	target := &cancellationTarget{done: make(chan struct{}), state: 0, kind: kind}
	r.mutex.Lock()
	r.targets[id] = target
	r.mutex.Unlock()
	return target
}

// active returns a target that has not established terminal delivery order.
func (r *targetRegistry) active(id string) (*cancellationTarget, bool) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	target, present := r.targets[id]
	return target, present && target.state == 0
}

// kind returns the tracked operation kind for payload validation and diagnostics.
func (r *targetRegistry) kind(id string) CommandKind {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if target, present := r.targets[id]; present {
		return target.kind
	}
	return CommandUnspecified
}

// finish records terminal delivery order before a cancellation operation can complete.
func (r *targetRegistry) finish(id string, state operation.TerminalState) {
	r.mutex.Lock()
	target, present := r.targets[id]
	if present && target.state == 0 {
		target.state = state
		close(target.done)
		delete(r.targets, id)
	}
	r.mutex.Unlock()
}

// remove clears metadata for prepared work that never started.
func (r *targetRegistry) remove(id string, target *cancellationTarget) {
	r.mutex.Lock()
	if r.targets[id] == target {
		delete(r.targets, id)
		close(target.done)
	}
	r.mutex.Unlock()
}

// close releases terminal observers when delivery is unavailable.
func (r *targetRegistry) close() {
	r.mutex.Lock()
	for _, target := range r.targets {
		if target.state == 0 {
			close(target.done)
		}
	}
	r.targets = make(map[string]*cancellationTarget)
	r.mutex.Unlock()
}

// registeredPrepared manages target metadata around Host-prepared work.
type registeredPrepared struct {
	// id identifies the registered operation.
	id string
	// prepared is the Host-prepared work.
	prepared operation.Prepared[AgentEvent, Response]
	// registry owns the operation metadata.
	registry *targetRegistry
	// target is the registry entry owned by this wrapper.
	target *cancellationTarget
	// mutex protects the started marker.
	mutex sync.Mutex
	// started reports whether Owner invoked domain Run.
	started bool
	// release delegates Host cleanup exactly once.
	release sync.Once
}

var _ operation.Prepared[AgentEvent, Response] = (*registeredPrepared)(nil)

// Run marks domain work started and passes the Owner context unchanged.
func (p *registeredPrepared) Run(
	ctx context.Context,
	reporter operation.Reporter[AgentEvent],
) operation.Outcome[Response] {
	p.mutex.Lock()
	p.started = true
	p.mutex.Unlock()
	return p.prepared.Run(ctx, reporter)
}

// Release delegates cleanup once and removes metadata when domain work never started.
func (p *registeredPrepared) Release() {
	p.release.Do(func() {
		p.mutex.Lock()
		started := p.started
		p.mutex.Unlock()
		if !started {
			p.registry.remove(p.id, p.target)
		}
		p.prepared.Release()
	})
}

// cancellationPrepared cancels one operation through its Owner after Running is queued.
type cancellationPrepared struct {
	// owner controls the target operation context and completion.
	owner *operation.Owner[AgentEvent, Response]
	// targetID identifies the operation selected during preparation.
	targetID string
	// target preserves terminal delivery state across Owner removal.
	target *cancellationTarget
}

var _ operation.Prepared[AgentEvent, Response] = (*cancellationPrepared)(nil)

// Run requests Owner cancellation and preserves captured terminal state if the target completed first.
func (p *cancellationPrepared) Run(ctx context.Context, _ operation.Reporter[AgentEvent]) operation.Outcome[Response] {
	state, err := p.owner.CancelAndWait(ctx, p.targetID)
	if errors.Is(err, operation.ErrTargetNotActive) {
		state, err = p.waitForCapturedTarget(ctx)
	}
	if err != nil {
		if ctx.Err() != nil {
			return operation.Canceled[Response]()
		}
		return operation.Failed[Response](FailureCodeInternal, err)
	}
	return operation.Completed(cancelCompletedResponse(state))
}

// waitForCapturedTarget recovers terminal state when Owner completed the admitted target first.
func (p *cancellationPrepared) waitForCapturedTarget(ctx context.Context) (operation.TerminalState, error) {
	select {
	case <-p.target.done:
		if p.target.state == 0 {
			return 0, operation.ErrTargetNotActive
		}
		return p.target.state, nil
	case <-ctx.Done():
		return 0, context.Cause(ctx)
	}
}

// cancelCompletedResponse builds the completed cancellation result.
func cancelCompletedResponse(state operation.TerminalState) Response {
	return Response{
		OperationID:       "",
		Kind:              ResponseCancelCompleted,
		State:             mo.None[RunStateResult](),
		Messages:          nil,
		Models:            mo.None[ModelsResult](),
		Selection:         mo.None[model.Selection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		SessionTree:       mo.None[SessionTree](),
		TreeNavigation:    mo.None[TreeNavigationResult](),
		Replacement:       mo.None[SessionReplacement](),
		Rejection:         mo.None[Rejection](),
		CancelTargetState: mo.Some(state),
	}
}

// Release has no admission reservation to free.
func (*cancellationPrepared) Release() {}
