// Package runtime connects Glyph Host to one Extension Contract v1 process.
package runtime

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Runtime owns one extension process connection.
type Runtime struct {
	// client owns the extension process connection.
	client *extensionsdk.Client

	// catalogMutex protects schemas during catalog access.
	catalogMutex sync.RWMutex
	// schemas contains compiled input schemas by tool name.
	schemas map[string]*jsonschema.Schema
}

var _ extensionservice.ExtensionRuntime = (*Runtime)(nil)

// Start connects to one extension process command.
func Start(ctx context.Context, command *exec.Cmd) (*Runtime, error) {
	client, err := extensionsdk.Connect(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("start extension runtime: %w", err)
	}
	return &Runtime{
		client:       client,
		catalogMutex: sync.RWMutex{},
		schemas:      make(map[string]*jsonschema.Schema),
	}, nil
}

// Register validates and caches the complete extension registration.
func (r *Runtime) Register(ctx context.Context) (extensionservice.Registration, error) {
	response, err := r.client.Service().Register(ctx, &extensionpb.RegisterRequest{})
	if err != nil {
		r.Close()
		return extensionservice.Registration{}, fmt.Errorf("register extension: %w", err)
	}

	registration, schemas, err := validateRegistration(response)
	if err != nil {
		r.Close()
		return extensionservice.Registration{}, fmt.Errorf("validate extension registration: %w", err)
	}

	// The validated registration and compiled schemas remain fixed for this process lifetime.
	r.catalogMutex.Lock()
	r.schemas = schemas
	r.catalogMutex.Unlock()
	return registration, nil
}

// Execute validates arguments and consumes one finite extension execution stream.
func (r *Runtime) Execute(
	ctx context.Context,
	toolName string,
	argumentsJSON []byte,
	handleProgress tool.ProgressHandler,
) (tool.Result, error) {
	if err := ctx.Err(); err != nil {
		return tool.Result{}, fmt.Errorf("execute extension tool %q: %w", toolName, err)
	}

	schema, ok := r.toolSchema(toolName)
	if !ok {
		return tool.Result{Contents: tool.TextContents(fmt.Sprintf("tool %q is unavailable", toolName)), IsError: true}, nil
	}
	if validationErr := validateArguments(schema, argumentsJSON); validationErr != nil {
		return tool.Result{
			Contents: tool.TextContents(fmt.Sprintf("invalid arguments for tool %q: %v", toolName, validationErr)),
			IsError:  true,
		}, nil
	}

	// A child context lets progress-delivery failure stop the active RPC without changing runtime availability.
	executionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := r.client.Service().Execute(executionContext, extensionpb.ExecuteRequest_builder{
		ToolName:      new(toolName),
		ArgumentsJson: argumentsJSON,
	}.Build())
	if err != nil {
		return tool.Result{}, r.executionError(ctx, toolName, err)
	}

	return r.consumeStream(ctx, toolName, stream, handleProgress, cancel)
}

// consumeStream enforces progress ordering and exactly one terminal result before clean completion.
func (r *Runtime) consumeStream(
	ctx context.Context,
	toolName string,
	stream extensionpb.ExtensionService_ExecuteClient,
	handleProgress tool.ProgressHandler,
	cancel context.CancelFunc,
) (tool.Result, error) {
	var terminalResult *tool.Result
	for {
		event, receiveErr := stream.Recv()
		if errors.Is(receiveErr, io.EOF) {
			if terminalResult == nil {
				return tool.Result{}, r.protocolViolation("stream completed without a terminal result")
			}
			return *terminalResult, nil
		}
		if receiveErr != nil {
			return tool.Result{}, r.executionError(ctx, toolName, receiveErr)
		}

		switch event.WhichContent() {
		case extensionpb.ExecuteResponse_Progress_case:
			if terminalResult != nil {
				return tool.Result{}, r.protocolViolation("event received after terminal result")
			}
			progress, progressErr := mapProgress(event.GetProgress())
			if progressErr != nil {
				return tool.Result{}, r.protocolViolation(progressErr.Error())
			}
			deliveryErr := handleProgress(progress)
			if deliveryErr != nil {
				cancel()
				return tool.Result{}, fmt.Errorf("deliver extension progress: %w", deliveryErr)
			}
		case extensionpb.ExecuteResponse_Result_case:
			if terminalResult != nil {
				return tool.Result{}, r.protocolViolation("second terminal result received")
			}
			if event.GetResult() == nil {
				return tool.Result{}, r.protocolViolation("terminal result payload is missing")
			}
			contents, mapErr := mapResultContents(event.GetResult().GetContents())
			if mapErr != nil {
				return tool.Result{}, r.protocolViolation(mapErr.Error())
			}
			terminalResult = &tool.Result{Contents: contents, IsError: event.GetResult().GetIsError()}
		case extensionpb.ExecuteResponse_Content_not_set_case:
			return tool.Result{}, r.protocolViolation("event contains neither progress nor result")
		default:
			return tool.Result{}, r.protocolViolation("event content is invalid")
		}
	}
}

// Done closes when the extension process terminates.
func (r *Runtime) Done() <-chan struct{} { return r.client.Done() }

// Close stops the extension process and waits for cleanup.
func (r *Runtime) Close() {
	r.client.Close()
}

// toolSchema returns the compiled schema cached during complete-catalog validation.
func (r *Runtime) toolSchema(toolName string) (*jsonschema.Schema, bool) {
	r.catalogMutex.RLock()
	defer r.catalogMutex.RUnlock()
	schema, ok := r.schemas[toolName]
	return schema, ok
}

// executionError preserves cancellation while treating other stream failures as process unavailability.
func (r *Runtime) executionError(ctx context.Context, toolName string, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("execute extension tool %q: %w", toolName, ctxErr)
	}
	if status.Code(err) == codes.Canceled {
		return fmt.Errorf("execute extension tool %q: %w", toolName, context.Canceled)
	}
	r.Close()
	return fmt.Errorf(
		"%w: execute extension tool %q: %w",
		extensionservice.ErrExtensionUnavailable,
		toolName,
		err,
	)
}

// protocolViolation stops the extension because later calls cannot trust its stream behavior.
func (r *Runtime) protocolViolation(reason string) error {
	r.Close()
	return fmt.Errorf(
		"%w: extension protocol violation: %s",
		extensionservice.ErrExtensionUnavailable,
		reason,
	)
}

// validateRegistration validates tool transport data and maps ordered handler descriptors.
func validateRegistration(
	response *extensionpb.RegisterResponse,
) (extensionservice.Registration, map[string]*jsonschema.Schema, error) {
	if response == nil {
		return extensionservice.Registration{}, nil, errors.New("registration response is missing")
	}

	tools := make([]tool.Descriptor, 0, len(response.GetTools()))
	schemas := make(map[string]*jsonschema.Schema, len(response.GetTools()))
	for index, descriptor := range response.GetTools() {
		if descriptor == nil {
			return extensionservice.Registration{}, nil, fmt.Errorf("descriptor %d is missing", index)
		}
		name := descriptor.GetName()
		if name == "" {
			return extensionservice.Registration{}, nil, fmt.Errorf("descriptor %d has an empty name", index)
		}
		if descriptor.GetDescription() == "" {
			return extensionservice.Registration{}, nil, fmt.Errorf("tool %q has an empty description", name)
		}
		if _, duplicate := schemas[name]; duplicate {
			return extensionservice.Registration{}, nil, fmt.Errorf("tool name %q is duplicated", name)
		}

		schema, err := compileToolSchema(descriptor.GetInputSchemaJson())
		if err != nil {
			return extensionservice.Registration{}, nil, fmt.Errorf("tool %q input schema: %w", name, err)
		}
		constraint, err := mapConstrainedSampling(descriptor, descriptor.GetInputSchemaJson())
		if err != nil {
			return extensionservice.Registration{}, nil, fmt.Errorf("tool %q constrained sampling: %w", name, err)
		}
		tools = append(tools, tool.Descriptor{
			Name: name, Description: descriptor.GetDescription(),
			InputSchemaJSON: bytes.Clone(descriptor.GetInputSchemaJson()), ConstrainedSampling: constraint,
		})
		schemas[name] = schema
	}

	handlers := make([]extensionservice.HandlerDescriptor, 0, len(response.GetHandlers()))
	for _, handler := range response.GetHandlers() {
		if handler == nil {
			handlers = append(handlers, extensionservice.HandlerDescriptor{})
			continue
		}
		handlers = append(handlers, extensionservice.HandlerDescriptor{
			ID:   handler.GetId(),
			Kind: mapHandlerKind(handler.GetKind()),
		})
	}
	return extensionservice.Registration{Tools: tools, Handlers: handlers}, schemas, nil
}

// mapHandlerKind maps known public kinds and leaves other values invalid for Host validation.
func mapHandlerKind(kind extensionpb.HandlerKind) extensionservice.HandlerKind {
	switch kind {
	case extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST:
		return extensionservice.HandlerKindSessionBeforeTreeRequest
	case extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT:
		return extensionservice.HandlerKindSessionBeforeTreeResult
	case extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE:
		return extensionservice.HandlerKindSessionTree
	case extensionpb.HandlerKind_HANDLER_KIND_UNSPECIFIED:
		return 0
	default:
		return 0
	}
}

// mapConstrainedSampling validates the public constraint and preserves its presence.
func mapConstrainedSampling(
	descriptor *extensionpb.ToolDescriptor,
	schemaJSON []byte,
) (mo.Option[tool.ConstrainedSampling], error) {
	constraint := descriptor.GetConstrainedSampling()
	if constraint == nil {
		return mo.None[tool.ConstrainedSampling](), nil
	}
	var mapped tool.ConstrainedSampling
	var err error
	switch constraint.WhichConfig() {
	case extensionpb.ConstrainedSampling_JsonSchema_case:
		mapped, err = mapJSONSchemaSampling(constraint.GetJsonSchema())
	case extensionpb.ConstrainedSampling_Grammar_case:
		mapped, err = mapGrammarSampling(constraint.GetGrammar(), schemaJSON)
	case extensionpb.ConstrainedSampling_Config_not_set_case:
		err = errors.New("config is missing")
	default:
		err = errors.New("config is invalid")
	}
	if err != nil {
		return mo.None[tool.ConstrainedSampling](), err
	}
	return mo.Some(mapped), nil
}

// mapJSONSchemaSampling converts the closed public strictness enum.
func mapJSONSchemaSampling(
	config *extensionpb.JsonSchemaConstrainedSampling,
) (tool.ConstrainedSampling, error) {
	if config == nil {
		return tool.ConstrainedSampling{}, errors.New("JSON Schema config is missing")
	}
	var strictness tool.JSONSchemaStrictness
	switch config.GetStrictness() {
	case extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_PREFER:
		strictness = tool.JSONSchemaStrictPrefer
	case extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_REQUIRE:
		strictness = tool.JSONSchemaStrictRequire
	case extensionpb.JsonSchemaStrictness_JSON_SCHEMA_STRICTNESS_UNSPECIFIED:
		return tool.ConstrainedSampling{}, errors.New("JSON Schema strictness is unspecified")
	default:
		return tool.ConstrainedSampling{}, errors.New("JSON Schema strictness is invalid")
	}
	return tool.ConstrainedSampling{
		Kind:                 tool.ConstrainedSamplingJSONSchema,
		JSONSchemaStrictness: mo.Some(strictness),
		Grammar:              mo.None[tool.GrammarVariants](),
		GrammarInputProperty: mo.None[string](),
	}, nil
}

// mapGrammarSampling validates grammar variants and retains the schema input property.
func mapGrammarSampling(
	config *extensionpb.GrammarConstrainedSampling,
	schemaJSON []byte,
) (tool.ConstrainedSampling, error) {
	if config == nil {
		return tool.ConstrainedSampling{}, errors.New("grammar config is missing")
	}
	lark := mo.None[string]()
	if config.HasLark() {
		lark = mo.Some(config.GetLark())
	}
	regex := mo.None[string]()
	if config.HasRegex() {
		regex = mo.Some(config.GetRegex())
	}
	larkValue, hasLark := lark.Get()
	regexValue, hasRegex := regex.Get()
	if (!hasLark || strings.TrimSpace(larkValue) == "") && (!hasRegex || strings.TrimSpace(regexValue) == "") {
		return tool.ConstrainedSampling{}, errors.New("grammar requires at least one nonempty grammar variant")
	}
	var schema struct {
		Properties map[string]jsontext.Value `json:"properties"`
		Required   []string                  `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return tool.ConstrainedSampling{}, fmt.Errorf("parse grammar schema: %w", err)
	}
	if len(schema.Properties) != 1 || len(schema.Required) != 1 || schema.Required[0] == "" {
		return tool.ConstrainedSampling{}, errors.New("grammar schema must have exactly one required string property")
	}
	if err := validateGrammarInputProperty(schema.Properties, schema.Required[0]); err != nil {
		return tool.ConstrainedSampling{}, err
	}
	return tool.ConstrainedSampling{
		Kind:                 tool.ConstrainedSamplingGrammar,
		JSONSchemaStrictness: mo.None[tool.JSONSchemaStrictness](),
		Grammar:              mo.Some(tool.GrammarVariants{Lark: lark, Regex: regex}),
		GrammarInputProperty: mo.Some(schema.Required[0]),
	}, nil
}

// validateGrammarInputProperty enforces the direct single-string input contract.
func validateGrammarInputProperty(properties map[string]jsontext.Value, required string) error {
	const rule = "grammar schema must have exactly one required string property"

	propertyJSON, exists := properties[required]
	if !exists {
		return errors.New(rule)
	}
	var property struct {
		Type jsontext.Value `json:"type"`
	}
	if err := json.Unmarshal(propertyJSON, &property); err != nil {
		// Keep parser diagnostics with the grammar rule that the property violates.
		return fmt.Errorf("%s: parse property JSON: %w", rule, err)
	}
	if len(property.Type) == 0 {
		return errors.New(rule)
	}
	var propertyType string
	if err := json.Unmarshal(property.Type, &propertyType); err != nil {
		// Keep parser diagnostics with the grammar rule that the property type violates.
		return fmt.Errorf("%s: parse property type JSON: %w", rule, err)
	}
	if propertyType != "string" {
		return errors.New(rule)
	}
	return nil
}

// compileToolSchema compiles a Draft 2020-12 object schema for tool arguments.
func compileToolSchema(schemaJSON []byte) (*jsonschema.Schema, error) {
	const schemaLocation = "glyph://extension/input-schema.json"

	var root struct {
		Type jsontext.Value `json:"type"`
	}
	if err := json.Unmarshal(schemaJSON, &root); err != nil {
		return nil, fmt.Errorf("parse JSON Schema: %w", err)
	}
	if len(root.Type) == 0 {
		return nil, errors.New("schema root type must be object")
	}
	var rootType string
	if err := json.Unmarshal(root.Type, &rootType); err != nil {
		// Keep parser diagnostics with the schema root rule that the type violates.
		return nil, fmt.Errorf("schema root type must be object: parse root type JSON: %w", err)
	}
	if rootType != "object" {
		return nil, errors.New("schema root type must be object")
	}

	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("parse JSON Schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	registerErr := compiler.AddResource(schemaLocation, document)
	if registerErr != nil {
		return nil, fmt.Errorf("register JSON Schema: %w", registerErr)
	}
	schema, err := compiler.Compile(schemaLocation)
	if err != nil {
		return nil, fmt.Errorf("compile JSON Schema: %w", err)
	}
	return schema, nil
}

// validateArguments parses one JSON value and applies its cached schema.
func validateArguments(schema *jsonschema.Schema, argumentsJSON []byte) error {
	arguments, err := jsonschema.UnmarshalJSON(bytes.NewReader(argumentsJSON))
	if err != nil {
		return fmt.Errorf("parse arguments JSON: %w", err)
	}
	validationErr := schema.Validate(arguments)
	if validationErr != nil {
		return fmt.Errorf("validate arguments JSON: %w", validationErr)
	}
	return nil
}

// mapResultContents converts ordered extension result blocks into domain values.
func mapResultContents(contents []*extensionpb.ToolResultContent) ([]tool.ResultContent, error) {
	if len(contents) == 0 {
		return nil, errors.New("result contents are empty")
	}
	return lo.MapErr(contents, func(content *extensionpb.ToolResultContent, index int) (tool.ResultContent, error) {
		if content == nil {
			return tool.ResultContent{}, fmt.Errorf("result content %d is missing", index)
		}
		switch content.WhichContent() {
		case extensionpb.ToolResultContent_Text_case:
			return tool.ResultContent{
				Kind: tool.ResultContentText, Text: mo.Some(content.GetText()), Image: mo.None[tool.ResultImage](),
			}, nil
		case extensionpb.ToolResultContent_Image_case:
			image := content.GetImage()
			if image == nil || image.GetMediaType() == "" || len(image.GetData()) == 0 {
				return tool.ResultContent{}, fmt.Errorf("result image %d is invalid", index)
			}
			return tool.ResultContent{
				Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{
					MediaType: image.GetMediaType(), Data: bytes.Clone(image.GetData()),
				}),
			}, nil
		case extensionpb.ToolResultContent_Content_not_set_case:
			return tool.ResultContent{}, fmt.Errorf("result content %d is missing", index)
		default:
			return tool.ResultContent{}, fmt.Errorf("result content %d is invalid", index)
		}
	})
}

// mapProgress maps the closed public enum into a Host infrastructure value.
func mapProgress(progress *extensionpb.ToolProgress) (tool.Progress, error) {
	if progress == nil {
		return tool.Progress{}, errors.New("progress payload is missing")
	}
	var channel tool.ProgressChannel
	switch progress.GetChannel() {
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STATUS:
		channel = tool.ProgressChannelStatus
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDOUT:
		channel = tool.ProgressChannelStdout
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_STDERR:
		channel = tool.ProgressChannelStderr
	case extensionpb.ProgressChannel_PROGRESS_CHANNEL_UNSPECIFIED:
		return tool.Progress{}, errors.New("progress channel is unspecified")
	default:
		return tool.Progress{}, fmt.Errorf("progress channel %q is invalid", progress.GetChannel().String())
	}
	return tool.Progress{Channel: channel, Content: progress.GetContent()}, nil
}
