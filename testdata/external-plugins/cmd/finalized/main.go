// Package main demonstrates finalized-message middleware using only the public SDK.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// finalizedMark identifies the handler that marks each finalized message.
	finalizedMark = "finalized-mark"
	// finalizedLater identifies the handler that preserves each finalized message after finalized-mark.
	finalizedLater = "finalized-later"
	// echoTool names the tool whose result the reference finalizes.
	echoTool = "finalized_echo"
	// echoOutput is the text result of echoTool.
	echoOutput = "echo output"
	// invalidArgument identifies rejected unknown tool calls.
	invalidArgument = "INVALID_ARGUMENT"
)

// service admits reference operations; the executable name selects acceptance scenarios.
type service struct {
	// mode selects a reference scenario through the executable name.
	mode string
	// userSeen records that finalized-mark already received a user message.
	userSeen atomic.Bool
	// toolSeen records that finalized-mark already received a tool result.
	toolSeen atomic.Bool
	// modelResponses counts the model responses that finalized-mark received.
	modelResponses atomic.Int64
	// traceMutex serializes trace file appends.
	traceMutex sync.Mutex
}

// Verify the reference service admits operations through the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration supplies ordered public declarations.
type registration struct{}

// Verify registration implements the public SDK registration contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// operation retains one admitted handler invocation.
type operation struct {
	// service supplies the scenario state.
	service *service
	// request contains the admitted public operation payload.
	request *extensionpb.HandleRequest
}

// Verify handler invocation implements the public SDK handler contract.
var _ extensionsdk.HandleOperation = (*operation)(nil)

// execution retains one admitted tool invocation.
type execution struct {
	// service supplies the trace file.
	service *service
	// arguments contains the JSON arguments of the call.
	arguments []byte
}

// Verify tool execution implements the public SDK execution contract.
var _ extensionsdk.ExecuteOperation = (*execution)(nil)

// main starts the public-only reference process.
func main() {
	extensionsdk.Serve(&service{
		mode: filepath.Base(os.Args[0]), userSeen: atomic.Bool{}, toolSeen: atomic.Bool{},
		modelResponses: atomic.Int64{}, traceMutex: sync.Mutex{},
	})
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

// PrepareExecute admits the echo tool.
func (s *service) PrepareExecute(
	_ context.Context,
	request *extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if request.GetToolName() != echoTool {
		return nil, extensionsdk.Reject(invalidArgument, errors.New("unknown tool "+request.GetToolName()))
	}
	return &execution{service: s, arguments: request.GetArgumentsJson()}, nil
}

// Run returns the echo tool and the two finalized-message handlers in their order.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	ids := []string{finalizedMark, finalizedLater}
	handlers := make([]*extensionpb.HandlerDescriptor, 0, len(ids))
	for _, id := range ids {
		handlers = append(handlers, extensionpb.HandlerDescriptor_builder{
			Id: new(id), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_FINALIZED_MESSAGE),
		}.Build())
	}
	tool := extensionpb.ToolDescriptor_builder{
		Name: new(echoTool), Description: new("Return a fixed text."),
		InputSchemaJson: []byte(`{"type":"object"}`), ConstrainedSampling: nil,
	}.Build()
	return extensionpb.RegisterResponse_builder{
		Tools: []*extensionpb.ToolDescriptor{tool}, Handlers: handlers,
	}.Build(), nil
}

// Release frees the registration admission.
func (r *registration) Release() {}

// Release frees the handler admission.
func (o *operation) Release() {}

// Release frees the tool admission.
func (e *execution) Release() {}

// Run records the tool call and returns the fixed echo text.
func (e *execution) Run(context.Context, *extensionsdk.ProgressReporter) (*extensionpb.ToolResult, error) {
	if err := e.service.trace(echoTool, e.arguments); err != nil {
		return nil, err
	}
	return extensionpb.ToolResult_builder{Contents: []*extensionpb.ToolResultContent{
		//nolint:exhaustruct_v5 // Only the active text result is populated.
		extensionpb.ToolResultContent_builder{Text: new(echoOutput)}.Build(),
	}, IsError: new(false)}.Build(), nil
}

// Run selects the action of the invoked handler after recording the invocation when tracing is enabled.
func (o *operation) Run(ctx context.Context) (*extensionpb.HandleResponse, error) {
	invocation := o.request.GetFinalizedMessage()
	if err := o.service.traceInvocation(o.request.GetHandlerId(), invocation); err != nil {
		return nil, err
	}
	switch o.request.GetHandlerId() {
	case finalizedMark:
		return o.mark(ctx, invocation.GetCurrent())
	case finalizedLater:
		return preserve(), nil
	default:
		return nil, errors.New("unknown finalized-message handler " + o.request.GetHandlerId())
	}
}

// preserve keeps the current message.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func preserve() *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		FinalizedMessage: extensionpb.FinalizedMessageAction_builder{Preserve: new(true)}.Build(),
	}.Build()
}

// replace returns a complete replacement message.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func replace(message *extensionpb.ModelContextEntry) *extensionpb.HandleResponse {
	return extensionpb.HandleResponse_builder{
		FinalizedMessage: extensionpb.FinalizedMessageAction_builder{Replace: message}.Build(),
	}.Build()
}
