package extension

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"

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

// operationResult maps cancellation to lifecycle cancellation and ordinary errors to completed tool data.
func operationResult(content string, err error) (*extensionv1.ToolResult, error) {
	if err == nil {
		return textResult(content, false), nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return textResult(err.Error(), true), nil
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

// reportProgress emits one ordered progress event.
func reportProgress(
	ctx context.Context,
	report func(context.Context, *extensionv1.ToolProgress) error,
	progress BashProgress,
) error {
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
	return report(ctx, extensionv1.ToolProgress_builder{
		Channel: new(channel), Content: new(progress.Content),
	}.Build())
}

// imageResult constructs one typed image result.
func imageResult(mediaType string, data []byte) *extensionv1.ToolResult {
	//nolint:exhaustruct_v5 // The content builder sets only the active image field.
	content := extensionv1.ToolResultContent_builder{
		Image: extensionv1.ToolResultImage_builder{MediaType: new(mediaType), Data: data}.Build(),
	}.Build()
	return extensionv1.ToolResult_builder{
		IsError: new(false), Contents: []*extensionv1.ToolResultContent{content},
	}.Build()
}

// textResult constructs one terminal text result.
func textResult(content string, isError bool) *extensionv1.ToolResult {
	//nolint:exhaustruct_v5 // The content builder sets only the active text field.
	resultContent := extensionv1.ToolResultContent_builder{Text: new(content)}.Build()
	return extensionv1.ToolResult_builder{
		IsError: new(isError), Contents: []*extensionv1.ToolResultContent{resultContent},
	}.Build()
}
