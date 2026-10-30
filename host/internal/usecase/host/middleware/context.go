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
	// handle invokes one handler through runtime management.
	handle func(context.Context, string, string, Transformation[T]) (Transformed[T], error)
}

// NewContextComposer captures ordered instruction and history handlers for every revision of one model execution.
func (s *Service) NewContextComposer() modelexecution.ContextComposer {
	handlers := s.snapshotHandlers()
	instructions := transformer[string]{
		kind:   SystemInstructions,
		clone:  func(value string) string { return value },
		accept: func(value string) (string, error) { return value, nil },
		handle: s.runtime.HandleSystemInstructions,
	}
	history := transformer[[]agent.HistoryEntry]{
		kind:   ModelContext,
		clone:  agent.CloneHistory,
		accept: agent.ValidatedCloneHistory,
		handle: s.runtime.HandleModelContext,
	}
	return func(
		ctx context.Context,
		request modelexecution.RequestContext,
	) (modelexecution.ComposedContext, error) {
		finalInstructions, _, err := compose(ctx, s, handlers, instructions, request.Instructions)
		if err != nil {
			return modelexecution.ComposedContext{}, err
		}
		finalHistory, replaced, err := compose(ctx, s, handlers, history, request.History)
		if err != nil {
			return modelexecution.ComposedContext{}, err
		}
		return modelexecution.ComposedContext{
			Context:         modelexecution.RequestContext{Instructions: finalInstructions, History: finalHistory},
			HistoryReplaced: replaced,
		}, nil
	}
}

// compose applies one transformer chain sequentially and reports whether a handler replaced the value.
// It stops at the first failure or invalid action.
func compose[T any](
	ctx context.Context,
	owner *Service,
	handlers []Handler,
	chain transformer[T],
	original T,
) (final T, replaced bool, failure error) {
	original = chain.clone(original)
	current := chain.clone(original)
	for _, handler := range handlers {
		if handler.Kind != chain.kind {
			continue
		}
		binding, err := owner.bind(ctx, handler)
		if err != nil {
			return final, false, err
		}
		action, err := chain.handle(ctx, handler.ExtensionID, handler.HandlerID, Transformation[T]{
			Context: binding, Original: chain.clone(original), Current: chain.clone(current),
		})
		if err != nil {
			return final, false, fmt.Errorf("middleware handler %q: %w", handler.HandlerID, err)
		}
		if action.Kind == Preserve {
			continue
		}
		if action.Kind != Replace {
			return final, false, fmt.Errorf(
				"middleware handler %q returned invalid %s action %d", handler.HandlerID, chain.kind, action.Kind,
			)
		}
		replacement, err := chain.accept(action.Replacement)
		if err != nil {
			return final, false, fmt.Errorf(
				"middleware handler %q %s replacement: %w",
				handler.HandlerID,
				chain.kind,
				err,
			)
		}
		current = replacement
		replaced = true
	}
	return current, replaced, nil
}
