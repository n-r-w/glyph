package compatible

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// chatToolState joins fragmented tool-call identity and arguments by choice index.
type chatToolState struct {
	id        string
	name      string
	arguments strings.Builder
	position  int
	started   bool
}

// chatAccumulator keeps provider ordering while translating deltas into semantic stream events.
type chatAccumulator struct {
	content           []model.Content
	textPosition      int
	refusalPosition   int
	reasoningPosition int
	parseReasoning    bool
	tools             map[int64]*chatToolState
	responseID        string
	responseModel     string
	usage             mo.Option[model.Usage]
	outcome           model.Outcome
}

func newChatAccumulator(parseReasoning bool) *chatAccumulator {
	return &chatAccumulator{
		content:           nil,
		textPosition:      -1,
		refusalPosition:   -1,
		reasoningPosition: -1,
		parseReasoning:    parseReasoning,
		tools:             make(map[int64]*chatToolState),
		responseID:        "",
		responseModel:     "",
		usage:             mo.None[model.Usage](),
		outcome:           0,
	}
}

func (s *Driver) streamChatCompletions(
	ctx context.Context,
	request run.ModelRequest,
	configuredModel modelConfig,
	key string,
	handle run.StreamHandler,
) (model.Response, error) {
	nativeReasoning := chatNativeReasoning(configuredModel.reasoningWireFormat)
	params, err := chatParams(request, configuredModel.reasoningWireFormat)
	if err != nil {
		return model.Response{}, err
	}
	opts := []option.RequestOption{
		option.WithBaseURL(s.baseURL),
		option.WithHTTPClient(s.httpClient),
		option.WithMaxRetries(0),
	}
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	service := openai.NewChatCompletionService(opts...)
	stream := service.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	state := newChatAccumulator(nativeReasoning)
	for stream.Next() {
		consumeErr := state.consume(stream.Current(), handle)
		if consumeErr != nil {
			return model.Response{}, handlerError(consumeErr)
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		if closeErr := state.finishContent(handle); closeErr != nil {
			return model.Response{}, handlerError(closeErr)
		}
		return model.Response{}, streamErr
	}
	if state.outcome == 0 {
		if closeErr := state.finishContent(handle); closeErr != nil {
			return model.Response{}, handlerError(closeErr)
		}
		return model.Response{}, errors.New("chat completions stream ended without a finish reason")
	}
	if finishErr := state.finish(handle); finishErr != nil {
		return model.Response{}, handlerError(finishErr)
	}
	return state.response(), nil
}

func chatParams(request run.ModelRequest, reasoningWireFormat string) (openai.ChatCompletionNewParams, error) {
	messages, err := chatMessages(request, chatNativeReasoning(reasoningWireFormat))
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
	if reasoningWireFormat == reasoningWireFormatOpenAIChatEffort {
		switch request.ReasoningChoice {
		case "", model.ReasoningChoiceOn:
		case model.ReasoningChoiceOff:
			params.ReasoningEffort = shared.ReasoningEffort("none")
		case model.ReasoningChoiceMinimal, model.ReasoningChoiceLow, model.ReasoningChoiceMedium,
			model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
			params.ReasoningEffort = shared.ReasoningEffort(request.ReasoningChoice)
		}
	}
	return params, nil
}

// chatNativeReasoning identifies formats that share Chat reasoning stream and history fields.
func chatNativeReasoning(reasoningWireFormat string) bool {
	return reasoningWireFormat == reasoningWireFormatOpenAIChatEffort ||
		reasoningWireFormat == reasoningWireFormatOllamaOrnith
}

func chatMessages(request run.ModelRequest, nativeReasoning bool) ([]openai.ChatCompletionMessageParamUnion, error) {
	messages := []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(request.Instructions)}
	for entryIndex := range request.History {
		entry := &request.History[entryIndex]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			content, err := chatUserContent(entry.User)
			if err != nil {
				return nil, err
			}
			messages = append(messages, openai.UserMessage(content))
		case agent.HistoryEntryModel:
			message, ok, err := chatAssistantMessage(entry.Model, nativeReasoning)
			if err != nil {
				return nil, err
			}
			if ok {
				messages = append(messages, message)
			}
		case agent.HistoryEntryToolResult:
			content, err := chatToolResult(entry.ToolResult.Contents)
			if err != nil {
				return nil, err
			}
			messages = append(messages, openai.ToolMessage(content, entry.ToolResult.CallID))
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
			var empty openai.ChatCompletionContentPartUnionParam
			switch item.Kind {
			case model.InputContentText:
				return openai.TextContentPart(item.Text.OrEmpty()), nil
			case model.InputContentImage:
				mediaType := item.MediaType.OrEmpty()
				data := item.Data.OrEmpty()
				if mediaType == "" || len(data) == 0 {
					return empty, fmt.Errorf("user image %d requires media type and data", index)
				}
				return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL:    dataURL(mediaType, data),
					Detail: "",
				}), nil
			default:
				return empty, fmt.Errorf("unsupported user content kind %d", item.Kind)
			}
		},
	)
}

func chatAssistantMessage(
	response model.Response,
	nativeReasoning bool,
) (openai.ChatCompletionMessageParamUnion, bool, error) {
	var text strings.Builder
	var reasoning strings.Builder
	var refusal string
	calls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0)
	for index := range response.Content {
		item := &response.Content[index]
		switch item.Kind {
		case model.ContentText:
			text.WriteString(item.Text.OrEmpty())
		case model.ContentRefusal:
			refusal += item.Text.OrEmpty()
		case model.ContentReasoning:
			visibleText := item.Text.OrEmpty()
			if visibleText == "" {
				continue
			}
			if nativeReasoning {
				reasoning.WriteString(visibleText)
			} else {
				text.WriteString(visibleText)
			}
		case model.ContentToolCall:
			call := item.ToolCall.OrEmpty()
			arguments, err := json.Marshal(call.Arguments)
			if err != nil {
				return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("encode tool-call arguments: %w", err)
			}
			calls = append(calls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: call.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Arguments: string(arguments),
						Name:      call.Name,
					},
					Type: "",
				},
				OfCustom: nil,
			})
		default:
			return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("unsupported model content kind %d", item.Kind)
		}
	}
	if text.Len() == 0 && reasoning.Len() == 0 && refusal == "" && len(calls) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false, nil
	}
	message := openai.AssistantMessage(text.String())
	message.OfAssistant.ToolCalls = calls
	if reasoning.Len() > 0 {
		message.OfAssistant.SetExtraFields(map[string]any{"reasoning": reasoning.String()})
	}
	if refusal != "" {
		message.OfAssistant.Refusal = param.NewOpt(refusal)
	}
	return message, true, nil
}

func chatToolResult(contents []tool.ResultContent) (string, error) {
	parts, err := lo.MapErr(contents, func(content tool.ResultContent, index int) (string, error) {
		switch content.Kind {
		case tool.ResultContentText:
			return content.Text.OrEmpty(), nil
		case tool.ResultContentImage:
			image := content.Image.OrEmpty()
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
			var schema map[string]any
			if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
				return openai.ChatCompletionToolUnionParam{}, fmt.Errorf("tool %d has invalid input schema: %w", index, err)
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

func (state *chatAccumulator) consume(chunk openai.ChatCompletionChunk, handle run.StreamHandler) error {
	if chunk.ID != "" {
		state.responseID = chunk.ID
	}
	if chunk.Model != "" {
		state.responseModel = chunk.Model
	}
	if chunk.JSON.Usage.Valid() {
		state.usage = mo.Some(model.Usage{
			InputTokens:       chunk.Usage.PromptTokens,
			OutputTokens:      chunk.Usage.CompletionTokens,
			CachedInputTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
			CacheWriteTokens:  0,
			ReasoningTokens:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			TotalTokens:       chunk.Usage.TotalTokens,
		})
	}
	for choiceIndex := range chunk.Choices {
		if err := state.consumeChoice(&chunk.Choices[choiceIndex], handle); err != nil {
			return err
		}
	}
	return nil
}

func (state *chatAccumulator) consumeChoice(
	choice *openai.ChatCompletionChunkChoice,
	handle run.StreamHandler,
) error {
	if state.parseReasoning {
		reasoning, err := chatReasoningDelta(choice.Delta)
		if err != nil {
			return err
		}
		if reasoning != "" {
			if deltaErr := state.contentDelta(model.ContentReasoning, reasoning, handle); deltaErr != nil {
				return deltaErr
			}
		}
	}
	if choice.Delta.Content != "" {
		if err := state.contentDelta(model.ContentText, choice.Delta.Content, handle); err != nil {
			return err
		}
	}
	if choice.Delta.Refusal != "" {
		if err := state.contentDelta(model.ContentRefusal, choice.Delta.Refusal, handle); err != nil {
			return err
		}
	}
	for deltaIndex := range choice.Delta.ToolCalls {
		if err := state.toolDelta(&choice.Delta.ToolCalls[deltaIndex], handle); err != nil {
			return err
		}
	}
	switch choice.FinishReason {
	case "":
	case "stop", "content_filter":
		state.outcome = model.OutcomeStop
	case "tool_calls", "function_call":
		state.outcome = model.OutcomeToolUse
	case "length":
		state.outcome = model.OutcomeLength
	default:
		return fmt.Errorf("unsupported Chat Completions finish reason %q", choice.FinishReason)
	}
	return nil
}

func chatReasoningDelta(delta openai.ChatCompletionChunkChoiceDelta) (string, error) {
	field, ok := delta.JSON.ExtraFields["reasoning"]
	if !ok {
		return "", nil
	}
	var reasoning string
	if err := json.Unmarshal([]byte(field.Raw()), &reasoning); err != nil {
		return "", fmt.Errorf("decode Chat Completions reasoning delta: %w", err)
	}
	return reasoning, nil
}

func (state *chatAccumulator) contentDelta(kind model.ContentKind, delta string, handle run.StreamHandler) error {
	position, positionErr := state.contentPosition(kind)
	if positionErr != nil {
		return positionErr
	}
	if *position < 0 {
		*position = len(state.content)
		state.content = append(state.content, model.Content{
			Kind:            kind,
			Text:            mo.Some(""),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		})
		startEvent := run.StreamEvent{
			Kind:     run.StreamEventContentStart,
			Position: *position,
			Content: model.Content{
				Kind:            kind,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
			Delta:    "",
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: model.Response{},
		}
		if handleErr := handle(startEvent); handleErr != nil {
			return handleErr
		}
	}
	state.content[*position].Text = mo.Some(state.content[*position].Text.OrEmpty() + delta)
	return handle(run.StreamEvent{
		Kind:     run.StreamEventTextDelta,
		Position: *position,
		Content: model.Content{
			Kind:            kind,
			Text:            mo.Some(delta),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
		Delta:    delta,
		Preview:  model.ToolCallPreview{},
		ToolCall: model.ToolCall{},
		Response: model.Response{},
	})
}

func (state *chatAccumulator) contentPosition(kind model.ContentKind) (*int, error) {
	switch kind {
	case model.ContentText:
		return &state.textPosition, nil
	case model.ContentRefusal:
		return &state.refusalPosition, nil
	case model.ContentReasoning:
		return &state.reasoningPosition, nil
	case model.ContentToolCall:
		return nil, errors.New("tool call cannot use a text delta")
	}
	return nil, fmt.Errorf("unsupported Chat Completions content kind %d", kind)
}

func (state *chatAccumulator) toolDelta(
	delta *openai.ChatCompletionChunkChoiceDeltaToolCall,
	handle run.StreamHandler,
) error {
	toolState, ok := state.tools[delta.Index]
	if !ok {
		toolState = &chatToolState{
			id:        "",
			name:      "",
			arguments: strings.Builder{},
			position:  len(state.content),
			started:   false,
		}
		state.tools[delta.Index] = toolState
		state.content = append(state.content, model.Content{
			Kind:            model.ContentToolCall,
			Text:            mo.None[string](),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		})
	}
	if delta.ID != "" {
		toolState.id = delta.ID
	}
	if delta.Function.Name != "" {
		toolState.name = delta.Function.Name
	}
	toolState.arguments.WriteString(delta.Function.Arguments)
	if toolState.id == "" || toolState.name == "" {
		return nil
	}
	preview := model.ToolCallPreview{
		CallID:      toolState.id,
		Name:        toolState.name,
		Position:    toolState.position,
		Provisional: true,
		Fields:      nil,
	}
	kind := run.StreamEventToolCallDelta
	if !toolState.started {
		kind = run.StreamEventToolCallStart
		toolState.started = true
	}
	return handle(run.StreamEvent{
		Kind:     kind,
		Position: toolState.position,
		Content:  model.Content{},
		Delta:    "",
		Preview:  preview,
		ToolCall: model.ToolCall{},
		Response: model.Response{},
	})
}

func (state *chatAccumulator) finish(handle run.StreamHandler) error {
	if err := state.finishContent(handle); err != nil {
		return err
	}
	for index := int64(0); index < int64(len(state.tools)); index++ {
		toolState, ok := state.tools[index]
		if !ok || !toolState.started {
			return errors.New("chat Completions returned an incomplete tool call")
		}
		var arguments map[string]any
		if err := json.Unmarshal([]byte(toolState.arguments.String()), &arguments); err != nil {
			return errors.New("chat Completions returned invalid tool-call arguments")
		}
		call := model.ToolCall{
			ID:        toolState.id,
			Name:      toolState.name,
			Arguments: arguments,
		}
		state.content[toolState.position] = model.Content{
			Kind:            model.ContentToolCall,
			Text:            mo.None[string](),
			Final:           true,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.Some(call),
		}
		if err := handle(run.StreamEvent{
			Kind:     run.StreamEventToolCallEnd,
			Position: toolState.position,
			Content:  model.Content{},
			Delta:    "",
			Preview:  model.ToolCallPreview{},
			ToolCall: call,
			Response: model.Response{},
		}); err != nil {
			return err
		}
	}
	return nil
}

// finishContent closes streamed text before any terminal event reaches Agent Core.
func (state *chatAccumulator) finishContent(handle run.StreamHandler) error {
	for position := range state.content {
		kind := state.content[position].Kind
		if (kind != model.ContentText && kind != model.ContentRefusal && kind != model.ContentReasoning) ||
			state.content[position].Final {
			continue
		}
		state.content[position].Final = true
		if err := handle(run.StreamEvent{
			Kind:     run.StreamEventContentEnd,
			Position: position,
			Content: model.Content{
				Kind:            kind,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
			Delta:    "",
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: model.Response{},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (state *chatAccumulator) response() model.Response {
	return model.Response{
		Content:       state.content,
		Outcome:       mo.Some(state.outcome),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.EmptyableToOption(model.ID(state.responseModel)),
		ResponseID:    mo.EmptyableToOption(state.responseID),
		Usage:         state.usage,
		Diagnostics:   nil,
	}
}
