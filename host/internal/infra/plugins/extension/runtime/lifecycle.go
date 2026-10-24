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
) (returnErr error) {
	invocation, err := mapLifecycleEvent(event)
	if err != nil {
		return fmt.Errorf("map lifecycle observer %q: %w", handlerID, err)
	}
	request := extensionpb.HandleRequest_builder{
		CompactionOutgoingInput: nil,
		CompletedConversation:   nil,
		Retry:                   nil,
		HandlerId:               new(handlerID), Context: mapContext(event.Context),
		SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil, SessionTree: nil, Lifecycle: invocation,
		ModelSelection: nil, ReasoningSelection: nil,
		CompactionRequest: nil, CompactionGenerate: nil, CompactionResult: nil,
		CompactionSuccess: nil, CompactionFailure: nil,

		ParentCompactionId: nil,
		CompactionPolicy:   nil,
	}.Build()
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(request)
	operationID := r.operationID()
	ctx, finish := r.beginInvocation(ctx, operationID)
	defer func() { returnErr = finish(classifyHandlerFailure(ctx, returnErr)) }()
	started, err := r.connection.Start(ctx, operationID, hostRequest)
	if err != nil {
		return r.handlerOperationError(ctx, handlerID, err)
	}
	completed, waitErr := r.waitForOperation(ctx, operationID, started, nil)
	if waitErr != nil {
		waitErr = r.handlerOperationError(ctx, handlerID, waitErr)
	}
	if completed == nil {
		return waitErr
	}
	response := completed.GetHandle()
	if response == nil {
		return errors.Join(waitErr, r.protocolViolation(errors.New("lifecycle observer response is missing")))
	}
	if handlerErr := response.GetError(); handlerErr != nil {
		return errors.Join(waitErr, newOrdinaryHandlerError(handlerErr.GetMessage()))
	}
	if response.GetLifecycle() == nil {
		return errors.Join(waitErr, r.protocolViolation(errors.New("lifecycle observer returned another action kind")))
	}
	return waitErr
}
