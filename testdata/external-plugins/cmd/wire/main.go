// Package main demonstrates ordinary provider-wire middleware using only the public SDK.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// headersA identifies the first public header transformation.
	headersA = "headers-a"
	// headersB identifies replacement from immutable original headers.
	headersB = "headers-b"
	// bodyA identifies the first safe-body transformation.
	bodyA = "body-a"
	// bodyB identifies replacement from immutable original safe JSON.
	bodyB = "body-b"
	// responseA identifies the first response observer.
	responseA = "response-a"
	// responseB identifies the later response observer.
	responseB = "response-b"
	// publicHeader carries a request-local demonstration value.
	publicHeader = "X-Glyph-Wire"
	// bodyMarker carries a request-local demonstration value.
	bodyMarker = "glyph_wire_marker"
	// markerA demonstrates a value replaced by the next handler.
	markerA = "A"
	// markerB demonstrates a final value derived from immutable original input.
	markerB = "B"
	// failureText is a complete ordinary error with a retry-like descendant in its text.
	failureText = "reference wire handler failed: unexpected EOF"
	// invalidArgument identifies rejected unsupported tool calls.
	invalidArgument = "INVALID_ARGUMENT"
	// failHeadersMode selects pre-dispatch header failure.
	failHeadersMode = "wire-fail-headers"
	// failBodyMode selects pre-dispatch body failure.
	failBodyMode = "wire-fail-body"
	// failResponseMode selects post-dispatch observer failure.
	failResponseMode = "wire-fail-response"
	// invalidMode selects an invalid action.
	invalidMode = "wire-invalid"
	// preserveMode preserves the original serialized body.
	preserveMode = "wire-preserve"
	// badReferenceMode selects an unknown private replay reference.
	badReferenceMode = "wire-bad-reference"
)

// service admits reference wire operations; executable suffixes select acceptance scenarios.
type service struct {
	// mode selects a reference scenario through the executable name.
	mode string
}

// Verify the reference service admits operations through the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration supplies ordered public wire declarations.
type registration struct{}

// Verify registration implements the public SDK registration contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// operation retains one admitted public invocation.
type operation struct {
	// mode selects a reference scenario through the executable name.
	mode string
	// request contains the admitted public operation payload.
	request *extensionpb.HandleRequest
}

// Verify wire invocation implements the public SDK handler contract.
var _ extensionsdk.HandleOperation = (*operation)(nil)

// main starts the public-only reference process.
func main() { extensionsdk.Serve(&service{mode: filepath.Base(os.Args[0])}) }

// PrepareRegister admits the fixed reference catalog.
func (s *service) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registration{}, nil
}

// PrepareHandle admits a wire invocation after SDK identity and payload validation.
func (s *service) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	return &operation{mode: s.mode, request: request}, nil
}

// PrepareExecute rejects tools because this extension contributes wire handlers only.
func (s *service) PrepareExecute(context.Context, *extensionpb.ExecuteRequest) (extensionsdk.ExecuteOperation, error) {
	return nil, extensionsdk.Reject(invalidArgument, errors.New("wire reference extension defines no tools"))
}

// Run returns the complete ordered declaration list.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: []*extensionpb.HandlerDescriptor{
		extensionpb.HandlerDescriptor_builder{
			Id:   new(headersA),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_HEADERS),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(headersB),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_HEADERS),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(bodyA),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_BODY),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(bodyB),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_BODY),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(responseA),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_RESPONSE),
		}.Build(),
		extensionpb.HandlerDescriptor_builder{
			Id:   new(responseB),
			Kind: new(extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_RESPONSE),
		}.Build(),
	}}.Build(), nil
}

// Release frees the registration admission.
func (r *registration) Release() {}

// Release frees the handler admission.
func (o *operation) Release() {}

// Run transforms public values or acknowledges immutable response metadata.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only the active action or ordinary failure.
func (o *operation) Run(context.Context) (*extensionpb.HandleResponse, error) {
	id := o.request.GetHandlerId()
	if o.mode == failHeadersMode && id == headersA || o.mode == failBodyMode && id == bodyA ||
		o.mode == failResponseMode && id == responseA {
		return extensionpb.HandleResponse_builder{
			Error: extensionpb.HandlerError_builder{Message: new(failureText)}.Build(),
		}.Build(), nil
	}
	if o.mode == invalidMode && id == bodyA {
		return extensionpb.HandleResponse_builder{
			ProviderBody: extensionpb.ProviderBodyAction_builder{}.Build(),
		}.Build(), nil
	}
	if input := o.request.GetProviderHeaders(); input != nil {
		return o.transformHeaders(input), nil
	}
	if input := o.request.GetProviderBody(); input != nil {
		return o.transformBody(input)
	}
	return extensionpb.HandleResponse_builder{
		ProviderResponse: extensionpb.ProviderResponseAction_builder{Acknowledge: new(true)}.Build(),
	}.Build(), nil
}
