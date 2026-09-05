package extensionv1

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"

	"github.com/n-r-w/glyph/internal/operation"
	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// hostDelivery sends lifecycle events for work owned by Host on the existing stream writer.
type hostDelivery struct {
	// connection owns ordered transport delivery and connection failure.
	connection *Connection
}

var _ operation.Delivery[struct{}, *extensionpb.HostCompleted] = (*hostDelivery)(nil)

// Accepted queues the acceptance acknowledgement before execution starts.
func (d *hostDelivery) Accepted(id string) (*operation.Acknowledgement, error) {
	event := new(extensionpb.HostEvent)
	event.SetAccepted(new(operationpb.Accepted))
	return d.send(id, event)
}

// Running queues the running event without waiting for transport delivery.
func (d *hostDelivery) Running(id string) error {
	event := new(extensionpb.HostEvent)
	event.SetRunning(new(operationpb.Running))
	err := d.connection.writer.Enqueue(hostEventEnvelope(id, event))
	if err != nil {
		d.connection.fail(mapDeliveryError(err))
	}
	return err
}

// Progress rejects progress because context operations have no progress contract.
func (d *hostDelivery) Progress(_ string, _ struct{}) error {
	return errors.New("extension context operations cannot emit progress")
}

// Terminal publishes the result or complete closed-category failure.
func (d *hostDelivery) Terminal(
	id string,
	outcome operation.Outcome[*extensionpb.HostCompleted],
) (*operation.Acknowledgement, error) {
	event := new(extensionpb.HostEvent)
	switch outcome.State() {
	case operation.TerminalStateCompleted:
		result, _ := outcome.Result()
		event.SetCompleted(result)
	case operation.TerminalStateCanceled:
		event.SetCanceled(new(operationpb.Canceled))
	case operation.TerminalStateFailed:
		if err := validateHostOutputFailureCode(outcome.Code()); err != nil {
			return d.terminalFailure(errors.Join(err, outcome.Err()))
		}
		event.SetFailed(
			operationpb.Failed_builder{Code: new(outcome.Code()), Message: new(outcome.Err().Error())}.Build(),
		)
	default:
		return d.terminalFailure(errors.New("complete Host operation: terminal state is required"))
	}
	return d.send(id, event)
}

// terminalFailure stops the stream when local output cannot satisfy the closed operation contract.
func (d *hostDelivery) terminalFailure(cause error) (*operation.Acknowledgement, error) {
	err := newProtocolStatusError(codes.Internal, cause.Error(), cause)
	d.connection.fail(err)
	return nil, err
}

// send queues an acknowledged event and propagates queue failure to both peer lifecycles.
func (d *hostDelivery) send(id string, event *extensionpb.HostEvent) (*operation.Acknowledgement, error) {
	ack, err := d.connection.writer.EnqueueAcknowledged(hostEventEnvelope(id, event))
	if err != nil {
		d.connection.fail(mapDeliveryError(err))
	}
	return ack, err
}

// hostEventEnvelope constructs an event in the extension initiator namespace.
func hostEventEnvelope(id string, event *extensionpb.HostEvent) *extensionpb.OpenRequest {
	return extensionpb.OpenRequest_builder{OperationId: new(id), Request: nil, Close: nil, Event: event}.Build()
}

// handleHostRequest admits extension-initiated work without executing it on stream receipt.
func (c *Connection) handleHostRequest(id string, request *extensionpb.ExtensionRequest) error {
	kind := classifyHostRequest(request)
	if id == "" || kind == hostRequestInvalid {
		return c.rejectHostRequest(
			id,
			rejectionCodeInvalidArgument,
			errors.New("extension-initiated operation identifier and implemented request payload are required"),
		)
	}
	prepare := func() (operation.Prepared[struct{}, *extensionpb.HostCompleted], error) {
		if kind == hostRequestCancel {
			target := request.GetCancel().GetTargetOperationId()
			if target == "" {
				return nil, Reject(rejectionCodeInvalidArgument, errors.New("cancellation target is required"))
			}
			cancel, active := c.hostOwner.Cancellation(target)
			if !active {
				return nil, Reject(
					rejectionCodeTargetNotActive,
					fmt.Errorf("cancel Host operation %q: target is not active", target),
				)
			}
			return &hostCancellationPrepared{cancel: cancel}, nil
		}
		c.mutex.Lock()
		service := c.hostService
		c.mutex.Unlock()
		if service == nil {
			return nil, Reject(rejectionCodeNotReady, errors.New("extension-initiated Host dispatch is not bound"))
		}
		admitted, err := service.Prepare(c.ctx, id, request)
		if err != nil {
			return nil, err
		}
		if admitted == nil {
			return nil, errors.New("prepare Host operation: admitted work is required")
		}
		return &hostPrepared{operation: admitted, kind: kind}, nil
	}
	if err := c.hostOwner.Start(id, prepare); err != nil {
		if errors.Is(err, operation.ErrIdentifierInUse) {
			return c.rejectHostRequest(id, rejectionCodeOperationIDInUse, err)
		}
		if rejection, ok := errors.AsType[*RejectionError](err); ok {
			if validationErr := validateHostRejectionCode(kind, rejection.Code()); validationErr != nil {
				return errors.Join(validationErr, rejection)
			}
			return c.rejectHostRequest(id, rejection.Code(), rejection)
		}
		return err
	}
	return nil
}

// rejectHostRequest sends an admission rejection without terminating the active target operation.
func (c *Connection) rejectHostRequest(id, code string, cause error) error {
	event := new(extensionpb.HostEvent)
	event.SetRejected(operationpb.Rejected_builder{Code: new(code), Message: new(cause.Error())}.Build())
	return c.writer.Enqueue(hostEventEnvelope(id, event))
}

// hostPrepared adapts public admitted Host work to the shared operation owner.
type hostPrepared struct {
	// operation owns context validation and runtime accounting.
	operation HostOperation
	// kind identifies the required completion payload.
	kind hostRequestKind
}

var _ operation.Prepared[struct{}, *extensionpb.HostCompleted] = (*hostPrepared)(nil)

// Run executes the admitted read and validates its terminal payload.
func (p *hostPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[struct{}],
) operation.Outcome[*extensionpb.HostCompleted] {
	result, err := p.operation.Run(ctx)
	if err != nil {
		return operationOutcome[*extensionpb.HostCompleted](err)
	}
	if !hostCompletedMatches(p.kind, result) {
		return operation.Failed[*extensionpb.HostCompleted](
			failureCodeInternal,
			errors.New("completed Host payload does not match its admitted request"),
		)
	}
	return operation.Completed(result)
}

// Release returns the runtime's active-operation reservation.
func (p *hostPrepared) Release() { p.operation.Release() }

// hostCancellationPrepared owns targeted cancellation of work initiated by the extension.
type hostCancellationPrepared struct {
	// cancel joins the target and returns its actual terminal state.
	cancel func(context.Context) (operation.TerminalState, error)
}

var _ operation.Prepared[struct{}, *extensionpb.HostCompleted] = (*hostCancellationPrepared)(nil)

// Run cancels and joins the target through the shared operation lifecycle.
func (p *hostCancellationPrepared) Run(
	ctx context.Context,
	_ operation.Reporter[struct{}],
) operation.Outcome[*extensionpb.HostCompleted] {
	state, err := p.cancel(ctx)
	if err != nil {
		return operationOutcome[*extensionpb.HostCompleted](err)
	}
	var target operationpb.TerminalState
	switch state {
	case operation.TerminalStateCompleted:
		target = operationpb.TerminalState_TERMINAL_STATE_COMPLETED
	case operation.TerminalStateCanceled:
		target = operationpb.TerminalState_TERMINAL_STATE_CANCELED
	case operation.TerminalStateFailed:
		target = operationpb.TerminalState_TERMINAL_STATE_FAILED
	default:
		return operation.Failed[*extensionpb.HostCompleted](
			failureCodeInternal,
			errors.New("cancel Host operation: target terminal state is invalid"),
		)
	}
	result := new(extensionpb.HostCompleted)
	result.SetCancel(operationpb.CancelCompleted_builder{TargetState: new(target)}.Build())
	return operation.Completed(result)
}

// Release has no separate cancellation reservation to release.
func (p *hostCancellationPrepared) Release() {}
