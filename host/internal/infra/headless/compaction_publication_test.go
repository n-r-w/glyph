//go:build !integration

package headless

import (
	"bytes"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestPublishSessionEntryAcceptsCommittedCompaction verifies headless commit acknowledgement.
func TestPublishSessionEntryAcceptsCommittedCompaction(t *testing.T) {
	t.Parallel()

	// Arrange one committed extension-produced compaction marker.
	renderer := NewRenderer(new(bytes.Buffer), new(bytes.Buffer))
	entry := session.Entry{
		ID: "entry", ParentID: mo.Some("parent"),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
		}),
	}

	// Act by publishing through the shared session-entry boundary.
	wait, err := renderer.PublishSessionEntry(entry)

	// Assert headless mode acknowledges the committed marker without client output.
	require.NoError(t, err)
	require.NotNil(t, wait)
	require.NoError(t, wait(t.Context()))
}
