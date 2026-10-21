//go:build !integration

package extension

import (
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestPreparedSourceKindsRoundTrip preserves original kinds independently of prepared roles.
func TestPreparedSourceKindsRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind session.HistorySourceKind
		wire pb.CompactionHistorySourceKind
	}{
		{session.HistorySourceUser, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_USER},
		{session.HistorySourceModel, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_MODEL},
		{session.HistorySourceToolResult, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_TOOL_RESULT},
		{
			session.HistorySourceExtensionMessage,
			pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_EXTENSION_MESSAGE,
		},
		{
			session.HistorySourceBranchSummary,
			pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_BRANCH_SUMMARY,
		},
		{session.HistorySourceCompaction, pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_COMPACTION},
	}
	for _, scenario := range cases {
		t.Run(string(scenario.kind), func(t *testing.T) {
			t.Parallel()
			// Arrange rendered user content or a result whose source is a stored record or producing response.
			entry := newEncodingTestEntry()
			entry.SourceKind = scenario.kind
			entry.PreviousSummary = scenario.kind == session.HistorySourceCompaction
			entry.History.Kind = agent.HistoryEntryUser
			entry.History.User = mo.Some(model.TextMessage("prepared source"))
			if scenario.kind == session.HistorySourceModel || scenario.kind == session.HistorySourceToolResult {
				entry.History.Kind = agent.HistoryEntryToolResult
				entry.History.User = mo.None[model.Message]()
				entry.History.ToolResult = mo.Some(
					agent.ToolResult{
						CallID:   "call",
						ToolName: "fixture",
						Contents: tool.TextContents("prepared result"),
						IsError:  scenario.kind == session.HistorySourceModel,
					},
				)
			}
			values := []CompactionRequestEntry{{Entry: entry, PrivateBytes: 19, EstimatedTokens: 7}}
			// Act through public composition encoding and decoding.
			wires, err := MapCompactionRequestEntries(values)
			require.NoError(t, err)
			decoded, err := decodePolicyRequestEntries(wires)
			// Assert the wire origin and detached record retain content, original kind, and accounting.
			require.NoError(t, err)
			require.Equal(t, scenario.wire, wires[0].GetSourceKind())
			require.Equal(t, values, decoded)
		})
	}
}

// TestPreparedSourceKindRejectsUnknownOrigin rejects absent and unrecognized origins in composed requests.
func TestPreparedSourceKindRejectsUnknownOrigin(t *testing.T) {
	t.Parallel()
	for _, kind := range []pb.CompactionHistorySourceKind{
		pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_UNSPECIFIED, -1, 99,
	} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			t.Parallel()
			// Arrange a source-bearing request record with an invalid original kind.
			wire := new(pb.CompactionHistoryRecord)
			wire.SetSourceEntryId("source")
			wire.SetSourceKind(kind)
			wire.SetUser(mapCompactionUser(model.TextMessage("prepared source")))
			// Act through the public request decoder.
			_, err := DecodeCompactionRequestEntry(wire)
			// Assert invalid correspondence remains a complete public validation cause.
			require.ErrorContains(t, err, "source kind")
		})
	}
}
