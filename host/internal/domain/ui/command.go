package ui

import "github.com/samber/mo"

// CommandKind identifies one UI-to-Host command.
type CommandKind uint8

const (
	// CommandSubmit starts one user request while idle.
	CommandSubmit CommandKind = iota + 1
	// CommandRetryAuthentication retries OAuth after failure.
	CommandRetryAuthentication
	// CommandSelectModel requests one configured model.
	CommandSelectModel
	// CommandSelectReasoningChoice requests one reasoning choice for the active model.
	CommandSelectReasoningChoice
	// CommandCreateSession requests a new active session.
	CommandCreateSession
	// CommandListSessions requests stored sessions.
	CommandListSessions
	// CommandResumeSession requests active-session replacement.
	CommandResumeSession
	// CommandSetSessionName requests a persisted name.
	CommandSetSessionName
	// CommandGetSessionInfo requests active-session information.
	CommandGetSessionInfo
	// CommandGetSessionTree requests the complete active-session tree.
	CommandGetSessionTree
	// CommandNavigateSessionTree requests tree navigation.
	CommandNavigateSessionTree
	// CommandForkSession requests active-session fork.
	CommandForkSession
	// CommandCloneSession requests active-branch clone.
	CommandCloneSession
	// CommandSetEntryLabel requests an entry-label mutation.
	CommandSetEntryLabel
)

// Command carries exactly one UI-to-Host command.
type Command struct {
	// OperationID identifies the public UI operation.
	OperationID string
	// Kind identifies the requested Host action and active payload.
	Kind CommandKind
	// Text contains submitted user text.
	Text mo.Option[string]
	// ProviderID identifies a requested model provider.
	ProviderID mo.Option[string]
	// ModelID identifies a requested provider model.
	ModelID mo.Option[string]
	// ReasoningChoice identifies a requested reasoning behavior.
	ReasoningChoice mo.Option[ReasoningChoice]
	// SessionID is present only for resume.
	SessionID mo.Option[string]
	// SessionName preserves presence so the Host can reject an explicitly empty name.
	SessionName mo.Option[string]
	// TargetEntryID identifies the selected navigation target.
	TargetEntryID mo.Option[string]
	// SummaryMode identifies branch-summary behavior.
	SummaryMode SummaryMode
	// CustomFocus preserves custom-prompt presence for validation.
	CustomFocus mo.Option[string]
	// EntryLabel preserves label presence, including an explicitly empty clear value.
	EntryLabel mo.Option[string]
}
