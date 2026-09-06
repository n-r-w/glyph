package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/samber/mo"

	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// ordinaryHandlerError preserves an extension-reported error without classifying a protocol failure.
type ordinaryHandlerError struct {
	// message contains the safe extension-provided failure text.
	message string
}

// Error returns the safe extension-provided failure text.
func (err ordinaryHandlerError) Error() string { return err.message }

// Handle maps one typed Host invocation to the extension operation stream.
func (r *Runtime) Handle(
	ctx context.Context,
	handlerID string,
	request extensionruntime.HandlerInvocation,
) (extensionruntime.HandlerAction, error) {
	mapped, err := mapHandleRequest(handlerID, request)
	if err != nil {
		return extensionruntime.HandlerAction{}, err
	}
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(mapped)
	operationID := r.operationID()
	slog.DebugContext(
		ctx,
		"invoke extension handler",
		"operation_id",
		operationID,
		"extension_id",
		request.Context.ExtensionID,
		"runtime_instance_id",
		request.Context.RuntimeInstanceID,
		"session_id",
		request.Context.SessionID,
	)
	started, err := r.connection.Start(ctx, operationID, hostRequest)
	if err != nil {
		return extensionruntime.HandlerAction{}, r.handlerOperationError(ctx, handlerID, err)
	}
	completed, err := started.Wait(ctx, nil)
	if err != nil {
		var cancellationErr error
		if ctx.Err() != nil {
			cancellationErr = r.cancelOperation(context.WithoutCancel(ctx), operationID)
		}
		if isConnectionFailure(err) || isConnectionFailure(cancellationErr) {
			r.Close()
		}
		return extensionruntime.HandlerAction{}, errors.Join(
			r.handlerOperationError(ctx, handlerID, err),
			cancellationErr,
		)
	}
	mappedResponse, err := mapHandleResponse(request, completed.GetHandle())
	if err != nil {
		if handlerErr, ok := errors.AsType[ordinaryHandlerError](err); ok {
			return extensionruntime.HandlerAction{}, handlerErr
		}
		return extensionruntime.HandlerAction{}, r.protocolViolation(err)
	}
	return mappedResponse, nil
}

// handlerOperationError preserves operation errors and classifies stream failures as unavailability.
func (r *Runtime) handlerOperationError(ctx context.Context, handlerID string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("handle extension handler %q: %w", handlerID, ctxErr)
	}
	if isExtensionTerminalError(err) {
		return fmt.Errorf("handle extension handler %q: %w", handlerID, err)
	}
	r.Close()
	return fmt.Errorf(
		"%w: handle extension handler %q: %w",
		extensionruntime.ErrExtensionUnavailable,
		handlerID,
		err,
	)
}

// mapHandleRequest encodes the runtime-owned filtered operation payload.
func mapHandleRequest(
	handlerID string,
	request extensionruntime.HandlerInvocation,
) (*extensionpb.HandleRequest, error) {
	//nolint:exhaustruct_v5 // The builder sets only the active operation payload.
	builder := extensionpb.HandleRequest_builder{HandlerId: new(handlerID), Context: mapContext(request.Context)}
	switch request.Kind {
	case extensionruntime.InvocationRequest, extensionruntime.InvocationResult:
		original, err := mapPreparation(request.Original)
		if err != nil {
			return nil, err
		}
		current, err := mapPreparation(request.Current)
		if err != nil {
			return nil, err
		}
		if request.Kind == extensionruntime.InvocationRequest {
			builder.SessionBeforeTreeRequest = extensionpb.SessionBeforeTreeRequestInvocation_builder{
				OriginalRequest: mapNavigationRequest(request.Original.Request), OriginalPreparation: original,
				CurrentRequest: mapNavigationRequest(request.Current.Request), CurrentPreparation: current,
				CurrentResult: mapOptionalSummaryResult(request.CurrentResult),
			}.Build()
		} else {
			builder.SessionBeforeTreeResult = extensionpb.SessionBeforeTreeResultInvocation_builder{
				OriginalRequest:     mapNavigationRequest(request.Original.Request),
				OriginalPreparation: original,
				CurrentRequest:      mapNavigationRequest(request.Current.Request),
				CurrentPreparation:  current,
				OriginalResult: mapOptionalSummaryResult(
					request.OriginalResult,
				),
				CurrentResult: mapOptionalSummaryResult(request.CurrentResult),
			}.Build()
		}
	case extensionruntime.InvocationObserver:
		commit, present := request.Commit.Get()
		if !present {
			return nil, fmt.Errorf("handler %q request has no single payload", handlerID)
		}
		builder.SessionTree = mapSessionTreeInvocation(commit)
	default:
		return nil, fmt.Errorf("handler %q has unsupported request kind %d", handlerID, request.Kind)
	}
	return builder.Build(), nil
}

// mapHandleResponse validates transport correlation and returns raw process actions without capability policy.
func mapHandleResponse(
	request extensionruntime.HandlerInvocation,
	response *extensionpb.HandleResponse,
) (extensionruntime.HandlerAction, error) {
	if response == nil {
		return extensionruntime.HandlerAction{}, errors.New("handler response is missing")
	}
	if handlerErr := response.GetError(); handlerErr != nil {
		return extensionruntime.HandlerAction{}, ordinaryHandlerError{message: handlerErr.GetMessage()}
	}
	result := extensionruntime.HandlerAction{
		Kind:          request.Kind,
		Cancel:        false,
		RequestAction: 0,
		Request:       mo.None[extensionruntime.Navigation](),
		ResultAction:  0,
		Result:        mo.None[extensionruntime.Summary](),
	}
	switch request.Kind {
	case extensionruntime.InvocationRequest:
		action := response.GetSessionBeforeTreeRequest()
		if action == nil {
			return extensionruntime.HandlerAction{}, errors.New("request handler returned another action kind")
		}
		result.Cancel = action.GetCancel()
		result.RequestAction = int32(action.GetRequestAction())
		if replacement := action.GetRequest(); replacement != nil {
			result.Request = mo.Some(mapNavigationRequestFromProto(replacement))
		}
		result.ResultAction = int32(action.GetResultAction())
		result.Result = mapOptionalSummaryResultFromProto(action.GetResult())
	case extensionruntime.InvocationResult:
		action := response.GetSessionBeforeTreeResult()
		if action == nil {
			return extensionruntime.HandlerAction{}, errors.New("result handler returned another action kind")
		}
		result.Cancel = action.GetCancel()
		result.ResultAction = int32(action.GetResultAction())
		result.Result = mapOptionalSummaryResultFromProto(action.GetResult())
	case extensionruntime.InvocationObserver:
		if response.GetSessionTree() == nil {
			return extensionruntime.HandlerAction{}, errors.New("session-tree observer returned another action kind")
		}
	default:
		return extensionruntime.HandlerAction{}, fmt.Errorf("unsupported request kind %d", request.Kind)
	}
	return result, nil
}
