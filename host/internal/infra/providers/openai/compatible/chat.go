package compatible

import (
	"context"

	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

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
	opts := s.requestOptions(key)
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
	case "tool_calls", responseItemTypeFunctionCall:
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
