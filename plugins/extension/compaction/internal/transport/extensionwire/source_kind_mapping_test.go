//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestPreparedHistorySourceKinds preserves source kinds across policy and participant representations.
func TestPreparedHistorySourceKinds(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		kind compaction.SourceKind
		wire pb.CompactionHistorySourceKind
	}{
		{compaction.SourceUser, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_USER},
		{compaction.SourceModel, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_MODEL},
		{compaction.SourceToolResult, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_TOOL_RESULT},
		{compaction.SourceExtensionMessage, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_EXTENSION_MESSAGE},
		{compaction.SourceBranchSummary, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_BRANCH_SUMMARY},
		{compaction.SourceCompaction, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_COMPACTION},
		{compaction.SourceUnspecified, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_UNSPECIFIED},
	} {
		t.Run(string(scenario.kind), func(t *testing.T) {
			t.Parallel()
			// Arrange prepared user-role content whose original kind is independently supplied.
			wire := new(pb.CompactionHistoryRecord)
			wire.SetSourceEntryId("source")
			wire.SetSourcePosition(3)
			wire.SetSourceKind(scenario.wire)
			wire.SetPreviousSummary(scenario.kind == compaction.SourceCompaction)
			wire.SetPrivatePayloadBytes(12)
			wire.SetEstimatedTokens(9)
			wire.SetUser(newMappingRequest().GetPrefix()[0].GetUser())
			// Act through policy decoding and participant-request encoding.
			decoded := DecodeHistory([]*pb.CompactionHistoryRecord{wire})
			encoded := encodeHistory(decoded)
			// Assert original kind and other control data survive independently of prepared role.
			require.Equal(t, scenario.kind, decoded[0].SourceKind)
			require.Equal(t, compaction.User, decoded[0].Kind)
			require.Equal(t, scenario.wire, encoded[0].GetSourceKind())
			require.Equal(t, wire.GetSourceEntryId(), encoded[0].GetSourceEntryId())
			require.Equal(t, wire.GetSourcePosition(), encoded[0].GetSourcePosition())
			require.Equal(t, wire.GetPreviousSummary(), encoded[0].GetPreviousSummary())
			require.Equal(t, wire.GetPrivatePayloadBytes(), encoded[0].GetPrivatePayloadBytes())
			require.Equal(t, wire.GetEstimatedTokens(), encoded[0].GetEstimatedTokens())
		})
	}
}

// TestComposedRequestRejectsUnknownSourceKind keeps unknown origins out of participant composition.
func TestComposedRequestRejectsUnknownSourceKind(t *testing.T) {
	t.Parallel()
	// Arrange a complete public request with an unrecognized original kind.
	wire := newMappingRequest()
	wire.GetPrefix()[0].SetSourceKind(pb.CompactionHistorySourceKind(99))
	// Act through public decoding and policy request validation.
	request := DecodeRequest(wire)
	err := request.Validate()
	// Assert unknown source metadata remains an explicit validation failure.
	require.Equal(t, compaction.SourceInvalid, request.Prefix[0].SourceKind)
	require.ErrorContains(t, err, "input entry")
}
