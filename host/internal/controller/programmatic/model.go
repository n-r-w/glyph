package programmatic

import (
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// ResponseKind identifies one operation result.
type ResponseKind uint8

// Response values enumerate operation results.
const (
	// ResponseUnspecified identifies a missing result payload.
	ResponseUnspecified ResponseKind = iota
	// ResponseUserRequestCompleted reports an agent run after settlement.
	ResponseUserRequestCompleted
	// ResponseCancelCompleted reports the target operation's observed terminal state.
	ResponseCancelCompleted
	// ResponseRunState contains a run-state snapshot.
	ResponseRunState
	// ResponseMessages contains public history.
	ResponseMessages
	// ResponseRejected contains a closed command rejection.
	ResponseRejected
	// ResponseModels contains configured models and active selection.
	ResponseModels
	// ResponseModelSelection contains committed model selection.
	ResponseModelSelection
	// ResponseSessionInfo contains active-session information.
	ResponseSessionInfo
	// ResponseSessions contains stored-session summaries.
	ResponseSessions
	// ResponseSessionEntries contains active-transcript entries.
	ResponseSessionEntries
	// ResponseSessionStats contains active-session statistics.
	ResponseSessionStats
	// ResponseSessionTree contains a complete tree snapshot.
	ResponseSessionTree
	// ResponseSessionTreeNavigation contains committed or canceled navigation.
	ResponseSessionTreeNavigation
	// ResponseForkSession contains a replacement session and exact next input.
	ResponseForkSession
	// ResponseCloneSession contains a replacement active session.
	ResponseCloneSession
	// ResponseSetEntryLabel contains the committed labeled tree.
	ResponseSetEntryLabel
)

// RejectionCode identifies why an operation was not executed.
type RejectionCode uint8

// Rejection values enumerate closed rejection reasons.
const (
	// RejectionUnspecified identifies a missing rejection code.
	RejectionUnspecified RejectionCode = iota
	// RejectionInvalidArgument reports invalid command fields.
	RejectionInvalidArgument
	// RejectionBusy reports an occupied operation gate.
	RejectionBusy
	// RejectionOperationIDInUse reports an active operation identifier.
	RejectionOperationIDInUse
	// RejectionInternal reports an unclassified Host failure.
	RejectionInternal
	// RejectionNotFound reports a missing requested resource.
	RejectionNotFound
	// RejectionReasoningUnsupported reports unsupported reasoning selection.
	RejectionReasoningUnsupported
	// RejectionCredentialUnavailable reports unavailable provider credentials.
	RejectionCredentialUnavailable
	// RejectionSessionUnavailable reports an unreadable stored session.
	RejectionSessionUnavailable
	// RejectionPersistenceUnavailable reports unavailable session persistence.
	RejectionPersistenceUnavailable
	// RejectionModelUnavailable reports an unavailable summary model.
	RejectionModelUnavailable
	// RejectionModelFailed reports summary-model execution failure.
	RejectionModelFailed
	// RejectionExtensionInvalidResult reports invalid extension output.
	RejectionExtensionInvalidResult
	// RejectionExtensionUnavailable reports extension transport failure.
	RejectionExtensionUnavailable
)

// Response is the completed result of one controller operation.
type Response struct {
	// OperationID identifies the completed operation.
	OperationID string
	// Kind identifies the response payload.
	Kind ResponseKind
	// State contains the requested run-state snapshot.
	State mo.Option[RunStateResult]
	// Messages contains the requested public history.
	Messages []HistoryEntry
	// Models contains configured models and the active selection.
	Models mo.Option[ModelsResult]
	// Selection contains the committed active model selection.
	Selection mo.Option[model.Selection]
	// SessionInfo is present for create, resume, name, and information results.
	SessionInfo mo.Option[session.Info]
	// Sessions contains the ordered list result.
	Sessions []session.Summary
	// SessionEntries contains detailed active-session text entries.
	SessionEntries []SessionEntry
	// SessionStatistics is present only for a statistics result.
	SessionStatistics mo.Option[session.Statistics]
	// SessionTree is present only for a complete tree query.
	SessionTree mo.Option[SessionTree]
	// TreeNavigation is present only for committed or canceled navigation.
	TreeNavigation mo.Option[TreeNavigationResult]
	// Replacement is present for fork and clone results.
	Replacement mo.Option[SessionReplacement]
	// Rejection contains an operation rejection that keeps the session open.
	Rejection mo.Option[Rejection]
	// CancelTargetState contains the terminal state observed by cancellation.
	CancelTargetState mo.Option[operation.TerminalState]
}

// SessionReplacement contains public active-session state after fork or clone.
type SessionReplacement struct {
	// Info contains replacement lifecycle information.
	Info session.Info
	// ActiveBranch contains the public replacement transcript.
	ActiveBranch []SessionEntry
	// NextInput contains exact editable user text only for a fork result.
	NextInput mo.Option[string]
}

// SessionEntry contains stable metadata and one public terminal payload.
type SessionEntry struct {
	// ID identifies the session record.
	ID string
	// CreatedAt is the persisted record creation time.
	CreatedAt time.Time
	// Kind identifies the entry payload.
	Kind HistoryEntryKind
	// User carries ordered public text and image content for detailed session entries.
	User mo.Option[model.Message]
	// Model contains a terminal model response.
	Model mo.Option[ModelResponse]
	// EstimatedCost contains persisted model response cost.
	EstimatedCost mo.Option[session.EstimatedCost]
	// ToolResult contains a terminal tool result.
	ToolResult mo.Option[ToolResult]
	// BranchSummary contains restored abandoned-branch context.
	BranchSummary mo.Option[BranchSummary]
}

// ModelsResult contains configured models and the active selection.
type ModelsResult struct {
	// Models lists configured provider models.
	Models []model.Descriptor
	// ActiveSelection contains the active provider and model selection.
	ActiveSelection mo.Option[model.Selection]
}

// Rejection describes one operation rejection that keeps the session open.
type Rejection struct {
	// Command identifies the rejected operation.
	Command CommandKind
	// Code classifies why the operation was rejected.
	Code RejectionCode
	// Message contains user-visible rejection details.
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
	// State identifies whether Agent Core is idle or running.
	State RunState
	// ActiveOperationID identifies the running operation.
	ActiveOperationID mo.Option[string]
}

// HistoryEntryKind identifies one public history entry.
type HistoryEntryKind uint8

// HistoryEntry values enumerate public message kinds.
const (
	HistoryEntryUnspecified HistoryEntryKind = iota
	HistoryEntryUser
	HistoryEntryModel
	HistoryEntryToolResult
	// HistoryEntryBranchSummary identifies restored abandoned-branch context.
	HistoryEntryBranchSummary
)

// HistoryEntry is one ordered public conversation entry.
type HistoryEntry struct {
	// Kind identifies the history payload.
	Kind HistoryEntryKind
	// User carries ordered public text and image content.
	User mo.Option[model.Message]
	// Model contains a terminal model response.
	Model mo.Option[ModelResponse]
	// ToolResult contains a terminal tool result.
	ToolResult mo.Option[ToolResult]
}

// AgentEventType identifies one agent progress event.
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
)

// AgentEvent is one progress event from an active user operation.
type AgentEvent struct {
	// OperationID identifies the active user operation.
	OperationID string
	// Type identifies the lifecycle transition and active payload.
	Type AgentEventType
	// RunID identifies the agent run.
	RunID string
	// ModelContent contains one model content update.
	ModelContent mo.Option[ModelContent]
	// ToolCallPreview contains provisional tool call state.
	ToolCallPreview mo.Option[ToolCallPreview]
	// FinalToolCall contains exact terminal tool call arguments.
	FinalToolCall mo.Option[FinalToolCall]
	// ToolExecution identifies an active tool invocation.
	ToolExecution mo.Option[ToolExecution]
	// ToolProgress contains one tool execution update.
	ToolProgress mo.Option[ToolProgress]
	// ToolResult contains one terminal tool result.
	ToolResult mo.Option[ToolResult]
	// ModelResponse contains one terminal model response.
	ModelResponse mo.Option[ModelResponse]
	// Turn contains one terminal turn summary.
	Turn mo.Option[TurnSummary]
	// Agent contains one terminal agent summary.
	Agent mo.Option[AgentSummary]
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
	// Kind identifies the public model content type.
	Kind ModelContentKind
	// Position identifies the content block order.
	Position int
	// Text contains a model text fragment.
	Text mo.Option[string]
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
	// Name identifies the argument field.
	Name string
	// Kind identifies whether the field value is complete.
	Kind ToolCallPreviewFieldKind
	// Value contains a fully received JSON value.
	Value mo.Option[any]
	// Prefix contains an exact received scalar prefix.
	Prefix mo.Option[string]
}

// ToolCallPreview is the complete current preview for one tool call.
type ToolCallPreview struct {
	// CallID identifies the provisional tool call.
	CallID string
	// Name identifies the requested tool.
	Name string
	// Position identifies the call order within the response.
	Position int
	// Provisional reports whether the preview can still change.
	Provisional bool
	// Fields contains ordered provisional argument fields.
	Fields []ToolCallPreviewField
}

// FinalToolCall is one finalized public tool call.
type FinalToolCall struct {
	// CallID identifies the finalized tool call.
	CallID string
	// Name identifies the requested tool.
	Name string
	// Position identifies the call order within the response.
	Position int
	// Arguments contains the finalized tool input.
	Arguments map[string]any
}

// ToolExecution identifies a tool invocation.
type ToolExecution struct {
	// CallID identifies the active tool call.
	CallID string
	// ToolName identifies the requested tool.
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
	// Channel identifies the progress fragment meaning.
	Channel ProgressChannel
	// Content contains the progress fragment text.
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
	// MediaType identifies the image format.
	MediaType string
	// Data contains encoded image bytes.
	Data []byte
}

// ToolResultContent is one ordered tool result block.
type ToolResultContent struct {
	// Kind identifies the content payload.
	Kind ToolResultContentKind
	// Text contains public text content.
	Text mo.Option[string]
	// Image contains public image content.
	Image mo.Option[ToolResultImage]
}

// ToolResult is one complete public tool result.
type ToolResult struct {
	// CallID identifies the model-requested tool call.
	CallID string
	// ToolName identifies the executed tool.
	ToolName string
	// Contents contains ordered public result blocks.
	Contents []ToolResultContent
	// IsError reports whether tool execution failed.
	IsError bool
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
	// Kind identifies the response item payload.
	Kind ModelResponseContentKind
	// Text contains finalized public text.
	Text mo.Option[string]
	// ToolCall contains a finalized tool request.
	ToolCall mo.Option[FinalToolCall]
}

// ModelUsage contains provider token accounting.
type ModelUsage struct {
	// InputTokens contains uncached input tokens.
	InputTokens int64
	// OutputTokens contains output tokens including reasoning tokens.
	OutputTokens int64
	// CachedInputTokens contains cache-read input tokens.
	CachedInputTokens int64
	// CacheWriteTokens contains cache creation input tokens.
	CacheWriteTokens int64
	// ReasoningTokens contains the reasoning subset of OutputTokens.
	ReasoningTokens int64
	// TotalTokens is the sum of disjoint input and output buckets.
	TotalTokens int64
}

// ModelDiagnostic is one provider diagnostic.
type ModelDiagnostic struct {
	// Code identifies the diagnostic type.
	Code string
	// Message contains diagnostic details.
	Message string
}

// ModelResponse is one provider-neutral public model response.
type ModelResponse struct {
	// Text contains flattened visible response text.
	Text string
	// Outcome identifies why the response ended.
	Outcome mo.Option[ModelOutcome]
	// ErrorMessage contains a terminal failure message.
	ErrorMessage mo.Option[string]
	// Provider identifies the provider used for the request.
	Provider mo.Option[string]
	// Model identifies the configured model used for the request.
	Model mo.Option[string]
	// ResponseModel identifies the model reported by the provider.
	ResponseModel mo.Option[string]
	// ResponseID identifies the response in the provider system.
	ResponseID mo.Option[string]
	// Usage contains provider-reported token accounting.
	Usage mo.Option[ModelUsage]
	// Diagnostics contains typed provider failure details.
	Diagnostics []ModelDiagnostic
	// Content contains ordered finalized response items.
	Content []ModelResponseContent
}

// TurnSummary contains one model response and its ordered tool results.
type TurnSummary struct {
	// Response contains the terminal model response.
	Response ModelResponse
	// ToolResults contains ordered terminal tool results.
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
	// Outcome identifies the terminal agent state.
	Outcome RunOutcome
	// ErrorMessage contains a terminal failure message.
	ErrorMessage mo.Option[string]
}
