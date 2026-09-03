package extensionv1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc/codes"

	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// requestKind identifies one Host-initiated extension operation.
type requestKind uint8

const (
	// unknownDiagnosticKind labels unrecognized request and event kinds in errors.
	unknownDiagnosticKind = "unknown"

	// requestRegister identifies startup registration.
	requestRegister requestKind = iota + 1
	// requestHandle identifies handler invocation.
	requestHandle
	// requestExecute identifies tool execution.
	requestExecute
	// requestCancel identifies targeted cancellation.
	requestCancel
)

// String returns the stable operation kind used in diagnostics.
func (kind requestKind) String() string {
	switch kind {
	case requestRegister:
		return "register"
	case requestHandle:
		return "handle"
	case requestExecute:
		return "execute"
	case requestCancel:
		return "cancel"
	default:
		return unknownDiagnosticKind
	}
}

// Connection owns the Host side of one extension operation stream.
type Connection struct {
	// ctx owns stream send and receive work.
	ctx context.Context
	// cancel stops stream work after failure or closure.
	cancel context.CancelCauseFunc
	// stream is the generated bidirectional extension stream.
	stream extensionpb.ExtensionService_OpenClient
	// writer serializes Host requests.
	writer *operation.Writer[*extensionpb.OpenRequest]
	// tracker validates extension lifecycle events.
	tracker *operation.Tracker[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted]
	// mutex protects request kinds and terminal connection state.
	mutex sync.Mutex
	// kinds maps initiated identifiers to expected completed payloads.
	kinds map[string]requestKind
	// err records the first stream failure.
	err error
	// writerDone reports completion of the writer goroutine.
	writerDone chan error
	// receiveDone reports completion of the receive goroutine.
	receiveDone chan error
	// closeOnce limits normal closure to one caller.
	closeOnce sync.Once
}

// Operation owns one initiated operation event queue.
type Operation struct {
	// connection owns stream failure state for this operation.
	connection *Connection
	// id identifies the operation for cancellation and diagnostics.
	id string
	// kind identifies the expected completed payload.
	kind requestKind
	// events contains validated lifecycle events.
	events <-chan operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted]
}

// Cancellation wraps one initiated cancellation operation.
type Cancellation struct {
	// operation carries the cancellation lifecycle.
	operation *Operation
}

// Open creates the single operation stream for a connected extension process.
func (c *Client) Open(ctx context.Context) (*Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open extension stream: %w", err)
	}
	streamContext, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	stream, err := c.service.Open(streamContext)
	if err != nil {
		cancel(err)
		return nil, fmt.Errorf("open extension stream: %w", err)
	}
	connection := &Connection{
		ctx:         streamContext,
		cancel:      cancel,
		stream:      stream,
		writer:      nil,
		tracker:     operation.NewTracker[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted](),
		mutex:       sync.Mutex{},
		kinds:       make(map[string]requestKind),
		err:         nil,
		writerDone:  make(chan error, 1),
		receiveDone: make(chan error, 1),
		closeOnce:   sync.Once{},
	}
	connection.writer = operation.NewWriter(func(request *extensionpb.OpenRequest) error {
		if sendErr := stream.Send(request); sendErr != nil {
			mapped := mapStreamError(sendErr)
			connection.fail(mapped)
			return mapped
		}
		return nil
	})
	writerResult := make(chan error, 1)
	go func() { writerResult <- connection.writer.Run(streamContext) }()
	go func() {
		writerErr := <-writerResult
		if writerErr != nil && !errors.Is(writerErr, context.Canceled) {
			connection.fail(writerErr)
		}
		connection.writerDone <- writerErr
	}()
	go func() { connection.receiveDone <- connection.receive() }()
	return connection, nil
}

// Start initiates one extension operation without waiting for acceptance.
func (c *Connection) Start(
	ctx context.Context,
	id string,
	request *extensionpb.HostRequest,
) (*Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start extension operation: %w", err)
	}
	kind, err := classifyRequest(request)
	if err != nil {
		return nil, err
	}
	events, err := c.tracker.Track(id)
	if err != nil {
		if connectionErr := c.connectionError(); connectionErr != nil {
			return nil, connectionErr
		}
		return nil, fmt.Errorf("track extension operation %q: %w", id, err)
	}
	c.mutex.Lock()
	c.kinds[id] = kind
	c.mutex.Unlock()
	message := extensionpb.OpenRequest_builder{
		OperationId: new(id), Request: request, Close: nil,
	}.Build()
	if err = c.writer.Enqueue(message); err != nil {
		mapped := mapDeliveryError(err)
		c.fail(mapped)
		return nil, fmt.Errorf("queue extension operation %q: %w", id, mapped)
	}
	return &Operation{connection: c, id: id, kind: kind, events: events}, nil
}

// Cancel initiates a separate operation that targets active work.
func (c *Connection) Cancel(
	ctx context.Context,
	id string,
	targetID string,
) (*Cancellation, error) {
	request := new(extensionpb.HostRequest)
	request.SetCancel(operationpb.CancelOperation_builder{TargetOperationId: new(targetID)}.Build())
	started, err := c.Start(ctx, id, request)
	if err != nil {
		return nil, err
	}
	return &Cancellation{operation: started}, nil
}

// Close requests orderly extension shutdown and joins stream work.
func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		if err := c.writer.Enqueue(extensionpb.OpenRequest_builder{
			OperationId: new(""), Request: nil, Close: new(operationpb.CloseConnection),
		}.Build()); err != nil {
			mapped := mapDeliveryError(err)
			c.fail(mapped)
		}
		c.join()
	})
	return c.connectionError()
}

// Fail marks a Host-detected peer protocol violation and joins failed connection work.
func (c *Connection) Fail(cause error) error {
	if cause == nil {
		panic("extension connection failure cause is required")
	}
	mapped := newProtocolStatusError(codes.FailedPrecondition, cause.Error(), cause)
	c.fail(mapped)
	c.closeOnce.Do(c.join)
	return c.connectionError()
}

// join stops the writer and waits for send, receive, and tracker cleanup.
func (c *Connection) join() {
	c.writer.Close()
	writerErr := <-c.writerDone
	if writerErr != nil && !errors.Is(writerErr, context.Canceled) {
		c.recordError(writerErr)
	}
	if err := c.stream.CloseSend(); err != nil {
		c.recordError(mapStreamError(err))
	}
	receiveErr := <-c.receiveDone
	if receiveErr != nil {
		c.recordError(receiveErr)
	}
	c.tracker.Close()
	c.cancel(context.Canceled)
}

// Wait delivers ordered progress and returns the completed payload or terminal error.
func (o *Operation) Wait(
	ctx context.Context,
	handleProgress func(*extensionpb.ToolProgress) error,
) (*extensionpb.ExtensionCompleted, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for extension operation %q: %w", o.id, ctx.Err())
		case event, open := <-o.events:
			if !open {
				if connectionErr := o.connection.connectionError(); connectionErr != nil {
					return nil, connectionErr
				}
				cause := fmt.Errorf("extension operation %q stream closed before terminal event", o.id)
				return nil, newProtocolStatusError(codes.FailedPrecondition, cause.Error(), cause)
			}
			result, done, err := o.handleEvent(event, handleProgress)
			if done || err != nil {
				return result, err
			}
		}
	}
}

// handleEvent processes one validated operation event on the waiting caller's goroutine.
func (o *Operation) handleEvent(
	event operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted],
	handleProgress func(*extensionpb.ToolProgress) error,
) (*extensionpb.ExtensionCompleted, bool, error) {
	switch event.Kind {
	case operation.EventAccepted, operation.EventRunning:
		return nil, false, nil
	case operation.EventProgress:
		if o.kind != requestExecute || event.Progress == nil {
			return nil, false, fmt.Errorf("extension operation %q returned mismatched progress", o.id)
		}
		if handleProgress == nil {
			return nil, false, nil
		}
		if err := handleProgress(event.Progress); err != nil {
			return nil, false, fmt.Errorf("deliver extension operation %q progress: %w", o.id, err)
		}
		return nil, false, nil
	case operation.EventCompleted:
		if !completedMatches(o.kind, event.Result) {
			return nil, false, fmt.Errorf("extension operation %q returned mismatched completion", o.id)
		}
		return event.Result, true, nil
	case operation.EventCanceled:
		return nil, true, newCanceledError()
	case operation.EventFailed:
		return nil, true, newRemoteFailure(event.Code, event.Message)
	case operation.EventRejected:
		return nil, true, newRemoteRejection(event.Code, event.Message)
	default:
		return nil, false, fmt.Errorf("extension operation %q returned unknown lifecycle event", o.id)
	}
}

// Wait returns the target terminal state after cancellation completes.
func (c *Cancellation) Wait(ctx context.Context) (*operationpb.CancelCompleted, error) {
	completed, err := c.operation.Wait(ctx, nil)
	if err != nil {
		return nil, err
	}
	return completed.GetCancel(), nil
}

// receive validates stream events without running user callbacks.
func (c *Connection) receive() error {
	for {
		response, err := c.stream.Recv()
		if errors.Is(err, io.EOF) {
			c.tracker.Close()
			return nil
		}
		if err != nil {
			mapped := mapStreamError(err)
			c.fail(mapped)
			return mapped
		}
		if err = c.handleResponse(response); err != nil {
			mapped := newProtocolStatusError(codes.FailedPrecondition, err.Error(), err)
			if errors.Is(err, operation.ErrQueueFull) {
				mapped = mapDeliveryError(err)
			}
			c.fail(mapped)
			return mapped
		}
	}
}

// handleResponse validates one lifecycle envelope and sends it to the tracker.
func (c *Connection) handleResponse(response *extensionpb.OpenResponse) error {
	if response == nil || response.GetEvent() == nil {
		return errors.New("extension response requires an event")
	}
	id := response.GetOperationId()
	c.mutex.Lock()
	kind, known := c.kinds[id]
	c.mutex.Unlock()
	if !known {
		return fmt.Errorf(
			"extension response references unknown operation %q with %s event",
			id,
			extensionEventName(response.GetEvent()),
		)
	}
	event, terminal, err := mapExtensionEvent(id, kind, response.GetEvent())
	if err != nil {
		return err
	}
	if trackerErr := c.tracker.Handle(event); trackerErr != nil {
		return trackerErr
	}
	if terminal {
		c.mutex.Lock()
		delete(c.kinds, id)
		c.mutex.Unlock()
	}
	return nil
}

// extensionEventName returns the stable event name used in protocol failure text.
func extensionEventName(event *extensionpb.ExtensionEvent) string {
	if event == nil {
		return "missing"
	}
	switch event.WhichEvent() {
	case extensionpb.ExtensionEvent_Accepted_case:
		return "accepted"
	case extensionpb.ExtensionEvent_Running_case:
		return "running"
	case extensionpb.ExtensionEvent_Progress_case:
		return "progress"
	case extensionpb.ExtensionEvent_Completed_case:
		return "completed"
	case extensionpb.ExtensionEvent_Canceled_case:
		return "canceled"
	case extensionpb.ExtensionEvent_Failed_case:
		return "failed"
	case extensionpb.ExtensionEvent_Rejected_case:
		return "rejected"
	case extensionpb.ExtensionEvent_Event_not_set_case:
		return "empty"
	default:
		return unknownDiagnosticKind
	}
}

// classifyRequest returns the exact request payload kind.
func classifyRequest(request *extensionpb.HostRequest) (requestKind, error) {
	if request == nil {
		return 0, errors.New("extension operation request is required")
	}
	switch request.WhichRequest() {
	case extensionpb.HostRequest_Register_case:
		return requestRegister, nil
	case extensionpb.HostRequest_Handle_case:
		return requestHandle, nil
	case extensionpb.HostRequest_Execute_case:
		return requestExecute, nil
	case extensionpb.HostRequest_Cancel_case:
		return requestCancel, nil
	case extensionpb.HostRequest_Request_not_set_case:
		return 0, errors.New("extension operation request payload is required")
	default:
		return 0, errors.New("extension operation request payload is unknown")
	}
}

// mapExtensionEvent maps one protobuf event into shared tracker input.
func mapExtensionEvent(
	id string,
	kind requestKind,
	event *extensionpb.ExtensionEvent,
) (operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted], bool, error) {
	mapped := operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted]{
		ID: id, Kind: 0, Progress: nil, Result: nil, Code: "", Message: "",
	}
	switch event.WhichEvent() {
	case extensionpb.ExtensionEvent_Accepted_case:
		mapped.Kind = operation.EventAccepted
	case extensionpb.ExtensionEvent_Running_case:
		mapped.Kind = operation.EventRunning
	case extensionpb.ExtensionEvent_Progress_case:
		mapped.Kind = operation.EventProgress
		mapped.Progress = event.GetProgress().GetTool()
		if kind != requestExecute || mapped.Progress == nil {
			return mapped, false, errors.New("extension progress does not match request kind")
		}
		if err := validateToolProgress(mapped.Progress); err != nil {
			return mapped, false, err
		}
	case extensionpb.ExtensionEvent_Completed_case,
		extensionpb.ExtensionEvent_Canceled_case,
		extensionpb.ExtensionEvent_Failed_case,
		extensionpb.ExtensionEvent_Rejected_case:
		return mapTerminalExtensionEvent(mapped, kind, event)
	case extensionpb.ExtensionEvent_Event_not_set_case:
		return mapped, false, errors.New("extension event payload is required")
	default:
		return mapped, false, errors.New("extension event payload is unknown")
	}
	return mapped, false, nil
}

// mapTerminalExtensionEvent maps and validates one terminal or rejected event.
func mapTerminalExtensionEvent(
	mapped operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted],
	kind requestKind,
	event *extensionpb.ExtensionEvent,
) (operation.Event[*extensionpb.ToolProgress, *extensionpb.ExtensionCompleted], bool, error) {
	switch event.WhichEvent() {
	case extensionpb.ExtensionEvent_Completed_case:
		mapped.Kind = operation.EventCompleted
		mapped.Result = event.GetCompleted()
		if !completedMatches(kind, mapped.Result) {
			return mapped, false, errors.New("extension completion does not match request kind")
		}
		if kind == requestCancel {
			if err := validateCancelCompleted(mapped.Result.GetCancel()); err != nil {
				return mapped, false, err
			}
		}
	case extensionpb.ExtensionEvent_Canceled_case:
		mapped.Kind = operation.EventCanceled
	case extensionpb.ExtensionEvent_Failed_case:
		mapped.Kind = operation.EventFailed
		mapped.Code = event.GetFailed().GetCode()
		mapped.Message = event.GetFailed().GetMessage()
		if err := validateFailureCode(mapped.Code); err != nil {
			return mapped, false, err
		}
	case extensionpb.ExtensionEvent_Rejected_case:
		mapped.Kind = operation.EventRejected
		mapped.Code = event.GetRejected().GetCode()
		mapped.Message = event.GetRejected().GetMessage()
		if err := validateRejectionCode(kind, mapped.Code); err != nil {
			return mapped, false, err
		}
	case extensionpb.ExtensionEvent_Event_not_set_case,
		extensionpb.ExtensionEvent_Accepted_case,
		extensionpb.ExtensionEvent_Running_case,
		extensionpb.ExtensionEvent_Progress_case:
		return mapped, false, errors.New("extension terminal event payload is invalid")
	default:
		return mapped, false, errors.New("extension terminal event payload is unknown")
	}
	return mapped, true, nil
}

// completedMatches reports whether completed data matches the initiating request.
func completedMatches(kind requestKind, completed *extensionpb.ExtensionCompleted) bool {
	if completed == nil {
		return false
	}
	switch kind {
	case requestRegister:
		return completed.GetRegister() != nil
	case requestHandle:
		return completed.GetHandle() != nil
	case requestExecute:
		return completed.GetTool() != nil
	case requestCancel:
		return completed.GetCancel() != nil
	default:
		return false
	}
}

// fail cancels stream work and closes tracked event queues.
func (c *Connection) fail(err error) {
	c.recordError(err)
	c.tracker.Close()
	c.cancel(err)
}

// connectionError returns the first connection failure.
func (c *Connection) connectionError() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.err
}

// recordError keeps the first connection error.
func (c *Connection) recordError(err error) {
	if err == nil {
		return
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.err == nil {
		c.err = err
	}
}

// mapStreamError preserves gRPC status and maps plain transport failures.
func mapStreamError(err error) error { return mapDeliveryError(err) }
