package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/internal/operation"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// operationDelivery maps shared lifecycle events to the Host-to-UI stream.
type operationDelivery struct {
	// ctx owns structured operation failure logs.
	ctx context.Context
	// writer serializes every Host stream message.
	writer *operation.Writer[*uiv1.OpenRequest]
	// fail closes the owning connection after outbound delivery failure.
	fail func(error)
	// mutex protects accepted operation kinds.
	mutex sync.Mutex
	// kinds maps accepted identifiers to public request kinds.
	kinds map[string]string
	// failureSources retains failed operation causes until terminal transport delivery.
	failureSources map[string]error
}

var _ operation.Delivery[controllerui.Frame, controllerui.OperationResult] = (*operationDelivery)(nil)

// Accepted queues and acknowledges one accepted event.
func (delivery *operationDelivery) Accepted(id string) (*operation.Acknowledgement, error) {
	event := new(uiv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return delivery.enqueueAcknowledged(hostEventRequest(id, event))
}

// Running queues one running event.
func (delivery *operationDelivery) Running(id string) error {
	event := new(uiv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return delivery.enqueue(hostEventRequest(id, event))
}

// Progress maps and queues one operation progress payload.
func (delivery *operationDelivery) Progress(id string, progress controllerui.Frame) error {
	request, err := mapFrame(progress)
	if err != nil {
		return err
	}
	if request.GetEvent().GetProgress() == nil {
		return errors.New("map UI operation progress: progress payload is required")
	}
	request.SetOperationId(id)
	return delivery.enqueue(request)
}

// Terminal maps and queues one terminal lifecycle event.
func (delivery *operationDelivery) Terminal(
	id string,
	outcome operation.Outcome[controllerui.OperationResult],
) (*operation.Acknowledgement, error) {
	var request *uiv1.OpenRequest
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		result, _ := outcome.Result()
		if state, present := result.Cancel.Get(); present {
			completed := new(uiv1.HostCompleted)
			completed.SetCancel(mapCancellationCompletion(state))
			event := new(uiv1.HostEvent)
			event.SetCompleted(completed)
			request = hostEventRequest(id, event)
		} else {
			mapped, err := mapFrame(result.Frame)
			if err != nil {
				return nil, err
			}
			if mapped.GetEvent().GetCompleted() == nil {
				return nil, errors.New("map UI operation terminal: completed payload is required")
			}
			mapped.SetOperationId(id)
			request = mapped
		}
	case operation.TerminalStateCanceled:
		event := new(uiv1.HostEvent)
		event.SetCanceled(new(operationv1.Canceled))
		request = hostEventRequest(id, event)
	case operation.TerminalStateFailed:
		delivery.setFailureSource(id, outcome.Err())
		slog.ErrorContext(delivery.ctx, "Host UI operation failed",
			slog.String("operation_id", id), slog.String("operation_kind", delivery.TakeKind(id)),
			slog.String("peer_kind", "ui"), slog.String("category", outcome.Code()),
			slog.Any("error", outcome.Err()),
		)
		event := new(uiv1.HostEvent)
		event.SetFailed(operationv1.Failed_builder{
			Code: new(outcome.Code()), Message: new(outcome.Err().Error()),
		}.Build())
		request = hostEventRequest(id, event)
	default:
		return nil, errors.New("map UI operation terminal: terminal state is required")
	}
	if outcome.State() != operation.TerminalStateFailed {
		delivery.TakeKind(id)
	}
	return delivery.enqueueAcknowledged(request)
}

// SetKind records one accepted operation kind before its worker starts.
func (delivery *operationDelivery) SetKind(id, kind string) {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	delivery.kinds[id] = kind
}

// TakeKind removes and returns one accepted operation kind.
func (delivery *operationDelivery) TakeKind(id string) string {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	kind := delivery.kinds[id]
	delete(delivery.kinds, id)
	return kind
}

// setFailureSource retains one source until the writer resolves terminal transport delivery.
func (delivery *operationDelivery) setFailureSource(id string, source error) {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	delivery.failureSources[id] = source
}

// takeFailureSource removes one retained terminal source.
func (delivery *operationDelivery) takeFailureSource(id string) error {
	delivery.mutex.Lock()
	defer delivery.mutex.Unlock()
	source := delivery.failureSources[id]
	delete(delivery.failureSources, id)
	return source
}

// enqueue queues one message and closes the connection on failure.
func (delivery *operationDelivery) enqueue(request *uiv1.OpenRequest) error {
	err := delivery.writer.Enqueue(request)
	if err != nil {
		delivery.fail(err)
	}
	return err
}

// enqueueAcknowledged queues one acknowledged message and closes the connection on failure.
func (delivery *operationDelivery) enqueueAcknowledged(
	request *uiv1.OpenRequest,
) (*operation.Acknowledgement, error) {
	acknowledgement, err := delivery.writer.EnqueueAcknowledged(request)
	if err != nil {
		err = errors.Join(delivery.takeFailureSource(request.GetOperationId()), err)
		delivery.fail(err)
	}
	return acknowledgement, err
}

// ClearOutput removes connection-scoped delivery handles.
func (c *Service) ClearOutput() {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.writer = nil
	c.progressReporter = operation.Reporter[controllerui.Frame]{}
	c.progressBound = false
	c.failConnection = nil
}

// rejectPrepared queues one request rejection without creating an operation.
func (delivery *operationDelivery) Reject(id, code string, cause error) error {
	event := new(uiv1.HostEvent)
	event.SetRejected(operationv1.Rejected_builder{Code: new(code), Message: new(cause.Error())}.Build())
	return delivery.writer.Enqueue(hostEventRequest(id, event))
}

// AttachOutput creates the ordered writer shared by operation and connection events.
func (c *Service) AttachOutput(
	ctx context.Context,
	fail func(error),
) (*operation.Writer[*uiv1.OpenRequest], controllerui.OperationOutput) {
	delivery := &operationDelivery{
		ctx: ctx, writer: nil,
		fail:  func(err error) { fail(fmt.Errorf("deliver UI operation event: %w", err)) },
		mutex: sync.Mutex{}, kinds: make(map[string]string), failureSources: make(map[string]error),
	}
	writer := operation.NewWriter(func(request *uiv1.OpenRequest) error {
		sendErr := c.stream.Send(request)
		var source error
		if request.GetEvent().GetFailed() != nil {
			source = delivery.takeFailureSource(request.GetOperationId())
		}
		if sendErr != nil {
			return errors.Join(source, sendErr)
		}
		return nil
	})
	delivery.writer = writer
	c.mutex.Lock()
	c.writer = writer
	c.failConnection = func(err error) { fail(fmt.Errorf("deliver UI connection event: %w", err)) }
	c.mutex.Unlock()
	return writer, delivery
}

// Recv receives the next UI input for controller processing.
func (c *Service) Recv() (*uiv1.OpenResponse, error) { return c.stream.Recv() }

// CloseSend half-closes Host output after ordered delivery has stopped.
func (c *Service) CloseSend() error { return c.stream.CloseSend() }

var (
	_ controllerui.Connection      = (*Service)(nil)
	_ controllerui.OperationOutput = (*operationDelivery)(nil)
)

var _ controllerui.StartupOutput = (*operationDelivery)(nil)

// CloseConnection queues the Host close request on the operation writer.
func (delivery *operationDelivery) CloseConnection() (*operation.Acknowledgement, error) {
	request := new(uiv1.OpenRequest)
	request.SetClose(new(operationv1.CloseConnection))
	return delivery.writer.EnqueueAcknowledged(request)
}

// mapCancellationCompletion encodes the actual target terminal state for the UI contract.
func mapCancellationCompletion(state operation.TerminalState) *operationv1.CancelCompleted {
	mapped := operationv1.TerminalState_TERMINAL_STATE_UNSPECIFIED
	switch state {
	case operation.TerminalStateCompleted:
		mapped = operationv1.TerminalState_TERMINAL_STATE_COMPLETED
	case operation.TerminalStateCanceled:
		mapped = operationv1.TerminalState_TERMINAL_STATE_CANCELED
	case operation.TerminalStateFailed:
		mapped = operationv1.TerminalState_TERMINAL_STATE_FAILED
	}
	return operationv1.CancelCompleted_builder{TargetState: new(mapped)}.Build()
}
