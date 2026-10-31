package middleware

import (
	"context"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// ProcessInput runs the user-input handlers and then the finalized-message handlers in registration order and returns
// the final unhandled message after checking it against the input modalities of the selected model, or no message
// when a handler fully handled it.
func (s *Service) ProcessInput(ctx context.Context, message model.Message) (mo.Option[model.Message], error) {
	chain := transformer[model.Message]{
		kind:  UserInput,
		clone: model.Message.Clone,
		accept: func(replacement model.Message) (model.Message, error) {
			if err := replacement.ValidateShape(); err != nil {
				return model.Message{}, err
			}
			return replacement.Clone(), nil
		},
		handles:      true,
		cancellation: failOnCancellation,
		handle:       s.runtime.HandleUserInput,
	}
	result, err := compose(ctx, s, s.snapshotHandlers(), chain, message)
	if err != nil {
		return mo.None[model.Message](), categorizeChainFailure(ctx, err)
	}
	if result.handled {
		return mo.None[model.Message](), nil
	}
	final, err := s.finalizeMessage(ctx, failOnCancellation, agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(result.value), Model: mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	})
	if err != nil {
		return mo.None[model.Message](), err
	}
	message = final.User.OrEmpty()
	descriptor := s.models.ActiveBinding().Model
	if index, found := descriptor.FindUnsupportedImage(message); found {
		return mo.None[model.Message](), &categorizedError{
			code:  inputInvalidCode,
			cause: fmt.Errorf("model %s does not accept image input at content %d", descriptor.Model, index),
		}
	}
	return mo.Some(message), nil
}
