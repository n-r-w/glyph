package codex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// reasoningContext is the opaque replay value set stored by Agent Core.
type reasoningContext struct {
	ID               string   `json:"id"`
	EncryptedContent string   `json:"encrypted_content"`
	Summary          []string `json:"summary"`
}

// buildInput converts complete projected history into ordered Responses input items.
func buildInput(
	history []agent.HistoryEntry,
	grammarInputProperties map[string]string,
	target model.ProviderContextSource,
) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(history))
	for entryIndex := range history {
		entry := &history[entryIndex]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			messageValue, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no user payload", entryIndex)
			}
			message, err := userMessageInput(messageValue)
			if err != nil {
				return nil, err
			}
			input = append(input, message)
		case agent.HistoryEntryModel:
			response, present := entry.Model.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no model payload", entryIndex)
			}
			modelInput, err := buildModelInput(response, grammarInputProperties, target)
			if err != nil {
				return nil, err
			}
			input = append(input, modelInput...)
		case agent.HistoryEntryToolResult:
			result, present := entry.ToolResult.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no tool result payload", entryIndex)
			}
			if _, custom := grammarInputProperties[result.ToolName]; custom {
				contents, err := customOutputContents(result.Contents)
				if err != nil {
					return nil, err
				}
				input = append(input, responses.ResponseInputItemParamOfCustomToolCallOutput(result.CallID, contents))
			} else {
				contents, err := functionOutputContents(result.Contents)
				if err != nil {
					return nil, err
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(result.CallID, contents))
			}
		}
	}
	return input, nil
}

// functionOutputContents maps typed result blocks into the Codex function-output format.
func functionOutputContents(contents []tool.ResultContent) (responses.ResponseFunctionCallOutputItemListParam, error) {
	return lo.MapErr(
		contents,
		func(content tool.ResultContent, index int) (responses.ResponseFunctionCallOutputItemUnionParam, error) {
			switch content.Kind {
			case tool.ResultContentText:
				text, present := content.Text.Get()
				if !present {
					return responses.ResponseFunctionCallOutputItemUnionParam{},
						fmt.Errorf("tool result text %d has no text", index)
				}
				return responses.ResponseFunctionCallOutputItemParamOfInputText(text), nil
			case tool.ResultContentImage:
				image, present := content.Image.Get()
				if !present {
					return responses.ResponseFunctionCallOutputItemUnionParam{},
						fmt.Errorf("tool result image %d has no image", index)
				}
				if image.MediaType == "" {
					return responses.ResponseFunctionCallOutputItemUnionParam{},
						fmt.Errorf("tool result image %d has no media type", index)
				}
				dataURL := "data:" + image.MediaType + ";base64," +
					base64.StdEncoding.EncodeToString(image.Data)
				//nolint:exhaustruct // responses.ResponseFunctionCallOutputItemUnionParam sets only the active OfInputImage field.
				return responses.ResponseFunctionCallOutputItemUnionParam{
					OfInputImage: &responses.ResponseInputImageContentParam{
						ImageURL:              param.NewOpt(dataURL),
						FileID:                param.Opt[string]{},
						Detail:                "",
						PromptCacheBreakpoint: responses.ResponseInputImageContentPromptCacheBreakpointParam{},
						Type:                  "",
					},
				}, nil
			default:
				return responses.ResponseFunctionCallOutputItemUnionParam{},
					fmt.Errorf("tool result content %d has unknown kind %d", index, content.Kind)
			}
		},
	)
}

// customOutputContents maps typed blocks into the Codex custom-tool output format.
func customOutputContents(
	contents []tool.ResultContent,
) ([]responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam, error) {
	return lo.MapErr(contents, func(content tool.ResultContent, index int) (
		responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam, error,
	) {
		switch content.Kind {
		case tool.ResultContentText:
			text, present := content.Text.Get()
			if !present {
				return responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{},
					fmt.Errorf("tool result text %d has no text", index)
			}
			//nolint:exhaustruct // ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam: OfInputText is active.
			return responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{
				OfInputText: &responses.ResponseInputTextParam{
					Text:                  text,
					PromptCacheBreakpoint: responses.ResponseInputTextPromptCacheBreakpointParam{},
					Type:                  "",
				},
			}, nil
		case tool.ResultContentImage:
			image, present := content.Image.Get()
			if !present {
				return responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{},
					fmt.Errorf("tool result image %d has no image", index)
			}
			if image.MediaType == "" {
				return responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{},
					fmt.Errorf("tool result image %d has no media type", index)
			}
			dataURL := "data:" + image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(image.Data)
			//nolint:exhaustruct // ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam: OfInputImage is active.
			return responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{
				OfInputImage: &responses.ResponseInputImageParam{
					ImageURL:              param.NewOpt(dataURL),
					Detail:                "",
					FileID:                param.Opt[string]{},
					PromptCacheBreakpoint: responses.ResponseInputImagePromptCacheBreakpointParam{},
					Type:                  "",
				},
			}, nil
		default:
			return responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{},
				fmt.Errorf("tool result content %d has unknown kind %d", index, content.Kind)
		}
	})
}

// buildModelInput preserves model item order and ignores context owned by other providers.
func buildModelInput(
	response model.Response,
	grammarInputProperties map[string]string,
	target model.ProviderContextSource,
) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(response.Content))
	for index := range response.Content {
		item := &response.Content[index]
		switch item.Kind {
		case model.ContentText, model.ContentRefusal:
			message, err := codexAssistantMessage(*item, index)
			if err != nil {
				return nil, err
			}
			input = append(input, message)
		case model.ContentReasoning:
			reasoning, err := codexReasoningInput(*item, target)
			if err != nil {
				return nil, err
			}
			input = append(input, reasoning...)
		case model.ContentToolCall:
			call, present := item.ToolCall.Get()
			if !present {
				return nil, fmt.Errorf("model content %d has no tool call", index)
			}
			if property, custom := grammarInputProperties[call.Name]; custom {
				value, ok := call.Arguments[property].(string)
				if !ok {
					return nil, fmt.Errorf("codex grammar tool %q requires string argument %q", call.Name, property)
				}
				input = append(input, responses.ResponseInputItemParamOfCustomToolCall(
					call.ID, value, call.Name,
				))
			} else {
				arguments, err := json.Marshal(call.Arguments)
				if err != nil {
					return nil, fmt.Errorf("encode Codex tool-call arguments: %w", err)
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(
					string(arguments), call.ID, call.Name,
				))
			}
		}
	}
	return input, nil
}

// codexReasoningInput maps replayable or visible reasoning to Codex SDK input.
func codexReasoningInput(
	item model.Content,
	target model.ProviderContextSource,
) (responses.ResponseInputParam, error) {
	providerContext, hasProviderContext := item.ProviderContext.Get()
	if hasProviderContext && providerContextCompatible(providerContext.Source, target) &&
		len(providerContext.Payload) != 0 {
		reasoning, err := reasoningInput(providerContext.Payload)
		if err != nil {
			return nil, err
		}
		return responses.ResponseInputParam{reasoning}, nil
	}
	if text, present := item.Text.Get(); present && text != "" {
		return responses.ResponseInputParam{messageInput(responses.EasyInputMessageRoleAssistant, text)}, nil
	}
	return nil, nil
}

// codexAssistantMessage maps selected assistant text to one Codex SDK message.
func codexAssistantMessage(item model.Content, index int) (responses.ResponseInputItemUnionParam, error) {
	text, present := item.Text.Get()
	if !present {
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("model content %d has no text", index)
	}
	return messageInput(responses.EasyInputMessageRoleAssistant, text), nil
}

func providerContextCompatible(source, target model.ProviderContextSource) bool {
	if source.ProviderID != target.ProviderID || source.API != target.API {
		return false
	}
	if source.Model == target.Model {
		return true
	}
	sourceKey, sourceHasKey := source.CompatibilityKey.Get()
	targetKey, targetHasKey := target.CompatibilityKey.Get()
	return sourceHasKey && targetHasKey && sourceKey != "" && sourceKey == targetKey
}

// reasoningInput validates stateless replay data and maps its summaries.
func reasoningInput(payload []byte) (responses.ResponseInputItemUnionParam, error) {
	var contextValue reasoningContext
	if err := json.Unmarshal(payload, &contextValue); err != nil {
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("decode OpenAI Codex reasoning context: %w", err)
	}
	if contextValue.ID == "" || contextValue.EncryptedContent == "" {
		return responses.ResponseInputItemUnionParam{}, errors.New(
			"OpenAI Codex stateless continuation requires encrypted reasoning",
		)
	}
	summary := lo.Map(contextValue.Summary, func(text string, _ int) responses.ResponseReasoningItemSummaryParam {
		return responses.ResponseReasoningItemSummaryParam{
			Text: text,
			Type: "",
		}
	})
	reasoning := responses.ResponseInputItemParamOfReasoning(contextValue.ID, summary)
	reasoning.OfReasoning.EncryptedContent = param.NewOpt(contextValue.EncryptedContent)
	return reasoning, nil
}

// userMessageInput serializes ordered text and inline image content without string shorthand.
func userMessageInput(message model.Message) (responses.ResponseInputItemUnionParam, error) {
	content := make(responses.ResponseInputMessageContentListParam, 0, len(message.Content))
	for index, item := range message.Content {
		switch item.Kind {
		case model.InputContentText:
			text, present := item.Text.Get()
			if !present {
				return responses.ResponseInputItemUnionParam{}, fmt.Errorf("codex text content %d has no text", index)
			}
			content = append(content, responses.ResponseInputContentParamOfInputText(text))
		case model.InputContentImage:
			mediaType, hasMediaType := item.MediaType.Get()
			data, hasData := item.Data.Get()
			if !hasMediaType || !hasData || mediaType == "" || len(data) == 0 {
				return responses.ResponseInputItemUnionParam{}, errors.New("codex image media type and data are required")
			}
			image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			image.OfInputImage.ImageURL = param.NewOpt(
				"data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
			)
			content = append(content, image)
		default:
			return responses.ResponseInputItemUnionParam{}, fmt.Errorf("unsupported user content kind %d", item.Kind)
		}
	}
	messageInput := responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
	messageInput.OfMessage.Type = responses.EasyInputMessageTypeMessage
	return messageInput, nil
}

// messageInput creates one ordered text message item without using request string shorthand.
func messageInput(role responses.EasyInputMessageRole, text string) responses.ResponseInputItemUnionParam {
	message := responses.ResponseInputItemParamOfMessage(text, role)
	message.OfMessage.Type = responses.EasyInputMessageTypeMessage
	return message
}

type toolCapabilities struct {
	strict bool
	lark   bool
	regex  bool
}

// buildTools maps provider-neutral schemas into Codex tool request types.
func buildTools(descriptors []tool.Descriptor, capabilities toolCapabilities) ([]responses.ToolUnionParam, error) {
	return lo.MapErr(descriptors, func(descriptor tool.Descriptor, _ int) (responses.ToolUnionParam, error) {
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return responses.ToolUnionParam{}, fmt.Errorf("decode schema for Codex tool %q: %w", descriptor.Name, err)
		}
		constraint, constrained := descriptor.ConstrainedSampling.Get()
		if constrained {
			if err := validateCodexConstraint(schema, constraint, descriptor.Name); err != nil {
				return responses.ToolUnionParam{}, err
			}
		}
		if constrained && constraint.Kind == tool.ConstrainedSamplingGrammar {
			if !capabilities.lark && !capabilities.regex {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but the selected Codex model does not support it",
					descriptor.Name,
				)
			}
			grammar, present := constraint.Grammar.Get()
			if !present {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
					descriptor.Name,
				)
			}
			property, hasProperty := constraint.GrammarInputProperty.Get()
			if !hasProperty || property == "" {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no grammar input property was provided",
					descriptor.Name,
				)
			}
			definition, syntax := preferredGrammar(grammar, capabilities)
			if definition == "" {
				return responses.ToolUnionParam{}, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
					descriptor.Name,
				)
			}
			//nolint:exhaustruct // responses.ToolUnionParam sets only the active OfCustom field.
			return responses.ToolUnionParam{
				OfCustom: &responses.CustomToolParam{
					Name:           descriptor.Name,
					Description:    param.NewOpt(descriptor.Description),
					Format:         shared.CustomToolInputFormatParamOfGrammar(definition, syntax),
					DeferLoading:   param.Opt[bool]{},
					AllowedCallers: nil,
					Type:           "",
				},
			}, nil
		}

		strict, err := codexStrict(schema, descriptor.ConstrainedSampling, capabilities, descriptor.Name)
		if err != nil {
			return responses.ToolUnionParam{}, err
		}
		//nolint:exhaustruct // responses.ToolUnionParam sets only the active OfFunction field.
		return responses.ToolUnionParam{
			OfFunction: &responses.FunctionToolParam{
				Name:           descriptor.Name,
				Description:    param.NewOpt(descriptor.Description),
				Parameters:     schema,
				Strict:         param.NewOpt(strict),
				DeferLoading:   param.Opt[bool]{},
				AllowedCallers: nil,
				OutputSchema:   nil,
				Type:           "",
			},
		}, nil
	})
}

// validateCodexConstraint rejects malformed constraint variants at the provider request boundary.
func validateCodexConstraint(schema map[string]any, constraint tool.ConstrainedSampling, toolName string) error {
	switch constraint.Kind {
	case tool.ConstrainedSamplingJSONSchema:
		strictness, present := constraint.JSONSchemaStrictness.Get()
		if constraint.Grammar.IsSome() || constraint.GrammarInputProperty.IsSome() {
			return fmt.Errorf("tool %q has inconsistent JSON Schema constraint options", toolName)
		}
		if !present {
			return fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
		}
		switch strictness {
		case tool.JSONSchemaStrictPrefer, tool.JSONSchemaStrictRequire:
			return nil
		default:
			return fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
		}
	case tool.ConstrainedSamplingGrammar:
		grammar, hasGrammar := constraint.Grammar.Get()
		property, hasProperty := constraint.GrammarInputProperty.Get()
		if constraint.JSONSchemaStrictness.IsSome() {
			return fmt.Errorf("tool %q has inconsistent grammar constraint options", toolName)
		}
		if !hasGrammar {
			return fmt.Errorf(
				"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
				toolName,
			)
		}
		if !hasProperty || property == "" {
			return fmt.Errorf(
				"tool %q requires grammar constrained sampling, but no grammar input property was provided",
				toolName,
			)
		}
		lark, hasLark := grammar.Lark.Get()
		regex, hasRegex := grammar.Regex.Get()
		if (!hasLark || strings.TrimSpace(lark) == "") && (!hasRegex || strings.TrimSpace(regex) == "") {
			return fmt.Errorf(
				"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
				toolName,
			)
		}
		return validateCodexGrammarSchema(schema, property, toolName)
	default:
		return fmt.Errorf("tool %q has invalid constrained sampling kind", toolName)
	}
}

// validateCodexGrammarSchema checks the single direct string property used as custom tool input.
func validateCodexGrammarSchema(schema map[string]any, property, toolName string) error {
	const rule = "grammar schema must have exactly one required string property"

	properties, hasProperties := schema["properties"].(map[string]any)
	required, hasRequired := schema["required"].([]any)
	if !hasProperties || len(properties) != 1 || !hasRequired || len(required) != 1 {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	requiredProperty, isString := required[0].(string)
	if !isString || requiredProperty == "" || requiredProperty != property {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	propertySchema, exists := properties[property].(map[string]any)
	if !exists {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	propertyType, isString := propertySchema["type"].(string)
	if !isString || propertyType != "string" {
		return fmt.Errorf("tool %q %s", toolName, rule)
	}
	return nil
}

// codexStrict selects provider strictness without changing the Glyph-owned schema.
func codexStrict(
	schema map[string]any,
	constraintOption mo.Option[tool.ConstrainedSampling],
	capabilities toolCapabilities,
	toolName string,
) (bool, error) {
	compatible := codexStrictSchemaCompatible(schema)
	strict := capabilities.strict && compatible
	constraint, constrained := constraintOption.Get()
	if !constrained {
		return strict, nil
	}
	if constraint.Kind != tool.ConstrainedSamplingJSONSchema {
		return false, fmt.Errorf("tool %q has invalid constrained sampling kind", toolName)
	}
	strictness, present := constraint.JSONSchemaStrictness.Get()
	if !present {
		return false, fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
	}
	switch strictness {
	case tool.JSONSchemaStrictPrefer:
		return strict, nil
	case tool.JSONSchemaStrictRequire:
		if !capabilities.strict {
			return false, fmt.Errorf(
				"tool %q requires JSON Schema constrained sampling, but the selected Codex model does not support it",
				toolName,
			)
		}
		if !compatible {
			return false, fmt.Errorf(
				"tool %q requires JSON Schema constrained sampling, "+
					"but its input schema is not compatible with Codex strict JSON Schema",
				toolName,
			)
		}
		return true, nil
	default:
		return false, fmt.Errorf("tool %q has invalid JSON Schema strictness", toolName)
	}
}

// codexStrictSchemaCompatible checks every object nested in a JSON Schema for Codex strict requirements.
func codexStrictSchemaCompatible(value any) bool {
	switch schema := value.(type) {
	case map[string]any:
		if codexObjectSchema(schema) && !codexStrictObjectSchema(schema) {
			return false
		}
		for _, child := range schema {
			if !codexStrictSchemaCompatible(child) {
				return false
			}
		}
	case []any:
		for _, child := range schema {
			if !codexStrictSchemaCompatible(child) {
				return false
			}
		}
	}
	return true
}

// codexObjectSchema reports whether a schema node declares object-specific keywords.
func codexObjectSchema(schema map[string]any) bool {
	typeName, isTyped := schema["type"].(string)
	_, hasProperties := schema["properties"]
	_, hasAdditionalProperties := schema["additionalProperties"]
	return isTyped && typeName == "object" || hasProperties || hasAdditionalProperties
}

// codexStrictObjectSchema checks that an object meets Codex strict requirements.
func codexStrictObjectSchema(schema map[string]any) bool {
	additionalProperties, hasAdditionalProperties := schema["additionalProperties"].(bool)
	if !hasAdditionalProperties || additionalProperties {
		return false
	}
	properties, hasProperties := schema["properties"].(map[string]any)
	if !hasProperties {
		return false
	}
	required, hasRequired := schema["required"].([]any)
	if !hasRequired || len(required) != len(properties) {
		return false
	}
	seen := make(map[string]struct{}, len(required))
	for _, name := range required {
		propertyName, isString := name.(string)
		if !isString {
			return false
		}
		if _, duplicate := seen[propertyName]; duplicate {
			return false
		}
		if _, isProperty := properties[propertyName]; !isProperty {
			return false
		}
		seen[propertyName] = struct{}{}
	}
	return true
}

// grammarInputProperties indexes custom input properties for request replay and stream conversion.
func grammarInputProperties(descriptors []tool.Descriptor) map[string]string {
	properties := make(map[string]string)
	for index := range descriptors {
		descriptor := &descriptors[index]
		constraint, constrained := descriptor.ConstrainedSampling.Get()
		if !constrained || constraint.Kind != tool.ConstrainedSamplingGrammar {
			continue
		}
		property, present := constraint.GrammarInputProperty.Get()
		if present {
			properties[descriptor.Name] = property
		}
	}
	return properties
}

// preferredGrammar selects the first model-supported nonempty format in provider preference order.
func preferredGrammar(variants tool.GrammarVariants, capabilities toolCapabilities) (definition, syntax string) {
	if lark, ok := variants.Lark.Get(); capabilities.lark && ok && strings.TrimSpace(lark) != "" {
		return lark, "lark"
	}
	if regex, ok := variants.Regex.Get(); capabilities.regex && ok && strings.TrimSpace(regex) != "" {
		return regex, "regex"
	}
	return "", ""
}
