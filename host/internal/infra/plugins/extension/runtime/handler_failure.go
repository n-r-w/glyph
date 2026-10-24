package runtime

import (
	"context"
	"errors"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// handlerFailureError retains a participating extension failure independently of its enclosing Host result mapping.
type handlerFailureError struct {
	// cause contains every invocation and extension source message.
	cause error
}

var (
	_ extensioncontroller.NestedExtensionFailure = (*handlerFailureError)(nil)
	_ errtree.CodedError                         = (*handlerFailureError)(nil)
)

// Code exposes the stable independent extension-failure category.
func (e *handlerFailureError) Code() string { return string(modelexecution.FailureExtensionFailed) }

// Error exposes the complete participating-handler cause.
func (e *handlerFailureError) Error() string { return e.cause.Error() }

// Unwrap exposes original local cause identity.
func (e *handlerFailureError) Unwrap() error { return e.cause }

// IsExtensionFailure distinguishes a participating handler failure from ordinary provider and tool results.
func (e *handlerFailureError) IsExtensionFailure() bool { return true }

// classifyHandlerFailure preserves pure owning cancellation while marking independent invocation failure.
func classifyHandlerFailure(ctx context.Context, cause error) error {
	if cause == nil || isReceivedCancellation(cause) ||
		ctx.Err() != nil && errtree.IsCurrentCancellation(cause, func(current error) bool {
			return errors.Is(current, ctx.Err()) || errors.Is(current, context.Cause(ctx))
		}) {
		return cause
	}
	return &handlerFailureError{cause: cause}
}
