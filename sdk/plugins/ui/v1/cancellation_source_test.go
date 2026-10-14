//go:build !integration

package uiv1

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/operation"
)

// TestUICancellationPreservesFailureSource checks SDK cleanup failure reporting after closure filtering.
func TestUICancellationPreservesFailureSource(t *testing.T) {
	t.Parallel()
	// Arrange a cancellation request with an independent failure alongside local cancellation.
	independent := errors.New("UI cleanup independent source")
	source := errors.Join(context.Canceled, independent)
	prepared := &cancellationPrepared{
		cancel: func(ctx context.Context) (operation.TerminalState, error) {
			return operation.TerminalStateFailed, errors.Join(context.Cause(ctx), source)
		},
		target: "ui-source-target",
	}
	// Act through the SDK cancellation outcome policy.
	outcome := prepared.Run(t.Context(), operation.Reporter[struct{}]{})
	// Assert that the SDK reports failure and retains every original cause.
	require.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.ErrorIs(t, outcome.SourceError(), source)
	require.ErrorIs(t, outcome.SourceError(), context.Canceled)
	require.ErrorIs(t, outcome.SourceError(), independent)
	require.ErrorContains(t, outcome.SourceError(), source.Error())
}
