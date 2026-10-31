package extensionruntime

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
)

// HandleWire invokes mechanics only for the exact generation bound by the middleware owner.
func (s *Service) HandleWire(
	ctx context.Context,
	extensionID, handlerID string,
	invocation middleware.Invocation,
) (middleware.Action, error) {
	return invokeMiddleware(
		ctx, s, extensionID, handlerID, invocation.Context.RuntimeInstanceID,
		func(runtime ExtensionRuntime) (middleware.Action, error) {
			return runtime.HandleWire(ctx, handlerID, invocation)
		},
	)
}

// HandleSystemInstructions invokes one instruction transformer on the exact generation bound by the owner.
func (s *Service) HandleSystemInstructions(
	ctx context.Context,
	extensionID, handlerID string,
	invocation middleware.Transformation[string],
) (middleware.Transformed[string], error) {
	return invokeMiddleware(
		ctx, s, extensionID, handlerID, invocation.Context.RuntimeInstanceID,
		func(runtime ExtensionRuntime) (middleware.Transformed[string], error) {
			return runtime.HandleSystemInstructions(ctx, handlerID, invocation)
		},
	)
}

// HandleModelContext invokes one history transformer on the exact generation bound by the owner.
func (s *Service) HandleModelContext(
	ctx context.Context,
	extensionID, handlerID string,
	invocation middleware.Transformation[[]agent.HistoryEntry],
) (middleware.Transformed[[]agent.HistoryEntry], error) {
	return invokeMiddleware(
		ctx, s, extensionID, handlerID, invocation.Context.RuntimeInstanceID,
		func(runtime ExtensionRuntime) (middleware.Transformed[[]agent.HistoryEntry], error) {
			return runtime.HandleModelContext(ctx, handlerID, invocation)
		},
	)
}

// HandleUserInput invokes one user-input handler on the exact generation bound by the owner.
func (s *Service) HandleUserInput(
	ctx context.Context,
	extensionID, handlerID string,
	invocation middleware.Transformation[model.Message],
) (middleware.Transformed[model.Message], error) {
	return invokeMiddleware(
		ctx, s, extensionID, handlerID, invocation.Context.RuntimeInstanceID,
		func(runtime ExtensionRuntime) (middleware.Transformed[model.Message], error) {
			return runtime.HandleUserInput(ctx, handlerID, invocation)
		},
	)
}

// invokeMiddleware runs one middleware operation only while its bound runtime generation is available.
func invokeMiddleware[A any](
	ctx context.Context,
	s *Service,
	extensionID, handlerID, runtimeInstanceID string,
	call func(ExtensionRuntime) (A, error),
) (A, error) {
	owner, available := s.beginOperation(extensionID, runtimeInstanceID)
	if !available {
		var empty A
		return empty, fmt.Errorf("%w: middleware handler %q is unavailable", ErrExtensionUnavailable, handlerID)
	}
	action, err := call(owner.state.runtime)
	s.finishAndReport(ctx, owner, err)
	return action, err
}
