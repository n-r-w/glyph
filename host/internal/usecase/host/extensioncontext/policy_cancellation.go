package extensioncontext

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/internal/errtree"
)

// PolicyCancellationError retains an explicitly canceled participant operation and its complete diagnostics.
type PolicyCancellationError struct {
	// Cause contains the received cancellation and every source diagnostic.
	Cause error
}

// Error exposes all received cancellation text.
func (e *PolicyCancellationError) Error() string { return e.Cause.Error() }

// Unwrap preserves received cancellation identity and complete diagnostics.
func (e *PolicyCancellationError) Unwrap() error { return e.Cause }

// isExplicitPolicyCancellation distinguishes received cancellation from a classified or mixed failure.
func isExplicitPolicyCancellation(cause error) bool {
	received := false
	for current := range errtree.IterateCurrentErrors(cause) {
		//nolint:errorlint // Current-node inspection excludes cancellation retained only in diagnostic history.
		if _, present := current.(*PolicyCancellationError); present {
			received = true
		}
	}
	return received && errtree.IsCurrentCancellation(cause, func(current error) bool {
		return errors.Is(current, context.Canceled) || errors.Is(current, context.DeadlineExceeded)
	})
}
