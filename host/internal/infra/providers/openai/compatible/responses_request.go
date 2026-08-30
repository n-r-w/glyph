package compatible

import (
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// responsesParams builds one Responses API request from provider-neutral input.
func responsesParams(
	request run.ModelRequest,
	target model.ProviderContextSource,
) (responses.ResponseNewParams, error) {
	input, err := responsesInput(request.History, target)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	tools, err := responsesTools(request.Tools, request.Model.ToolCapabilities.StrictJSONSchema)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	params := responses.ResponseNewParams{
		Model:             string(request.Model.Model),
		Instructions:      param.NewOpt(request.Instructions),
		Store:             param.NewOpt(false),
		ParallelToolCalls: param.NewOpt(false),
		Include:           []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent},
		//nolint:exhaustruct_v5 // responses.ResponseNewParamsInputUnion sets only the active OfInputItemList field.
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Tools:                tools,
		Background:           param.Opt[bool]{},
		MaxOutputTokens:      param.Opt[int64]{},
		MaxToolCalls:         param.Opt[int64]{},
		PreviousResponseID:   param.Opt[string]{},
		PromptCacheKey:       param.Opt[string]{},
		SafetyIdentifier:     param.Opt[string]{},
		Temperature:          param.Opt[float64]{},
		TopLogprobs:          param.Opt[int64]{},
		TopP:                 param.Opt[float64]{},
		User:                 param.Opt[string]{},
		ContextManagement:    nil,
		Conversation:         responses.ResponseNewParamsConversationUnion{},
		Metadata:             nil,
		Moderation:           responses.ResponseNewParamsModeration{},
		Prompt:               responses.ResponsePromptParam{},
		PromptCacheRetention: "",
		ServiceTier:          "",
		StreamOptions:        responses.ResponseNewParamsStreamOptions{},
		Truncation:           "",
		PromptCacheOptions:   responses.ResponseNewParamsPromptCacheOptions{},
		Reasoning:            shared.ReasoningParam{},
		Text:                 responses.ResponseTextConfigParam{},
		ToolChoice:           responses.ResponseNewParamsToolChoiceUnion{},
	}
	if request.Model.ReasoningCapabilities.Supported {
		switch request.ReasoningChoice {
		case model.ReasoningChoiceOff:
			params.Reasoning.Effort = shared.ReasoningEffortNone
		case model.ReasoningChoiceOn:
		case model.ReasoningChoiceMinimal, model.ReasoningChoiceLow, model.ReasoningChoiceMedium,
			model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
			params.Reasoning.Effort = shared.ReasoningEffort(request.ReasoningChoice)
		default:
			return responses.ResponseNewParams{}, errors.New("OpenAI-compatible reasoning choice is invalid")
		}
	}
	return params, nil
}

// responsesTerminalError validates the terminal outcome returned by the Responses API.
func responsesTerminalError(response model.Response) error {
	outcome, present := response.Outcome.Get()
	if !present || outcome == 0 {
		return errors.New("responses request has no terminal outcome")
	}
	if outcome == model.OutcomeFailed {
		if message := response.ErrorMessage.OrEmpty(); message != "" {
			return errors.New(message)
		}
		return errors.New("responses request failed")
	}
	return nil
}

func responsesInput(
	history []agent.HistoryEntry,
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
			message, err := responsesUserMessage(messageValue)
			if err != nil {
				return nil, err
			}
			input = append(input, message)
		case agent.HistoryEntryModel:
			response, present := entry.Model.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no model payload", entryIndex)
			}
			items, err := responsesModelItems(response, target)
			if err != nil {
				return nil, err
			}
			input = append(input, items...)
		case agent.HistoryEntryToolResult:
			result, present := entry.ToolResult.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no tool result payload", entryIndex)
			}
			output, err := responsesToolOutput(result.Contents)
			if err != nil {
				return nil, err
			}
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(result.CallID, output))
		default:
			return nil, fmt.Errorf("unsupported history entry kind %d", entry.Kind)
		}
	}
	return input, nil
}

func responsesUserMessage(message model.Message) (responses.ResponseInputItemUnionParam, error) {
	content := make(responses.ResponseInputMessageContentListParam, 0, len(message.Content))
	for index, item := range message.Content {
		switch item.Kind {
		case model.InputContentText:
			text, present := item.Text.Get()
			if !present {
				return responses.ResponseInputItemUnionParam{}, fmt.Errorf("user text %d has no text", index)
			}
			content = append(content, responses.ResponseInputContentParamOfInputText(text))
		case model.InputContentImage:
			mediaType, hasMediaType := item.MediaType.Get()
			data, hasData := item.Data.Get()
			if !hasMediaType || !hasData || mediaType == "" || len(data) == 0 {
				return responses.ResponseInputItemUnionParam{}, fmt.Errorf(
					"user image %d requires media type and data",
					index,
				)
			}
			image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			image.OfInputImage.ImageURL = param.NewOpt(dataURL(mediaType, data))
			content = append(content, image)
		default:
			return responses.ResponseInputItemUnionParam{}, fmt.Errorf("unsupported user content kind %d", item.Kind)
		}
	}
	messageItem := responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
	messageItem.OfMessage.Type = responses.EasyInputMessageTypeMessage
	return messageItem, nil
}

func responsesModelItems(
	response model.Response,
	target model.ProviderContextSource,
) (responses.ResponseInputParam, error) {
	items := make(responses.ResponseInputParam, 0, len(response.Content))
	for index := range response.Content {
		item := &response.Content[index]
		switch item.Kind {
		case model.ContentText, model.ContentRefusal:
			text, present := item.Text.Get()
			if !present {
				return nil, fmt.Errorf("model content %d has no text", index)
			}
			message := responses.ResponseInputItemParamOfMessage(text, responses.EasyInputMessageRoleAssistant)
			message.OfMessage.Type = responses.EasyInputMessageTypeMessage
			items = append(items, message)
		case model.ContentReasoning:
			providerContext, hasProviderContext := item.ProviderContext.Get()
			if hasProviderContext && providerContext.Source.CompatibleWith(target) &&
				len(providerContext.Payload) != 0 {
				reasoning, err := responsesReasoningItem(providerContext.Payload)
				if err != nil {
					return nil, err
				}
				items = append(items, reasoning)
			} else if text, present := item.Text.Get(); present && text != "" {
				message := responses.ResponseInputItemParamOfMessage(text, responses.EasyInputMessageRoleAssistant)
				message.OfMessage.Type = responses.EasyInputMessageTypeMessage
				items = append(items, message)
			}
		case model.ContentToolCall:
			call, present := item.ToolCall.Get()
			if !present {
				return nil, fmt.Errorf("model content %d has no tool call", index)
			}
			arguments, err := json.Marshal(call.Arguments)
			if err != nil {
				return nil, fmt.Errorf("encode tool-call arguments: %w", err)
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(
				string(arguments), call.ID, call.Name,
			))
		default:
			return nil, fmt.Errorf("unsupported model content kind %d", item.Kind)
		}
	}
	return items, nil
}

func responsesReasoningItem(payload []byte) (responses.ResponseInputItemUnionParam, error) {
	var contextValue responseContext
	if err := json.Unmarshal(payload, &contextValue); err != nil {
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("decode OpenAI-compatible provider context: %w", err)
	}
	if contextValue.ID == "" || contextValue.EncryptedContent == "" {
		return responses.ResponseInputItemUnionParam{}, errors.New("OpenAI-compatible provider context is malformed")
	}
	summary := lo.Map(contextValue.Summary, func(text string, _ int) responses.ResponseReasoningItemSummaryParam {
		return responses.ResponseReasoningItemSummaryParam{
			Text: text,
			Type: "",
		}
	})
	item := responses.ResponseInputItemParamOfReasoning(contextValue.ID, summary)
	item.OfReasoning.EncryptedContent = param.NewOpt(contextValue.EncryptedContent)
	return item, nil
}

func responsesToolOutput(contents []tool.ResultContent) (responses.ResponseFunctionCallOutputItemListParam, error) {
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
				if image.MediaType == "" || len(image.Data) == 0 {
					return responses.ResponseFunctionCallOutputItemUnionParam{},
						fmt.Errorf("tool result image %d requires media type and data", index)
				}
				imageURL := dataURL(image.MediaType, image.Data)
				//nolint:exhaustruct_v5 // Only OfInputImage is active.
				return responses.ResponseFunctionCallOutputItemUnionParam{
					OfInputImage: &responses.ResponseInputImageContentParam{
						FileID:                param.Opt[string]{},
						Detail:                "",
						ImageURL:              param.NewOpt(imageURL),
						PromptCacheBreakpoint: responses.ResponseInputImageContentPromptCacheBreakpointParam{},
						Type:                  "",
					},
				}, nil
			default:
				return responses.ResponseFunctionCallOutputItemUnionParam{},
					fmt.Errorf("unsupported tool result content kind %d", content.Kind)
			}
		},
	)
}

func responsesTools(descriptors []tool.Descriptor, strictSupported bool) ([]responses.ToolUnionParam, error) {
	return lo.MapErr(descriptors, func(descriptor tool.Descriptor, index int) (responses.ToolUnionParam, error) {
		if err := validateConstrainedSampling(descriptor, strictSupported); err != nil {
			return responses.ToolUnionParam{}, fmt.Errorf("tool %d constrained sampling: %w", index, err)
		}
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return responses.ToolUnionParam{}, fmt.Errorf("tool %d has invalid input schema: %w", index, err)
		}
		toolParam := responses.ToolParamOfFunction(descriptor.Name, schema, strictSupported)
		toolParam.OfFunction.Description = param.NewOpt(descriptor.Description)
		return toolParam, nil
	})
}
