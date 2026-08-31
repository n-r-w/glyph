//go:build !integration

package programmatic

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestMapSessionEntriesPreservesPersistedEstimatedCost verifies detailed model entries expose stored cost.
func TestMapSessionEntriesPreservesPersistedEstimatedCost(t *testing.T) {
	t.Parallel()

	// Arrange one public model entry with present persisted cost.
	cost := session.EstimatedCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10}
	entries := []SessionEntry{{
		ID: "model-entry", CreatedAt: time.Unix(1, 0).UTC(), Kind: HistoryEntryModel,
		User: mo.None[model.Message](), Model: mo.Some(ModelResponse{
			Text: "", Outcome: mo.Some(ModelOutcomeStop), ErrorMessage: mo.None[string](),
			Provider: mo.Some("provider"), Model: mo.Some("model"), ResponseModel: mo.None[string](),
			ResponseID: mo.None[string](), Usage: mo.None[ModelUsage](), Diagnostics: nil, Content: nil,
		}),
		EstimatedCost: mo.Some(cost), ToolResult: mo.None[ToolResult](),
		BranchSummary: mo.None[BranchSummary](),
	}}

	// Act by mapping the detailed entry to the public protobuf contract.
	mapped, err := mapSessionEntries(entries)

	// Assert all persisted cost buckets and total remain present.
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	require.True(t, mapped[0].HasEstimatedCost())
	mappedCost := mapped[0].GetEstimatedCost()
	assert.InDelta(t, 1, mappedCost.GetInput(), 1e-12)
	assert.InDelta(t, 2, mappedCost.GetOutput(), 1e-12)
	assert.InDelta(t, 3, mappedCost.GetCacheRead(), 1e-12)
	assert.InDelta(t, 4, mappedCost.GetCacheWrite(), 1e-12)
	assert.InDelta(t, 10, mappedCost.GetTotal(), 1e-12)
}
