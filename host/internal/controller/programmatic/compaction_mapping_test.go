//go:build !integration

package programmatic

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapCompactionEntryCoversDetailedAndTreeContracts verifies both Programmatic Control variants.
func TestMapCompactionEntryCoversDetailedAndTreeContracts(t *testing.T) {
	t.Parallel()

	// Arrange one complete controller compaction projection.
	compaction := Compaction{
		Summary: "summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.Some([]byte("details")),
	}

	// Act by mapping detailed entry and tree payloads.
	detailed, err := mapSessionEntries([]SessionEntry{{
		ID: "entry", CreatedAt: time.Unix(1, 0).UTC(), Kind: HistoryEntryCompaction,
		User: mo.None[session.UserMessage](), Model: mo.None[ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
		BranchSummary: mo.None[BranchSummary](), Compaction: mo.Some(compaction),
		ExtensionMessage: mo.None[ExtensionMessage](),
	}})
	require.NoError(t, err)
	tree, err := EncodeSessionTreeEntry(SessionTreeEntry{
		ID: "entry", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(), Label: "",
		Kind: SessionTreeEntryCompaction,
		User: mo.None[session.UserMessage](), Model: mo.None[ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](), ToolResult: mo.None[ToolResult](),
		Extension: mo.None[ExtensionEntry](), BranchSummary: mo.None[BranchSummary](),
		Compaction: mo.Some(compaction), ExtensionMessage: mo.None[ExtensionMessage](),
	})

	// Assert both wire unions select compaction and preserve presence-sensitive details.
	require.NoError(t, err)
	require.Len(t, detailed, 1)
	require.Equal(t, programmaticv1.SessionEntry_Compaction_case, detailed[0].WhichEntry())
	require.Equal(t, "kept", detailed[0].GetCompaction().GetFirstKeptEntryId())
	require.Equal(t, []byte("details"), detailed[0].GetCompaction().GetDetails())
	require.Equal(t, programmaticv1.SessionTreeEntry_Compaction_case, tree.WhichEntry())
}
