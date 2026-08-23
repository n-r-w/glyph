package codex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// reasoningContext is the provider-owned opaque replay payload stored by Agent Core.
type reasoningContext struct {
	ID               string   `json:"id"`
	EncryptedContent string   `json:"encrypted_content"`
	Summary          []string `json:"summary"`
}

// buildInput converts complete projected history into ordered Responses input items.
func buildInput(
	history []agent.HistoryEntry,
	grammarInputProperties map[string]string,
) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(history))
	for entryIndex := range history {
		entry := &history[entryIndex]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			message, err := userMessageInput(entry.User)
			if err != nil {
				return nil, err
			}
			input = append(input, message)
		case agent.HistoryEntryModel:
			modelInput, err := buildModelInput(entry.Model, grammarInputProperties)
			if err != nil {
				return nil, err
			}
			input = append(input, modelInput...)
		case agent.HistoryEntryToolResult:
			if _, custom := grammarInputProperties[entry.ToolResult.ToolName]; custom {
				contents, err := customOutputContents(entry.ToolResult.Contents)
				if err != nil {
					return nil, err
				}
				input = append(input, responses.ResponseInputItemParamOfCustomToolCallOutput(entry.ToolResult.CallID, contents))
			} else {
				contents, err := functionOutputContents(entry.ToolResult.Contents)
				if err != nil {
					return nil, err
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(entry.ToolResult.CallID, contents))
			}
		}
	}
	return input, nil
}

// functionOutputContents maps typed result blocks into the Codex function-output format.
//
//nolint:exhaustruct // SDK union values set exactly one active variant.
func functionOutputContents(contents []tool.ResultContent) (responses.ResponseFunctionCallOutputItemListParam, error) {
	mapped := make(responses.ResponseFunctionCallOutputItemListParam, 0, len(contents))
	for index, content := range contents {
		switch content.Kind {
		case tool.ResultContentText:
			mapped = append(mapped, responses.ResponseFunctionCallOutputItemParamOfInputText(content.Text))
		case tool.ResultContentImage:
			if content.Image.MediaType == "" {
				return nil, fmt.Errorf("tool result image %d has no media type", index)
			}
			dataURL := "data:" + content.Image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(content.Image.Data)
			mapped = append(mapped, responses.ResponseFunctionCallOutputItemUnionParam{
				OfInputImage: &responses.ResponseInputImageContentParam{ImageURL: param.NewOpt(dataURL)},
			})
		default:
			return nil, fmt.Errorf("tool result content %d has unknown kind %d", index, content.Kind)
		}
	}
	return mapped, nil
}

// customOutputContents maps typed blocks into the Codex custom-tool output format.
//
//nolint:exhaustruct // SDK union values set exactly one active variant.
func customOutputContents(
	contents []tool.ResultContent,
) ([]responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam, error) {
	mapped := make([]responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam, 0, len(contents))
	for index, content := range contents {
		switch content.Kind {
		case tool.ResultContentText:
			mapped = append(mapped, responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{
				OfInputText: &responses.ResponseInputTextParam{Text: content.Text},
			})
		case tool.ResultContentImage:
			if content.Image.MediaType == "" {
				return nil, fmt.Errorf("tool result image %d has no media type", index)
			}
			dataURL := "data:" + content.Image.MediaType + ";base64," + base64.StdEncoding.EncodeToString(content.Image.Data)
			mapped = append(mapped, responses.ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam{
				OfInputImage: &responses.ResponseInputImageParam{ImageURL: param.NewOpt(dataURL)},
			})
		default:
			return nil, fmt.Errorf("tool result content %d has unknown kind %d", index, content.Kind)
		}
	}
	return mapped, nil
}

// buildModelInput preserves model item order and ignores context owned by other providers.
func buildModelInput(
	response model.Response,
	grammarInputProperties map[string]string,
) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(response.Content))
	for _, item := range response.Content {
		switch item.Kind {
		case model.ContentText, model.ContentRefusal:
			input = append(input, messageInput(responses.EasyInputMessageRoleAssistant, item.Text))
		case model.ContentReasoning:
			continue
		case model.ContentProviderContext:
			if item.ProviderContext.ProviderID != ProviderID {
				continue
			}
			reasoning, err := reasoningInput(item.ProviderContext.Payload)
			if err != nil {
				return nil, err
			}
			input = append(input, reasoning)
		case model.ContentToolCall:
			if property, custom := grammarInputProperties[item.ToolCall.Name]; custom {
				value, ok := item.ToolCall.Arguments[property].(string)
				if !ok {
					return nil, fmt.Errorf("codex grammar tool %q requires string argument %q", item.ToolCall.Name, property)
				}
				input = append(input, responses.ResponseInputItemParamOfCustomToolCall(
					item.ToolCall.ID, value, item.ToolCall.Name,
				))
			} else {
				arguments, err := json.Marshal(item.ToolCall.Arguments)
				if err != nil {
					return nil, fmt.Errorf("encode Codex tool-call arguments: %w", err)
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(
					string(arguments), item.ToolCall.ID, item.ToolCall.Name,
				))
			}
		}
	}
	return input, nil
}

// reasoningInput validates stateless replay data and maps its summaries.
func reasoningInput(payload []byte) (responses.ResponseInputItemUnionParam, error) {
	var contextValue reasoningContext
	if err := json.Unmarshal(payload, &contextValue); err != nil {
		return responses.ResponseInputItemUnionParam{}, errors.New("OpenAI Codex reasoning context is malformed")
	}
	if contextValue.ID == "" || contextValue.EncryptedContent == "" {
		return responses.ResponseInputItemUnionParam{}, errors.New(
			"OpenAI Codex stateless continuation requires encrypted reasoning",
		)
	}
	summary := make([]responses.ResponseReasoningItemSummaryParam, len(contextValue.Summary))
	for index, text := range contextValue.Summary {
		//nolint:exhaustruct // SDK sets the fixed summary type during JSON encoding.
		summary[index] = responses.ResponseReasoningItemSummaryParam{Text: text}
	}
	reasoning := responses.ResponseInputItemParamOfReasoning(contextValue.ID, summary)
	reasoning.OfReasoning.EncryptedContent = param.NewOpt(contextValue.EncryptedContent)
	return reasoning, nil
}

// userMessageInput serializes ordered text and inline image content without string shorthand.
func userMessageInput(message model.Message) (responses.ResponseInputItemUnionParam, error) {
	content := make(responses.ResponseInputMessageContentListParam, 0, len(message.Content))
	for _, item := range message.Content {
		switch item.Kind {
		case model.InputContentText:
			content = append(content, responses.ResponseInputContentParamOfInputText(item.Text))
		case model.InputContentImage:
			if item.MediaType == "" || len(item.Data) == 0 {
				return responses.ResponseInputItemUnionParam{}, errors.New("codex image media type and data are required")
			}
			image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			image.OfInputImage.ImageURL = param.NewOpt(
				"data:" + item.MediaType + ";base64," + base64.StdEncoding.EncodeToString(item.Data),
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
	tools := make([]responses.ToolUnionParam, 0, len(descriptors))
	for _, descriptor := range descriptors {
		constraint := descriptor.ConstrainedSampling
		if constraint.Kind == tool.ConstrainedSamplingGrammar {
			if !capabilities.lark && !capabilities.regex {
				return nil, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but the selected Codex model does not support it",
					descriptor.Name,
				)
			}
			definition, syntax := preferredGrammar(constraint.Grammar, capabilities)
			if definition == "" {
				return nil, fmt.Errorf(
					"tool %q requires grammar constrained sampling, but no supported grammar variant was provided",
					descriptor.Name,
				)
			}
			//nolint:exhaustruct // Other SDK tool variants are intentionally omitted.
			tools = append(tools, responses.ToolUnionParam{OfCustom: &responses.CustomToolParam{
				Name: descriptor.Name, Description: param.NewOpt(descriptor.Description),
				Format: shared.CustomToolInputFormatParamOfGrammar(definition, syntax),
			}})
			continue
		}

		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return nil, fmt.Errorf("decode schema for Codex tool %q: %w", descriptor.Name, err)
		}
		strict := capabilities.strict
		if constraint.Kind == tool.ConstrainedSamplingJSONSchema {
			switch constraint.JSONSchemaStrictness {
			case tool.JSONSchemaStrictPrefer:
			case tool.JSONSchemaStrictRequire:
				if !capabilities.strict {
					return nil, fmt.Errorf(
						"tool %q requires JSON Schema constrained sampling, but the selected Codex model does not support it",
						descriptor.Name,
					)
				}
			default:
				return nil, fmt.Errorf("tool %q has invalid JSON Schema strictness", descriptor.Name)
			}
		} else if constraint.Kind != 0 {
			return nil, fmt.Errorf("tool %q has invalid constrained sampling kind", descriptor.Name)
		}
		//nolint:exhaustruct // Other SDK tool variants are intentionally omitted.
		tools = append(tools, responses.ToolUnionParam{
			//nolint:exhaustruct // Optional Codex function fields use SDK zero values.
			OfFunction: &responses.FunctionToolParam{
				Name: descriptor.Name, Description: param.NewOpt(descriptor.Description),
				Parameters: schema, Strict: param.NewOpt(strict),
			},
		})
	}
	return tools, nil
}

// grammarInputProperties indexes custom input properties for request replay and stream conversion.
func grammarInputProperties(descriptors []tool.Descriptor) map[string]string {
	properties := make(map[string]string)
	for _, descriptor := range descriptors {
		if descriptor.ConstrainedSampling.Kind == tool.ConstrainedSamplingGrammar {
			properties[descriptor.Name] = descriptor.ConstrainedSampling.GrammarInputProperty
		}
	}
	return properties
}

// preferredGrammar selects the first model-supported nonempty format in provider preference order.
func preferredGrammar(variants tool.GrammarVariants, capabilities toolCapabilities) (definition, syntax string) {
	if capabilities.lark && strings.TrimSpace(variants.Lark) != "" {
		return variants.Lark, "lark"
	}
	if capabilities.regex && strings.TrimSpace(variants.Regex) != "" {
		return variants.Regex, "regex"
	}
	return "", ""
}
