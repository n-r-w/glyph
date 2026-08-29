package codex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// reasoningContext is the opaque replay value set stored by Agent Core.
type reasoningContext struct {
	// ID identifies the provider reasoning item.
	ID string `json:"id"`
	// EncryptedContent contains opaque provider replay state.
	EncryptedContent string `json:"encrypted_content"`
	// Summary contains provider reasoning summary fragments.
	Summary []string `json:"summary"`
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
				//nolint:exhaustruct_v5 // Only OfInputImage is active.
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
			//nolint:exhaustruct_v5 // ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam: OfInputText is active.
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
			//nolint:exhaustruct_v5 // ResponseCustomToolCallOutputOutputOutputContentListItemUnionParam: OfInputImage is active.
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
