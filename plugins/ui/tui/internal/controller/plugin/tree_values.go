package plugin

import (
	"time"

	"github.com/samber/mo"
)

// TreeNavigationStatus identifies one navigation terminal result.
type TreeNavigationStatus int

const (
	// TreeNavigationUnspecified identifies a missing navigation result.
	TreeNavigationUnspecified TreeNavigationStatus = iota
	// TreeNavigationCommitted identifies a durable navigation commit.
	TreeNavigationCommitted
	// TreeNavigationCanceled identifies navigation canceled before commit.
	TreeNavigationCanceled
)

// OperationIssue contains one safe nonterminal Host issue.
type OperationIssue struct {
	// Code is the stable issue code.
	Code string
	// ExtensionID identifies the extension when present.
	ExtensionID string
	// HandlerID identifies the handler when present.
	HandlerID string
	// Message is the safe Host-owned issue text.
	Message string
}

// TreeEntryKind identifies one public session-tree entry payload.
type TreeEntryKind int

const (
	// TreeEntryUnspecified identifies a missing tree entry kind.
	TreeEntryUnspecified TreeEntryKind = iota
	// TreeEntryUser identifies a user message.
	TreeEntryUser
	// TreeEntryModel identifies a model response.
	TreeEntryModel
	// TreeEntryToolResult identifies a tool result.
	TreeEntryToolResult
	// TreeEntryExtension identifies an opaque extension entry.
	TreeEntryExtension
	// TreeEntryBranchSummary identifies an abandoned-branch summary.
	TreeEntryBranchSummary
	// TreeEntryExtensionMessage identifies a model-visible extension message.
	TreeEntryExtensionMessage
)

// ClientVisibility controls ordinary transcript presentation for an extension message.
type ClientVisibility uint8

const (
	// ClientVisibilityVisible includes the message in the ordinary transcript.
	ClientVisibilityVisible ClientVisibility = iota + 1
	// ClientVisibilityHidden excludes the message from the ordinary transcript.
	ClientVisibilityHidden
)

// ExtensionMessage contains exact model-visible extension data retained in tree state.
type ExtensionMessage struct {
	// ExtensionID identifies the owning extension.
	ExtensionID string
	// EntryType identifies the extension-defined entry kind.
	EntryType string
	// Text contains exact message text.
	Text string
	// Visibility controls ordinary transcript presentation.
	Visibility ClientVisibility
}

// TreeEntry contains one public tree entry and presentation text.
type TreeEntry struct {
	// ID is the persisted entry identifier.
	ID string
	// ParentID identifies the parent entry when present.
	ParentID mo.Option[string]
	// CreatedAt is the persisted entry timestamp.
	CreatedAt time.Time
	// Label is the committed user label. An empty value means no label.
	Label string
	// Kind identifies the entry payload.
	Kind TreeEntryKind
	// ExtensionMessage retains exact model-visible extension data when present.
	ExtensionMessage mo.Option[ExtensionMessage]
	// Text contains the public entry text used for rendering and search.
	Text string
}

// SessionTree contains one complete public tree snapshot.
type SessionTree struct {
	// Entries are ordered by Host persistence order.
	Entries []TreeEntry
	// ActiveLeafID identifies the current leaf when present.
	ActiveLeafID mo.Option[string]
}
