//go:build !integration

package extensionruntime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestProjectTreeEntryPreservesPublicCompactionPayload verifies extension session-tree projection.
func TestProjectTreeEntryPreservesPublicCompactionPayload(t *testing.T) {
	t.Parallel()

	// Arrange one persisted compaction entry with opaque result details.
	entry := session.Entry{
		ID: "entry", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.Some([]byte("details")),
		}),
	}

	// Act by projecting the session-tree entry for extension transport.
	projected := projectTreeEntry(entry)

	// Assert the complete public compaction payload is retained independently.
	compaction, present := projected.Compaction.Get()
	require.True(t, present)
	require.Equal(t, "kept", compaction.FirstKeptEntryID)
	require.Equal(t, []byte("details"), compaction.Details.MustGet())
}
