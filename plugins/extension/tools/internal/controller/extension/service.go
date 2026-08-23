// Package extension maps the public extension contract to standard tool use cases.
//
//nolint:exhaustruct // Protobuf oneof builders intentionally set only the active field.
package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/grpc/status"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	standardToolCount = 3
	readToolName      = "read"
	editToolName      = "edit"
	bashToolName      = "bash"

	readInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the text file to read."}},` +
		`"required":["path"],"additionalProperties":false}`
	editInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the text file to edit."},` +
		`"oldText":{"type":"string","description":"Exact source text to replace."},` +
		`"newText":{"type":"string","description":"Replacement text."}},` +
		`"required":["path","oldText","newText"],"additionalProperties":false}`
	bashInputSchemaJSON = `{"type":"object","properties":` +
		`{"command":{"type":"string","description":"Bash command to execute."}},` +
		`"required":["command"],"additionalProperties":false}`
)

// Service exposes standard tools through Extension Contract v1.
type Service struct {
	extensionv1.UnimplementedExtensionServiceServer
	readTool ReadTool
	editTool EditTool
	bashTool BashTool
	schemas  map[string]*jsonschema.Schema
}

var _ extensionv1.ExtensionServiceServer = (*Service)(nil)

// readArguments is the transport-local read input.
type readArguments struct {
	Path string `json:"path"`
}

// editArguments is the transport-local edit input.
type editArguments struct {
	Path    string `json:"path"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// bashArguments is the transport-local bash input.
type bashArguments struct {
	Command string `json:"command"`
}

// bashResult is the model-visible terminal command result.
type bashResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

// New creates an extension controller for the standard tools.
func New(readTool ReadTool, editTool EditTool, bashTool BashTool) (*Service, error) {
	schemas := make(map[string]*jsonschema.Schema, standardToolCount)
	for name, source := range map[string]string{
		readToolName: readInputSchemaJSON,
		editToolName: editInputSchemaJSON,
		bashToolName: bashInputSchemaJSON,
	} {
		schema, err := compileSchema(name, source)
		if err != nil {
			return nil, err
		}
		schemas[name] = schema
	}
	return &Service{
		UnimplementedExtensionServiceServer: extensionv1.UnimplementedExtensionServiceServer{},
		readTool:                            readTool,
		editTool:                            editTool,
		bashTool:                            bashTool,
		schemas:                             schemas,
	}, nil
}

// ListTools returns the complete standard tool catalog.
func (s *Service) ListTools(
	_ context.Context,
	_ *extensionv1.ListToolsRequest,
) (*extensionv1.ListToolsResponse, error) {
	return extensionv1.ListToolsResponse_builder{Tools: []*extensionv1.ToolDescriptor{
		extensionv1.ToolDescriptor_builder{
			Name: new(readToolName), Description: new("Read the complete contents of a text file in the working project."),
			InputSchemaJson: []byte(readInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(editToolName), Description: new("Replace one uniquely occurring text fragment in a project file."),
			InputSchemaJson: []byte(editInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(bashToolName), Description: new("Execute one bash command in the working project."),
			InputSchemaJson: []byte(bashInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
	}}.Build(), nil
}

// strictPreferSampling requests strict schema generation while permitting provider fallback.
func strictPreferSampling() *extensionv1.ConstrainedSampling {
	return extensionv1.ConstrainedSampling_builder{
		JsonSchema: extensionv1.JsonSchemaConstrainedSampling_builder{
			Strictness: new(extensionv1.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER),
		}.Build(),
	}.Build()
}

// Execute validates and executes one standard tool call.
func (s *Service) Execute(
	request *extensionv1.ExecuteRequest,
	stream extensionv1.ExtensionService_ExecuteServer,
) error {
	schema, exists := s.schemas[request.GetToolName()]
	if !exists {
		return sendResult(stream, fmt.Sprintf("unknown tool %q", request.GetToolName()), true)
	}
	arguments, err := validateArguments(schema, request.GetArgumentsJson())
	if err != nil {
		return sendResult(stream, fmt.Sprintf("invalid %s arguments: %v", request.GetToolName(), err), true)
	}

	switch request.GetToolName() {
	case readToolName:
		return s.executeRead(arguments, stream)
	case editToolName:
		return s.executeEdit(arguments, stream)
	case bashToolName:
		return s.executeBash(arguments, stream)
	default:
		return sendResult(stream, fmt.Sprintf("unknown tool %q", request.GetToolName()), true)
	}
}

// executeRead decodes and executes read.
func (s *Service) executeRead(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input readArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode read arguments: %v", err), true)
	}
	content, err := s.readTool.Read(stream.Context(), input.Path)
	return operationResult(stream, content, err)
}

// executeEdit decodes and executes edit.
func (s *Service) executeEdit(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input editArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode edit arguments: %v", err), true)
	}
	if err := s.editTool.Edit(stream.Context(), input.Path, input.OldText, input.NewText); err != nil {
		return operationResult(stream, "", err)
	}
	return sendResult(stream, "replaced text in "+input.Path, false)
}

// executeBash decodes and executes bash while mapping progress channels.
func (s *Service) executeBash(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input bashArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode bash arguments: %v", err), true)
	}
	result, err := s.bashTool.Execute(stream.Context(), input.Command, func(progress BashProgress) error {
		return sendProgress(stream, progress)
	})
	if err != nil {
		return operationResult(stream, "", err)
	}
	content, err := json.Marshal(bashResult(result))
	if err != nil {
		return fmt.Errorf("encode bash result: %w", err)
	}
	return sendResult(stream, string(content), result.ExitCode != 0)
}

// validateArguments applies the compiled schema before typed decoding.
func validateArguments(schema *jsonschema.Schema, source []byte) ([]byte, error) {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	validationErr := schema.Validate(value)
	if validationErr != nil {
		return nil, validationErr
	}
	return source, nil
}

// operationResult maps cancellation to gRPC and operation failures to terminal results.
func operationResult(stream extensionv1.ExtensionService_ExecuteServer, content string, err error) error {
	if err == nil {
		return sendResult(stream, content, false)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return sendResult(stream, err.Error(), true)
}

// compileSchema compiles one immutable tool schema.
func compileSchema(name, source string) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewBufferString(source))
	if err != nil {
		return nil, fmt.Errorf("parse %s input schema: %w", name, err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	location := "glyph://tools/" + name + "/input-schema.json"
	registerErr := compiler.AddResource(location, document)
	if registerErr != nil {
		return nil, fmt.Errorf("register %s input schema: %w", name, registerErr)
	}
	schema, err := compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("compile %s input schema: %w", name, err)
	}
	return schema, nil
}

// sendProgress emits one ordered progress event.
func sendProgress(stream extensionv1.ExtensionService_ExecuteServer, progress BashProgress) error {
	var channel extensionv1.ProgressChannel
	switch progress.Channel {
	case BashProgressStatus:
		channel = extensionv1.ProgressChannel_PROGRESS_CHANNEL_STATUS
	case BashProgressStdout:
		channel = extensionv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT
	case BashProgressStderr:
		channel = extensionv1.ProgressChannel_PROGRESS_CHANNEL_STDERR
	default:
		return fmt.Errorf("unknown bash progress channel %d", progress.Channel)
	}
	response := extensionv1.ExecuteResponse_builder{
		Progress: extensionv1.ToolProgress_builder{Channel: new(channel), Content: new(progress.Content)}.Build(),
	}.Build()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("send tool progress: %w", err)
	}
	return nil
}

// sendResult emits the one terminal event required for every completed tool operation.
func sendResult(stream extensionv1.ExtensionService_ExecuteServer, content string, isError bool) error {
	response := extensionv1.ExecuteResponse_builder{
		Result: extensionv1.ToolResult_builder{
			Contents: []*extensionv1.ToolResultContent{
				extensionv1.ToolResultContent_builder{Text: new(content)}.Build(),
			},
			IsError: new(isError),
		}.Build(),
	}.Build()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("send terminal tool result: %w", err)
	}
	return nil
}
