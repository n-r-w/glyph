package programmatic

import (
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// CommandKind identifies one Programmatic Control operation.
type CommandKind uint8

// Command values enumerate supported operations.
const (
	CommandUnspecified CommandKind = iota
	CommandUserRequest
	CommandAbort
	CommandGetRunState
	CommandGetMessages
	CommandGetModels
	CommandSelectModel
	CommandSelectReasoningChoice
	CommandCreateSession
	CommandListSessions
	CommandResumeSession
	CommandSetSessionName
	CommandGetSessionInfo
	CommandGetSessionEntries
	CommandGetSessionStats
)

// Command is one correlated transport-independent controller operation.
type Command struct {
	CorrelationID   string
	Kind            CommandKind
	UserText        mo.Option[string]
	ProviderID      mo.Option[model.ProviderID]
	ModelID         mo.Option[model.ID]
	ReasoningChoice mo.Option[model.ReasoningChoice]
	// SessionID is present only for resume.
	SessionID mo.Option[session.ID]
	// SessionName is present only for naming and preserves an explicitly empty value for validation.
	SessionName mo.Option[string]
}

// ResponseKind identifies one command result.
type ResponseKind uint8

// Response values enumerate command results.
const (
	ResponseUnspecified ResponseKind = iota
	ResponseUserRequestAccepted
	ResponseAbortCompleted
	ResponseRunState
	ResponseMessages
	ResponseRejected
	ResponseModels
	ResponseModelSelection
	ResponseSessionInfo
	ResponseSessions
	ResponseSessionEntries
	ResponseSessionStats
)

// RejectionCode identifies why a correlated command was not executed.
type RejectionCode uint8

// Rejection values enumerate closed rejection reasons.
const (
	RejectionUnspecified RejectionCode = iota
	RejectionInvalidArgument
	RejectionBusy
	RejectionNoActiveRun
	RejectionCorrelationInUse
	RejectionInternal
	RejectionNotFound
	RejectionReasoningUnsupported
	RejectionCredentialUnavailable
)

// Response is the single result of one correlated command.
type Response struct {
	CorrelationID string
	Kind          ResponseKind
	State         mo.Option[RunStateResult]
	Messages      []HistoryEntry
	Models        mo.Option[ModelsResult]
	Selection     mo.Option[model.Selection]
	// SessionInfo is present for create, resume, name, and information results.
	SessionInfo mo.Option[session.Info]
	// Sessions contains the ordered list result.
	Sessions []session.Summary
	// SessionEntries contains detailed active-session text entries.
	SessionEntries []SessionEntry
	// SessionStatistics is present only for a statistics result.
	SessionStatistics mo.Option[session.Statistics]
	Rejection         mo.Option[Rejection]
}

// SessionEntry contains stable metadata and one public terminal payload.
type SessionEntry struct {
	ID        string
	CreatedAt time.Time
	Kind      HistoryEntryKind
	// User carries ordered public text and image content for detailed session entries.
	User       mo.Option[model.Message]
	Model      mo.Option[ModelResponse]
	ToolResult mo.Option[ToolResult]
}

// ModelsResult contains configured models and the active selection.
type ModelsResult struct {
	Models          []model.Descriptor
	ActiveSelection mo.Option[model.Selection]
}

// Rejection describes one command failure that keeps the session open.
type Rejection struct {
	Command CommandKind
	Code    RejectionCode
	Message string
}

// RunState identifies whether Agent Core can accept a user request.
type RunState uint8

// RunState values enumerate public run states.
const (
	RunStateUnspecified RunState = iota
	RunStateIdle
	RunStateRunning
)

// RunStateResult is a public snapshot without partial provider state.
type RunStateResult struct {
	State               RunState
	ActiveCorrelationID mo.Option[string]
}

// HistoryEntryKind identifies one public history entry.
type HistoryEntryKind uint8

// HistoryEntry values enumerate public message kinds.
const (
	HistoryEntryUnspecified HistoryEntryKind = iota
	HistoryEntryUser
	HistoryEntryModel
	HistoryEntryToolResult
)

// HistoryEntry is one ordered public conversation entry.
type HistoryEntry struct {
	Kind HistoryEntryKind
	// User carries ordered public text and image content.
	User       mo.Option[model.Message]
	Model      mo.Option[ModelResponse]
	ToolResult mo.Option[ToolResult]
}

// AgentEventType identifies one correlated agent lifecycle event.
type AgentEventType uint8

// AgentEvent values enumerate public lifecycle events.
const (
	AgentEventUnspecified AgentEventType = iota
	AgentEventAgentStart
	AgentEventTurnStart
	AgentEventMessageStart
	AgentEventModelContentStart
	AgentEventModelTextDelta
	AgentEventModelContentEnd
	AgentEventToolCallStart
	AgentEventToolCallDelta
	AgentEventToolCallEnd
	AgentEventMessageEnd
	AgentEventToolExecutionStart
	AgentEventToolExecutionUpdate
	AgentEventToolExecutionEnd
	AgentEventToolResult
	AgentEventTurnEnd
	AgentEventAgentEnd
	AgentEventAgentSettled
)

// AgentEvent is one synchronous event for the accepted user request.
type AgentEvent struct {
	CorrelationID   string
	Type            AgentEventType
	RunID           string
	ModelContent    mo.Option[ModelContent]
	ToolCallPreview mo.Option[ToolCallPreview]
	FinalToolCall   mo.Option[FinalToolCall]
	ToolExecution   mo.Option[ToolExecution]
	ToolProgress    mo.Option[ToolProgress]
	ToolResult      mo.Option[ToolResult]
	ModelResponse   mo.Option[ModelResponse]
	Turn            mo.Option[TurnSummary]
	Agent           mo.Option[AgentSummary]
}

// ModelContentKind identifies public model content.
type ModelContentKind uint8

// ModelContent values enumerate public model content kinds.
const (
	ModelContentUnspecified ModelContentKind = iota
	ModelContentText
	ModelContentReasoning
	ModelContentRefusal
)

// ModelContent carries one model content lifecycle update.
type ModelContent struct {
	Kind     ModelContentKind
	Position int
	Text     mo.Option[string]
}

// ToolCallPreviewFieldKind identifies the present preview field payload.
type ToolCallPreviewFieldKind uint8

// ToolCallPreviewField values preserve complete and prefix payload presence.
const (
	ToolCallPreviewFieldUnspecified ToolCallPreviewFieldKind = iota
	ToolCallPreviewFieldComplete
	ToolCallPreviewFieldPrefix
)

// ToolCallPreviewField is one complete or provisional argument field.
type ToolCallPreviewField struct {
	Name   string
	Kind   ToolCallPreviewFieldKind
	Value  mo.Option[any]
	Prefix mo.Option[string]
}

// ToolCallPreview is the complete current preview for one tool call.
type ToolCallPreview struct {
	CallID      string
	Name        string
	Position    int
	Provisional bool
	Fields      []ToolCallPreviewField
}

// FinalToolCall is one finalized public tool call.
type FinalToolCall struct {
	CallID    string
	Name      string
	Position  int
	Arguments map[string]any
}

// ToolExecution identifies a tool invocation.
type ToolExecution struct {
	CallID   string
	ToolName string
}

// ProgressChannel identifies one tool progress stream.
type ProgressChannel uint8

// ProgressChannel values enumerate tool progress streams.
const (
	ProgressChannelUnspecified ProgressChannel = iota
	ProgressChannelStatus
	ProgressChannelStdout
	ProgressChannelStderr
)

// ToolProgress carries one tool execution update.
type ToolProgress struct {
	Channel ProgressChannel
	Content string
}

// ToolResultContentKind identifies text or image output.
type ToolResultContentKind uint8

// ToolResultContent values enumerate public tool result blocks.
const (
	ToolResultContentUnspecified ToolResultContentKind = iota
	ToolResultContentText
	ToolResultContentImage
)

// ToolResultImage carries one exact image result.
type ToolResultImage struct {
	MediaType string
	Data      []byte
}

// ToolResultContent is one ordered tool result block.
type ToolResultContent struct {
	Kind  ToolResultContentKind
	Text  mo.Option[string]
	Image mo.Option[ToolResultImage]
}

// ToolResult is one complete public tool result.
type ToolResult struct {
	CallID   string
	ToolName string
	Contents []ToolResultContent
	IsError  bool
}

// ModelOutcome identifies the terminal model response outcome.
type ModelOutcome uint8

// ModelOutcome values enumerate terminal model outcomes.
const (
	ModelOutcomeUnspecified ModelOutcome = iota
	ModelOutcomeStop
	ModelOutcomeToolUse
	ModelOutcomeLength
	ModelOutcomeAborted
	ModelOutcomeFailed
)

// ModelResponseContentKind identifies one public response item.
type ModelResponseContentKind uint8

// ModelResponseContent values enumerate ordered public response items.
const (
	ModelResponseContentUnspecified ModelResponseContentKind = iota
	ModelResponseContentText
	ModelResponseContentRefusal
	ModelResponseContentReasoning
	ModelResponseContentToolCall
)

// ModelResponseContent is one ordered public response item.
type ModelResponseContent struct {
	Kind     ModelResponseContentKind
	Text     mo.Option[string]
	ToolCall mo.Option[FinalToolCall]
}

// ModelUsage contains provider token accounting.
type ModelUsage struct {
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	ReasoningTokens   int64
	TotalTokens       int64
}

// ModelDiagnostic is one provider diagnostic.
type ModelDiagnostic struct {
	Code    string
	Message string
}

// ModelResponse is one provider-neutral public model response.
type ModelResponse struct {
	Text          string
	Outcome       mo.Option[ModelOutcome]
	ErrorMessage  mo.Option[string]
	Provider      mo.Option[string]
	Model         mo.Option[string]
	ResponseModel mo.Option[string]
	ResponseID    mo.Option[string]
	Usage         mo.Option[ModelUsage]
	Diagnostics   []ModelDiagnostic
	Content       []ModelResponseContent
}

// TurnSummary contains one model response and its ordered tool results.
type TurnSummary struct {
	Response    ModelResponse
	ToolResults []ToolResult
}

// RunOutcome identifies the terminal agent outcome.
type RunOutcome uint8

// RunOutcome values enumerate terminal agent outcomes.
const (
	RunOutcomeUnspecified RunOutcome = iota
	RunOutcomeCompleted
	RunOutcomeAborted
	RunOutcomeFailed
)

// AgentSummary contains the public terminal agent result.
type AgentSummary struct {
	Outcome      RunOutcome
	ErrorMessage mo.Option[string]
}
