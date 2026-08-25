//nolint:exhaustruct // Provider mappings set only fields supported by Agent Core.
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
	content         []model.Content
	textPosition    int
	refusalPosition int
	tools           map[int64]*chatToolState
	responseID      string
	responseModel   string
	usage           model.Usage
	outcome         model.Outcome
}

func newChatAccumulator() *chatAccumulator {
	return &chatAccumulator{textPosition: -1, refusalPosition: -1, tools: make(map[int64]*chatToolState)}
}

func (s *Driver) streamChatCompletions(
	ctx context.Context,
	request run.ModelRequest,
	key string,
	handle run.StreamHandler,
) (model.Response, error) {
	params, err := chatParams(request)
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
	state := newChatAccumulator()
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

func chatParams(request run.ModelRequest) (openai.ChatCompletionNewParams, error) {
	messages, err := chatMessages(request)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	tools, err := chatTools(request.Tools, request.Model.ToolCapabilities.StrictJSONSchema)
	if err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	//nolint:exhaustruct // Optional SDK request fields intentionally use zero values.
	params := openai.ChatCompletionNewParams{
		Messages:          messages,
		Model:             shared.ChatModel(request.Model.Model),
		ParallelToolCalls: param.NewOpt(false),
		StreamOptions:     openai.ChatCompletionStreamOptionsParam{IncludeUsage: param.NewOpt(true)},
		Tools:             tools,
	}
	if request.ReasoningChoice != "" && request.ReasoningChoice != model.ReasoningChoiceOff {
		params.ReasoningEffort = shared.ReasoningEffort(request.ReasoningChoice)
	}
	return params, nil
}

func chatMessages(request run.ModelRequest) ([]openai.ChatCompletionMessageParamUnion, error) {
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
			message, ok, err := chatAssistantMessage(entry.Model)
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
	content := make([]openai.ChatCompletionContentPartUnionParam, 0, len(message.Content))
	for index, item := range message.Content {
		switch item.Kind {
		case model.InputContentText:
			content = append(content, openai.TextContentPart(item.Text))
		case model.InputContentImage:
			if item.MediaType == "" || len(item.Data) == 0 {
				return nil, fmt.Errorf("user image %d requires media type and data", index)
			}
			content = append(content, openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
				URL: dataURL(item.MediaType, item.Data),
			}))
		default:
			return nil, fmt.Errorf("unsupported user content kind %d", item.Kind)
		}
	}
	return content, nil
}

func chatAssistantMessage(response model.Response) (openai.ChatCompletionMessageParamUnion, bool, error) {
	var text strings.Builder
	var refusal string
	calls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0)
	for index := range response.Content {
		item := &response.Content[index]
		switch item.Kind {
		case model.ContentText:
			text.WriteString(item.Text)
		case model.ContentRefusal:
			refusal += item.Text
		case model.ContentReasoning:
			continue
		case model.ContentToolCall:
			arguments, err := json.Marshal(item.ToolCall.Arguments)
			if err != nil {
				return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("encode tool-call arguments: %w", err)
			}
			calls = append(calls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: item.ToolCall.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Arguments: string(arguments), Name: item.ToolCall.Name,
					},
				},
			})
		default:
			return openai.ChatCompletionMessageParamUnion{}, false, fmt.Errorf("unsupported model content kind %d", item.Kind)
		}
	}
	if text.Len() == 0 && refusal == "" && len(calls) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false, nil
	}
	message := openai.AssistantMessage(text.String())
	message.OfAssistant.ToolCalls = calls
	if refusal != "" {
		message.OfAssistant.Refusal = param.NewOpt(refusal)
	}
	return message, true, nil
}

func chatToolResult(contents []tool.ResultContent) (string, error) {
	parts := make([]string, 0, len(contents))
	for index, content := range contents {
		switch content.Kind {
		case tool.ResultContentText:
			parts = append(parts, content.Text)
		case tool.ResultContentImage:
			if content.Image.MediaType == "" || len(content.Image.Data) == 0 {
				return "", fmt.Errorf("tool result image %d requires media type and data", index)
			}
			parts = append(parts, dataURL(content.Image.MediaType, content.Image.Data))
		default:
			return "", fmt.Errorf("unsupported tool result content kind %d", content.Kind)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func chatTools(descriptors []tool.Descriptor, strictSupported bool) ([]openai.ChatCompletionToolUnionParam, error) {
	tools := make([]openai.ChatCompletionToolUnionParam, 0, len(descriptors))
	for index, descriptor := range descriptors {
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchemaJSON, &schema); err != nil {
			return nil, fmt.Errorf("tool %d has invalid input schema: %w", index, err)
		}
		definition := shared.FunctionDefinitionParam{
			Name:        descriptor.Name,
			Description: param.NewOpt(descriptor.Description),
			Parameters:  schema,
		}
		if strictSupported {
			definition.Strict = param.NewOpt(true)
		}
		tools = append(tools, openai.ChatCompletionFunctionTool(definition))
	}
	return tools, nil
}

func dataURL(mediaType string, data []byte) string {
	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

//nolint:gocyclo // The branches map the closed Chat Completions stream union.
func (state *chatAccumulator) consume(chunk openai.ChatCompletionChunk, handle run.StreamHandler) error {
	if chunk.ID != "" {
		state.responseID = chunk.ID
	}
	if chunk.Model != "" {
		state.responseModel = chunk.Model
	}
	if chunk.JSON.Usage.Valid() {
		state.usage = model.Usage{
			InputTokens:       chunk.Usage.PromptTokens,
			OutputTokens:      chunk.Usage.CompletionTokens,
			CachedInputTokens: chunk.Usage.PromptTokensDetails.CachedTokens,
			ReasoningTokens:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			TotalTokens:       chunk.Usage.TotalTokens,
		}
	}
	for choiceIndex := range chunk.Choices {
		choice := &chunk.Choices[choiceIndex]
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
			delta := &choice.Delta.ToolCalls[deltaIndex]
			if err := state.toolDelta(delta, handle); err != nil {
				return err
			}
		}
		switch choice.FinishReason {
		case "":
		case "stop":
			state.outcome = model.OutcomeStop
		case "tool_calls", "function_call":
			state.outcome = model.OutcomeToolUse
		case "length":
			state.outcome = model.OutcomeLength
		case "content_filter":
			state.outcome = model.OutcomeStop
		default:
			return fmt.Errorf("unsupported Chat Completions finish reason %q", choice.FinishReason)
		}
	}
	return nil
}

func (state *chatAccumulator) contentDelta(kind model.ContentKind, delta string, handle run.StreamHandler) error {
	position := state.textPosition
	if kind == model.ContentRefusal {
		position = state.refusalPosition
	}
	if position < 0 {
		position = len(state.content)
		state.content = append(state.content, model.Content{Kind: kind})
		if kind == model.ContentText {
			state.textPosition = position
		} else {
			state.refusalPosition = position
		}
		startEvent := run.StreamEvent{
			Kind: run.StreamEventContentStart, Position: position, Content: model.Content{Kind: kind},
		}
		if err := handle(startEvent); err != nil {
			return err
		}
	}
	state.content[position].Text += delta
	return handle(run.StreamEvent{
		Kind: run.StreamEventTextDelta, Position: position,
		Content: model.Content{Kind: kind}, Delta: delta,
	})
}

func (state *chatAccumulator) toolDelta(
	delta *openai.ChatCompletionChunkChoiceDeltaToolCall,
	handle run.StreamHandler,
) error {
	toolState, ok := state.tools[delta.Index]
	if !ok {
		toolState = &chatToolState{position: len(state.content)}
		state.tools[delta.Index] = toolState
		state.content = append(state.content, model.Content{Kind: model.ContentToolCall})
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
		CallID: toolState.id, Name: toolState.name, Position: toolState.position, Provisional: true,
	}
	kind := run.StreamEventToolCallDelta
	if !toolState.started {
		kind = run.StreamEventToolCallStart
		toolState.started = true
	}
	return handle(run.StreamEvent{Kind: kind, Position: toolState.position, Preview: preview})
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
		call := model.ToolCall{ID: toolState.id, Name: toolState.name, Arguments: arguments}
		state.content[toolState.position] = model.Content{Kind: model.ContentToolCall, Final: true, ToolCall: call}
		if err := handle(run.StreamEvent{
			Kind: run.StreamEventToolCallEnd, Position: toolState.position, ToolCall: call,
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
		if (kind != model.ContentText && kind != model.ContentRefusal) || state.content[position].Final {
			continue
		}
		state.content[position].Final = true
		if err := handle(run.StreamEvent{
			Kind: run.StreamEventContentEnd, Position: position, Content: model.Content{Kind: kind},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (state *chatAccumulator) response() model.Response {
	var responseModel *model.ID
	if state.responseModel != "" {
		value := model.ID(state.responseModel)
		responseModel = &value
	}
	return model.Response{
		Content: state.content, Outcome: state.outcome,
		ResponseModel: responseModel, ResponseID: state.responseID, Usage: state.usage,
	}
}
