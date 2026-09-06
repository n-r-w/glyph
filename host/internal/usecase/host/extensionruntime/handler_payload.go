package extensionruntime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// InvocationKind identifies one process handler operation variant.
type InvocationKind uint8

const (
	// InvocationRequest invokes a navigation-request handler.
	InvocationRequest InvocationKind = iota + 1
	// InvocationResult invokes a summary-result handler.
	InvocationResult
	// InvocationObserver invokes a committed-navigation observer.
	InvocationObserver
)

// Navigation contains the process-visible navigation request, before capability validation.
type Navigation struct {
	// TargetEntryID identifies the requested destination.
	TargetEntryID string
	// SummaryMode preserves the raw summary-mode discriminator.
	SummaryMode int32
	// CustomFocus preserves omitted versus present custom text.
	CustomFocus mo.Option[string]
	// SummaryModel identifies the configured summary model.
	SummaryModel model.Selection
}

// Preparation contains only the extension-visible part of a prepared navigation.
type Preparation struct {
	// SessionID identifies the active session.
	SessionID string
	// PrecedingActiveLeafID preserves source-position presence.
	PrecedingActiveLeafID mo.Option[string]
	// Request contains the associated process-visible request.
	Request Navigation
	// DestinationID preserves destination presence.
	DestinationID mo.Option[string]
	// CommonAncestorID preserves common-ancestor presence.
	CommonAncestorID mo.Option[string]
	// Entries contains ordered projected abandoned entries without hidden state.
	Entries []TreeEntry
}

// Content contains public model content without a provider-context field.
type Content struct {
	// Kind identifies the provider-neutral content alternative.
	Kind model.ContentKind
	// Text preserves public text presence and exact value.
	Text mo.Option[string]
	// ToolCall preserves a finalized provider-neutral call.
	ToolCall mo.Option[model.ToolCall]
}

// ExtensionIdentity exposes ownership and type without hidden extension bytes.
type ExtensionIdentity struct {
	// ExtensionID identifies the owner of hidden state.
	ExtensionID string
	// EntryType identifies the owner-defined state kind.
	EntryType string
}

// TreeEntry contains only the process-visible projection of one stored entry.
type TreeEntry struct {
	// ID identifies the stored entry.
	ID string
	// User contains ordered user input when present.
	User mo.Option[session.UserMessage]
	// Model contains finalized public model content when present.
	Model mo.Option[[]Content]
	// ToolResult contains a terminal tool result when present.
	ToolResult mo.Option[session.ToolResult]
	// BranchSummary contains summary text when present.
	BranchSummary mo.Option[string]
	// Extension identifies hidden extension state without its bytes.
	Extension mo.Option[ExtensionIdentity]
	// ExtensionMessage contains model-visible extension message text and client visibility.
	ExtensionMessage mo.Option[session.ExtensionMessage]
}

// Summary contains public summary output and its provider-neutral source.
type Summary struct {
	// Source identifies the summary producer and usage.
	Source session.BranchSummarySource
	// Summary preserves exact summary text.
	Summary string
}

// CommittedSummary contains the complete public summary projection after navigation commit.
type CommittedSummary struct {
	// ID identifies the committed summary entry.
	ID string
	// Summary contains the domain summary and its accounting facts.
	Summary session.BranchSummaryEntry
}

// TreeCommit contains the process-visible result of a committed navigation.
type TreeCommit struct {
	// SessionID identifies the active session.
	SessionID string
	// TargetEntryID identifies the final requested target.
	TargetEntryID string
	// PrecedingActiveLeafID preserves source-position presence.
	PrecedingActiveLeafID mo.Option[string]
	// NavigationDestinationID preserves destination presence.
	NavigationDestinationID mo.Option[string]
	// CommittedActiveLeafID preserves the committed position presence.
	CommittedActiveLeafID mo.Option[string]
	// CreatedSummary contains only the public committed summary data.
	CreatedSummary mo.Option[CommittedSummary]
}

// HandlerInvocation binds one process operation to filtered original and current state.
type HandlerInvocation struct {
	// Context is the trusted runtime and active-session binding.
	Context extension.Context
	// Kind identifies the operation payload to encode.
	Kind InvocationKind
	// Original contains the immutable original request and preparation.
	Original Preparation
	// Current contains the latest accepted request and preparation.
	Current Preparation
	// OriginalResult preserves the original result entering result handlers.
	OriginalResult mo.Option[Summary]
	// CurrentResult preserves current result presence, including a cleared result.
	CurrentResult mo.Option[Summary]
	// Commit contains committed facts only for observer operations.
	Commit mo.Option[TreeCommit]
}

// HandlerAction contains decoded process action values before capability policy.
type HandlerAction struct {
	// Kind identifies the response variant validated by transport.
	Kind InvocationKind
	// Cancel carries the process cancellation intent.
	Cancel bool
	// RequestAction retains the raw request preservation or replacement discriminator.
	RequestAction int32
	// Request preserves replacement request presence.
	Request mo.Option[Navigation]
	// ResultAction retains the raw preserve, replace, or clear discriminator.
	ResultAction int32
	// Result preserves replacement summary presence.
	Result mo.Option[Summary]
}
