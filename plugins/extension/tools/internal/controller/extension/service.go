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
	"math"
	"os"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/grpc/status"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	standardToolCount = 7
	readToolName      = "read"
	writeToolName     = "write"
	editToolName      = "edit"
	grepToolName      = "grep"
	findToolName      = "find"
	listToolName      = "ls"
	bashToolName      = "bash"

	readInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the file to read."},` +
		`"offset":{"type":"integer","minimum":1,"description":"One-based line offset."},` +
		`"limit":{"type":"integer","minimum":1,"description":"Maximum number of lines."}},` +
		`"required":["path"],"additionalProperties":false}`
	writeInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the file to write."},` +
		`"content":{"type":"string","description":"Complete file content."}},` +
		`"required":["path","content"],"additionalProperties":false}`
	editInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the file to edit."},` +
		`"edits":{"type":"array","minItems":1,"items":{"type":"object","properties":` +
		`{"oldText":{"type":"string","minLength":1},"newText":{"type":"string"}},` +
		`"required":["oldText","newText"],"additionalProperties":false}}},` +
		`"required":["path","edits"],"additionalProperties":false}`
	grepInputSchemaJSON = `{"type":"object","properties":` +
		`{"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},` +
		`"ignoreCase":{"type":"boolean"},"literal":{"type":"boolean"},"context":{"type":"integer","minimum":0},` +
		`"limit":{"type":"integer","minimum":1}},"required":["pattern"],"additionalProperties":false}`
	findInputSchemaJSON = `{"type":"object","properties":` +
		`{"pattern":{"type":"string"},"path":{"type":"string"},"limit":{"type":"integer","minimum":1}},` +
		`"required":["pattern"],"additionalProperties":false}`
	listInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string"},"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`
	bashInputSchemaJSON = `{"type":"object","properties":` +
		`{"command":{"type":"string","description":"Bash command to execute."},` +
		`"timeout":{"type":"number","exclusiveMinimum":0,"description":"Timeout in seconds; no default timeout."}},` +
		`"required":["command"],"additionalProperties":false}`
)

// Service exposes standard tools through Extension Contract v1.
type Service struct {
	extensionv1.UnimplementedExtensionServiceServer
	readTool   ReadTool
	writeTool  WriteTool
	editTool   EditTool
	bashTool   BashTool
	searchTool SearchTool
	schemas    map[string]*jsonschema.Schema
}

var _ extensionv1.ExtensionServiceServer = (*Service)(nil)

// readArguments is the transport-local read input.
type readArguments struct {
	Path   string `json:"path"`
	Offset uint   `json:"offset"`
	Limit  uint   `json:"limit"`
}

// writeArguments is the transport-local write input.
type writeArguments struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// editArguments is the transport-local edit input.
type editArguments struct {
	Path  string        `json:"path"`
	Edits []Replacement `json:"edits"`
}

// bashArguments is the transport-local bash input.
type bashArguments struct {
	Command string   `json:"command"`
	Timeout *float64 `json:"timeout"`
}

// bashTimeoutError distinguishes a tool timeout from caller cancellation.
type bashTimeoutError struct{ seconds float64 }

// Error returns the model-visible timeout outcome.
func (e bashTimeoutError) Error() string {
	return fmt.Sprintf("bash command timed out after %g seconds", e.seconds)
}

// grepArguments is the transport-local grep input.
type grepArguments struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Glob       string `json:"glob"`
	IgnoreCase bool   `json:"ignoreCase"`
	Literal    bool   `json:"literal"`
	Context    uint   `json:"context"`
	Limit      uint   `json:"limit"`
}

// findArguments is the transport-local find input.
type findArguments struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Limit   uint   `json:"limit"`
}

// listArguments is the transport-local ls input.
type listArguments struct {
	Path  string `json:"path"`
	Limit uint   `json:"limit"`
}

// New creates an extension controller for the standard tools.
func New(
	readTool ReadTool,
	writeTool WriteTool,
	editTool EditTool,
	bashTool BashTool,
	searchTool SearchTool,
) (*Service, error) {
	schemas := make(map[string]*jsonschema.Schema, standardToolCount)
	for name, source := range map[string]string{
		readToolName:  readInputSchemaJSON,
		writeToolName: writeInputSchemaJSON,
		editToolName:  editInputSchemaJSON,
		grepToolName:  grepInputSchemaJSON,
		findToolName:  findInputSchemaJSON,
		listToolName:  listInputSchemaJSON,
		bashToolName:  bashInputSchemaJSON,
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
		writeTool:                           writeTool,
		editTool:                            editTool,
		bashTool:                            bashTool,
		searchTool:                          searchTool,
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
			Name:            new(readToolName),
			Description:     new("Read bounded text or supported image contents from a file in the working project."),
			InputSchemaJson: []byte(readInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(writeToolName), Description: new("Create or replace a file in the working project."),
			InputSchemaJson: []byte(writeInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(editToolName), Description: new("Apply ordered unique exact text replacements to a project file."),
			InputSchemaJson: []byte(editInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(grepToolName), Description: new("Search project files for matching lines."),
			InputSchemaJson: []byte(grepInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(findToolName), Description: new("Find project paths matching a glob."),
			InputSchemaJson: []byte(findInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name: new(listToolName), Description: new("List direct project directory entries."),
			InputSchemaJson: []byte(listInputSchemaJSON), ConstrainedSampling: strictPreferSampling(),
		}.Build(),
		extensionv1.ToolDescriptor_builder{
			Name:            new(bashToolName),
			Description:     new("Execute a bash command with optional timeout and bounded combined output."),
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
	case writeToolName:
		return s.executeWrite(arguments, stream)
	case editToolName:
		return s.executeEdit(arguments, stream)
	case grepToolName:
		return s.executeGrep(arguments, stream)
	case findToolName:
		return s.executeFind(arguments, stream)
	case listToolName:
		return s.executeList(arguments, stream)
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
	result, err := s.readTool.Read(stream.Context(), input.Path, input.Offset, input.Limit)
	if err != nil {
		return operationResult(stream, "", err)
	}
	if result.Image != nil {
		return sendImageResult(stream, result.Image.MediaType, result.Image.Data)
	}
	return operationResult(stream, result.Text, nil)
}

// executeWrite decodes and executes write.
func (s *Service) executeWrite(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input writeArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode write arguments: %v", err), true)
	}
	if err := s.writeTool.Write(stream.Context(), input.Path, input.Content); err != nil {
		return operationResult(stream, "", err)
	}
	return sendResult(stream, "wrote file "+input.Path, false)
}

// executeEdit decodes and executes edit.
func (s *Service) executeEdit(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input editArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode edit arguments: %v", err), true)
	}
	if err := s.editTool.Edit(stream.Context(), input.Path, input.Edits); err != nil {
		return operationResult(stream, "", err)
	}
	return sendResult(stream, "replaced text in "+input.Path, false)
}

// executeGrep decodes and executes grep.
func (s *Service) executeGrep(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input grepArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode grep arguments: %v", err), true)
	}
	command := GrepArguments(input)
	result, err := s.searchTool.Grep(stream.Context(), command)
	return operationResult(stream, result, err)
}

// executeFind decodes and executes find.
func (s *Service) executeFind(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input findArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode find arguments: %v", err), true)
	}
	command := FindArguments(input)
	result, err := s.searchTool.Find(stream.Context(), command)
	return operationResult(stream, result, err)
}

// executeList decodes and executes ls.
func (s *Service) executeList(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input listArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode ls arguments: %v", err), true)
	}
	result, err := s.searchTool.List(stream.Context(), ListArguments(input))
	return operationResult(stream, result, err)
}

// executeBash decodes and executes bash while mapping progress channels.
func (s *Service) executeBash(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input bashArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode bash arguments: %v", err), true)
	}
	executionContext, stopTimeout, err := bashExecutionContext(stream.Context(), input.Timeout)
	if err != nil {
		return sendResult(stream, err.Error(), true)
	}
	defer stopTimeout()
	result, err := s.bashTool.Execute(executionContext, input.Command, func(progress BashProgress) error {
		return sendProgress(stream, progress)
	})
	if err != nil {
		if result.Text != "" && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return sendBashResult(stream, result, true)
		}
		return operationResult(stream, "", err)
	}
	return sendBashResult(stream, result, result.ExitCode != 0)
}

// sendBashResult retains complete output only when its path reaches the caller.
func sendBashResult(sender ResultSender, result BashResult, isError bool) error {
	if err := sendResult(sender, result.Text, isError); err != nil {
		return errors.Join(err, removeBashOutput(result.Truncation.FullOutputPath))
	}
	return nil
}

// removeBashOutput removes an undelivered complete-output file.
func removeBashOutput(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove undelivered bash output: %w", err)
	}
	return nil
}

// bashExecutionContext cancels one command with a timeout-specific cause.
func bashExecutionContext(parent context.Context, seconds *float64) (context.Context, func(), error) {
	if seconds == nil {
		return parent, func() {}, nil
	}
	if *seconds <= 0 {
		return nil, nil, errors.New("bash timeout must be positive")
	}
	maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
	if *seconds > maximumSeconds {
		return nil, nil, errors.New("bash timeout exceeds supported duration")
	}
	duration := time.Duration(*seconds * float64(time.Second))
	if duration == 0 {
		duration = time.Nanosecond
	}
	ctx, cancel := context.WithCancelCause(parent)
	timer := time.AfterFunc(duration, func() {
		cancel(bashTimeoutError{seconds: *seconds})
	})
	stop := func() {
		timer.Stop()
		cancel(nil)
	}
	return ctx, stop, nil
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

// sendImageResult emits one typed image result.
func sendImageResult(stream extensionv1.ExtensionService_ExecuteServer, mediaType string, data []byte) error {
	response := extensionv1.ExecuteResponse_builder{Result: extensionv1.ToolResult_builder{
		Contents: []*extensionv1.ToolResultContent{extensionv1.ToolResultContent_builder{
			Image: extensionv1.ToolResultImage_builder{MediaType: new(mediaType), Data: data}.Build(),
		}.Build()},
		IsError: new(false),
	}.Build()}.Build()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("send terminal tool result: %w", err)
	}
	return nil
}

// sendResult emits the one terminal event required for every completed tool operation.
func sendResult(stream ResultSender, content string, isError bool) error {
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
