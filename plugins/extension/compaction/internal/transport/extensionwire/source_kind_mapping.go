package extensionwire

import (
	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// newHistorySourceKinds pairs policy source kinds with public original-record correspondence.
func newHistorySourceKinds() map[compaction.SourceKind]pb.CompactionHistorySourceKind {
	return map[compaction.SourceKind]pb.CompactionHistorySourceKind{
		compaction.SourceUser:             pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_USER,
		compaction.SourceModel:            pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_MODEL,
		compaction.SourceToolResult:       pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_TOOL_RESULT,
		compaction.SourceExtensionMessage: pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_EXTENSION_MESSAGE,
		compaction.SourceBranchSummary:    pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_BRANCH_SUMMARY,
		compaction.SourceCompaction:       pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_COMPACTION,
	}
}

// decodeHistorySourceKind maps public source kinds, including absent and unrecognized kinds.
func decodeHistorySourceKind(kind pb.CompactionHistorySourceKind) compaction.SourceKind {
	if kind == pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_UNSPECIFIED {
		return compaction.SourceUnspecified
	}
	for value, wire := range newHistorySourceKinds() {
		if kind == wire {
			return value
		}
	}
	return compaction.SourceInvalid
}

// encodeHistorySourceKind maps original-record correspondence to the public contract.
func encodeHistorySourceKind(kind compaction.SourceKind) pb.CompactionHistorySourceKind {
	return newHistorySourceKinds()[kind]
}
