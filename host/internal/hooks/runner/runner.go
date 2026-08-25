// Package runner applies Host-owned internal model request transformations.
package runner

import (
	"bytes"
	"context"
	"maps"
	"slices"

	"github.com/samber/mo"

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
		contextHandlers:  slices.Clone(contextHandlers),
		requestHandlers:  slices.Clone(requestHandlers),
		responseHandlers: slices.Clone(responseHandlers),
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
	value.History = slices.Clone(value.History)
	for index := range value.History {
		value.History[index].User = cloneMessage(value.History[index].User)
		value.History[index].Model = cloneModelResponse(value.History[index].Model)
	}
	return value
}

func cloneRequest(value hooks.Request) hooks.Request {
	return hooks.Request{
		Provider: value.Provider, Model: value.Model, Payload: bytes.Clone(value.Payload),
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
	cloned := maps.Clone(header)
	for name, values := range cloned {
		cloned[name] = slices.Clone(values)
	}
	return cloned
}

func cloneMessage(message model.Message) model.Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = message.Content[index].Data.MapValue(bytes.Clone)
	}
	return message
}

func cloneModelResponse(response model.Response) model.Response {
	content := slices.Clone(response.Content)
	for index := range content {
		if providerContext, ok := content[index].ProviderContext.Get(); ok {
			providerContext.Payload = bytes.Clone(providerContext.Payload)
			content[index].ProviderContext = mo.Some(providerContext)
		}
		if call, ok := content[index].ToolCall.Get(); ok {
			call.Arguments = cloneArguments(call.Arguments)
			content[index].ToolCall = mo.Some(call)
		}
	}
	return model.Response{
		Content: content, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: response.ResponseModel,
		ResponseID: response.ResponseID, Usage: response.Usage,
		Diagnostics: slices.Clone(response.Diagnostics),
	}
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := maps.Clone(arguments)
	for name, value := range cloned {
		cloned[name] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index, item := range cloned {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	default:
		return value
	}
}
