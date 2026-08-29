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
	// id identifies the provider tool call.
	id string
	// name identifies the requested tool.
	name string
	// arguments accumulates streamed tool input.
	arguments strings.Builder
	// position identifies the compact response content order.
	position int
	// started reports whether the tool lifecycle is open.
	started bool
}

// chatAccumulator keeps provider ordering while translating deltas into semantic stream events.
type chatAccumulator struct {
	// content contains finalized provider-neutral response blocks.
	content []model.Content
	// textPosition identifies the visible text block.
	textPosition int
	// refusalPosition identifies the refusal text block.
	refusalPosition int
	// reasoningPosition identifies the reasoning text block.
	reasoningPosition int
	// reasoningFormat selects provider-specific reasoning extraction and replay.
	reasoningFormat reasoningFormat
	// reasoningDetails accumulates opaque OpenRouter replay data.
	reasoningDetails []reasoningDetail
	// reasoningTarget identifies the request contract that can replay opaque details.
	reasoningTarget model.ProviderContextSource
	// tools contains active tool calls by provider index.
	tools map[int64]*chatToolState
	// responseID identifies the provider response.
	responseID string
	// responseModel identifies the model reported by the provider.
	responseModel string
	// usage contains provider-reported token accounting.
	usage mo.Option[model.Usage]
	// outcome identifies why the response ended.
	outcome model.Outcome
}

// newChatAccumulator creates empty state for one Chat Completions stream.
func newChatAccumulator(format reasoningFormat, target model.ProviderContextSource) *chatAccumulator {
	return &chatAccumulator{
		content:           nil,
		textPosition:      -1,
		refusalPosition:   -1,
		reasoningPosition: -1,
		reasoningFormat:   format,
		reasoningDetails:  nil,
		reasoningTarget:   target,
		tools:             make(map[int64]*chatToolState),
		responseID:        "",
		responseModel:     "",
		usage:             mo.None[model.Usage](),
		outcome:           0,
	}
}

// streamChatCompletions maps one Chat Completions stream into provider-neutral events.
func (s *Driver) streamChatCompletions(
	ctx context.Context,
	request run.ModelRequest,
	configuredModel modelConfig,
	key string,
	handle run.StreamHandler,
) (model.Response, error) {
	target := model.ProviderContextSource{
		ProviderID:       s.providerID,
		API:              string(configuredModel.api),
		Model:            request.Model.Model,
		CompatibilityKey: configuredModel.reasoningCompatibilityKey,
	}
	params, err := chatParams(request, configuredModel.reasoningFormat, target)
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
	state := newChatAccumulator(configuredModel.reasoningFormat, target)
	handleEvent := tagHandlerErrors(handle)
	for stream.Next() {
		consumeErr := state.consume(stream.Current(), handleEvent)
		if consumeErr != nil {
			return model.Response{}, consumeErr
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		if closeErr := state.finishContent(handleEvent); closeErr != nil {
			return model.Response{}, errors.Join(streamErr, closeErr)
		}
		return model.Response{}, streamErr
	}
	if state.outcome == 0 {
		if closeErr := state.finishContent(handleEvent); closeErr != nil {
			return model.Response{}, closeErr
		}
		return model.Response{}, errors.New("chat completions stream ended without a finish reason")
	}
	if finishErr := state.finish(handleEvent); finishErr != nil {
		return model.Response{}, finishErr
	}
	return state.response(), nil
}

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
				return openai.ChatCompletionContentPartUnionParam{}, fmt.Errorf("unsupported user content kind %d", item.Kind)
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
	var reasoningDetails []json.RawMessage
	calls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0)
	for index := range response.Content {
		item := &response.Content[index]
		switch item.Kind {
		case model.ContentText:
			value, present := item.Text.Get()
			if !present {
				return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("model content %d has no text", index)
			}
			text.WriteString(value)
		case model.ContentRefusal:
			value, present := item.Text.Get()
			if !present {
				return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("model content %d has no text", index)
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
			if usesChatReasoning(format) {
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
			return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("unsupported model content kind %d", item.Kind)
		}
	}
	return buildChatAssistantMessage(text.String(), reasoning.String(), reasoningDetails, refusal.String(), calls)
}

// buildChatAssistantMessage creates the external assistant union from accumulated content.
func buildChatAssistantMessage(
	text string,
	reasoning string,
	reasoningDetails []json.RawMessage,
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
		message.OfAssistant.SetExtraFields(map[string]any{"reasoning": reasoning})
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
		state.usage = mo.Some(model.NormalizeUsage(model.Usage{
			InputTokens:       chunk.Usage.PromptTokens,
			OutputTokens:      chunk.Usage.CompletionTokens,
			CachedInputTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
			CacheWriteTokens:  0,
			ReasoningTokens:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			TotalTokens:       chunk.Usage.TotalTokens,
		}))
	}
	for choiceIndex := range chunk.Choices {
		if err := state.consumeChoice(&chunk.Choices[choiceIndex], handle); err != nil {
			return err
		}
	}
	return nil
}

// consumeChoice accumulates one streamed choice and emits its visible deltas.
func (state *chatAccumulator) consumeChoice(
	choice *openai.ChatCompletionChunkChoice,
	handle run.StreamHandler,
) error {
	reasoning, err := chatReasoningDelta(state.reasoningFormat, choice.Delta)
	if err != nil {
		return err
	}
	if reasoning != "" {
		if deltaErr := state.contentDelta(model.ContentReasoning, reasoning, handle); deltaErr != nil {
			return deltaErr
		}
	}
	details, err := openRouterReasoningDetailsDelta(state.reasoningFormat, choice.Delta)
	if err != nil {
		return err
	}
	state.reasoningDetails = appendOpenRouterReasoningDetails(state.reasoningDetails, details)
	if choice.Delta.Content != "" {
		if contentErr := state.contentDelta(model.ContentText, choice.Delta.Content, handle); contentErr != nil {
			return contentErr
		}
	}
	if choice.Delta.Refusal != "" {
		if refusalErr := state.contentDelta(model.ContentRefusal, choice.Delta.Refusal, handle); refusalErr != nil {
			return refusalErr
		}
	}
	for deltaIndex := range choice.Delta.ToolCalls {
		if toolErr := state.toolDelta(&choice.Delta.ToolCalls[deltaIndex], handle); toolErr != nil {
			return toolErr
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
			Position: mo.PointerToOption(position),
			Content: mo.Some(model.Content{
				Kind:            kind,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}),
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
		}
		if handleErr := handle(startEvent); handleErr != nil {
			return handleErr
		}
	}
	text, present := state.content[*position].Text.Get()
	if !present {
		return fmt.Errorf("model content %d has no accumulated text", *position)
	}
	state.content[*position].Text = mo.Some(text + delta)
	return handle(run.StreamEvent{
		Kind:     run.StreamEventTextDelta,
		Position: mo.PointerToOption(position),
		Content: mo.Some(model.Content{
			Kind:            kind,
			Text:            mo.Some(delta),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		}),
		Delta:    mo.Some(delta),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
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
		Position: mo.Some(toolState.position),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.Some(preview),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	})
}

func (state *chatAccumulator) finish(handle run.StreamHandler) error {
	if err := state.finishContent(handle); err != nil {
		return err
	}
	if err := state.attachReasoningDetails(); err != nil {
		return err
	}
	for index := int64(0); index < int64(len(state.tools)); index++ {
		toolState, ok := state.tools[index]
		if !ok || !toolState.started {
			return errors.New("chat Completions returned an incomplete tool call")
		}
		var arguments map[string]any
		if err := json.Unmarshal([]byte(toolState.arguments.String()), &arguments); err != nil {
			return fmt.Errorf("decode chat Completions tool-call arguments: %w", err)
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
			Position: mo.Some(toolState.position),
			Content:  mo.None[model.Content](),
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.Some(call),
			Response: mo.None[model.Response](),
		}); err != nil {
			return err
		}
	}
	return nil
}

// attachReasoningDetails stores OpenRouter replay data on the provider-neutral reasoning block.
func (state *chatAccumulator) attachReasoningDetails() error {
	if len(state.reasoningDetails) == 0 {
		return nil
	}
	providerContext, err := openRouterProviderContext(state.reasoningDetails, state.reasoningTarget)
	if err != nil {
		return err
	}
	if state.reasoningPosition >= 0 {
		state.content[state.reasoningPosition].ProviderContext = mo.Some(providerContext)
		return nil
	}
	state.content = append(state.content, model.Content{
		Kind:            model.ContentReasoning,
		Text:            mo.Some(""),
		Final:           true,
		ProviderContext: mo.Some(providerContext),
		ToolCall:        mo.None[model.ToolCall](),
	})
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
			Position: mo.Some(position),
			Content: mo.Some(model.Content{
				Kind:            kind,
				Text:            mo.Some(""),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}),
			Delta:    mo.None[string](),
			Preview:  mo.None[model.ToolCallPreview](),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
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
