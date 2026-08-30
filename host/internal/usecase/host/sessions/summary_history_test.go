package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// TestHistoryFromEntriesProjectsBranchSummaryAsSyntheticUserContext verifies provider-neutral summary replay and extension exclusion.
func TestHistoryFromEntriesProjectsBranchSummaryAsSyntheticUserContext(t *testing.T) {
	t.Parallel()

	// Arrange one model-hidden extension and one persisted branch summary.
	entries := []session.Entry{
		{
			ID: "extension", ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0).UTC(),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(session.ExtensionEnvelope{ExtensionID: "hidden", EntryType: "state", Data: []byte("secret")}),
			BranchSummary: mo.None[session.BranchSummaryEntry](),
		},
		{
			ID: "summary", ParentID: mo.Some("extension"), CreatedAt: time.Unix(2, 0).UTC(),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			BranchSummary: mo.Some(session.BranchSummaryEntry{
				Summary: "exact persisted summary", FirstEntryID: "source-first", LastEntryID: "source-last",
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
				Usage: mo.None[session.TokenUsage](), EstimatedCost: mo.None[session.EstimatedCost](),
			}),
		},
	}

	// Act by projecting the active entries into provider history.
	history := sessiontree.HistoryFromEntries(entries)

	// Assert the summary is one synthetic user message and hidden extension data is absent.
	require.Len(t, history, 1)
	assert.True(t, history[0].User.IsSome())
	assert.True(t, history[0].Model.IsNone())
	assert.True(t, history[0].ToolResult.IsNone())
}
