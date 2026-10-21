package extension

import (
	"fmt"

	sess "github.com/n-r-w/glyph/host/internal/domain/session"
	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// newHistorySourceKinds pairs original entry kinds with their public correspondence values.
func newHistorySourceKinds() map[sess.HistorySourceKind]pb.CompactionHistorySourceKind {
	return map[sess.HistorySourceKind]pb.CompactionHistorySourceKind{
		sess.HistorySourceUser:             pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_USER,
		sess.HistorySourceModel:            pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_MODEL,
		sess.HistorySourceToolResult:       pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_TOOL_RESULT,
		sess.HistorySourceExtensionMessage: pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_EXTENSION_MESSAGE,
		sess.HistorySourceBranchSummary:    pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_BRANCH_SUMMARY,
		sess.HistorySourceCompaction:       pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_COMPACTION,
	}
}

// mapHistorySourceKind maps captured original entry kinds to public correspondence.
func mapHistorySourceKind(kind sess.HistorySourceKind) (pb.CompactionHistorySourceKind, error) {
	if value, valid := newHistorySourceKinds()[kind]; valid {
		return value, nil
	}
	return pb.CompactionHistorySourceKind_COMPACTION_HISTORY_SOURCE_KIND_UNSPECIFIED, fmt.Errorf(
		"unknown prepared history source kind %q",
		kind,
	)
}

// decodeHistorySourceKind validates public source correspondence before composition.
func decodeHistorySourceKind(kind pb.CompactionHistorySourceKind) (sess.HistorySourceKind, error) {
	for value, wire := range newHistorySourceKinds() {
		if kind == wire {
			return value, nil
		}
	}
	return "", fmt.Errorf("unknown prepared history source kind %d", kind)
}
