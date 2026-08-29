package compatible

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/openai/openai-go/v3/responses"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type responseContext struct {
	// ID identifies the provider reasoning item.
	ID string `json:"id"`
	// EncryptedContent contains opaque provider replay state.
	EncryptedContent string `json:"encrypted_content"`
	// Summary contains provider reasoning summary fragments.
	Summary []string `json:"summary"`
}

// responsesToolState correlates SDK item IDs with stable Agent Core call IDs.
type responsesToolState struct {
	// itemID identifies the provider output item.
	itemID string
	// callID identifies the provider tool call.
	callID string
	// name identifies the requested tool.
	name string
	// position identifies the compact response content order.
	position int
	// arguments accumulates streamed tool input.
	arguments strings.Builder
	// started reports whether the tool lifecycle is open.
	started bool
}

// responsesAccumulator assigns compact semantic positions independent of sparse provider indexes.
type responsesAccumulator struct {
	// handle receives provider-neutral stream events.
	handle run.StreamHandler
	// positions maps provider item keys to compact content positions.
	positions map[string]int
	// active contains open content lifecycles by compact position.
	active map[int]model.ContentKind
	// tools contains active tool calls by provider item ID.
	tools map[string]*responsesToolState
	// next is the next compact response content position.
	next int
	// terminal contains the authoritative terminal response.
	terminal *model.Response
}

func newResponsesAccumulator(handle run.StreamHandler) *responsesAccumulator {
	return &responsesAccumulator{
		handle:    handle,
		positions: make(map[string]int),
		active:    make(map[int]model.ContentKind),
		tools:     make(map[string]*responsesToolState),
		next:      0,
		terminal:  nil,
	}
}

func (s *Driver) streamResponses(
	ctx context.Context,
	request run.ModelRequest,
	key string,
	handle run.StreamHandler,
) (model.Response, error) {
	configured := s.models[request.Model.Model]
	target := model.ProviderContextSource{
		ProviderID:       s.providerID,
		API:              string(configured.api),
		Model:            request.Model.Model,
		CompatibilityKey: configured.reasoningCompatibilityKey,
	}
	params, err := responsesParams(request, target)
	if err != nil {
		return model.Response{}, err
	}
	opts := s.requestOptions(key)
	service := responses.NewResponseService(opts...)
	stream := service.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	state := newResponsesAccumulator(tagHandlerErrors(handle))
	for stream.Next() {
		consumeErr := state.consume(stream.Current(), s.providerID)
		if consumeErr != nil {
			return model.Response{}, consumeErr
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		if closeErr := state.finishContent(); closeErr != nil {
			return model.Response{}, errors.Join(streamErr, closeErr)
		}
		return model.Response{}, streamErr
	}
	if state.terminal == nil {
		if closeErr := state.finishContent(); closeErr != nil {
			return model.Response{}, closeErr
		}
		return model.Response{}, errors.New("responses stream ended without a terminal response")
	}
	if finishErr := state.finish(); finishErr != nil {
		return model.Response{}, finishErr
	}
	for index := range state.terminal.Content {
		content := &state.terminal.Content[index]
		providerContext, ok := content.ProviderContext.Get()
		if content.Kind == model.ContentReasoning && ok && len(providerContext.Payload) != 0 {
			providerContext.Source = target
			content.ProviderContext = mo.Some(providerContext)
		}
	}
	if terminalErr := responsesTerminalError(*state.terminal); terminalErr != nil {
		return *state.terminal, terminalErr
	}
	return *state.terminal, nil
}

//nolint:gocyclo // The branches map the closed Responses stream union.
func (state *responsesAccumulator) consume(
	event responses.ResponseStreamEventUnion,
	providerID model.ProviderID,
) error {
	switch event.Type {
	case "response.output_text.delta":
		delta := event.AsResponseOutputTextDelta()
		key := responseContentKey("text", delta.OutputIndex, delta.ContentIndex)
		return state.contentDelta(key, model.ContentText, delta.Delta)
	case "response.refusal.delta":
		delta := event.AsResponseRefusalDelta()
		key := responseContentKey("refusal", delta.OutputIndex, delta.ContentIndex)
		return state.contentDelta(key, model.ContentRefusal, delta.Delta)
	case responseReasoningSummaryDelta, "response.reasoning_text.delta":
		var outputIndex int64
		var delta string
		if event.Type == responseReasoningSummaryDelta {
			value := event.AsResponseReasoningSummaryTextDelta()
			outputIndex, delta = value.OutputIndex, value.Delta
		} else {
			value := event.AsResponseReasoningTextDelta()
			outputIndex, delta = value.OutputIndex, value.Delta
		}
		return state.contentDelta("reasoning:"+strconv.FormatInt(outputIndex, 10), model.ContentReasoning, delta)
	case "response.output_item.added":
		added := event.AsResponseOutputItemAdded()
		if added.Item.Type == responseItemTypeFunctionCall {
			call := added.Item.AsFunctionCall()
			position := state.allocate("tool:" + call.CallID)
			toolState := &responsesToolState{
				itemID:    added.Item.ID,
				callID:    call.CallID,
				name:      call.Name,
				position:  position,
				arguments: strings.Builder{},
				started:   true,
			}
			state.tools[call.CallID] = toolState
			return state.handle(run.StreamEvent{
				Kind:     run.StreamEventToolCallStart,
				Position: mo.Some(position),
				Content:  mo.None[model.Content](),
				Delta:    mo.None[string](),
				Preview: mo.Some(model.ToolCallPreview{
					CallID:      call.CallID,
					Name:        call.Name,
					Position:    position,
					Provisional: true,
					Fields:      nil,
				}),
				ToolCall: mo.None[model.ToolCall](),
				Response: mo.None[model.Response](),
			})
		}
	case "response.function_call_arguments.delta":
		delta := event.AsResponseFunctionCallArgumentsDelta()
		toolState := state.toolByItem(delta.ItemID)
		if toolState == nil {
			return errors.New("responses returned arguments before a tool call")
		}
		toolState.arguments.WriteString(delta.Delta)
		return state.handle(run.StreamEvent{
			Kind:     run.StreamEventToolCallDelta,
			Position: mo.Some(toolState.position),
			Content:  mo.None[model.Content](),
			Delta:    mo.None[string](),
			Preview: mo.Some(model.ToolCallPreview{
				CallID:      toolState.callID,
				Name:        toolState.name,
				Position:    toolState.position,
				Provisional: true,
				Fields:      nil,
			}),
			ToolCall: mo.None[model.ToolCall](),
			Response: mo.None[model.Response](),
		})
	case "response.function_call_arguments.done":
		done := event.AsResponseFunctionCallArgumentsDone()
		toolState := state.toolByItem(done.ItemID)
		if toolState == nil {
			return errors.New("responses completed an unknown tool call")
		}
		return state.finishTool(toolState, done.Arguments, done.Name)
	case "response.completed":
		response, err := responsesModelResponse(event.AsResponseCompleted().Response, providerID, model.OutcomeStop)
		if err != nil {
			return err
		}
		state.terminal = &response
	case "response.incomplete":
		response, err := responsesModelResponse(event.AsResponseIncomplete().Response, providerID, model.OutcomeLength)
		if err != nil {
			return err
		}
		state.terminal = &response
	case "response.failed":
		failed := event.AsResponseFailed().Response
		message := requestFailedMessage
		if providerMessage := strings.TrimSpace(failed.Error.Message); providerMessage != "" {
			message = "responses request failed: " + providerMessage
		}
		response := failureResponse(model.OutcomeFailed, message)
		state.terminal = &response
	}
	return nil
}

func (state *responsesAccumulator) contentDelta(key string, kind model.ContentKind, delta string) error {
	position, ok := state.positions[key]
	if !ok {
		position = state.allocate(key)
		state.active[position] = kind
		if err := state.handle(textStreamEvent(
			run.StreamEventContentStart, position, kind, "", mo.None[string](),
		)); err != nil {
			return err
		}
	}
	return state.handle(textStreamEvent(
		run.StreamEventTextDelta, position, kind, delta, mo.Some(delta),
	))
}

func (state *responsesAccumulator) allocate(key string) int {
	position := state.next
	state.next++
	state.positions[key] = position
	return position
}

func (state *responsesAccumulator) toolByItem(itemID string) *responsesToolState {
	for _, toolState := range state.tools {
		if toolState.itemID == itemID {
			return toolState
		}
	}
	return nil
}

func (state *responsesAccumulator) finishTool(toolState *responsesToolState, arguments, name string) error {
	if arguments == "" {
		arguments = toolState.arguments.String()
	}
	if name == "" {
		name = toolState.name
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(arguments), &decoded); err != nil {
		return fmt.Errorf("decode Responses tool-call arguments: %w", err)
	}
	call := model.ToolCall{
		ID:        toolState.callID,
		Name:      name,
		Arguments: decoded,
	}
	toolState.started = false
	return state.handle(toolCallEndStreamEvent(toolState.position, call))
}

func (state *responsesAccumulator) finish() error {
	if err := state.finishContent(); err != nil {
		return err
	}
	for position := 0; position < state.next; position++ {
		for _, toolState := range state.tools {
			if toolState.started && toolState.position == position {
				if err := state.finishTool(toolState, toolState.arguments.String(), toolState.name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// finishContent closes active text blocks in their assigned provider order.
func (state *responsesAccumulator) finishContent() error {
	for position := 0; position < state.next; position++ {
		kind, active := state.active[position]
		if !active {
			continue
		}
		if err := state.handle(textStreamEvent(
			run.StreamEventContentEnd, position, kind, "", mo.None[string](),
		)); err != nil {
			return err
		}
		delete(state.active, position)
	}
	return nil
}

func responseContentKey(kind string, outputIndex, contentIndex int64) string {
	return kind + ":" + strconv.FormatInt(outputIndex, 10) + ":" + strconv.FormatInt(contentIndex, 10)
}

// responsesModelResponse builds the authoritative terminal snapshot from completed output items.
func responsesModelResponse(
	response responses.Response,
	providerID model.ProviderID,
	defaultOutcome model.Outcome,
) (model.Response, error) {
	content := make([]model.Content, 0, len(response.Output))
	hasToolCall := false
	for outputIndex := range response.Output {
		output := &response.Output[outputIndex]
		switch output.Type {
		case "message":
			message := output.AsMessage()
			for partIndex := range message.Content {
				part := &message.Content[partIndex]
				switch part.Type {
				case "output_text":
					content = append(content, model.Content{
						Kind:            model.ContentText,
						Text:            mo.Some(part.AsOutputText().Text),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
					})
				case "refusal":
					content = append(content, model.Content{
						Kind:            model.ContentRefusal,
						Text:            mo.Some(part.AsRefusal().Refusal),
						Final:           true,
						ProviderContext: mo.None[model.ProviderContext](),
						ToolCall:        mo.None[model.ToolCall](),
					})
				default:
					return model.Response{}, fmt.Errorf("responses returned unsupported message content %q", part.Type)
				}
			}
		case responseItemTypeReasoning:
			reasoning := output.AsReasoning()
			summary := lo.Map(reasoning.Summary, func(item responses.ResponseReasoningItemSummary, _ int) string {
				return item.Text
			})
			visible := model.Content{
				Kind:            model.ContentReasoning,
				Text:            mo.Some(strings.Join(summary, "")),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}
			// Only encrypted items with stable IDs can be replayed on the next stateless request.
			if reasoning.ID != "" && reasoning.EncryptedContent != "" {
				payload, err := json.Marshal(responseContext{
					ID:               reasoning.ID,
					EncryptedContent: reasoning.EncryptedContent,
					Summary:          summary,
				})
				if err != nil {
					return model.Response{}, fmt.Errorf("encode provider context: %w", err)
				}
				visible.ProviderContext = mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID:       providerID,
						API:              string(APIResponses),
						Model:            model.ID(response.Model),
						CompatibilityKey: mo.None[string](),
					},
					Payload: payload,
				})
			}
			content = append(content, visible)
		case responseItemTypeFunctionCall:
			call := output.AsFunctionCall()
			var arguments map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
				return model.Response{}, fmt.Errorf("decode Responses tool-call arguments: %w", err)
			}
			content = append(content, model.Content{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall: mo.Some(model.ToolCall{
					ID:        call.CallID,
					Name:      call.Name,
					Arguments: arguments,
				}),
			})
			hasToolCall = true
		default:
			return model.Response{}, fmt.Errorf("responses returned unsupported output item %q", output.Type)
		}
	}
	outcome := defaultOutcome
	if outcome == model.OutcomeStop && hasToolCall {
		outcome = model.OutcomeToolUse
	}
	usage := model.NormalizeUsage(model.Usage{
		InputTokens:       response.Usage.InputTokens,
		OutputTokens:      response.Usage.OutputTokens,
		CachedInputTokens: response.Usage.InputTokensDetails.CachedTokens,
		CacheWriteTokens:  response.Usage.InputTokensDetails.CacheWriteTokens,
		ReasoningTokens:   response.Usage.OutputTokensDetails.ReasoningTokens,
		TotalTokens:       response.Usage.TotalTokens,
	})
	responseUsage := mo.None[model.Usage]()
	if response.JSON.Usage.Valid() {
		responseUsage = mo.Some(usage)
	}
	return model.Response{
		Content:       content,
		Outcome:       mo.Some(outcome),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.EmptyableToOption(model.ID(response.Model)),
		ResponseID:    mo.EmptyableToOption(response.ID),
		Usage:         responseUsage,
		Diagnostics:   nil,
	}, nil
}
