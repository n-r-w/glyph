//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestExplicitPolicyFailureWinsCancellation checks a classified independent source with a cancellation-only cause.
func TestExplicitPolicyFailureWinsCancellation(t *testing.T) {
	t.Parallel()
	// Arrange an explicit extension failure whose original cause is cancellation.
	source := &PolicyOperationError{Category: modelexecution.FailureExtensionFailed, Cause: context.Canceled}
	// Act through the shared current-cause classification used at Host boundaries.
	canceled := errtree.IsCurrentCancellation(source, func(cause error) bool {
		return errors.Is(cause, context.Canceled)
	})
	// Assert explicit failure meaning cannot be erased by the underlying cancellation sentinel.
	require.False(t, canceled)
	require.ErrorIs(t, source, context.Canceled)
	require.True(t, source.IsExtensionFailure())
}
