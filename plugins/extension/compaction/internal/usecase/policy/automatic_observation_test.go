//go:build !integration

package policy

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestAutomaticUsageDecision checks usage-selected thresholds and invalidation before replacement.
func TestAutomaticUsageDecision(t *testing.T) {
	t.Parallel()
	for _, above := range []bool{false, true} {
		t.Run(map[bool]string{false: "equal", true: "above"}[above], func(t *testing.T) {
			t.Parallel()
			// Arrange reported usage larger than the content estimate.
			observed, next := newObservedContinuation()
			next.Purpose = compaction.PurposeAutomatic
			next.Model.Window = next.Model.Reserve + 98 + next.History[3].EstimateTokens()
			if above {
				next.Model.Window--
			}
			service := New(nil, 1)
			require.NoError(t, service.Observe(t.Context(), observed))
			// Act at the usage-based budget boundary.
			outcome, err := service.Compact(t.Context(), next)
			// Assert that Compact uses reported usage for the decision and discards the prior anchor when compacting.
			if above {
				require.ErrorContains(t, err, "generator")
				require.False(t, outcome.Preserve)
				require.Equal(
					t,
					int64(98)+next.History[3].EstimateTokens(),
					outcome.State.MustGet().Original.ContextTokens,
				)
				require.Equal(
					t,
					compaction.EstimateInput(next.History, next.Instructions, next.ToolText),
					service.estimateInput(next),
				)
			} else {
				require.NoError(t, err)
				require.True(t, outcome.Preserve)
				require.Equal(t, int64(98)+next.History[3].EstimateTokens(), service.estimateInput(next))
			}
		})
	}
}

// TestObservationZeroUsage keeps reported zero distinguishable from missing accounting.
func TestObservationZeroUsage(t *testing.T) {
	t.Parallel()
	// Arrange a completed request with explicit zero usage.
	observed, next := newObservedContinuation()
	observed.Usage = mo.Some(compaction.Usage{Counts: [6]int64{}})
	service := New(nil, 1)
	require.NoError(t, service.Observe(t.Context(), observed))
	// Act on subsequent input growth.
	actual := service.estimateInput(next)
	// Assert only new input contributes to the reported zero baseline.
	require.Equal(t, next.History[3].EstimateTokens(), actual)
}
