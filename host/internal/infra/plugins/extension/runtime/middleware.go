package runtime

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// HandleWire maps one ordinary wire operation over the existing accepted operation stream.
func (r *Runtime) HandleWire(
	ctx context.Context,
	handlerID string,
	input middleware.Invocation,
) (middleware.Action, error) {
	var action middleware.Action
	err := r.handleMiddleware(ctx, mapWireRequest(handlerID, input), func(response *extensionpb.HandleResponse) error {
		action = mapWireAction(input.Operation, response)
		return nil
	})
	return action, err
}

// handleMiddleware sends one typed middleware payload and decodes its action inside the invocation.
func (r *Runtime) handleMiddleware(
	ctx context.Context,
	request *extensionpb.HandleRequest,
	decode func(*extensionpb.HandleResponse) error,
) (returnErr error) {
	handlerID := request.GetHandlerId()
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
		return errors.Join(waitErr, errors.New("middleware handler response is missing"))
	}
	if handlerErr := response.GetError(); handlerErr != nil {
		return errors.Join(waitErr, newOrdinaryHandlerError(handlerErr.GetMessage()))
	}
	return errors.Join(waitErr, decode(response))
}

// mapWireRequest converts detached owner data into its declared public payload.
func mapWireRequest(handlerID string, input middleware.Invocation) *extensionpb.HandleRequest {
	request := new(extensionpb.HandleRequest)
	request.SetHandlerId(handlerID)
	request.SetContext(mapContext(input.Context))
	switch input.Operation {
	case middleware.HeadersOperation:
		request.SetProviderHeaders(extensionpb.ProviderHeadersInvocation_builder{
			Original: mapWireHeaders(input.OriginalHeaders), Current: mapWireHeaders(input.CurrentHeaders),
		}.Build())
	case middleware.BodyOperation:
		request.SetProviderBody(
			extensionpb.ProviderBodyInvocation_builder{
				Original: input.OriginalBody,
				Current:  input.CurrentBody,
			}.Build(),
		)
	case middleware.ResponseOperation:
		request.SetProviderResponse(
			extensionpb.ProviderResponseInvocation_builder{
				Status:  new(input.Response.Status),
				Headers: mapWireHeaders(input.Response.Headers),
			}.Build(),
		)
	}
	return request
}

// mapWireHeaders converts public header collections without sharing mutable slices.
func mapWireHeaders(headers modelexecution.WireHeaders) *extensionpb.WireHeaders {
	entries := make(map[string]*extensionpb.WireHeaderValues, len(headers))
	for name, values := range headers {
		entries[name] = extensionpb.WireHeaderValues_builder{Values: append([]string(nil), values...)}.Build()
	}
	return extensionpb.WireHeaders_builder{Entries: entries}.Build()
}

// decodeWireHeaders detaches a public replacement for owner validation.
func decodeWireHeaders(headers *extensionpb.WireHeaders) modelexecution.WireHeaders {
	result := make(modelexecution.WireHeaders, len(headers.GetEntries()))
	for name, values := range headers.GetEntries() {
		result[name] = append([]string(nil), values.GetValues()...)
	}
	return result
}

// mapWireAction leaves mismatched or false actions invalid for ordinary owner failure mapping.
func mapWireAction(operation middleware.WireOperation, response *extensionpb.HandleResponse) middleware.Action {
	switch operation {
	case middleware.HeadersOperation:
		value := response.GetProviderHeaders()
		if value.GetPreserve() {
			return middleware.Action{Kind: middleware.Preserve, Headers: nil, Body: nil}
		}
		if value.HasReplace() {
			return middleware.Action{
				Kind:    middleware.Replace,
				Headers: decodeWireHeaders(value.GetReplace()),
				Body:    nil,
			}
		}
	case middleware.BodyOperation:
		value := response.GetProviderBody()
		if value.GetPreserve() {
			return middleware.Action{Kind: middleware.Preserve, Headers: nil, Body: nil}
		}
		if value.HasReplace() {
			return middleware.Action{
				Kind:    middleware.Replace,
				Headers: nil,
				Body:    append([]byte(nil), value.GetReplace()...),
			}
		}
	case middleware.ResponseOperation:
		if response.GetProviderResponse().GetAcknowledge() {
			return middleware.Action{Kind: middleware.Acknowledge, Headers: nil, Body: nil}
		}
	}
	return middleware.Action{}
}
