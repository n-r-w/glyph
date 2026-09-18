//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestMapSessionEntryMapsCompactionVariant verifies the Extension Contract tree projection.
func TestMapSessionEntryMapsCompactionVariant(t *testing.T) {
	t.Parallel()

	// Arrange one complete extension-runtime compaction projection.
	entry := extensionruntime.TreeEntry{
		ID: "entry", User: mo.None[session.UserMessage](), Model: mo.None[[]extensionruntime.Content](),
		ToolResult: mo.None[session.ToolResult](), BranchSummary: mo.None[string](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.Some([]byte("details")),
		}),
		Extension:        mo.None[extensionruntime.ExtensionIdentity](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
	}

	// Act by mapping to the public Extension Contract.
	wire, err := mapSessionEntry(entry)

	// Assert the compaction union and all presence-sensitive fields are preserved.
	require.NoError(t, err)
	require.Equal(t, extensionpb.SessionTreeEntry_Compaction_case, wire.WhichContent())
	require.Equal(t, "kept", wire.GetCompaction().GetFirstKeptEntryId())
	require.Equal(t, []byte("details"), wire.GetCompaction().GetDetails())
}
