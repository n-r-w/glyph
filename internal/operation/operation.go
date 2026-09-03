// Package operation coordinates transport-neutral operation lifecycles.
package operation

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrIdentifierInUse reports an identifier reserved by an active operation.
var ErrIdentifierInUse = errors.New("operation identifier is in use")

// ErrTargetNotActive reports a cancellation target that is not active.
var ErrTargetNotActive = errors.New("operation target is not active")

// ErrQueueFull reports a bounded queue that has no available capacity.
var ErrQueueFull = errors.New("operation queue is full")

// ErrClosed reports a runtime component that no longer accepts work.
var ErrClosed = errors.New("operation runtime is closed")

// TerminalState identifies an operation terminal state.
type TerminalState uint8

const (
	// TerminalStateCompleted reports successful completion.
	TerminalStateCompleted TerminalState = iota + 1
	// TerminalStateCanceled reports cancellation.
	TerminalStateCanceled
	// TerminalStateFailed reports failure.
	TerminalStateFailed
)

//go:generate go tool mockgen -source=operation.go -destination=interfaces_mock_test.go -package=operation

// Prepared holds admitted operation work and its reservation.
type Prepared[P, R any] interface {
	// Run executes admitted work and returns its terminal outcome.
	Run(context.Context, Reporter[P]) Outcome[R]
	// Release frees the operation-specific admission reservation.
	Release()
}

// Delivery accepts lifecycle events in operation order.
type Delivery[P, R any] interface {
	// Accepted queues admission and returns its delivery acknowledgement.
	Accepted(string) (*Acknowledgement, error)
	// Running queues notification that operation work is starting.
	Running(string) error
	// Progress queues one contract-owned progress payload.
	Progress(string, P) error
	// Terminal queues the selected outcome and returns its delivery acknowledgement.
	Terminal(string, Outcome[R]) (*Acknowledgement, error)
}

// Outcome contains one terminal operation result.
type Outcome[R any] struct {
	// state identifies the selected terminal state.
	state TerminalState
	// result contains completed operation data.
	result R
	// code contains a machine-readable failure code.
	code string
	// err contains the internal failure cause.
	err error
}

// Completed constructs a successful outcome.
func Completed[R any](result R) Outcome[R] {
	return Outcome[R]{state: TerminalStateCompleted, result: result, code: "", err: nil}
}

// Canceled constructs a canceled outcome.
func Canceled[R any]() Outcome[R] {
	var zero R
	return Outcome[R]{state: TerminalStateCanceled, result: zero, code: "", err: nil}
}

// Failed constructs a failed outcome with a machine code and internal error.
func Failed[R any](code string, err error) Outcome[R] {
	if code == "" {
		panic("failed outcome code is required")
	}
	if err == nil {
		panic("failed outcome error is required")
	}
	var zero R
	return Outcome[R]{state: TerminalStateFailed, result: zero, code: code, err: err}
}

// State returns the terminal state.
func (o Outcome[R]) State() TerminalState { return o.state }

// Result returns the completed result and whether it is present.
func (o Outcome[R]) Result() (R, bool) { return o.result, o.state == TerminalStateCompleted }

// Code returns the failure machine code.
func (o Outcome[R]) Code() string { return o.code }

// Err returns the internal failure error.
func (o Outcome[R]) Err() error { return o.err }

// reporterState serializes progress delivery with reporter shutdown.
type reporterState[P any] struct {
	// mutex keeps in-flight progress ahead of terminal selection.
	mutex sync.Mutex
	// active permits progress while Prepared.Run is active.
	active bool
	// report delivers one typed progress payload.
	report func(P) error
}

// Reporter delivers progress for one running operation.
type Reporter[P any] struct {
	// state is shared by copies passed into operation work.
	state *reporterState[P]
}

// Report enqueues progress without waiting for queue capacity.
func (r Reporter[P]) Report(progress P) error {
	if r.state == nil {
		return ErrClosed
	}
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	if !r.state.active {
		return ErrClosed
	}
	return r.state.report(progress)
}

// deactivate waits for in-flight progress and stops later reports.
func (r Reporter[P]) deactivate() {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	r.state.active = false
}

// ownedOperation contains one accepted operation's cancellation and completion state.
type ownedOperation struct {
	// cancel requests operation work cancellation after admission succeeds.
	cancel context.CancelCauseFunc
	// active reports that preparation succeeded and cancellation is available.
	active bool
	// done closes after terminal delivery or connection delivery failure.
	done chan struct{}
	// state records the selected terminal state.
	state TerminalState
	// err records a delivery failure that prevented lifecycle completion.
	err error
}

// Owner owns accepted operations for one connection.
type Owner[P, R any] struct {
	// workContext is the parent of every operation context.
	workContext context.Context
	// cancelWork cancels every operation without stopping normal terminal delivery.
	cancelWork context.CancelCauseFunc
	// deliveryContext stops acknowledgement waits after transport failure.
	deliveryContext context.Context
	// cancelDelivery stops acknowledgement waits that cannot complete.
	cancelDelivery context.CancelCauseFunc
	// delivery receives lifecycle events in transport queue order.
	delivery Delivery[P, R]
	// mutex protects ownership, closure, and failure state.
	mutex sync.Mutex
	// operations contains identifiers until terminal delivery resolves.
	operations map[string]*ownedOperation
	// closed prevents new accepted operations.
	closed bool
	// cause records the first connection delivery failure.
	cause error
	// preparations joins bounded admission callbacks before closure returns.
	preparations sync.WaitGroup
	// workers joins operation coordination goroutines.
	workers sync.WaitGroup
}

// NewOwner constructs an operation owner bound to a connection context.
func NewOwner[P, R any](ctx context.Context, delivery Delivery[P, R]) *Owner[P, R] {
	if delivery == nil {
		panic("operation delivery is required")
	}
	workContext, cancelWork := context.WithCancelCause(ctx)
	deliveryContext, cancelDelivery := context.WithCancelCause(ctx)
	return &Owner[P, R]{
		workContext:     workContext,
		cancelWork:      cancelWork,
		deliveryContext: deliveryContext,
		cancelDelivery:  cancelDelivery,
		delivery:        delivery,
		mutex:           sync.Mutex{},
		operations:      make(map[string]*ownedOperation),
		closed:          false,
		cause:           nil,
		preparations:    sync.WaitGroup{},
		workers:         sync.WaitGroup{},
	}
}

// Start claims an identifier, performs bounded admission, and starts accepted-delivery coordination.
func (o *Owner[P, R]) Start(id string, prepare func() (Prepared[P, R], error)) error {
	if id == "" {
		return errors.New("start operation: empty identifier")
	}
	if prepare == nil {
		return fmt.Errorf("start operation %q: preparation is required", id)
	}

	owned := &ownedOperation{
		cancel: nil,
		active: false,
		done:   make(chan struct{}),
		state:  0,
		err:    nil,
	}
	o.mutex.Lock()
	if o.closed || o.workContext.Err() != nil {
		o.mutex.Unlock()
		return ErrClosed
	}
	if _, exists := o.operations[id]; exists {
		o.mutex.Unlock()
		return ErrIdentifierInUse
	}
	o.operations[id] = owned
	o.preparations.Add(1)
	o.mutex.Unlock()

	prepared, err := prepare()
	if err != nil {
		o.releaseClaim(id, owned)
		o.preparations.Done()
		return err
	}
	if prepared == nil {
		o.releaseClaim(id, owned)
		o.preparations.Done()
		return fmt.Errorf("start operation %q: prepared work is required", id)
	}

	o.mutex.Lock()
	if o.closed || o.workContext.Err() != nil {
		delete(o.operations, id)
		o.mutex.Unlock()
		prepared.Release()
		o.preparations.Done()
		return ErrClosed
	}
	operationContext, cancel := context.WithCancelCause(o.workContext)
	owned.cancel = cancel
	owned.active = true
	o.workers.Add(1)
	o.mutex.Unlock()
	o.preparations.Done()

	go o.run(operationContext, id, owned, prepared)
	return nil
}

// releaseClaim removes an identifier after preparation does not create accepted work.
func (o *Owner[P, R]) releaseClaim(id string, owned *ownedOperation) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	if o.operations[id] == owned {
		delete(o.operations, id)
	}
}

// run coordinates lifecycle delivery and admitted work for one operation.
func (o *Owner[P, R]) run(
	ctx context.Context,
	id string,
	owned *ownedOperation,
	prepared Prepared[P, R],
) {
	defer o.workers.Done()
	release := sync.OnceFunc(prepared.Release)

	accepted, err := o.delivery.Accepted(id)
	if err != nil {
		release()
		o.failOperation(id, owned, err)
		return
	}
	if err = accepted.Wait(o.deliveryContext); err != nil {
		release()
		o.failOperation(id, owned, err)
		return
	}

	if err = o.delivery.Running(id); err != nil {
		release()
		o.failOperation(id, owned, err)
		return
	}

	var outcome Outcome[R]
	if ctx.Err() != nil {
		outcome = Canceled[R]()
	} else {
		reporter := Reporter[P]{state: &reporterState[P]{
			mutex:  sync.Mutex{},
			active: true,
			report: func(progress P) error {
				progressErr := o.delivery.Progress(id, progress)
				if progressErr != nil {
					o.Fail(progressErr)
				}
				return progressErr
			},
		}}
		outcome = prepared.Run(ctx, reporter)
		reporter.deactivate()
	}
	release()

	// Unexpected cleanup cannot deliver a terminal event, so only release ownership.
	if err = context.Cause(o.deliveryContext); err != nil {
		o.finishOperation(id, owned, 0, err)
		return
	}
	terminal, err := o.delivery.Terminal(id, outcome)
	if err != nil {
		o.failOperation(id, owned, err)
		return
	}
	if err = terminal.Wait(o.deliveryContext); err != nil {
		o.failOperation(id, owned, err)
		return
	}
	o.finishOperation(id, owned, outcome.State(), nil)
}

// failOperation records delivery failure and completes one operation waiter.
func (o *Owner[P, R]) failOperation(id string, owned *ownedOperation, err error) {
	o.Fail(err)
	o.finishOperation(id, owned, 0, err)
}

// finishOperation releases identifier ownership after delivery resolves.
func (o *Owner[P, R]) finishOperation(
	id string,
	owned *ownedOperation,
	state TerminalState,
	err error,
) {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	current, exists := o.operations[id]
	if !exists || current != owned {
		return
	}
	owned.state = state
	owned.err = err
	delete(o.operations, id)
	close(owned.done)
}

// Cancellation reserves an active target for cancellation after the cancellation operation starts.
func (o *Owner[P, R]) Cancellation(id string) (func(context.Context) (TerminalState, error), bool) {
	o.mutex.Lock()
	owned, exists := o.operations[id]
	active := exists && owned.active
	o.mutex.Unlock()
	if !active {
		return nil, false
	}
	return func(ctx context.Context) (TerminalState, error) {
		o.mutex.Lock()
		owned.cancel(context.Canceled)
		o.mutex.Unlock()
		select {
		case <-owned.done:
			return owned.state, owned.err
		case <-ctx.Done():
			return 0, context.Cause(ctx)
		}
	}, true
}

// CancelAndWait cancels an active target and waits for terminal delivery.
func (o *Owner[P, R]) CancelAndWait(ctx context.Context, id string) (TerminalState, error) {
	cancel, active := o.Cancellation(id)
	if !active {
		return 0, ErrTargetNotActive
	}
	return cancel(ctx)
}

// Fail cancels all owned work after a connection delivery failure.
func (o *Owner[P, R]) Fail(err error) {
	if err == nil {
		err = ErrClosed
	}
	o.mutex.Lock()
	if o.cause == nil {
		o.cause = err
	}
	o.closed = true
	o.mutex.Unlock()
	o.cancelDelivery(err)
	o.cancelWork(err)
}

// Close cancels and joins all owned work.
func (o *Owner[P, R]) Close() {
	o.mutex.Lock()
	o.closed = true
	o.mutex.Unlock()
	o.cancelWork(context.Canceled)
	o.preparations.Wait()
	o.workers.Wait()
}

// Wait joins all preparation and owned work without initiating cancellation.
func (o *Owner[P, R]) Wait() {
	o.preparations.Wait()
	o.workers.Wait()
}

// Err returns the connection failure that stopped the owner.
func (o *Owner[P, R]) Err() error {
	o.mutex.Lock()
	defer o.mutex.Unlock()
	return o.cause
}
