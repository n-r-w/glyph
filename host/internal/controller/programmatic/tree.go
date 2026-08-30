package programmatic

import (
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// SummaryMode identifies requested branch-summary behavior.
type SummaryMode uint8

// Summary modes enumerate public tree-navigation choices.
const (
	// SummaryModeNoSummary disables branch summarization.
	SummaryModeNoSummary SummaryMode = iota
	// SummaryModeSummarize requests built-in branch summarization.
	SummaryModeSummarize
	// SummaryModeSummarizeWithCustomPrompt requests summarization with custom focus.
	SummaryModeSummarizeWithCustomPrompt
)

// TreeNavigationStatus identifies one navigation terminal result.
type TreeNavigationStatus uint8

// Tree navigation statuses enumerate committed and canceled results.
const (
	// TreeNavigationStatusUnspecified identifies a missing terminal status.
	TreeNavigationStatusUnspecified TreeNavigationStatus = iota
	// TreeNavigationStatusCommitted identifies a committed navigation.
	TreeNavigationStatusCommitted
	// TreeNavigationStatusCanceled identifies cancellation before commit.
	TreeNavigationStatusCanceled
)

// TreeNavigationResult contains one committed result or a cancellation marker.
type TreeNavigationResult struct {
	// Status identifies whether navigation committed or was canceled.
	Status TreeNavigationStatus
	// Committed contains state only when Status is committed.
	Committed mo.Option[TreeNavigationCommitted]
}

// TreeNavigationCommitted contains the exact state published after navigation.
type TreeNavigationCommitted struct {
	// Tree is the complete committed tree.
	Tree SessionTree
	// ActiveBranch contains the committed public transcript.
	ActiveBranch []SessionEntry
	// NextInput contains exact editable input when the target is a user message.
	NextInput mo.Option[string]
}

// SessionTree contains every public tree entry and the optional active leaf.
type SessionTree struct {
	// Entries are ordered by persistence order.
	Entries []SessionTreeEntry
	// ActiveLeafID identifies the active leaf when the tree is not at its implicit root.
	ActiveLeafID mo.Option[string]
}

// SessionTreeEntryKind identifies one tree entry payload.
type SessionTreeEntryKind uint8

// Tree entry kinds enumerate public tree payloads.
const (
	// SessionTreeEntryUnspecified identifies no tree payload.
	SessionTreeEntryUnspecified SessionTreeEntryKind = iota
	// SessionTreeEntryUser identifies a user message.
	SessionTreeEntryUser
	// SessionTreeEntryModel identifies a model response.
	SessionTreeEntryModel
	// SessionTreeEntryToolResult identifies a tool result.
	SessionTreeEntryToolResult
	// SessionTreeEntryExtension identifies opaque extension metadata.
	SessionTreeEntryExtension
	// SessionTreeEntryBranchSummary identifies a persisted branch summary.
	SessionTreeEntryBranchSummary
)

// SessionTreeEntry contains one public payload, parent relation, and label state.
type SessionTreeEntry struct {
	// ID identifies the persisted entry.
	ID string
	// ParentID identifies the parent when present.
	ParentID mo.Option[string]
	// CreatedAt is the persisted creation time.
	CreatedAt time.Time
	// Label contains the current label or an empty value.
	Label string
	// Kind identifies the active payload.
	Kind SessionTreeEntryKind
	// User contains a user message.
	User mo.Option[model.Message]
	// Model contains a model response.
	Model mo.Option[ModelResponse]
	// EstimatedCost contains persisted model cost when available.
	EstimatedCost mo.Option[session.EstimatedCost]
	// ToolResult contains a tool result.
	ToolResult mo.Option[ToolResult]
	// Extension contains opaque extension metadata.
	Extension mo.Option[ExtensionEntry]
	// BranchSummary contains a persisted branch summary.
	BranchSummary mo.Option[BranchSummary]
}

// ExtensionEntry identifies one opaque extension entry.
type ExtensionEntry struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// EntryType identifies the extension-defined entry type.
	EntryType string
}

// BranchSummary contains one persisted branch-summary projection.
type BranchSummary struct {
	// Summary contains the persisted summary text.
	Summary string
	// FirstEntryID identifies the first summarized abandoned entry.
	FirstEntryID string
	// LastEntryID identifies the last summarized abandoned entry.
	LastEntryID string
	// Provider identifies the configured summary provider.
	Provider model.ProviderID
	// Model identifies the configured summary model.
	Model model.ID
	// ReasoningChoice identifies summary reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// Usage contains normalized summary usage when available.
	Usage mo.Option[session.TokenUsage]
	// EstimatedCost contains persisted summary cost when available.
	EstimatedCost mo.Option[session.EstimatedCost]
}
