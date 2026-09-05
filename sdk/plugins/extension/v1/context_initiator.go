package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// contextInitiator owns extension-initiated requests independently of Host-initiated identifiers.
type contextInitiator struct {
	// ctx cancels all pending local work when this stream ends.
	ctx context.Context
	// cancel stops automatic cancellation and waiting work.
	cancel context.CancelCauseFunc
	// writer shares ordered outbound delivery with extension-owned operation events.
	writer *operation.Writer[*extensionpb.OpenResponse]
	// tracker validates lifecycle order and bounds per-operation event buffering.
	tracker *operation.Tracker[struct{}, *extensionpb.HostCompleted]
	// fail terminates the connection on local delivery or peer protocol failure.
	fail func(error)
	// mutex protects pending operations, identifier allocation, and shutdown admission.
	mutex sync.Mutex
	// pending records expected completion kinds and cancellation lifetimes.
	pending map[string]*contextOperation
	// sequence allocates identifiers only in the extension initiator namespace.
	sequence uint64
	// closing prevents new starts while owned work is joined.
	closing bool
	// workers joins every automatic cancellation worker.
	workers sync.WaitGroup
}

// contextOperation owns one initiated request and its local event queue.
type contextOperation struct {
	// initiator owns the stream lifetime and connection error.
	initiator *contextInitiator
	// id identifies this request within the extension initiator namespace.
	id string
	// kind identifies the required terminal payload.
	kind hostRequestKind
	// events contains validated lifecycle events for local waiting.
	events <-chan operation.Event[struct{}, *extensionpb.HostCompleted]
	// done closes when a remote terminal event arrives.
	done chan struct{}
}

// contextOperationError preserves incoming Host error provenance across an extension handler's own outcome.
type contextOperationError struct {
	// id identifies the nested Host operation and is empty before local admission.
	id string
	// code retains the Host operation's category independently of an outer tool or handler category.
	code string
	// cause retains the classified public SDK error and its full text.
	cause error
}

// Error includes the nested operation category even when an outer operation uses another closed category set.
func (e *contextOperationError) Error() string {
	return fmt.Sprintf("Host operation %q category %s: %v", e.id, e.code, e.cause)
}

// Unwrap retains public FailureError and RejectionError classification for extension callers.
func (e *contextOperationError) Unwrap() error { return e.cause }

// newContextInitiator creates the extension-owned tracker on the existing writer.
func newContextInitiator(
	ctx context.Context,
	writer *operation.Writer[*extensionpb.OpenResponse],
	fail func(error),
) *contextInitiator {
	owned, cancel := context.WithCancelCause(ctx)
	return &contextInitiator{
		ctx:      owned,
		cancel:   cancel,
		writer:   writer,
		tracker:  operation.NewTracker[struct{}, *extensionpb.HostCompleted](),
		fail:     fail,
		mutex:    sync.Mutex{},
		pending:  make(map[string]*contextOperation),
		sequence: 0,
		closing:  false,
		workers:  sync.WaitGroup{},
	}
}

// start tracks and enqueues a request without waiting for remote acceptance.
func (i *contextInitiator) start(
	ctx context.Context,
	request *extensionpb.ExtensionRequest,
) (*contextOperation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start Host operation: %w", err)
	}
	i.mutex.Lock()
	if i.closing || i.ctx.Err() != nil {
		i.mutex.Unlock()
		return nil, &contextOperationError{
			id:   "",
			code: contextCodeStale,
			cause: Fail(
				contextCodeStale,
				fmt.Errorf("start Host operation through a closed runtime binding: %w", context.Cause(i.ctx)),
			),
		}
	}
	i.sequence++
	id := "extension-host-" + strconv.FormatUint(i.sequence, 10)
	events, err := i.tracker.Track(id)
	if err != nil {
		i.mutex.Unlock()
		return nil, fmt.Errorf("track Host operation %q: %w", id, err)
	}
	started := &contextOperation{
		initiator: i,
		id:        id,
		kind:      classifyHostRequest(request),
		events:    events,
		done:      make(chan struct{}),
	}
	i.pending[id] = started
	// Reserve worker ownership before Close can start waiting for concurrent admission to finish.
	if request.GetCancel() == nil {
		i.workers.Add(1)
	}
	i.mutex.Unlock()
	if err = i.writer.Enqueue(
		extensionpb.OpenResponse_builder{OperationId: new(id), Event: nil, Request: request}.Build(),
	); err != nil {
		i.fail(mapDeliveryError(err))
		if request.GetCancel() == nil {
			i.workers.Done()
		}
		return nil, fmt.Errorf("queue Host operation %q: %w", id, err)
	}
	if request.GetCancel() == nil {
		go i.cancelOnContext(ctx, started)
	}
	return started, nil
}

// cancelOnContext targets remote work only when its operation-start context is canceled.
func (i *contextInitiator) cancelOnContext(ctx context.Context, target *contextOperation) {
	defer i.workers.Done()
	select {
	case <-target.done:
		return
	case <-i.ctx.Done():
		return
	case <-ctx.Done():
	}
	request := new(extensionpb.ExtensionRequest)
	request.SetCancel(operationpb.CancelOperation_builder{TargetOperationId: new(target.id)}.Build())
	cancel, err := i.start(i.ctx, request)
	if err != nil {
		return
	}
	_, err = cancel.wait(i.ctx)
	if rejection, ok := errors.AsType[*RejectionError](err); ok && rejection.Code() == rejectionCodeTargetNotActive {
		return
	}
	if err != nil && i.ctx.Err() == nil {
		i.fail(fmt.Errorf("cancel Host operation %q: %w", target.id, err))
	}
}

// wait consumes lifecycle events without changing remote cancellation state.
func (o *contextOperation) wait(ctx context.Context) (*extensionpb.HostCompleted, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for Host operation %q: %w", o.id, ctx.Err())
		case event, open := <-o.events:
			if !open {
				if cause := context.Cause(o.initiator.ctx); cause != nil {
					return nil, fmt.Errorf("wait for Host operation %q: connection closed: %w", o.id, cause)
				}
				return nil, fmt.Errorf("wait for Host operation %q: stream ended without terminal result", o.id)
			}
			switch event.Kind {
			case operation.EventAccepted, operation.EventRunning:
			case operation.EventCompleted:
				return event.Result, nil
			case operation.EventCanceled:
				return nil, newCanceledError()
			case operation.EventFailed:
				return nil, &contextOperationError{
					id:    o.id,
					code:  event.Code,
					cause: newRemoteFailure(event.Code, event.Message),
				}
			case operation.EventRejected:
				return nil, &contextOperationError{
					id:    o.id,
					code:  event.Code,
					cause: newRemoteRejection(event.Code, event.Message),
				}
			case operation.EventProgress:
				return nil, fmt.Errorf("catalog operation %q returned unexpected progress", o.id)
			default:
				return nil, fmt.Errorf("wait for Host operation %q: invalid lifecycle kind", o.id)
			}
		}
	}
}

// handle validates and routes Host events without running extension callbacks.
func (i *contextInitiator) handle(id string, payload *extensionpb.HostEvent) error {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	pending, found := i.pending[id]
	if !found {
		return hostPeerError(fmt.Errorf("received Host event references unknown extension operation %q", id), payload)
	}
	event, terminal, err := mapHostEvent(id, pending.kind, payload)
	if err != nil {
		return err
	}
	if err = i.tracker.Handle(event); err != nil {
		return hostPeerError(err, payload)
	}
	if terminal {
		close(pending.done)
		delete(i.pending, id)
	}
	return nil
}

// close cancels and joins automatic cancellation work and settles every pending waiter.
func (i *contextInitiator) close(cause error) {
	i.mutex.Lock()
	i.closing = true
	i.cancel(cause)
	i.tracker.Close()
	for _, pending := range i.pending {
		close(pending.done)
	}
	clear(i.pending)
	i.mutex.Unlock()
	i.workers.Wait()
}
