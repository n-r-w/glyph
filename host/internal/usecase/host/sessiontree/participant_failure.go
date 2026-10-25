package sessiontree

import (
	"context"
	"errors"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/internal/errtree"
)

// navigationExtensionFailedCode identifies a failed selected navigation participant.
const navigationExtensionFailedCode = "EXTENSION_FAILED"

// participantError retains independent navigation failure meaning and original causes.
type participantError struct {
	// cause contains the complete attributed participant failure.
	cause error
}

// Compile-time assertions verify participant failure classification contracts.
var (
	_ extensioncontroller.NestedExtensionFailure = (*participantError)(nil)
	_ errtree.CodedError                         = (*participantError)(nil)
)

// Error returns the complete attributed failure text.
func (failure *participantError) Error() string { return failure.cause.Error() }

// Unwrap preserves original failure identity.
func (failure *participantError) Unwrap() error { return failure.cause }

// Code preserves independent failure priority over cleanup cancellation.
func (*participantError) Code() string { return navigationExtensionFailedCode }

// IsExtensionFailure lets Host retain consumed nested participant failure.
func (*participantError) IsExtensionFailure() bool { return true }

// classifyParticipantFailure preserves pure owning cancellation and classifies independent failure.
func classifyParticipantFailure(ctx context.Context, cause error) error {
	if ctx.Err() != nil && errtree.IsCurrentCancellation(cause, func(leaf error) bool {
		return errors.Is(leaf, ctx.Err()) || errors.Is(leaf, context.Cause(ctx))
	}) {
		return cause
	}
	return &participantError{cause: errors.Join(cause, ctx.Err())}
}
