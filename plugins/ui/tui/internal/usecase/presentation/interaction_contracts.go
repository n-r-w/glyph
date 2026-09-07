package presentation

import (
	"github.com/samber/mo"
)

// CommandKind identifies one accepted command sent to the Host.
type CommandKind uint8

const (
	// CommandUnspecified represents a missing UI command kind.
	CommandUnspecified CommandKind = iota
	// CommandSubmit sends one user request.
	CommandSubmit
	// CommandStop requests cancellation of the active run.
	CommandStop
	// CommandRetryAuthentication requests a new authentication attempt.
	CommandRetryAuthentication
	// CommandQuit requests UI-mode termination.
	CommandQuit
	// CommandSelectModel requests one configured model.
	CommandSelectModel
	// CommandSelectReasoningChoice requests one reasoning choice.
	CommandSelectReasoningChoice
	// CommandCreateSession requests a new session.
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
	// CommandNavigateSessionTree requests navigation to one selected entry.
	CommandNavigateSessionTree
	// CommandForkSession requests a replacement session before one user message.
	CommandForkSession
	// CommandCloneSession requests a replacement session from the active branch.
	CommandCloneSession
	// CommandSetEntryLabel requests one persistent entry-label mutation.
	CommandSetEntryLabel
)

// Command is one user request emitted through the UI stream.
type Command struct {
	// Kind identifies the Host action and active payload.
	Kind CommandKind
	// Text contains submitted user text.
	Text mo.Option[string]
	// ProviderID identifies a requested model provider.
	ProviderID mo.Option[string]
	// ModelID identifies a requested provider model.
	ModelID mo.Option[string]
	// ReasoningChoice identifies a requested reasoning behavior.
	ReasoningChoice mo.Option[ReasoningChoice]
	// SessionID is present only when the user confirms a resume row.
	SessionID mo.Option[string]
	// SessionName preserves an explicitly empty value for Host validation.
	SessionName mo.Option[string]
	// TreeCommand contains session-tree command data when present.
	TreeCommand mo.Option[TreeCommand]
}

// TreeCommand contains one tree command payload.
type TreeCommand struct {
	// TargetEntryID identifies the selected entry when required.
	TargetEntryID mo.Option[string]
	// SummaryMode identifies navigation summary behavior.
	SummaryMode SummaryMode
	// CustomFocus preserves exact custom summary focus when present.
	CustomFocus mo.Option[string]
	// Label preserves an explicitly empty label for clearing.
	Label mo.Option[string]
}
