package middleware

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/internal/errtree"
)

const (
	// inputInvalidCode is the public category of final user input that the selected model does not accept.
	inputInvalidCode = "INPUT_INVALID"
	// extensionFailedCode is the public category of a failed handler chain.
	extensionFailedCode = "EXTENSION_FAILED"
)

// categorizedError reports a middleware failure with its public category.
type categorizedError struct {
	// code is the public failure category.
	code string
	// cause contains the complete failure.
	cause error
}

// Public failure mapping recognizes the category of this error.
var _ errtree.CodedError = (*categorizedError)(nil)

// Error returns the complete failure text.
func (e *categorizedError) Error() string { return e.cause.Error() }

// Unwrap returns the complete failure.
func (e *categorizedError) Unwrap() error { return e.cause }

// Code returns the public failure category.
func (e *categorizedError) Code() string { return e.code }

// categorizeChainFailure marks a failed handler chain as an extension failure, except a failure whose current cause
// is only the cancellation of the owning context, which keeps its uncategorized cancellation meaning.
func categorizeChainFailure(ctx context.Context, err error) error {
	if isOwningCancellation(ctx, err) {
		return err
	}
	return &categorizedError{code: extensionFailedCode, cause: err}
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
