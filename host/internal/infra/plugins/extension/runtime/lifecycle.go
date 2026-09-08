package runtime

import (
	"context"
	"errors"
	"fmt"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// ObserveLifecycle invokes one lifecycle observer through the shared handler operation.
func (r *Runtime) ObserveLifecycle(
	ctx context.Context,
	handlerID string,
	event extensionruntime.LifecycleInvocation,
) error {
	invocation, err := mapLifecycleEvent(event)
	if err != nil {
		return fmt.Errorf("map lifecycle observer %q: %w", handlerID, err)
	}
	request := extensionpb.HandleRequest_builder{
		HandlerId: new(handlerID), Context: mapContext(event.Context),
		SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, SessionTree: nil, Lifecycle: invocation,
	}.Build()
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(request)
	operationID := r.operationID()
	started, err := r.connection.Start(ctx, operationID, hostRequest)
	if err != nil {
		return r.handlerOperationError(ctx, handlerID, err)
	}
	completed, err := started.Wait(ctx, nil)
	if err != nil {
		var cancellationErr error
		if ctx.Err() != nil {
			cancellationErr = r.cancelOperation(context.WithoutCancel(ctx), operationID)
		}
		if isConnectionFailure(err) || isConnectionFailure(cancellationErr) {
			_ = r.Close()
		}
		return errors.Join(r.handlerOperationError(ctx, handlerID, err), cancellationErr)
	}
	response := completed.GetHandle()
	if response == nil {
		return r.protocolViolation(errors.New("lifecycle observer response is missing"))
	}
	if handlerErr := response.GetError(); handlerErr != nil {
		return ordinaryHandlerError{message: handlerErr.GetMessage()}
	}
	if response.GetLifecycle() == nil {
		return r.protocolViolation(errors.New("lifecycle observer returned another action kind"))
	}
	return nil
}
