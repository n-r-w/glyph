// Package main demonstrates user-input middleware using only the public SDK.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// inputHandle identifies the handler that fully handles a recognizable input.
	inputHandle = "input-handle"
	// inputText identifies the handler that transforms every text block.
	inputText = "input-text"
	// inputImages identifies the handler that replaces every image block with text.
	inputImages = "input-images"
	// handledMarker is the text that makes input-handle fully handle the input.
	handledMarker = "glyph-handle-input"
	// textSuffix is appended to every text block by input-text.
	textSuffix = " glyph-input"
	// removedImageText replaces every image block in the input-images replacement.
	removedImageText = "[image removed]"
	// failureText is the complete ordinary error of the failure scenario.
	failureText = "reference input handler failed"
	// invalidArgument identifies rejected unsupported tool calls.
	invalidArgument = "INVALID_ARGUMENT"
)

// service admits reference operations; the executable name selects acceptance scenarios.
type service struct {
	// mode selects a reference scenario through the executable name.
	mode string
	// failed records that the single scenario failure was already returned.
	failed atomic.Bool
	// traceMutex serializes trace file appends.
	traceMutex sync.Mutex
}

// Verify the reference service admits operations through the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration supplies ordered public middleware declarations.
type registration struct{}

// Verify registration implements the public SDK registration contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// operation retains one admitted public invocation.
type operation struct {
	// service supplies the scenario state.
	service *service
	// request contains the admitted public operation payload.
	request *extensionpb.HandleRequest
}

// Verify handler invocation implements the public SDK handler contract.
var _ extensionsdk.HandleOperation = (*operation)(nil)

// main starts the public-only reference process.
func main() {
	extensionsdk.Serve(&service{mode: filepath.Base(os.Args[0]), failed: atomic.Bool{}, traceMutex: sync.Mutex{}})
}

// PrepareRegister admits the fixed reference catalog.
func (s *service) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registration{}, nil
}

// PrepareHandle admits an invocation after SDK identity and payload validation.
func (s *service) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	return &operation{service: s, request: request}, nil
}

// PrepareExecute rejects tools because this extension contributes middleware handlers only.
func (s *service) PrepareExecute(context.Context, *extensionpb.ExecuteRequest) (extensionsdk.ExecuteOperation, error) {
	return nil, extensionsdk.Reject(invalidArgument, errors.New("input reference extension defines no tools"))
}

// Run returns the complete ordered declaration list.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	ids := []string{inputHandle, inputText, inputImages}
	handlers := make([]*extensionpb.HandlerDescriptor, 0, len(ids))
	for _, id := range ids {
		handlers = append(handlers, extensionpb.HandlerDescriptor_builder{
			Id: new(id), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_USER_INPUT),
		}.Build())
	}
	return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: handlers}.Build(), nil
}

// Release frees the registration admission.
func (r *registration) Release() {}

// Release frees the handler admission.
func (o *operation) Release() {}

// Run selects the action of the invoked handler after recording the invocation when tracing is enabled.
func (o *operation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	input := o.request.GetUserInput()
	if err := o.service.trace(o.request.GetHandlerId(), input); err != nil {
		return nil, err
	}
	switch o.request.GetHandlerId() {
	case inputHandle:
		return o.handle(input.GetCurrent()), nil
	case inputText:
		return o.text(ctx, input)
	case inputImages:
		return o.images(input), nil
	default:
		return nil, errors.New("unknown input handler " + o.request.GetHandlerId())
	}
}

// handle fully handles input whose text contains the handled marker and preserves any other input.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func (o *operation) handle(current *extensionpb.SessionTreeUserMessage) *extensionpb.HandleResponse {
	recognized := slices.ContainsFunc(current.GetContent(), func(content *extensionpb.SessionTreeUserContent) bool {
		return strings.Contains(content.GetText(), handledMarker)
	})
	if !recognized {
		return preserve()
	}
	return extensionpb.HandleResponse_builder{
		UserInput: extensionpb.UserInputAction_builder{Handle: new(true)}.Build(),
	}.Build()
}

// transformText appends the reference suffix to every text block.
func transformText(current *extensionpb.SessionTreeUserMessage) *extensionpb.SessionTreeUserMessage {
	content := make([]*extensionpb.SessionTreeUserContent, 0, len(current.GetContent()))
	for _, block := range current.GetContent() {
		if block.HasText() {
			block = textBlock(block.GetText() + textSuffix)
		}
		content = append(content, block)
	}
	return extensionpb.SessionTreeUserMessage_builder{Content: content}.Build()
}

// removeImages replaces every image block with a text block.
func removeImages(current *extensionpb.SessionTreeUserMessage) *extensionpb.SessionTreeUserMessage {
	content := make([]*extensionpb.SessionTreeUserContent, 0, len(current.GetContent()))
	for _, block := range current.GetContent() {
		if block.HasImage() {
			block = textBlock(removedImageText)
		}
		content = append(content, block)
	}
	return extensionpb.SessionTreeUserMessage_builder{Content: content}.Build()
}

// textBlock creates one text input block.
func textBlock(text string) *extensionpb.SessionTreeUserContent {
	return extensionpb.SessionTreeUserContent_builder{Text: new(text), Image: nil}.Build()
}

// preserve keeps the current input.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func preserve() *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		UserInput: extensionpb.UserInputAction_builder{Preserve: new(true)}.Build(),
	}.Build()
}

// replace returns a complete replacement input.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func replace(input *extensionpb.SessionTreeUserMessage) *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		UserInput: extensionpb.UserInputAction_builder{Replace: input}.Build(),
	}.Build()
}
