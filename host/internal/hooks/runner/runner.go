// Package runner applies Host-owned internal model request transformations.
package runner

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"
)

// Runner owns ordered internal hook handlers supplied by Host composition.
type Runner struct {
	contextHandlers  []hooks.ContextHandler
	requestHandlers  []hooks.RequestHandler
	responseHandlers []hooks.ResponseHandler
}

var (
	_ hooks.ContextRunner  = (*Runner)(nil)
	_ hooks.ProviderRunner = (*Runner)(nil)
)

// New creates an internal runner with copied handler lists.
func New(
	contextHandlers []hooks.ContextHandler,
	requestHandlers []hooks.RequestHandler,
	responseHandlers []hooks.ResponseHandler,
) *Runner {
	return &Runner{
		contextHandlers:  append([]hooks.ContextHandler(nil), contextHandlers...),
		requestHandlers:  append([]hooks.RequestHandler(nil), requestHandlers...),
		responseHandlers: append([]hooks.ResponseHandler(nil), responseHandlers...),
	}
}

// TransformContext applies context handlers in registration order.
func (runner *Runner) TransformContext(ctx context.Context, value hooks.Context) (hooks.Context, error) {
	current := cloneContext(value)
	for _, handler := range runner.contextHandlers {
		transformed, err := handler(ctx, cloneContext(current))
		if err != nil {
			return hooks.Context{}, hooks.HookError{Stage: hooks.StageContext}
		}
		current = cloneContext(transformed)
	}
	return current, nil
}

// TransformRequest applies request handlers in registration order.
func (runner *Runner) TransformRequest(ctx context.Context, value hooks.Request) (hooks.Request, error) {
	current := cloneRequest(value)
	for _, handler := range runner.requestHandlers {
		transformed, err := handler(ctx, cloneRequest(current))
		if err != nil {
			return hooks.Request{}, hooks.HookError{Stage: hooks.StageRequest}
		}
		current = cloneRequest(transformed)
	}
	return current, nil
}

// ObserveResponse invokes response handlers in registration order.
func (runner *Runner) ObserveResponse(ctx context.Context, value hooks.Response) error {
	for _, handler := range runner.responseHandlers {
		if err := handler(ctx, cloneResponse(value)); err != nil {
			return hooks.HookError{Stage: hooks.StageResponse}
		}
	}
	return nil
}

func cloneContext(value hooks.Context) hooks.Context {
	history := make([]agent.HistoryEntry, len(value.History))
	for index := range value.History {
		entry := &value.History[index]
		history[index] = agent.HistoryEntry{
			Kind: entry.Kind, User: cloneMessage(entry.User), Model: cloneModelResponse(entry.Model),
			ToolResult: entry.ToolResult,
		}
	}
	return hooks.Context{History: history}
}

func cloneRequest(value hooks.Request) hooks.Request {
	return hooks.Request{
		Provider: value.Provider, Model: value.Model, Payload: append([]byte(nil), value.Payload...),
		Headers: cloneHeader(value.Headers),
	}
}

func cloneResponse(value hooks.Response) hooks.Response {
	return hooks.Response{
		Provider: value.Provider, Model: value.Model, Status: value.Status,
		Headers: cloneHeader(value.Headers),
	}
}

func cloneHeader(header hooks.Header) hooks.Header {
	if header == nil {
		return nil
	}
	cloned := make(hooks.Header, len(header))
	for name, values := range header {
		cloned[name] = append([]string(nil), values...)
	}
	return cloned
}

func cloneMessage(message model.Message) model.Message {
	content := make([]model.InputContent, len(message.Content))
	for index, item := range message.Content {
		content[index] = model.InputContent{
			Kind: item.Kind, Text: item.Text, MediaType: item.MediaType,
			Data: append([]byte(nil), item.Data...),
		}
	}
	return model.Message{Content: content}
}

func cloneModelResponse(response model.Response) model.Response {
	content := make([]model.Content, len(response.Content))
	for index, item := range response.Content {
		content[index] = model.Content{
			Kind: item.Kind, Text: item.Text, Final: item.Final,
			ProviderContext: model.ProviderContext{
				ProviderID: item.ProviderContext.ProviderID,
				Payload:    append([]byte(nil), item.ProviderContext.Payload...),
			},
			ToolCall: model.ToolCall{
				ID: item.ToolCall.ID, Name: item.ToolCall.Name,
				Arguments: cloneArguments(item.ToolCall.Arguments),
			},
		}
	}
	var responseModel *model.ID
	if response.ResponseModel != nil {
		value := *response.ResponseModel
		responseModel = &value
	}
	return model.Response{
		Content: content, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: responseModel,
		ResponseID: response.ResponseID, Usage: response.Usage,
		Diagnostics: append([]model.Diagnostic(nil), response.Diagnostics...),
	}
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := make(map[string]any, len(arguments))
	for name, value := range arguments {
		cloned[name] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return value
	}
}
