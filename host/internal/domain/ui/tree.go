package ui

import (
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// SummaryMode identifies requested branch-summary behavior.
type SummaryMode uint8

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

const (
	// TreeNavigationStatusUnspecified identifies a missing terminal status.
	TreeNavigationStatusUnspecified TreeNavigationStatus = iota
	// TreeNavigationStatusCommitted identifies committed navigation.
	TreeNavigationStatusCommitted
	// TreeNavigationStatusCanceled identifies cancellation before commit.
	TreeNavigationStatusCanceled
)

// OperationIssueCode identifies one safe nonterminal extension issue.
type OperationIssueCode uint8

const (
	// OperationIssueHandlerError reports an ordinary request or result handler error.
	OperationIssueHandlerError OperationIssueCode = iota + 1
	// OperationIssueInvalidHandlerAction reports an invalid request or result action.
	OperationIssueInvalidHandlerAction
	// OperationIssueObserverError reports a failed post-commit observer.
	OperationIssueObserverError
)

// OperationIssue reports one safe ordered handler or observer issue.
type OperationIssue struct {
	// Code identifies the issue class.
	Code OperationIssueCode
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// HandlerID identifies the registered handler.
	HandlerID string
	// Message contains a safe Host-owned description.
	Message string
}

// TreeNavigationResult contains one committed result or cancellation marker.
type TreeNavigationResult struct {
	// Status identifies whether navigation committed or was canceled.
	Status TreeNavigationStatus
	// Committed contains state only for committed navigation.
	Committed mo.Option[TreeNavigationCommitted]
	// Issues contains safe nonterminal extension issues in occurrence order.
	Issues []OperationIssue
}

// TreeNavigationCommitted contains exact state published after navigation.
type TreeNavigationCommitted struct {
	// Tree is the complete committed tree.
	Tree SessionTree
	// ActiveBranch contains the committed public transcript.
	ActiveBranch []SessionEntry
	// NextInput contains exact editable user input when present.
	NextInput mo.Option[string]
}

// SessionTree contains every public tree entry and optional active leaf.
type SessionTree struct {
	// Entries are ordered by persistence order.
	Entries []SessionTreeEntry
	// ActiveLeafID identifies the active leaf when present.
	ActiveLeafID mo.Option[string]
}

// SessionTreeEntryKind identifies one tree payload.
type SessionTreeEntryKind uint8

const (
	// SessionTreeEntryUnspecified identifies a missing payload.
	SessionTreeEntryUnspecified SessionTreeEntryKind = iota
	// SessionTreeEntryUser identifies a user message.
	SessionTreeEntryUser
	// SessionTreeEntryModel identifies a model response.
	SessionTreeEntryModel
	// SessionTreeEntryToolResult identifies a tool result.
	SessionTreeEntryToolResult
	// SessionTreeEntryExtension identifies opaque extension metadata.
	SessionTreeEntryExtension
	// SessionTreeEntryBranchSummary identifies a branch summary.
	SessionTreeEntryBranchSummary
)

// SessionTreeEntry contains one payload, parent relation, and label state.
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
	// ToolResult contains a tool result.
	ToolResult mo.Option[agent.ToolResult]
	// Extension contains opaque extension metadata.
	Extension mo.Option[ExtensionEntry]
	// BranchSummary contains a persisted summary.
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
	// Summary contains persisted summary text.
	Summary string
	// FirstEntryID identifies the first summarized entry.
	FirstEntryID string
	// LastEntryID identifies the last summarized entry.
	LastEntryID string
	// Provider identifies the configured summary provider.
	Provider model.ProviderID
	// Model identifies the configured summary model.
	Model model.ID
	// ReasoningChoice identifies summary reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// Usage contains normalized usage when available.
	Usage mo.Option[session.TokenUsage]
	// EstimatedCost contains persisted cost when available.
	EstimatedCost mo.Option[session.EstimatedCost]
}
