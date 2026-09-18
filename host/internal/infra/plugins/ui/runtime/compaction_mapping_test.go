//go:build !integration

package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapCompactionEntryCoversRestoredAndTreeContracts verifies both UI Plugin Contract variants.
func TestMapCompactionEntryCoversRestoredAndTreeContracts(t *testing.T) {
	t.Parallel()

	// Arrange one complete controller compaction projection.
	compaction := controllerui.Compaction{
		Summary: "summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.Some([]byte("details")),
	}

	// Act by mapping restored transcript and tree payloads.
	restored, err := mapRestoredSessionEntries([]controllerui.SessionEntry{{
		ID: "entry", CreatedAt: time.Unix(1, 0).UTC(), Kind: controllerui.SessionEntryCompaction,
		User: mo.None[session.UserMessage](), Model: mo.None[controllerui.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), BranchSummary: mo.None[controllerui.BranchSummary](),
		Compaction: mo.Some(compaction), ExtensionMessage: mo.None[controllerui.ExtensionMessage](),
	}})
	require.NoError(t, err)
	tree, err := mapSessionTreeEntry(controllerui.SessionTreeEntry{
		ID: "entry", ParentID: mo.Some("parent"), CreatedAt: time.Unix(1, 0).UTC(), Label: "",
		Kind: controllerui.SessionTreeEntryCompaction,
		User: mo.None[session.UserMessage](), Model: mo.None[controllerui.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[controllerui.ExtensionEntry](),
		BranchSummary: mo.None[controllerui.BranchSummary](), Compaction: mo.Some(compaction),
		ExtensionMessage: mo.None[controllerui.ExtensionMessage](),
	})

	// Assert both wire unions select compaction and preserve presence-sensitive details.
	require.NoError(t, err)
	require.Len(t, restored, 1)
	require.Equal(t, uiv1.SessionEntry_Compaction_case, restored[0].WhichEntry())
	require.Equal(t, "kept", restored[0].GetCompaction().GetFirstKeptEntryId())
	require.Equal(t, []byte("details"), restored[0].GetCompaction().GetDetails())
	require.Equal(t, uiv1.SessionTreeEntry_Compaction_case, tree.WhichEntry())
}
