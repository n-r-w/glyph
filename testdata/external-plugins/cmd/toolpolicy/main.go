// Package main demonstrates tool-policy middleware using only the public SDK.
package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// policyHandler identifies the tool-policy handler of a policy executable.
	policyHandler = "tool-policy"
	// policyTool names the tool whose calls the reference checks and runs.
	policyTool = "policy_echo"
	// toolBehavior is the behavior of the executable that owns policyTool and registers no handler.
	toolBehavior = "tool"
	// replaceBehavior replaces the input of every call with replacementArguments.
	replaceBehavior = "replace"
	// allowBehavior allows every call.
	allowBehavior = "allow"
	// rejectBehavior rejects a call whose value is forbiddenValue and allows every other call.
	rejectBehavior = "reject"
	// failBehavior returns the ordinary error failureText for every call.
	failBehavior = "fail"
	// invalidBehavior replaces the input of every call with invalidArguments, which violate the tool input schema.
	invalidBehavior = "invalid"
	// emptyBehavior returns an empty action for every call.
	emptyBehavior = "empty"
	// replacementArguments is the input that replaceBehavior sets.
	replacementArguments = `{"value":"replaced"}`
	// invalidArguments violate the input schema of policyTool.
	invalidArguments = `{"value":1}`
	// forbiddenValue is the value of a call that rejectBehavior rejects.
	forbiddenValue = "forbidden"
	// rejectReason is the complete reason of a rejection.
	rejectReason = "tool policy forbids the value forbidden; use another value"
	// failureText is the complete ordinary error of failBehavior.
	failureText = "reference tool policy failed"
	// toolOutputPrefix starts the result text of policyTool, followed by the value of the call.
	toolOutputPrefix = "policy tool ran with "
	// invalidArgument identifies rejected unknown tool calls.
	invalidArgument = "INVALID_ARGUMENT"
)

// service admits reference operations; the executable name selects the behavior.
type service struct {
	// mode is the executable name, which the trace records to tell processes apart.
	mode string
	// behavior selects the policy through the last dash-separated part of the executable name.
	behavior string
	// traceMutex serializes trace file appends.
	traceMutex sync.Mutex
}

// Verify the reference service admits operations through the public SDK contract.
var _ extensionsdk.Service = (*service)(nil)

// registration supplies the public declarations.
type registration struct {
	// behavior selects whether the extension owns the tool or registers the handler.
	behavior string
}

// Verify registration implements the public SDK registration contract.
var _ extensionsdk.RegisterOperation = (*registration)(nil)

// operation retains one admitted handler invocation.
type operation struct {
	// service supplies the behavior and the trace file.
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
	mode := filepath.Base(os.Args[0])
	extensionsdk.Serve(&service{
		mode: mode, behavior: mode[strings.LastIndex(mode, "-")+1:], traceMutex: sync.Mutex{},
	})
}

// PrepareRegister admits the fixed reference catalog.
func (s *service) PrepareRegister(
	context.Context,
	*extensionpb.RegisterRequest,
) (extensionsdk.RegisterOperation, error) {
	return &registration{behavior: s.behavior}, nil
}

// PrepareHandle admits an invocation after SDK identity and payload validation.
func (s *service) PrepareHandle(
	_ context.Context,
	request *extensionpb.HandleRequest,
) (extensionsdk.HandleOperation, error) {
	return &operation{service: s, request: request}, nil
}

// PrepareExecute admits the policy tool.
func (s *service) PrepareExecute(
	_ context.Context,
	request *extensionpb.ExecuteRequest,
) (extensionsdk.ExecuteOperation, error) {
	if s.behavior != toolBehavior || request.GetToolName() != policyTool {
		return nil, extensionsdk.Reject(invalidArgument, errors.New("unknown tool "+request.GetToolName()))
	}
	return &execution{service: s, arguments: request.GetArgumentsJson()}, nil
}

// Run returns the tool for the tool executable and the tool-policy handler for every policy executable.
func (r *registration) Run(context.Context) (*extensionpb.RegisterResponse, error) {
	if r.behavior != toolBehavior {
		handler := extensionpb.HandlerDescriptor_builder{
			Id: new(policyHandler), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_TOOL_POLICY),
		}.Build()
		return extensionpb.RegisterResponse_builder{
			Tools: nil, Handlers: []*extensionpb.HandlerDescriptor{handler},
		}.Build(), nil
	}
	tool := extensionpb.ToolDescriptor_builder{
		Name: new(policyTool), Description: new("Return the value of the call."),
		InputSchemaJson: []byte(
			`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`,
		),
		ConstrainedSampling: nil,
	}.Build()
	return extensionpb.RegisterResponse_builder{
		Tools: []*extensionpb.ToolDescriptor{tool}, Handlers: nil,
	}.Build(), nil
}

// Release frees the registration admission.
func (r *registration) Release() {}

// Release frees the handler admission.
func (o *operation) Release() {}

// Release frees the tool admission.
func (e *execution) Release() {}

// Run records the tool call and returns a text with the value of the call.
func (e *execution) Run(context.Context, *extensionsdk.ProgressReporter) (*extensionpb.ToolResult, error) {
	if err := e.service.trace(toolBehavior, e.arguments); err != nil {
		return nil, err
	}
	value, err := callValue(e.arguments)
	if err != nil {
		return nil, err
	}
	return extensionpb.ToolResult_builder{Contents: []*extensionpb.ToolResultContent{
		//nolint:exhaustruct_v5 // Only the active text result is populated.
		extensionpb.ToolResultContent_builder{Text: new(toolOutputPrefix + value)}.Build(),
	}, IsError: new(false)}.Build(), nil
}
