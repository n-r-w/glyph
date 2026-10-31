package middleware

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// transformer describes one ordered chain whose handlers preserve or replace a detached value.
type transformer[T any] struct {
	// kind selects the handlers of the chain.
	kind Kind
	// clone detaches a value before an ownership transfer.
	clone func(T) T
	// accept validates a replacement and detaches it for the owner.
	accept func(T) (T, error)
	// handles permits a handler to fully handle the value and end the chain.
	handles bool
	// handle invokes one handler through runtime management.
	handle func(context.Context, string, string, Transformation[T]) (Transformed[T], error)
}

// NewContextComposer captures ordered instruction and history handlers for every revision of one model execution.
func (s *Service) NewContextComposer() modelexecution.ContextComposer {
	handlers := s.snapshotHandlers()
	instructions := transformer[string]{
		kind:    SystemInstructions,
		clone:   func(value string) string { return value },
		accept:  func(value string) (string, error) { return value, nil },
		handles: false,
		handle:  s.runtime.HandleSystemInstructions,
	}
	history := transformer[[]agent.HistoryEntry]{
		kind:    ModelContext,
		clone:   agent.CloneHistory,
		accept:  agent.ValidatedCloneHistory,
		handles: false,
		handle:  s.runtime.HandleModelContext,
	}
	return func(
		ctx context.Context,
		request modelexecution.RequestContext,
	) (modelexecution.ComposedContext, error) {
		finalInstructions, err := compose(ctx, s, handlers, instructions, request.Instructions)
		if err != nil {
			return modelexecution.ComposedContext{}, err
		}
		finalHistory, err := compose(ctx, s, handlers, history, request.History)
		if err != nil {
			return modelexecution.ComposedContext{}, err
		}
		return modelexecution.ComposedContext{
			Context: modelexecution.RequestContext{
				Instructions: finalInstructions.value, History: finalHistory.value,
			},
			HistoryReplaced: finalHistory.replaced,
		}, nil
	}
}

// composition is the result of one transformer chain.
type composition[T any] struct {
	// value contains the final value when no handler fully handled it.
	value T
	// replaced reports that a handler replaced the value.
	replaced bool
	// handled reports that a handler fully handled the value and ended the chain.
	handled bool
}

// compose applies one transformer chain sequentially in registration order.
// It stops at the first failure, invalid action, or permitted handling.
func compose[T any](
	ctx context.Context,
	owner *Service,
	handlers []Handler,
	chain transformer[T],
	original T,
) (composition[T], error) {
	original = chain.clone(original)
	result := composition[T]{value: chain.clone(original), replaced: false, handled: false}
	for _, handler := range handlers {
		if handler.Kind != chain.kind {
			continue
		}
		binding, err := owner.bind(ctx, handler)
		if err != nil {
			return composition[T]{}, err
		}
		action, err := chain.handle(ctx, handler.ExtensionID, handler.HandlerID, Transformation[T]{
			Context: binding, Original: chain.clone(original), Current: chain.clone(result.value),
		})
		if err != nil {
			return composition[T]{}, fmt.Errorf("middleware handler %q: %w", handler.HandlerID, err)
		}
		switch {
		case action.Kind == Preserve:
			continue
		case action.Kind == Handle && chain.handles:
			var empty T
			return composition[T]{value: empty, replaced: result.replaced, handled: true}, nil
		case action.Kind == Replace:
			replacement, acceptErr := chain.accept(action.Replacement)
			if acceptErr != nil {
				return composition[T]{}, fmt.Errorf(
					"middleware handler %q %s replacement: %w", handler.HandlerID, chain.kind, acceptErr,
				)
			}
			result.value = replacement
			result.replaced = true
		default:
			return composition[T]{}, fmt.Errorf(
				"middleware handler %q returned invalid %s action %d", handler.HandlerID, chain.kind, action.Kind,
			)
		}
	}
	return result, nil
}
