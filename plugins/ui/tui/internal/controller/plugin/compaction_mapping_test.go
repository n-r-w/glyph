//go:build !integration

package plugin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapSessionTreeEntryMapsCompaction verifies the standard TUI tree projection variant.
func TestMapSessionTreeEntryMapsCompaction(t *testing.T) {
	t.Parallel()

	// Arrange one complete session-tree compaction payload.
	entry := uiv1.SessionTreeEntry_builder{
		Id: new("entry"), CreatedTime: timestamppb.New(time.Unix(1, 0).UTC()),
		Compaction: uiv1.Compaction_builder{
			Summary: new("summary"), FirstKeptEntryId: new("kept"),
			Source: uiv1.BranchSummarySource_builder{ExtensionId: new("extension")}.Build(),
		}.Build(),
	}.Build()

	// Act by projecting one tree-entry display value.
	kind, text, err := mapTreeEntryContent(entry)

	// Assert the compaction remains distinct and searchable by its summary.
	require.NoError(t, err)
	require.Equal(t, TreeEntryCompaction, kind)
	require.Equal(t, "summary", text)
}

// TestMapRestoredTranscriptMapsCompactionSummary verifies the standard TUI projection variant.
func TestMapRestoredTranscriptMapsCompactionSummary(t *testing.T) {
	t.Parallel()

	// Arrange one complete restored compaction entry.
	entry := uiv1.SessionEntry_builder{
		Id: new("entry"), CreatedTime: timestamppb.New(time.Unix(1, 0).UTC()),
		Compaction: uiv1.Compaction_builder{
			Summary: new("summary"), FirstKeptEntryId: new("kept"),
			Source: uiv1.BranchSummarySource_builder{ExtensionId: new("extension")}.Build(),
		}.Build(),
	}.Build()

	// Act by rebuilding the standard TUI transcript.
	lines, err := mapRestoredTranscript([]*uiv1.SessionEntry{entry})

	// Assert the compaction remains a distinct display line with exact summary text.
	require.NoError(t, err)
	require.Len(t, lines, 1)
	require.Equal(t, TranscriptCompaction, lines[0].Kind)
	require.Equal(t, "summary", lines[0].Text.MustGet())
}
