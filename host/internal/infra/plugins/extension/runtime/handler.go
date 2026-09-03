package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
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
	request extensionservice.HandlerRequest,
) (extensionservice.HandlerResponse, error) {
	mapped, err := mapHandleRequest(handlerID, request)
	if err != nil {
		return extensionservice.HandlerResponse{}, err
	}
	hostRequest := new(extensionpb.HostRequest)
	hostRequest.SetHandle(mapped)
	operationID := r.operationID()
	started, err := r.connection.Start(ctx, operationID, hostRequest)
	if err != nil {
		return extensionservice.HandlerResponse{}, r.handlerOperationError(ctx, handlerID, err)
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
		return extensionservice.HandlerResponse{}, errors.Join(
			r.handlerOperationError(ctx, handlerID, err),
			cancellationErr,
		)
	}
	mappedResponse, err := mapHandleResponse(request, completed.GetHandle())
	if err != nil {
		if handlerErr, ok := errors.AsType[ordinaryHandlerError](err); ok {
			return extensionservice.HandlerResponse{}, handlerErr
		}
		return extensionservice.HandlerResponse{}, r.protocolViolation(err)
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
		extensionservice.ErrExtensionUnavailable,
		handlerID,
		err,
	)
}

// mapHandleRequest maps the closed internal request variants to protobuf payloads.
func mapHandleRequest(handlerID string, request extensionservice.HandlerRequest) (*extensionpb.HandleRequest, error) {
	//nolint:exhaustruct_v5 // The request builder sets only the active handler payload.
	builder := extensionpb.HandleRequest_builder{HandlerId: new(handlerID)}
	kind, valid := request.Kind()
	if !valid {
		return nil, fmt.Errorf("handler %q request has no single payload", handlerID)
	}
	switch kind {
	case extensionservice.HandlerKindSessionBeforeTreeRequest:
		invocation, _ := request.SessionBeforeTreeRequest.Get()
		originalPreparation, mapErr := mapPreparation(invocation.Original)
		if mapErr != nil {
			return nil, mapErr
		}
		currentPreparation, mapErr := mapPreparation(invocation.Current)
		if mapErr != nil {
			return nil, mapErr
		}
		builder.SessionBeforeTreeRequest = extensionpb.SessionBeforeTreeRequestInvocation_builder{
			OriginalRequest:     mapNavigationRequest(invocation.Original.Request),
			OriginalPreparation: originalPreparation,
			CurrentRequest:      mapNavigationRequest(invocation.Current.Request),
			CurrentPreparation:  currentPreparation,
			CurrentResult:       mapOptionalSummaryResult(invocation.CurrentResult),
		}.Build()
	case extensionservice.HandlerKindSessionBeforeTreeResult:
		invocation, _ := request.SessionBeforeTreeResult.Get()
		originalPreparation, mapErr := mapPreparation(invocation.Original)
		if mapErr != nil {
			return nil, mapErr
		}
		currentPreparation, mapErr := mapPreparation(invocation.Current)
		if mapErr != nil {
			return nil, mapErr
		}
		builder.SessionBeforeTreeResult = extensionpb.SessionBeforeTreeResultInvocation_builder{
			OriginalRequest:     mapNavigationRequest(invocation.Original.Request),
			OriginalPreparation: originalPreparation,
			CurrentRequest:      mapNavigationRequest(invocation.Current.Request),
			CurrentPreparation:  currentPreparation,
			OriginalResult:      mapSummaryResult(invocation.OriginalResult),
			CurrentResult:       mapSummaryResult(invocation.CurrentResult),
		}.Build()
	case extensionservice.HandlerKindSessionTree:
		invocation, _ := request.SessionTree.Get()
		builder.SessionTree = mapSessionTreeInvocation(invocation)
	default:
		return nil, fmt.Errorf("handler %q has unsupported request kind %d", handlerID, kind)
	}
	return builder.Build(), nil
}

// mapHandleResponse maps and validates the response variant for the invoked handler kind.
func mapHandleResponse(
	request extensionservice.HandlerRequest,
	response *extensionpb.HandleResponse,
) (extensionservice.HandlerResponse, error) {
	if response == nil {
		return extensionservice.HandlerResponse{}, errors.New("handler response is missing")
	}
	if handlerErr := response.GetError(); handlerErr != nil {
		return extensionservice.HandlerResponse{}, ordinaryHandlerError{message: handlerErr.GetMessage()}
	}
	kind, valid := request.Kind()
	if !valid {
		return extensionservice.HandlerResponse{}, errors.New("handler request has no single payload")
	}
	switch kind {
	case extensionservice.HandlerKindSessionBeforeTreeRequest:
		action := response.GetSessionBeforeTreeRequest()
		if action == nil {
			return extensionservice.HandlerResponse{}, errors.New("request handler returned another action kind")
		}
		return mapRequestAction(action), nil
	case extensionservice.HandlerKindSessionBeforeTreeResult:
		action := response.GetSessionBeforeTreeResult()
		if action == nil {
			return extensionservice.HandlerResponse{}, errors.New("result handler returned another action kind")
		}
		return mapResultAction(action), nil
	case extensionservice.HandlerKindSessionTree:
		if response.GetSessionTree() == nil {
			return extensionservice.HandlerResponse{}, errors.New("session-tree observer returned another action kind")
		}
		return extensionservice.HandlerResponse{
			SessionBeforeTreeRequest: mo.None[extensionservice.SessionBeforeTreeRequestAction](),
			SessionBeforeTreeResult:  mo.None[extensionservice.SessionBeforeTreeResultAction](),
			SessionTree:              mo.Some(extensionservice.SessionTreeAction{}),
		}, nil
	default:
		return extensionservice.HandlerResponse{}, fmt.Errorf("unsupported request kind %d", kind)
	}
}

// mapRequestAction maps one request-handler action without applying composition rules.
func mapRequestAction(action *extensionpb.SessionBeforeTreeRequestAction) extensionservice.HandlerResponse {
	request := mo.None[extensionservice.NavigationRequest]()
	if action.GetRequest() != nil {
		request = mo.Some(mapNavigationRequestFromProto(action.GetRequest()))
	}
	return extensionservice.HandlerResponse{
		SessionBeforeTreeRequest: mo.Some(extensionservice.SessionBeforeTreeRequestAction{
			Cancel: action.GetCancel(), RequestAction: mapRequestActionKind(action.GetRequestAction()),
			Request: request, ResultAction: mapResultActionKind(action.GetResultAction()),
			Result: mapOptionalSummaryResultFromProto(action.GetResult()),
		}),
		SessionBeforeTreeResult: mo.None[extensionservice.SessionBeforeTreeResultAction](),
		SessionTree:             mo.None[extensionservice.SessionTreeAction](),
	}
}

// mapResultAction maps one result-handler action without applying composition rules.
func mapResultAction(action *extensionpb.SessionBeforeTreeResultAction) extensionservice.HandlerResponse {
	return extensionservice.HandlerResponse{
		SessionBeforeTreeRequest: mo.None[extensionservice.SessionBeforeTreeRequestAction](),
		SessionBeforeTreeResult: mo.Some(extensionservice.SessionBeforeTreeResultAction{
			Cancel: action.GetCancel(), ResultAction: mapResultActionKind(action.GetResultAction()),
			Result: mapOptionalSummaryResultFromProto(action.GetResult()),
		}),
		SessionTree: mo.None[extensionservice.SessionTreeAction](),
	}
}

// mapRequestActionKind maps known actions and leaves invalid values for composition validation.
func mapRequestActionKind(action extensionpb.RequestAction) extensionservice.RequestAction {
	switch action {
	case extensionpb.RequestAction_REQUEST_ACTION_PRESERVE:
		return extensionservice.RequestActionPreserve
	case extensionpb.RequestAction_REQUEST_ACTION_REPLACE:
		return extensionservice.RequestActionReplace
	case extensionpb.RequestAction_REQUEST_ACTION_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapResultActionKind maps known actions and leaves invalid values for composition validation.
func mapResultActionKind(action extensionpb.ResultAction) extensionservice.ResultAction {
	switch action {
	case extensionpb.ResultAction_RESULT_ACTION_PRESERVE:
		return extensionservice.ResultActionPreserve
	case extensionpb.ResultAction_RESULT_ACTION_REPLACE:
		return extensionservice.ResultActionReplace
	case extensionpb.ResultAction_RESULT_ACTION_CLEAR:
		return extensionservice.ResultActionClear
	case extensionpb.ResultAction_RESULT_ACTION_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}
