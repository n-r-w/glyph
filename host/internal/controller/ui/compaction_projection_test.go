//go:build !integration

package ui

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestProjectSessionCompactionMapsTranscriptAndTreeVariants verifies both public UI entry projections.
func TestProjectSessionCompactionMapsTranscriptAndTreeVariants(t *testing.T) {
	t.Parallel()

	// Arrange one extension-produced compaction marker with present details.
	entry := session.Entry{
		ID: "compaction", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(),
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

	// Act by projecting restored transcript and full tree entries.
	transcript, present, err := ProjectSessionEntry(entry, 0)
	require.NoError(t, err)
	tree, err := ProjectSessionTreeEntry(entry, "label")

	// Assert both owning projections preserve the compaction payload and kind.
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, SessionEntryCompaction, transcript.Kind)
	require.Equal(t, "kept", transcript.Compaction.MustGet().FirstKeptEntryID)
	require.Equal(t, []byte("details"), transcript.Compaction.MustGet().Details.MustGet())
	require.Equal(t, SessionTreeEntryCompaction, tree.Kind)
	require.Equal(t, "summary", tree.Compaction.MustGet().Summary)
}
