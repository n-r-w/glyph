//go:build !integration

package uiv1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	wire "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

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
