//go:build !integration

package uiv1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	wire "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestValidateCompactionCompletionAcceptsCommittedFailure verifies durable state and diagnostics coexist.
func TestValidateCompactionCompletionAcceptsCommittedFailure(t *testing.T) {
	t.Parallel()
	// Arrange one complete committed marker with post-commit failure text and category.
	compaction := wire.Compaction_builder{
		Summary: new("summary"), FirstKeptEntryId: new("kept"),
		Source:        wire.BranchSummarySource_builder{ExtensionId: new("extension")}.Build(),
		EstimatedCost: nil, Details: nil,
	}.Build()
	entry := wire.SessionEntry_builder{
		Id: new("compaction"), CreatedTime: timestamppb.New(time.Unix(1, 0).UTC()),
		User: nil, Model: nil, ToolResult: nil, BranchSummary: nil, ExtensionMessage: nil,
		Compaction: compaction,
	}.Build()
	completed := new(wire.HostCompleted)
	completed.SetCompaction(wire.CompactionResult_builder{
		Committed: entry, Canceled: new(false), Error: new("observer failed after commit"),
		FailureCode: new("EXTENSION_FAILED"),
	}.Build())

	// Act and assert the SDK accepts one usable committed public outcome.
	require.NoError(t, validateHostCompletedFields(completed))
}

// TestValidateSessionCompactionAcceptsEntryAndTreeVariants verifies SDK handling of the closed variants.
func TestValidateSessionCompactionAcceptsEntryAndTreeVariants(t *testing.T) {
	t.Parallel()

	// Arrange one complete extension-produced compaction payload.
	compaction := wire.Compaction_builder{
		Summary: new("summary"), FirstKeptEntryId: new("kept"),
		Source:  wire.BranchSummarySource_builder{ExtensionId: new("extension")}.Build(),
		Details: []byte{},
	}.Build()
	createdTime := timestamppb.New(time.Unix(1, 0).UTC())
	entry := wire.SessionEntry_builder{
		Id: new("entry"), CreatedTime: createdTime, Compaction: compaction,
	}.Build()
	tree := wire.SessionTreeEntry_builder{
		Id: new("entry"), CreatedTime: createdTime, Compaction: compaction,
	}.Build()

	// Act and assert both public closed unions accept the compaction variant.
	require.NoError(t, validateSessionEntry(entry))
	require.NoError(t, validateSessionTreeEntry(tree))
}
