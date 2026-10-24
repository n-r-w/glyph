package runtime

import (
	"context"
	"errors"
	"sync"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// invocationLifetime owns accepted children of one Host invocation on this connection.
type invocationLifetime struct {
	// mutex protects admission and retained failure.
	mutex sync.Mutex
	// done reports cancellation of the invocation.
	done <-chan struct{}
	// cancel stops the invocation and its children.
	cancel context.CancelCauseFunc
	// children joins admitted work before the parent returns.
	children sync.WaitGroup
	// closed prevents new admissions after failure or teardown.
	closed bool
	// failure retains qualifying independent causes.
	failure error
}

// invocationChild releases one accepted child exactly once.
type invocationChild struct {
	// parent owns cancellation and failure retention.
	parent *invocationLifetime
	// once protects child settlement.
	once sync.Once
	// failureOnce retains the independent child source once before settlement.
	failureOnce sync.Once
}

var _ extensioncontroller.InvocationChild = (*invocationChild)(nil)

// fatalInvocationError retains the independent nested cause at the Core tool boundary.
type fatalInvocationError struct {
	// cause contains complete nested failure text and identity.
	cause error
}

var (
	_ run.FatalOperationFailure = (*fatalInvocationError)(nil)
	_ errtree.CodedError        = (*fatalInvocationError)(nil)
)

// Code preserves fatal failure meaning when the underlying source contains only cancellation.
func (e *fatalInvocationError) Code() string { return string(modelexecution.FailureExtensionFailed) }

// Error exposes all acquired failure causes.
func (e *fatalInvocationError) Error() string { return e.cause.Error() }

// Unwrap exposes the retained failure tree.
func (e *fatalInvocationError) Unwrap() error { return e.cause }

// FatalOperation requires the owning run to stop.
func (e *fatalInvocationError) FatalOperation() {}

// beginInvocation binds the envelope identifier before the extension can start child work.
func (r *Runtime) beginInvocation(ctx context.Context, id string) (owned context.Context, finish func(error) error) {
	owned, cancel := context.WithCancelCause(ctx)
	lifetime := &invocationLifetime{
		mutex: sync.Mutex{}, done: owned.Done(), cancel: cancel,
		children: sync.WaitGroup{}, closed: false, failure: nil,
	}
	r.invocationsMutex.Lock()
	if r.invocations == nil {
		r.invocations = make(map[string]*invocationLifetime)
	}
	r.invocations[id] = lifetime
	r.invocationsMutex.Unlock()
	return owned, func(source error) error {
		lifetime.mutex.Lock()
		lifetime.closed = true
		lifetime.mutex.Unlock()
		cancel(context.Canceled)
		lifetime.children.Wait()
		r.invocationsMutex.Lock()
		delete(r.invocations, id)
		r.invocationsMutex.Unlock()
		lifetime.mutex.Lock()
		defer lifetime.mutex.Unlock()
		if lifetime.failure != nil {
			return &fatalInvocationError{cause: errors.Join(lifetime.failure, source)}
		}
		return source
	}
}

// BeginInvocationChild admits child work only while its exact parent is live on this connection.
func (r *Runtime) BeginInvocationChild(id string) (extensioncontroller.InvocationChild, error) {
	r.invocationsMutex.Lock()
	parent, found := r.invocations[id]
	r.invocationsMutex.Unlock()
	if !found {
		return nil, errors.New("parent invocation is unavailable")
	}
	parent.mutex.Lock()
	defer parent.mutex.Unlock()
	select {
	case <-parent.done:
		return nil, errors.Join(errors.New("parent invocation has ended"), parent.failure)
	default:
	}
	if parent.closed {
		return nil, errors.Join(errors.New("parent invocation has ended"), parent.failure)
	}
	parent.children.Add(1)
	return &invocationChild{parent: parent, once: sync.Once{}, failureOnce: sync.Once{}}, nil
}

// Link applies parent cancellation without replacing the child's caller cancellation.
func (c *invocationChild) Link(ctx context.Context) (linked context.Context, unlink func()) {
	linked, cancel := context.WithCancelCause(ctx)
	select {
	case <-c.parent.done:
		cancel(context.Canceled)
	default:
	}
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-c.parent.done:
			cancel(context.Canceled)
		case <-linked.Done():
		}
	}()
	return linked, func() { cancel(context.Canceled); <-joined }
}

// Fail retains a qualifying source and stops the parent while accepted cleanup remains owned.
func (c *invocationChild) Fail(failure error) {
	if failure == nil {
		return
	}
	c.failureOnce.Do(func() {
		c.parent.mutex.Lock()
		c.parent.failure = errors.Join(c.parent.failure, failure)
		c.parent.closed = true
		c.parent.cancel(failure)
		c.parent.mutex.Unlock()
	})
}

// Release ends accepted-child ownership exactly once after caller cleanup.
func (c *invocationChild) Release() {
	c.once.Do(c.parent.children.Done)
}
