package uiv1

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// Host exposes SDK-owned Host operation initiation and connection events.
type Host struct {
	// context owns initiated operations and event receipt.
	context context.Context
	// writer serializes UI requests with lifecycle delivery.
	writer *operation.Writer[*uiv1.OpenResponse]
	// tracker validates Host lifecycle events.
	tracker *operation.Tracker[*uiv1.HostProgress, *uiv1.HostCompleted]
	// events carries bounded Host connection events.
	events chan *uiv1.HostConnectionEvent
	// mutex protects closure state.
	mutex sync.Mutex
	// closed prevents new requests after local or peer closure.
	closed bool
	// operations correlates initiated identifiers with their request payload kind.
	operations map[string]requestKind
	// fail closes the owning connection after outbound delivery failure.
	fail func(error)
	// requestClose notifies the one endpoint closure coordinator.
	requestClose func()
	// eventsOnce closes the connection-event queue once.
	eventsOnce sync.Once
}

// newHost constructs one SDK-owned Host connection.
func newHost(
	ctx context.Context,
	writer *operation.Writer[*uiv1.OpenResponse],
	tracker *operation.Tracker[*uiv1.HostProgress, *uiv1.HostCompleted],
	fail func(error),
	requestClose func(),
) *Host {
	return &Host{
		context: ctx, writer: writer, tracker: tracker,
		events: make(chan *uiv1.HostConnectionEvent, connectionEventQueueCapacity),
		mutex:  sync.Mutex{}, closed: false, operations: make(map[string]requestKind),
		fail: fail, requestClose: requestClose, eventsOnce: sync.Once{},
	}
}

// Operation owns SDK state for one UI-initiated Host operation.
type Operation struct {
	// id is the caller-assigned operation identifier.
	id string
	// events contains validated lifecycle events in order.
	events <-chan operation.Event[*uiv1.HostProgress, *uiv1.HostCompleted]
}

// Cancellation owns SDK state for one UI-initiated cancellation operation.
type Cancellation struct {
	// operation carries the cancellation lifecycle.
	operation *Operation
}

// Start registers and queues one ordinary Host operation without waiting for acceptance.
func (h *Host) Start(
	ctx context.Context,
	id string,
	request *uiv1.UIRequest,
) (*Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start UI operation: %w", err)
	}
	kind, err := classifyUIRequest(request)
	if err != nil {
		return nil, fmt.Errorf("start UI operation: %w", err)
	}
	h.mutex.Lock()
	if h.closed {
		h.mutex.Unlock()
		return nil, operation.ErrClosed
	}
	events, err := h.tracker.Track(id)
	if err != nil {
		h.mutex.Unlock()
		return nil, fmt.Errorf("start UI operation: %w", err)
	}
	h.operations[id] = kind
	response := uiv1.OpenResponse_builder{OperationId: new(id), Request: request, Event: nil, Close: nil}.Build()
	err = h.writer.Enqueue(response)
	if err != nil {
		h.closed = true
		delete(h.operations, id)
	}
	h.mutex.Unlock()
	if err != nil {
		h.tracker.Close()
		h.fail(fmt.Errorf("start UI operation: %w", err))
		return nil, fmt.Errorf("start UI operation: %w", err)
	}
	return &Operation{id: id, events: events}, nil
}

// Cancel starts a separate cancellation operation for one target.
func (h *Host) Cancel(ctx context.Context, id, target string) (*Cancellation, error) {
	if target == "" {
		return nil, errors.New("cancel UI operation: target identifier is required")
	}
	cancel := operationv1.CancelOperation_builder{TargetOperationId: new(target)}.Build()
	request := new(uiv1.UIRequest)
	request.SetCancel(cancel)
	started, err := h.Start(ctx, id, request)
	if err != nil {
		return nil, err
	}
	return &Cancellation{operation: started}, nil
}

// Receive returns the next Host connection event.
func (h *Host) Receive(ctx context.Context) (*uiv1.HostConnectionEvent, error) {
	select {
	case event, open := <-h.events:
		if !open {
			return nil, operation.ErrClosed
		}
		return event, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-h.context.Done():
		return nil, context.Cause(h.context)
	}
}

// Close requests normal UI connection closure.
func (h *Host) Close(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("close UI connection: %w", err)
	}
	h.mutex.Lock()
	defer h.mutex.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	h.requestClose()
	return nil
}

// handleOperationEvent validates payload correlation before tracker state changes.
func (h *Host) handleOperationEvent(id string, event *uiv1.HostEvent) error {
	h.mutex.Lock()
	kind, tracked := h.operations[id]
	if !tracked {
		h.mutex.Unlock()
		return fmt.Errorf("handle Host operation event %q: identifier is not tracked", id)
	}
	if err := validateHostPayload(kind, event); err != nil {
		h.mutex.Unlock()
		return fmt.Errorf("handle Host operation event %q: %w", id, err)
	}
	terminal := event.GetCompleted() != nil || event.GetCanceled() != nil ||
		event.GetFailed() != nil || event.GetRejected() != nil
	if terminal {
		delete(h.operations, id)
	}
	h.mutex.Unlock()
	return h.tracker.Handle(mapHostEvent(id, event))
}

// deliverConnectionEvent queues one validated connection event without blocking receipt.
func (h *Host) deliverConnectionEvent(event *uiv1.HostConnectionEvent) error {
	if err := validateConnectionEvent(event); err != nil {
		return err
	}
	select {
	case h.events <- event:
		return nil
	default:
		return operation.ErrQueueFull
	}
}

// validateConnectionEvent checks every required connection-event field before delivery.
func validateConnectionEvent(event *uiv1.HostConnectionEvent) error {
	if event == nil || event.WhichEvent() == uiv1.HostConnectionEvent_Event_not_set_case {
		return errors.New("Host connection event is required")
	}
	switch event.WhichEvent() {
	case uiv1.HostConnectionEvent_Information_case:
		if !event.GetInformation().HasText() || event.GetInformation().GetText() == "" {
			return errors.New("Host connection information text is required")
		}
	case uiv1.HostConnectionEvent_Error_case:
		payload := event.GetError()
		if !payload.HasCode() || payload.GetCode() == "" {
			return errors.New("Host connection error category is required")
		}
		if !payload.HasText() || payload.GetText() == "" {
			return errors.New("Host connection error text is required")
		}
	case uiv1.HostConnectionEvent_AvailabilityChanged_case:
		payload := event.GetAvailabilityChanged()
		if !payload.HasAvailability() || payload.GetAvailability() == uiv1.Availability_AVAILABILITY_UNSPECIFIED {
			return errors.New("Host connection availability is required")
		}
	case uiv1.HostConnectionEvent_Event_not_set_case:
		return errors.New("Host connection event is required")
	default:
		return errors.New("Host connection event is unknown")
	}
	return nil
}

// markPeerClosed prevents new UI requests after Host closure.
func (h *Host) markPeerClosed() {
	h.mutex.Lock()
	h.closed = true
	h.mutex.Unlock()
}

// closeEvents releases connection-event receivers.
func (h *Host) closeEvents() { h.eventsOnce.Do(func() { close(h.events) }) }

// Wait delivers ordered progress and returns one terminal result.
func (o *Operation) Wait(
	ctx context.Context,
	onProgress func(*uiv1.HostProgress),
) (*uiv1.HostCompleted, error) {
	for {
		select {
		case event, open := <-o.events:
			if !open {
				return nil, operation.ErrClosed
			}
			switch event.Kind {
			case operation.EventAccepted, operation.EventRunning:
				continue
			case operation.EventProgress:
				if onProgress != nil {
					onProgress(event.Progress)
				}
			case operation.EventCompleted:
				return event.Result, nil
			case operation.EventCanceled:
				return nil, newCanceledError()
			case operation.EventFailed:
				return nil, newRemoteFailure(event.Code, event.Message)
			case operation.EventRejected:
				return nil, newRemoteRejection(event.Code, event.Message)
			}
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
}

// Wait returns the cancellation operation result.
func (c *Cancellation) Wait(ctx context.Context) (*operationv1.CancelCompleted, error) {
	completed, err := c.operation.Wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	if completed == nil || completed.GetCancel() == nil {
		return nil, errors.New("cancel UI operation: cancellation result is required")
	}
	return completed.GetCancel(), nil
}
