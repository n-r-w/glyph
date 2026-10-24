//go:build !integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/errtree"
)

// TestExplicitFatalFailureWinsCancellation checks fatal meaning with a cancellation-only retained cause.
func TestExplicitFatalFailureWinsCancellation(t *testing.T) {
	t.Parallel()
	// Arrange the explicit fatal operation meaning independently of its underlying cause.
	source := &fatalInvocationError{cause: context.Canceled}
	// Act through the shared current-cause classification used by enclosing operation adapters.
	canceled := errtree.IsCurrentCancellation(source, func(cause error) bool {
		return errors.Is(cause, context.Canceled)
	})
	// Assert cleanup cancellation does not turn an explicit fatal operation into a canceled outcome.
	require.False(t, canceled)
	require.ErrorIs(t, source, context.Canceled)
}
