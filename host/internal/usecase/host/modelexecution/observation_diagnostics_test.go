//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/errtree"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// TestObservationCancellationRetainsRuntimeDiagnostics checks that model execution retains
// runtime and provider diagnostics after owning cancellation.
func TestObservationCancellationRetainsRuntimeDiagnostics(t *testing.T) {
	t.Parallel()
	for index, history := range []error{nil, errors.New("recovered provider source")} {
		t.Run("history="+strconv.Itoa(index), func(t *testing.T) {
			t.Parallel()
			// Arrange an owning cancellation with runtime diagnostics.
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			runtimeCause := errors.New("corroborated runtime cancellation rejection")
			source := diagnostic.NewCurrentCause(context.Canceled, runtimeCause)
			// Act through the observation failure boundary.
			result := observationFailure(ctx, source, history)
			// Assert that the caller can recognize source errors by identity and classify
			// the current causes as cancellation.
			require.ErrorIs(t, result, source)
			require.ErrorIs(t, result, runtimeCause)
			require.True(
				t,
				errtree.IsCurrentCancellation(
					result,
					func(leaf error) bool { return errors.Is(leaf, context.Canceled) },
				),
			)
			if history != nil {
				require.ErrorIs(t, result, history)
			}
		})
	}
}
