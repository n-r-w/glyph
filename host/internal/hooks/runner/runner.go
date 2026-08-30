// Package runner applies Host-owned internal model request transformations.
package runner

import (
	"context"
	"slices"

	"github.com/n-r-w/glyph/host/internal/hooks"
)

// Runner owns ordered internal hook handlers supplied by Host composition.
type Runner struct {
	// contextHandlers contains ordered context transformations.
	contextHandlers []hooks.ContextHandler
	// requestHandlers contains ordered request transformations.
	requestHandlers []hooks.RequestHandler
	// responseHandlers contains ordered response observers.
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
	return transform(ctx, value, runner.contextHandlers, hooks.Context.Clone, hooks.StageContext)
}

// TransformRequest applies request handlers in registration order.
func (runner *Runner) TransformRequest(ctx context.Context, value hooks.Request) (hooks.Request, error) {
	return transform(ctx, value, runner.requestHandlers, hooks.Request.Clone, hooks.StageRequest)
}

// transform applies ordered copy-isolated transformations and identifies hook failures by stage.
func transform[T any, H ~func(context.Context, T) (T, error)](
	ctx context.Context,
	value T,
	handlers []H,
	clone func(T) T,
	stage hooks.Stage,
) (T, error) {
	current := clone(value)
	for _, handler := range handlers {
		transformed, err := handler(ctx, clone(current))
		if err != nil {
			var zero T
			return zero, hooks.HookError{Stage: stage, Cause: err}
		}
		current = clone(transformed)
	}
	return current, nil
}

// ObserveResponse invokes response handlers in registration order.
func (runner *Runner) ObserveResponse(ctx context.Context, value hooks.Response) error {
	for _, handler := range runner.responseHandlers {
		if err := handler(ctx, value.Clone()); err != nil {
			return hooks.HookError{Stage: hooks.StageResponse, Cause: err}
		}
	}
	return nil
}
