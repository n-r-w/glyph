package compatible

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"

	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/samber/lo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// chatParams builds one format-specific Chat Completions request.
func chatParams(
	request run.ModelRequest,
	format reasoningFormat,
	target model.ProviderContextSource,
) (openai.ChatCompletionNewParams, error) {
	messages, err := chatMessages(request, format, target)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	tools, err := chatTools(request.Tools, request.Model.ToolCapabilities.StrictJSONSchema)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	params := openai.ChatCompletionNewParams{
		Messages:          messages,
		Model:             shared.ChatModel(request.Model.Model),
		ParallelToolCalls: param.NewOpt(false),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage:       param.NewOpt(true),
			IncludeObfuscation: param.Opt[bool]{},
		},
		Tools:                tools,
		FrequencyPenalty:     param.Opt[float64]{},
		Logprobs:             param.Opt[bool]{},
		MaxCompletionTokens:  param.Opt[int64]{},
		MaxTokens:            param.Opt[int64]{},
		N:                    param.Opt[int64]{},
		PresencePenalty:      param.Opt[float64]{},
		PromptCacheKey:       param.Opt[string]{},
		SafetyIdentifier:     param.Opt[string]{},
		Seed:                 param.Opt[int64]{},
		Store:                param.Opt[bool]{},
		Temperature:          param.Opt[float64]{},
		TopLogprobs:          param.Opt[int64]{},
		TopP:                 param.Opt[float64]{},
		User:                 param.Opt[string]{},
		Audio:                openai.ChatCompletionAudioParam{},
		LogitBias:            nil,
		Metadata:             nil,
		Modalities:           nil,
		Moderation:           openai.ChatCompletionNewParamsModeration{},
		PromptCacheRetention: "",
		ReasoningEffort:      "",
		ServiceTier:          "",
		Stop:                 openai.ChatCompletionNewParamsStopUnion{},
		Verbosity:            "",
		FunctionCall:         openai.ChatCompletionNewParamsFunctionCallUnion{},
		Functions:            nil,
		Prediction:           openai.ChatCompletionPredictionContentParam{},
		PromptCacheOptions:   openai.ChatCompletionNewParamsPromptCacheOptions{},
		ResponseFormat:       openai.ChatCompletionNewParamsResponseFormatUnion{},
		ToolChoice:           openai.ChatCompletionToolChoiceOptionUnionParam{},
		WebSearchOptions:     openai.ChatCompletionNewParamsWebSearchOptions{},
	}
	if controlErr := applyChatReasoningControl(&params, format, request.ReasoningChoice); controlErr != nil {
		return openai.ChatCompletionNewParams{}, controlErr
	}
	return params, nil
}

// chatMessages maps provider-neutral history into Chat Completions messages.
func chatMessages(
	request run.ModelRequest,
	format reasoningFormat,
	target model.ProviderContextSource,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	messages := []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(request.Instructions)}
	for entryIndex := range request.History {
		entry := &request.History[entryIndex]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			message, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no user payload", entryIndex)
			}
			content, err := chatUserContent(message)
			if err != nil {
				return nil, err
			}
			messages = append(messages, openai.UserMessage(content))
		case agent.HistoryEntryModel:
			response, present := entry.Model.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no model payload", entryIndex)
			}
			message, ok, err := chatAssistantMessage(response, format, target)
			if err != nil {
				return nil, err
			}
			if ok {
				messages = append(messages, message)
			}
		case agent.HistoryEntryToolResult:
			result, present := entry.ToolResult.Get()
			if !present {
				return nil, fmt.Errorf("history entry %d has no tool result payload", entryIndex)
			}
			content, err := chatToolResult(result.Contents)
			if err != nil {
				return nil, err
			}
			messages = append(messages, openai.ToolMessage(content, result.CallID))
		default:
			return nil, fmt.Errorf("unsupported history entry kind %d", entry.Kind)
		}
	}
	return messages, nil
}

func chatUserContent(message model.Message) ([]openai.ChatCompletionContentPartUnionParam, error) {
	return lo.MapErr(
		message.Content,
		func(item model.InputContent, index int) (openai.ChatCompletionContentPartUnionParam, error) {
			switch item.Kind {
			case model.InputContentText:
				text, present := item.Text.Get()
				if !present {
					return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf("user text %d has no text", index)
				}
				return openai.TextContentPart(text), nil
			case model.InputContentImage:
				mediaType, hasMediaType := item.MediaType.Get()
				data, hasData := item.Data.Get()
				if !hasMediaType || !hasData || mediaType == "" || len(data) == 0 {
					return openai.ChatCompletionContentPartUnionParam{},
						fmt.Errorf("user image %d requires media type and data", index)
				}
				return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL:    dataURL(mediaType, data),
					Detail: "",
				}), nil
			default:
				return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf(
					"unsupported user content kind %d",
					item.Kind,
				)
			}
		},
	)
}

// chatAssistantMessage maps one model response into a replayable assistant message.
func chatAssistantMessage(
	response model.Response,
	format reasoningFormat,
	target model.ProviderContextSource,
) (openai.ChatCompletionMessageParamUnion, bool, error) {
	var text strings.Builder
	var reasoning strings.Builder
	var refusal strings.Builder
	var reasoningDetails []jsontext.Value
	calls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0)
	for index := range response.Content {
		item := &response.Content[index]
		switch item.Kind {
		case model.ContentText:
			value, present := item.Text.Get()
			if !present {
				return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf(
					"model content %d has no text",
					index,
				)
			}
			text.WriteString(value)
		case model.ContentRefusal:
			value, present := item.Text.Get()
			if !present {
				return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf(
					"model content %d has no text",
					index,
				)
			}
			refusal.WriteString(value)
		case model.ContentReasoning:
			details, err := openRouterReplayDetails(*item, format, target)
			if err != nil {
				return openai.ChatCompletionMessageParamUnion{}, false, err
			}
			reasoningDetails = append(reasoningDetails, details...)
			visibleText, present := item.Text.Get()
			if !present || visibleText == "" {
				continue
			}
			if format.usesChatReasoning() {
				reasoning.WriteString(visibleText)
			} else {
				text.WriteString(visibleText)
			}
		case model.ContentToolCall:
			call, err := chatToolCallParam(*item, index)
			if err != nil {
				return openai.ChatCompletionMessageParamUnion{}, false, err
			}
			calls = append(calls, call)
		default:
			return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf(
				"unsupported model content kind %d",
				item.Kind,
			)
		}
	}
	return buildChatAssistantMessage(text.String(), reasoning.String(), reasoningDetails, refusal.String(), calls)
}

// buildChatAssistantMessage creates the external assistant union from accumulated content.
func buildChatAssistantMessage(
	text string,
	reasoning string,
	reasoningDetails []jsontext.Value,
	refusal string,
	calls []openai.ChatCompletionMessageToolCallUnionParam,
) (openai.ChatCompletionMessageParamUnion, bool, error) {
	if text == "" && reasoning == "" && len(reasoningDetails) == 0 && refusal == "" && len(calls) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false, nil
	}
	message := openai.AssistantMessage(text)
	message.OfAssistant.ToolCalls = calls
	if len(reasoningDetails) != 0 {
		message.OfAssistant.SetExtraFields(map[string]any{"reasoning_details": reasoningDetails})
	} else if reasoning != "" {
		message.OfAssistant.SetExtraFields(map[string]any{reasoningField: reasoning})
	}
	if refusal != "" {
		message.OfAssistant.Refusal = param.NewOpt(refusal)
	}
	return message, true, nil
}

// chatToolCallParam maps one selected tool call to the external Chat Completions union.
func chatToolCallParam(item model.Content, index int) (openai.ChatCompletionMessageToolCallUnionParam, error) {
	call, present := item.ToolCall.Get()
	if !present {
		return openai.ChatCompletionMessageToolCallUnionParam{}, fmt.Errorf("model content %d has no tool call", index)
	}
	arguments, err := json.Marshal(call.Arguments)
	if err != nil {
		return openai.ChatCompletionMessageToolCallUnionParam{}, fmt.Errorf("encode tool-call arguments: %w", err)
	}
	return openai.ChatCompletionMessageToolCallUnionParam{
		OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
			ID: call.ID,
			Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
				Arguments: string(arguments),
				Name:      call.Name,
			},
			Type: "",
		},
		OfCustom: nil,
	}, nil
}

func chatToolResult(contents []tool.ResultContent) (string, error) {
	parts, err := lo.MapErr(contents, func(content tool.ResultContent, index int) (string, error) {
		switch content.Kind {
		case tool.ResultContentText:
			text, present := content.Text.Get()
			if !present {
				return "", fmt.Errorf("tool result text %d has no text", index)
			}
			return text, nil
		case tool.ResultContentImage:
			image, present := content.Image.Get()
			if !present {
				return "", fmt.Errorf("tool result image %d has no image", index)
			}
			if image.MediaType == "" || len(image.Data) == 0 {
				return "", fmt.Errorf("tool result image %d requires media type and data", index)
			}
			return dataURL(image.MediaType, image.Data), nil
		default:
			return "", fmt.Errorf("unsupported tool result content kind %d", content.Kind)
		}
	})
	if err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}

func chatTools(descriptors []tool.Descriptor, strictSupported bool) ([]openai.ChatCompletionToolUnionParam, error) {
	return lo.MapErr(
		descriptors,
		func(descriptor tool.Descriptor, index int) (openai.ChatCompletionToolUnionParam, error) {
			if err := validateConstrainedSampling(descriptor, strictSupported); err != nil {
				return openai.ChatCompletionToolUnionParam{}, fmt.Errorf("tool %d constrained sampling: %w", index, err)
			}
			var schema map[string]any
			if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
				return openai.ChatCompletionToolUnionParam{}, fmt.Errorf(
					"tool %d has invalid input schema: %w",
					index,
					err,
				)
			}
			definition := shared.FunctionDefinitionParam{
				Name:        descriptor.Name,
				Strict:      param.Opt[bool]{},
				Description: param.NewOpt(descriptor.Description),
				Parameters:  schema,
			}
			if strictSupported {
				definition.Strict = param.NewOpt(true)
			}
			return openai.ChatCompletionFunctionTool(definition), nil
		},
	)
}

func dataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}
