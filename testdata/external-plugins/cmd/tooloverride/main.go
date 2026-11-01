// Package main demonstrates a tool replacement using only the public SDK.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"example.com/glyph-external-plugins/internal/tracefile"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// replacedTool is the name of the tool that the extension replaces.
	replacedTool = "read"
	// replacementDescription is the model-visible description of the replacing tool.
	replacementDescription = "Read the value of a note. This replacement of the read tool takes the note as value."
	// replacementSchema is the input schema of the replacing tool; it differs from the schema of any bundled tool.
	replacementSchema = `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],` +
		`"additionalProperties":false}`
	// observerHandler identifies the tool-policy handler that records the calls of every tool.
	observerHandler = "tool-policy"
	// toolOutputPrefix starts the result text of the replacing tool, followed by the value of the call.
	toolOutputPrefix = "replacement read ran with "
	// invalidArgument identifies rejected unknown tool calls.
	invalidArgument = "INVALID_ARGUMENT"
)

// service admits reference operations; the executable name identifies the process in the trace.
type service struct {
	// mode is the executable name, which the trace records to tell processes apart.
	mode string
	// traceLog appends the trace lines.
	traceLog *tracefile.Log
}

// Verify the reference service admits operations through the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration supplies the public declarations.
type registration struct{}

// Verify registration implements the public SDK registration contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// operation retains one admitted handler invocation.
type operation struct {
	// service supplies the trace file.
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
	extensionsdk.Serve(&service{mode: filepath.Base(os.Args[0]), traceLog: tracefile.New(traceEnvironment)})
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

// PrepareExecute admits the replacing tool.
func (s *service) PrepareExecute(
	_ context.Context,
	request *extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if request.GetToolName() != replacedTool {
		return nil, extensionsdk.Reject(invalidArgument, errors.New("unknown tool "+request.GetToolName()))
	}
	return &execution{service: s, arguments: request.GetArgumentsJson()}, nil
}

// Run declares the replacement of the read tool and one tool-policy handler.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	replacement := extensionpb.ToolDescriptor_builder{
		Name: new(replacedTool), Description: new(replacementDescription),
		InputSchemaJson:     []byte(replacementSchema),
		ConstrainedSampling: nil,
		ReplacesTool:        new(true),
	}.Build()
	handler := extensionpb.HandlerDescriptor_builder{
		Id: new(observerHandler), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_TOOL_POLICY),
	}.Build()
	return extensionpb.RegisterResponse_builder{
		Tools: []*extensionpb.ToolDescriptor{replacement}, Handlers: []*extensionpb.HandlerDescriptor{handler},
	}.Build(), nil
}

// Release frees the registration admission.
func (r *registration) Release() {}

// Release frees the handler admission.
func (o *operation) Release() {}

// Release frees the tool admission.
func (e *execution) Release() {}

// Run records the call and allows it.
//
//nolint:exhaustruct_v5 // Protobuf oneof builders set only their active action.
func (o *operation) Run(context.Context) (*extensionpb.HandleResponse, error) {
	if err := o.service.trace(handlerTrace, o.request.GetToolPolicy().GetCurrent().GetName()); err != nil {
		return nil, err
	}
	return extensionpb.HandleResponse_builder{
		ToolPolicy: extensionpb.ToolPolicyAction_builder{Allow: new(true)}.Build(),
	}.Build(), nil
}

// Run records the call and returns a text with the value of the call.
func (e *execution) Run(context.Context, *extensionsdk.ProgressReporter) (*extensionpb.ToolResult, error) {
	value, err := callValue(e.arguments)
	if err != nil {
		return nil, err
	}
	if traceErr := e.service.trace(toolTrace, replacedTool); traceErr != nil {
		return nil, traceErr
	}
	return extensionpb.ToolResult_builder{Contents: []*extensionpb.ToolResultContent{
		//nolint:exhaustruct_v5 // Only the active text result is populated.
		extensionpb.ToolResultContent_builder{Text: new(toolOutputPrefix + value)}.Build(),
	}, IsError: new(false)}.Build(), nil
}
