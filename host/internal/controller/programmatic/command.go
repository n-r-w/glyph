package programmatic

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// CommandKind identifies one Programmatic Control operation.
type CommandKind uint8

// Command values enumerate supported operations.
const (
	// CommandUnspecified identifies a missing command payload.
	CommandUnspecified CommandKind = iota
	// CommandUserRequest identifies one agent request.
	CommandUserRequest
	// CommandAbort identifies active-run cancellation.
	CommandAbort
	// CommandGetRunState identifies a run-state query.
	CommandGetRunState
	// CommandGetMessages identifies a public-history query.
	CommandGetMessages
	// CommandGetModels identifies a configured-model query.
	CommandGetModels
	// CommandSelectModel identifies provider and model selection.
	CommandSelectModel
	// CommandSelectReasoningChoice identifies reasoning selection.
	CommandSelectReasoningChoice
	// CommandCreateSession identifies active-session creation.
	CommandCreateSession
	// CommandListSessions identifies a stored-session query.
	CommandListSessions
	// CommandResumeSession identifies active-session replacement.
	CommandResumeSession
	// CommandSetSessionName identifies a session-name mutation.
	CommandSetSessionName
	// CommandGetSessionInfo identifies an active-session information query.
	CommandGetSessionInfo
	// CommandGetSessionEntries identifies an active-transcript query.
	CommandGetSessionEntries
	// CommandGetSessionStats identifies an active-session statistics query.
	CommandGetSessionStats
	// CommandGetSessionTree identifies a complete tree query.
	CommandGetSessionTree
	// CommandNavigateSessionTree identifies tree navigation.
	CommandNavigateSessionTree
)

// Command is one correlated transport-independent controller operation.
type Command struct {
	// CorrelationID identifies the command and its result.
	CorrelationID string
	// Kind identifies the requested controller operation.
	Kind CommandKind
	// UserText contains submitted user text.
	UserText mo.Option[string]
	// ProviderID identifies a requested model provider.
	ProviderID mo.Option[model.ProviderID]
	// ModelID identifies a requested provider model.
	ModelID mo.Option[model.ID]
	// ReasoningChoice identifies a requested reasoning behavior.
	ReasoningChoice mo.Option[model.ReasoningChoice]
	// SessionID is present only for resume.
	SessionID mo.Option[session.ID]
	// SessionName is present only for naming and preserves an explicitly empty value for validation.
	SessionName mo.Option[string]
	// TargetEntryID identifies the selected navigation target.
	TargetEntryID mo.Option[string]
	// SummaryMode identifies requested branch-summary behavior.
	SummaryMode SummaryMode
	// CustomFocus preserves custom-prompt presence for validation.
	CustomFocus mo.Option[string]
}
