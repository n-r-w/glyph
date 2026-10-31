package middleware

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/errtree"
)

const (
	// inputInvalidCode is the public category of final user input that the selected model does not accept.
	inputInvalidCode = "INPUT_INVALID"
	// extensionFailedCode is the public category of a failed user-input handler chain.
	extensionFailedCode = "EXTENSION_FAILED"
)

// inputError reports a failure of user-input processing with its public category.
type inputError struct {
	// code is the public failure category.
	code string
	// cause contains the complete failure.
	cause error
}

// Public failure mapping recognizes the category of this error.
var _ errtree.CodedError = (*inputError)(nil)

// Error returns the complete failure text.
func (e *inputError) Error() string { return e.cause.Error() }

// Unwrap returns the complete failure.
func (e *inputError) Unwrap() error { return e.cause }

// Code returns the public failure category.
func (e *inputError) Code() string { return e.code }

// ProcessInput runs the user-input handlers in registration order and returns the final unhandled message after
// checking it against the input modalities of the selected model, or no message when a handler fully handled it.
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
		handles: true,
		handle:  s.runtime.HandleUserInput,
	}
	result, err := compose(ctx, s, s.snapshotHandlers(), chain, message)
	if err != nil {
		if isOwningCancellation(ctx, err) {
			return mo.None[model.Message](), err
		}
		return mo.None[model.Message](), &inputError{code: extensionFailedCode, cause: err}
	}
	if result.handled {
		return mo.None[model.Message](), nil
	}
	descriptor := s.models.ActiveBinding().Model
	if index, found := descriptor.FindUnsupportedImage(result.value); found {
		return mo.None[model.Message](), &inputError{
			code:  inputInvalidCode,
			cause: fmt.Errorf("model %s does not accept image input at content %d", descriptor.Model, index),
		}
	}
	return mo.Some(result.value), nil
}

// isOwningCancellation reports a failure whose current cause is only the cancellation of the owning context.
func isOwningCancellation(ctx context.Context, err error) bool {
	if ctx.Err() == nil {
		return false
	}
	return errtree.IsCurrentCancellation(err, func(cause error) bool {
		return errors.Is(cause, ctx.Err()) || errors.Is(cause, context.Cause(ctx))
	})
}
