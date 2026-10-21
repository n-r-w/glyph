package compaction

// SourceKind identifies the kind of original entry associated with prepared content.
type SourceKind string

const (
	// SourceUnspecified identifies content-only observations and projections.
	SourceUnspecified SourceKind = ""
	// SourceUser identifies an original user message.
	SourceUser SourceKind = "user"
	// SourceModel identifies a model response, including one that causes a skipped result.
	SourceModel SourceKind = "model"
	// SourceToolResult identifies an original stored tool result.
	SourceToolResult SourceKind = "tool_result"
	// SourceExtensionMessage identifies a model-visible extension message.
	SourceExtensionMessage SourceKind = "extension_message"
	// SourceBranchSummary identifies a branch-summary entry.
	SourceBranchSummary SourceKind = "branch_summary"
	// SourceCompaction identifies a compaction marker.
	SourceCompaction SourceKind = "compaction"
	// SourceInvalid identifies unrecognized source correspondence.
	SourceInvalid SourceKind = "invalid"
)

// Valid reports whether a request record identifies an original source kind.
func (k SourceKind) Valid() bool {
	switch k {
	case SourceUser, SourceModel, SourceToolResult, SourceExtensionMessage, SourceBranchSummary, SourceCompaction:
		return true
	case SourceUnspecified, SourceInvalid:
		return false
	}
	return false
}
