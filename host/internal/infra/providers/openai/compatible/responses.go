//nolint:exhaustruct // Provider mappings set only fields supported by Agent Core.
package compatible

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type responseContext struct {
	ID               string   `json:"id"`
	EncryptedContent string   `json:"encrypted_content"`
	Summary          []string `json:"summary"`
}

// responsesToolState correlates SDK item IDs with stable Agent Core call IDs.
type responsesToolState struct {
	itemID    string
	callID    string
	name      string
	position  int
	arguments strings.Builder
	started   bool
}

// responsesAccumulator assigns compact semantic positions independent of sparse provider indexes.
type responsesAccumulator struct {
	handle    run.StreamHandler
	positions map[string]int
	active    map[int]model.ContentKind
	tools     map[string]*responsesToolState
	next      int
	terminal  *model.Response
}

func newResponsesAccumulator(handle run.StreamHandler) *responsesAccumulator {
	return &responsesAccumulator{
		handle: handle, positions: make(map[string]int), active: make(map[int]model.ContentKind),
		tools: make(map[string]*responsesToolState),
	}
}

func (s *Service) streamResponses(
	ctx context.Context,
	request run.ModelRequest,
	key string,
	handle run.StreamHandler,
) (model.Response, error) {
	params, err := responsesParams(request, s.providerID)
	if err != nil {
		return model.Response{}, err
	}
	opts := []option.RequestOption{
		option.WithBaseURL(s.baseURL), option.WithHTTPClient(s.httpClient), option.WithMaxRetries(0),
	}
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	service := responses.NewResponseService(opts...)
	stream := service.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()
	state := newResponsesAccumulator(handle)
	for stream.Next() {
		consumeErr := state.consume(stream.Current(), s.providerID)
		if consumeErr != nil {
			return model.Response{}, handlerError(consumeErr)
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		if closeErr := state.finishContent(); closeErr != nil {
			return model.Response{}, handlerError(closeErr)
		}
		return model.Response{}, streamErr
	}
	if state.terminal == nil {
		if closeErr := state.finishContent(); closeErr != nil {
			return model.Response{}, handlerError(closeErr)
		}
		return model.Response{}, errors.New("responses stream ended without a terminal response")
	}
	if finishErr := state.finish(); finishErr != nil {
		return model.Response{}, handlerError(finishErr)
	}
	if state.terminal.Outcome == model.OutcomeFailed {
		return *state.terminal, errors.New("responses request failed")
	}
	return *state.terminal, nil
}

func responsesParams(request run.ModelRequest, providerID model.ProviderID) (responses.ResponseNewParams, error) {
	input, err := responsesInput(request.History, providerID)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	tools, err := responsesTools(request.Tools, request.Model.ToolCapabilities.StrictJSONSchema)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	//nolint:exhaustruct // Optional SDK request fields intentionally use zero values.
	params := responses.ResponseNewParams{
		Model:             string(request.Model.Model),
		Instructions:      param.NewOpt(request.Instructions),
		Store:             param.NewOpt(false),
		ParallelToolCalls: param.NewOpt(false),
		Include:           []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent},
		Input:             responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Tools:             tools,
	}
	if request.ReasoningLevel != "" && request.ReasoningLevel != model.ReasoningLevelNone {
		params.Reasoning.Effort = shared.ReasoningEffort(request.ReasoningLevel)
	}
	return params, nil
}

func responsesInput(history []agent.HistoryEntry, providerID model.ProviderID) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(history))
	for entryIndex := range history {
		entry := &history[entryIndex]
		switch entry.Kind {
		case agent.HistoryEntryUser:
			message, err := responsesUserMessage(entry.User)
			if err != nil {
				return nil, err
			}
			input = append(input, message)
		case agent.HistoryEntryModel:
			items, err := responsesModelItems(entry.Model, providerID)
			if err != nil {
				return nil, err
			}
			input = append(input, items...)
		case agent.HistoryEntryToolResult:
			output, err := responsesToolOutput(entry.ToolResult.Contents)
			if err != nil {
				return nil, err
			}
			input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(entry.ToolResult.CallID, output))
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
			content = append(content, responses.ResponseInputContentParamOfInputText(item.Text))
		case model.InputContentImage:
			if item.MediaType == "" || len(item.Data) == 0 {
				return responses.ResponseInputItemUnionParam{}, fmt.Errorf("user image %d requires media type and data", index)
			}
			image := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
			image.OfInputImage.ImageURL = param.NewOpt(dataURL(item.MediaType, item.Data))
			content = append(content, image)
		default:
			return responses.ResponseInputItemUnionParam{}, fmt.Errorf("unsupported user content kind %d", item.Kind)
		}
	}
	messageItem := responses.ResponseInputItemParamOfMessage(content, responses.EasyInputMessageRoleUser)
	messageItem.OfMessage.Type = responses.EasyInputMessageTypeMessage
	return messageItem, nil
}

func responsesModelItems(response model.Response, providerID model.ProviderID) (responses.ResponseInputParam, error) {
	items := make(responses.ResponseInputParam, 0, len(response.Content))
	for _, item := range response.Content {
		switch item.Kind {
		case model.ContentText, model.ContentRefusal:
			message := responses.ResponseInputItemParamOfMessage(item.Text, responses.EasyInputMessageRoleAssistant)
			message.OfMessage.Type = responses.EasyInputMessageTypeMessage
			items = append(items, message)
		case model.ContentReasoning:
			continue
		case model.ContentProviderContext:
			if item.ProviderContext.ProviderID != providerID {
				continue
			}
			reasoning, err := responsesReasoningItem(item.ProviderContext.Payload)
			if err != nil {
				return nil, err
			}
			items = append(items, reasoning)
		case model.ContentToolCall:
			arguments, err := json.Marshal(item.ToolCall.Arguments)
			if err != nil {
				return nil, fmt.Errorf("encode tool-call arguments: %w", err)
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(
				string(arguments), item.ToolCall.ID, item.ToolCall.Name,
			))
		default:
			return nil, fmt.Errorf("unsupported model content kind %d", item.Kind)
		}
	}
	return items, nil
}

func responsesReasoningItem(payload []byte) (responses.ResponseInputItemUnionParam, error) {
	var contextValue responseContext
	decodeErr := json.Unmarshal(payload, &contextValue)
	if decodeErr != nil || contextValue.ID == "" || contextValue.EncryptedContent == "" {
		return responses.ResponseInputItemUnionParam{}, errors.New("OpenAI-compatible provider context is malformed")
	}
	summary := make([]responses.ResponseReasoningItemSummaryParam, len(contextValue.Summary))
	for index, text := range contextValue.Summary {
		summary[index] = responses.ResponseReasoningItemSummaryParam{Text: text}
	}
	item := responses.ResponseInputItemParamOfReasoning(contextValue.ID, summary)
	item.OfReasoning.EncryptedContent = param.NewOpt(contextValue.EncryptedContent)
	return item, nil
}

func responsesToolOutput(contents []tool.ResultContent) (responses.ResponseFunctionCallOutputItemListParam, error) {
	output := make(responses.ResponseFunctionCallOutputItemListParam, 0, len(contents))
	for index, content := range contents {
		switch content.Kind {
		case tool.ResultContentText:
			output = append(output, responses.ResponseFunctionCallOutputItemParamOfInputText(content.Text))
		case tool.ResultContentImage:
			if content.Image.MediaType == "" || len(content.Image.Data) == 0 {
				return nil, fmt.Errorf("tool result image %d requires media type and data", index)
			}
			imageURL := dataURL(content.Image.MediaType, content.Image.Data)
			output = append(output, responses.ResponseFunctionCallOutputItemUnionParam{
				OfInputImage: &responses.ResponseInputImageContentParam{ImageURL: param.NewOpt(imageURL)},
			})
		default:
			return nil, fmt.Errorf("unsupported tool result content kind %d", content.Kind)
		}
	}
	return output, nil
}

func responsesTools(descriptors []tool.Descriptor, strictSupported bool) ([]responses.ToolUnionParam, error) {
	tools := make([]responses.ToolUnionParam, 0, len(descriptors))
	for index, descriptor := range descriptors {
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return nil, fmt.Errorf("tool %d has invalid input schema: %w", index, err)
		}
		toolParam := responses.ToolParamOfFunction(descriptor.Name, schema, strictSupported)
		toolParam.OfFunction.Description = param.NewOpt(descriptor.Description)
		tools = append(tools, toolParam)
	}
	return tools, nil
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
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		var outputIndex int64
		var delta string
		if event.Type == "response.reasoning_summary_text.delta" {
			value := event.AsResponseReasoningSummaryTextDelta()
			outputIndex, delta = value.OutputIndex, value.Delta
		} else {
			value := event.AsResponseReasoningTextDelta()
			outputIndex, delta = value.OutputIndex, value.Delta
		}
		return state.contentDelta("reasoning:"+strconv.FormatInt(outputIndex, 10), model.ContentReasoning, delta)
	case "response.output_item.added":
		added := event.AsResponseOutputItemAdded()
		if added.Item.Type == "function_call" {
			call := added.Item.AsFunctionCall()
			position := state.allocate("tool:" + call.CallID)
			toolState := &responsesToolState{
				itemID: added.Item.ID, callID: call.CallID, name: call.Name, position: position, started: true,
			}
			state.tools[call.CallID] = toolState
			return state.handle(run.StreamEvent{
				Kind: run.StreamEventToolCallStart, Position: position,
				Preview: model.ToolCallPreview{CallID: call.CallID, Name: call.Name, Position: position, Provisional: true},
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
			Kind: run.StreamEventToolCallDelta, Position: toolState.position,
			Preview: model.ToolCallPreview{
				CallID: toolState.callID, Name: toolState.name, Position: toolState.position, Provisional: true,
			},
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
		response := failureResponse(model.OutcomeFailed, requestFailedMessage)
		state.terminal = &response
	}
	return nil
}

func (state *responsesAccumulator) contentDelta(key string, kind model.ContentKind, delta string) error {
	position, ok := state.positions[key]
	if !ok {
		position = state.allocate(key)
		state.active[position] = kind
		if err := state.handle(run.StreamEvent{
			Kind: run.StreamEventContentStart, Position: position, Content: model.Content{Kind: kind},
		}); err != nil {
			return err
		}
	}
	return state.handle(run.StreamEvent{
		Kind: run.StreamEventTextDelta, Position: position, Content: model.Content{Kind: kind}, Delta: delta,
	})
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
		return errors.New("responses returned invalid tool-call arguments")
	}
	call := model.ToolCall{ID: toolState.callID, Name: name, Arguments: decoded}
	toolState.started = false
	return state.handle(run.StreamEvent{
		Kind: run.StreamEventToolCallEnd, Position: toolState.position, ToolCall: call,
	})
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
		if err := state.handle(run.StreamEvent{
			Kind: run.StreamEventContentEnd, Position: position, Content: model.Content{Kind: kind},
		}); err != nil {
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
//
//nolint:gocyclo // The branches map the closed Responses output union.
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
					content = append(content, model.Content{Kind: model.ContentText, Text: part.AsOutputText().Text, Final: true})
				case "refusal":
					content = append(content, model.Content{Kind: model.ContentRefusal, Text: part.AsRefusal().Refusal, Final: true})
				default:
					return model.Response{}, fmt.Errorf("responses returned unsupported message content %q", part.Type)
				}
			}
		case "reasoning":
			reasoning := output.AsReasoning()
			summary := make([]string, len(reasoning.Summary))
			for index := range reasoning.Summary {
				summary[index] = reasoning.Summary[index].Text
			}
			content = append(content, model.Content{
				Kind: model.ContentReasoning, Text: strings.Join(summary, ""), Final: true,
			})
			// Only encrypted items with stable IDs can be replayed on the next stateless request.
			if reasoning.ID != "" && reasoning.EncryptedContent != "" {
				payload, err := json.Marshal(responseContext{
					ID: reasoning.ID, EncryptedContent: reasoning.EncryptedContent, Summary: summary,
				})
				if err != nil {
					return model.Response{}, fmt.Errorf("encode provider context: %w", err)
				}
				content = append(content, model.Content{
					Kind: model.ContentProviderContext, Final: true,
					ProviderContext: model.ProviderContext{ProviderID: providerID, Payload: payload},
				})
			}
		case "function_call":
			call := output.AsFunctionCall()
			var arguments map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
				return model.Response{}, errors.New("responses returned invalid tool-call arguments")
			}
			content = append(content, model.Content{
				Kind: model.ContentToolCall, Final: true,
				ToolCall: model.ToolCall{ID: call.CallID, Name: call.Name, Arguments: arguments},
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
	var responseModel *model.ID
	if response.Model != "" {
		value := model.ID(response.Model)
		responseModel = &value
	}
	return model.Response{
		Content: content, Outcome: outcome, ResponseModel: responseModel, ResponseID: response.ID,
		Usage: model.Usage{
			InputTokens:       response.Usage.InputTokens,
			OutputTokens:      response.Usage.OutputTokens,
			CachedInputTokens: response.Usage.InputTokensDetails.CachedTokens,
			CacheWriteTokens:  response.Usage.InputTokensDetails.CacheWriteTokens,
			ReasoningTokens:   response.Usage.OutputTokensDetails.ReasoningTokens,
			TotalTokens:       response.Usage.TotalTokens,
		},
	}, nil
}
