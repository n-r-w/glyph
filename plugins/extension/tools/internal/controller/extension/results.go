package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/grpc/status"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

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
	//nolint:exhaustruct_v5 // extensionv1.ExecuteResponse_builder sets only the active Progress field.
	response := extensionv1.ExecuteResponse_builder{
		Progress: extensionv1.ToolProgress_builder{
			Channel: new(channel),
			Content: new(progress.Content),
		}.Build(),
	}.Build()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("send tool progress: %w", err)
	}
	return nil
}

// sendImageResult emits one typed image result.
func sendImageResult(stream extensionv1.ExtensionService_ExecuteServer, mediaType string, data []byte) error {
	//nolint:exhaustruct_v5 // extensionv1.ExecuteResponse_builder sets only the active Result field.
	response := extensionv1.ExecuteResponse_builder{
		Result: extensionv1.ToolResult_builder{
			Contents: []*extensionv1.ToolResultContent{
				//nolint:exhaustruct_v5 // extensionv1.ToolResultContent_builder sets only the active Image field.
				extensionv1.ToolResultContent_builder{
					Image: extensionv1.ToolResultImage_builder{
						MediaType: new(mediaType),
						Data:      data,
					}.Build(),
				}.Build(),
			},
			IsError: new(false),
		}.Build(),
	}.Build()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("send terminal tool result: %w", err)
	}
	return nil
}

// sendResult emits the one terminal event required for every completed tool operation.
func sendResult(stream ResultSender, content string, isError bool) error {
	//nolint:exhaustruct_v5 // extensionv1.ExecuteResponse_builder sets only the active Result field.
	response := extensionv1.ExecuteResponse_builder{
		Result: extensionv1.ToolResult_builder{
			Contents: []*extensionv1.ToolResultContent{
				//nolint:exhaustruct_v5 // extensionv1.ToolResultContent_builder sets only the active Text field.
				extensionv1.ToolResultContent_builder{
					Text: new(content),
				}.Build(),
			},
			IsError: new(isError),
		}.Build(),
	}.Build()
	if err := stream.Send(response); err != nil {
		return fmt.Errorf("send terminal tool result: %w", err)
	}
	return nil
}
