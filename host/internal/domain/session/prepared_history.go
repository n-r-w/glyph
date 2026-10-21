package session

import "github.com/n-r-w/glyph/host/internal/domain/agent"

// PreparedHistoryEntry associates model content with its original branch source.
type PreparedHistoryEntry struct {
	// History contains one prepared provider-neutral record.
	History agent.HistoryEntry
	// SourceID identifies the original entry that produced the record.
	SourceID string
	// SourcePosition is the zero-based position on the captured original branch.
	SourcePosition int64
	// SourceKind identifies the kind of original entry associated with the prepared record.
	SourceKind HistorySourceKind
	// PreviousSummary identifies the rendered preceding compaction summary.
	PreviousSummary bool
}

// HistorySourceKind identifies the kind of original entry associated with prepared content.
type HistorySourceKind string

const (
	// HistorySourceUser identifies an original user message.
	HistorySourceUser HistorySourceKind = "user"
	// HistorySourceModel identifies an original model response.
	HistorySourceModel HistorySourceKind = "model"
	// HistorySourceToolResult identifies an original tool result.
	HistorySourceToolResult HistorySourceKind = "tool_result"
	// HistorySourceExtensionMessage identifies a model-visible extension message.
	HistorySourceExtensionMessage HistorySourceKind = "extension_message"
	// HistorySourceBranchSummary identifies a branch-summary entry.
	HistorySourceBranchSummary HistorySourceKind = "branch_summary"
	// HistorySourceCompaction identifies a compaction marker.
	HistorySourceCompaction HistorySourceKind = "compaction"
)

// PreparedHistory contains source-associated model records in prepared order.
type PreparedHistory []PreparedHistoryEntry

// Content returns detached model content without session source metadata.
func (h PreparedHistory) Content() []agent.HistoryEntry {
	history := make([]agent.HistoryEntry, len(h))
	for index := range h {
		history[index] = h[index].History.Clone()
	}
	return history
}

// Clone detaches the content while preserving source correspondence.
func (e PreparedHistoryEntry) Clone() PreparedHistoryEntry {
	e.History = e.History.Clone()
	return e
}
