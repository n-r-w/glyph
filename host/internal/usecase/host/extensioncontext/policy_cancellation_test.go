//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// TestReceivedPolicyCancellationKeepsMeaning checks received cancellation and mixed independent failure classification.
func TestReceivedPolicyCancellationKeepsMeaning(t *testing.T) {
	t.Parallel()
	// Arrange an explicitly received canceled terminal and a historical independent failure.
	canceled := &PolicyCancellationError{Cause: context.Canceled}
	historical := &PolicyOperationError{
		Category: modelexecution.FailureExtensionFailed,
		Cause:    errors.New("earlier diagnostic"),
	}
	current := diagnostic.NewCurrentCause(canceled, historical)
	independent := errors.New("independent cleanup failure")
	// Act through policy classification with a live owner and with mixed cancellation plus failure.
	pure := NewPolicyOperationError(t.Context(), modelexecution.FailureExtensionFailed, current)
	mixed := NewPolicyOperationError(
		t.Context(),
		modelexecution.FailureExtensionFailed,
		errors.Join(canceled, independent),
	)
	// Assert pure current cancellation survives while independent current failure wins and every source remains visible.
	require.Same(t, current, pure)
	require.True(t, errtree.IsCurrentCancellation(pure, func(cause error) bool {
		return errors.Is(cause, context.Canceled)
	}))
	require.ErrorIs(t, pure, historical)
	require.ErrorIs(t, mixed, independent)
	failure, classified := errors.AsType[*PolicyOperationError](mixed)
	require.True(t, classified)
	require.True(t, failure.IsExtensionFailure())
	require.False(t, errtree.IsCurrentCancellation(mixed, func(cause error) bool {
		return errors.Is(cause, context.Canceled)
	}))
}
