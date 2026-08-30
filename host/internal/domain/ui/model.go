// Package ui defines provider-neutral Host UI lifecycle models.
package ui

import (
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// ContentSeverity identifies startup content importance.
type ContentSeverity uint8

const (
	// ContentSeverityInformation identifies normal startup content.
	ContentSeverityInformation ContentSeverity = iota + 1
	// ContentSeverityError identifies one startup failure.
	ContentSeverityError
	// ContentSeverityWarning identifies one non-fatal automatic exclusion.
	ContentSeverityWarning
)

// Availability identifies whether the Host can accept a user request.
type Availability uint8

const (
	// AvailabilityCheckingAuthentication blocks input during the startup credential check.
	AvailabilityCheckingAuthentication Availability = iota + 1
	// AvailabilityAuthenticating blocks input during browser OAuth.
	AvailabilityAuthenticating
	// AvailabilityAuthenticationFailed permits only explicit authentication retry.
	AvailabilityAuthenticationFailed
	// AvailabilityIdle permits one user request.
	AvailabilityIdle
	// AvailabilityRunning permits stop or quit but rejects another request.
	AvailabilityRunning
)

// LifecycleType identifies one ordered Host or Agent lifecycle transition.
type LifecycleType uint8

const (
	// LifecycleAgentStart starts one agent run.
	LifecycleAgentStart LifecycleType = iota + 1
	// LifecycleTurnStart starts one model turn.
	LifecycleTurnStart
	// LifecycleMessageStart starts one model message.
	LifecycleMessageStart
	// LifecycleModelContentStart starts one typed model text block.
	LifecycleModelContentStart
	// LifecycleModelTextDelta carries one model text fragment.
	LifecycleModelTextDelta
	// LifecycleModelContentEnd finalizes one typed model text block.
	LifecycleModelContentEnd
	// LifecycleToolCallStart starts one provisional function call.
	LifecycleToolCallStart
	// LifecycleToolCallDelta replaces one provisional function-call preview.
	LifecycleToolCallDelta
	// LifecycleToolCallEnd carries exact final function-call arguments.
	LifecycleToolCallEnd
	// LifecycleMessageEnd finalizes one model message.
	LifecycleMessageEnd
	// LifecycleToolExecutionStart starts one tool call.
	LifecycleToolExecutionStart
	// LifecycleToolExecutionUpdate carries one tool progress fragment.
	LifecycleToolExecutionUpdate
	// LifecycleToolExecutionEnd finalizes one tool execution.
	LifecycleToolExecutionEnd
	// LifecycleToolResult carries one model-visible tool result.
	LifecycleToolResult
	// LifecycleTurnEnd finalizes one model turn.
	LifecycleTurnEnd
	// LifecycleAgentEnd finalizes Agent Core work.
	LifecycleAgentEnd
	// LifecycleAgentSettled marks Host recipient completion and idle settlement.
	LifecycleAgentSettled
	// LifecycleAvailabilityChanged updates task or authentication availability.
	LifecycleAvailabilityChanged
)

// ProgressChannel identifies one tool progress fragment channel.
type ProgressChannel uint8

const (
	// ProgressChannelStatus carries status text.
	ProgressChannelStatus ProgressChannel = iota + 1
	// ProgressChannelStdout carries standard output.
	ProgressChannelStdout
	// ProgressChannelStderr carries standard error.
	ProgressChannelStderr
)

// Candidate identifies one executable UI plugin candidate.
type Candidate struct {
	// ID identifies the UI plugin.
	ID string
	// Path is the UI plugin executable path.
	Path string
}

// Directory identifies the effective UI catalog directory.
type Directory struct {
	// Path is the effective UI catalog directory path.
	Path string
}

// Discovery is one complete valid UI catalog.
type Discovery struct {
	// Candidates contains valid UI plugins in discovery order.
	Candidates []Candidate
}

// Capabilities contains immutable startup behavior for one UI plugin.
type Capabilities struct {
	// ControlsTerminal reports whether the UI plugin owns terminal setup.
	ControlsTerminal bool
}

// StartupContent carries one initialization information or error item.
type StartupContent struct {
	// Severity identifies the content importance.
	Severity ContentSeverity
	// Text contains the user-visible startup message.
	Text string
}

// ExtensionAvailability identifies one available extension and its tool names.
type ExtensionAvailability struct {
	// PluginID identifies the available extension.
	PluginID string
	// Path is the extension executable path.
	Path string
	// Tools lists model-callable tool names.
	Tools []string
}

// ReasoningChoice identifies one provider-neutral reasoning choice.
type ReasoningChoice uint8

const (
	// ReasoningChoiceOff disables reasoning.
	ReasoningChoiceOff ReasoningChoice = iota + 1
	// ReasoningChoiceOn enables reasoning with the provider default.
	ReasoningChoiceOn
	// ReasoningChoiceMinimal requests minimal reasoning effort.
	ReasoningChoiceMinimal
	// ReasoningChoiceLow requests low reasoning effort.
	ReasoningChoiceLow
	// ReasoningChoiceMedium requests medium reasoning effort.
	ReasoningChoiceMedium
	// ReasoningChoiceHigh requests high reasoning effort.
	ReasoningChoiceHigh
	// ReasoningChoiceXHigh requests extra-high reasoning effort.
	ReasoningChoiceXHigh
	// ReasoningChoiceMax requests maximum reasoning effort.
	ReasoningChoiceMax
)

// ReasoningCapabilities describes one model reasoning contract.
type ReasoningCapabilities struct {
	// Supported reports whether the model supports reasoning controls.
	Supported bool
	// Choices lists supported reasoning choices in display order.
	Choices []ReasoningChoice
	// Default is the reasoning choice used without an explicit selection.
	Default ReasoningChoice
}

// ConfiguredModel identifies one selectable model and its reasoning contract.
type ConfiguredModel struct {
	// ProviderID identifies the configured provider.
	ProviderID string
	// ModelID identifies the configured provider model.
	ModelID string
	// Reasoning describes the model reasoning contract.
	Reasoning ReasoningCapabilities
}

// ModelSelection identifies one Host-confirmed active selection.
type ModelSelection struct {
	// ProviderID identifies the selected provider.
	ProviderID string
	// ModelID identifies the selected provider model.
	ModelID string
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice ReasoningChoice
}

// Initialization is the first Host frame sent to a selected UI.
type Initialization struct {
	// SelectedUIID identifies the UI plugin selected by the Host.
	SelectedUIID string
	// StartupContent contains ordered startup messages.
	StartupContent []StartupContent
	// Extensions lists available extensions and their tools.
	Extensions []ExtensionAvailability
	// Availability identifies which user actions the Host accepts.
	Availability Availability
	// Models lists selectable configured models.
	Models []ConfiguredModel
	// ModelSelection contains the active model selection when configured.
	ModelSelection mo.Option[ModelSelection]
	// SessionInfo identifies the empty active session created before UI startup.
	SessionInfo session.Info
}

// ModelContentType identifies one model content transition.
type ModelContentType uint8

const (
	// ModelContentStart starts one content block.
	ModelContentStart ModelContentType = iota + 1
	// ModelContentTextDelta appends one text-bearing fragment.
	ModelContentTextDelta
	// ModelContentEnd finalizes one content block.
	ModelContentEnd
)

// ModelContentKind identifies public model text or hidden reasoning content.
type ModelContentKind uint8

const (
	// ModelContentKindText contains visible model text.
	ModelContentKindText ModelContentKind = iota + 1
	// ModelContentKindRefusal contains visible model refusal text.
	ModelContentKindRefusal
	// ModelContentKindReasoning contains hidden reasoning text.
	ModelContentKindReasoning
)

// ModelContent carries one typed model content transition.
type ModelContent struct {
	// Type identifies the content lifecycle transition.
	Type ModelContentType
	// Kind identifies visible text, refusal, or hidden reasoning content.
	Kind ModelContentKind
	// Position identifies the content block order within the response.
	Position int
	// Text contains a text fragment for a delta transition.
	Text mo.Option[string]
}

// ModelResponseContent carries one ordered finalized model block.
type ModelResponseContent struct {
	// Kind identifies the finalized content type.
	Kind ModelContentKind
	// Text contains finalized text content.
	Text string
	// ToolCall contains a finalized tool call when present.
	ToolCall mo.Option[FinalToolCall]
}

// ModelUsage carries provider-reported token accounting.
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

// ModelDiagnostic carries typed provider diagnostics.
type ModelDiagnostic struct {
	// Code identifies the diagnostic type.
	Code string
	// Message contains diagnostic details.
	Message string
}

// ModelResponse carries typed terminal model data.
type ModelResponse struct {
	// Text contains the flattened visible response text.
	Text string
	// Outcome identifies why the response ended.
	Outcome mo.Option[string]
	// ErrorMessage contains a terminal provider or runtime failure message.
	ErrorMessage mo.Option[string]
	// Provider identifies the provider used for the request.
	Provider mo.Option[string]
	// Model identifies the configured model used for the request.
	Model mo.Option[string]
	// ResponseModel identifies the model reported by the provider.
	ResponseModel mo.Option[string]
	// ResponseID identifies the response in the provider system.
	ResponseID mo.Option[string]
	// Content contains ordered finalized response blocks.
	Content []ModelResponseContent
	// Usage contains provider-reported token accounting.
	Usage mo.Option[ModelUsage]
	// Diagnostics contains typed provider or runtime failure details.
	Diagnostics []ModelDiagnostic
}

// ToolCallPreviewField carries one complete value or exact scalar prefix.
type ToolCallPreviewField struct {
	// Name identifies the argument field.
	Name string
	// Value contains a fully received JSON value.
	Value mo.Option[any]
	// Prefix contains an exact received scalar prefix.
	Prefix mo.Option[string]
	// Complete reports whether Value is final.
	Complete bool
}

// ToolCallPreview carries transient function-call state.
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

// FinalToolCall carries exact terminal arguments.
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

// Lifecycle carries one explicit provider-neutral lifecycle event.
type Lifecycle struct {
	// Type identifies the lifecycle transition and active payload.
	Type LifecycleType
	// RunID identifies the agent run.
	RunID mo.Option[string]
	// Text contains transition-specific text.
	Text mo.Option[string]
	// ToolResultContents contains ordered terminal tool result blocks.
	ToolResultContents mo.Option[[]tool.ResultContent]
	// ModelContent contains one model content transition.
	ModelContent mo.Option[ModelContent]
	// ModelResponse contains typed terminal model data.
	ModelResponse mo.Option[ModelResponse]
	// ToolCallPreview contains provisional tool call state.
	ToolCallPreview mo.Option[ToolCallPreview]
	// FinalToolCall contains exact terminal tool call arguments.
	FinalToolCall mo.Option[FinalToolCall]
	// ToolCallID identifies the active tool call.
	ToolCallID mo.Option[string]
	// ToolName identifies the active tool.
	ToolName mo.Option[string]
	// ProgressChannel identifies the meaning of progress Text.
	ProgressChannel mo.Option[ProgressChannel]
	// IsError reports whether a terminal tool result failed.
	IsError mo.Option[bool]
	// Outcome identifies why the agent or model run ended.
	Outcome mo.Option[string]
	// ErrorMessage contains a terminal failure message.
	ErrorMessage mo.Option[string]
	// Availability contains the updated Host availability.
	Availability mo.Option[Availability]
}

// FrameKind identifies one Host-to-UI frame payload.
type FrameKind uint8

const (
	// FrameInitialization carries the one startup state.
	FrameInitialization FrameKind = iota + 1
	// FrameLifecycle carries one lifecycle transition.
	FrameLifecycle
	// FrameAuthorization carries one browser OAuth URL.
	FrameAuthorization
	// FrameInformation carries one user-visible notification.
	FrameInformation
	// FrameError carries one user-visible failure.
	FrameError
	// FrameModelSelectionChanged confirms one committed selection.
	FrameModelSelectionChanged
	// FrameSessionList carries stored sessions.
	FrameSessionList
	// FrameSessionChanged confirms active-session replacement.
	FrameSessionChanged
	// FrameSessionInformation carries active-session information.
	FrameSessionInformation
	// FrameSessionTree carries a complete tree query result.
	FrameSessionTree
	// FrameSessionTreeNavigation carries committed or canceled navigation.
	FrameSessionTreeNavigation
	// FrameSessionTreeFailed carries a closed navigation failure.
	FrameSessionTreeFailed
)

// Frame carries exactly one Host-to-UI payload.
type Frame struct {
	// Kind identifies the frame payload.
	Kind FrameKind
	// Initialization contains startup state for an initialization frame.
	Initialization mo.Option[Initialization]
	// Lifecycle contains one lifecycle transition.
	Lifecycle mo.Option[Lifecycle]
	// AuthorizationURL contains the browser OAuth URL.
	AuthorizationURL mo.Option[string]
	// Text contains user-visible information or error text.
	Text mo.Option[string]
	// RetryAuthentication reports whether the UI may retry authentication.
	RetryAuthentication mo.Option[bool]
	// ModelSelection contains the committed active selection.
	ModelSelection mo.Option[ModelSelection]
	// SessionInfo is present on replacement and information frames.
	SessionInfo mo.Option[session.Info]
	// Sessions is populated only by a list frame.
	Sessions []session.Summary
	// SessionEntries replaces the transcript on a session-change frame.
	SessionEntries []SessionEntry
	// SessionStatistics is present only on a session-information frame.
	SessionStatistics mo.Option[session.Statistics]
	// SessionTree is present only on a complete tree frame.
	SessionTree mo.Option[SessionTree]
	// TreeNavigation is present only on a navigation result frame.
	TreeNavigation mo.Option[TreeNavigationResult]
	// TreeFailure is present only on a navigation failure frame.
	TreeFailure mo.Option[TreeFailure]
}

// SessionEntry carries one restored public terminal item.
type SessionEntry struct {
	// ID identifies the restored session record.
	ID string
	// CreatedAt is the persisted record creation time.
	CreatedAt time.Time
	// Kind identifies the restored entry payload.
	Kind SessionEntryKind
	// User carries ordered public text and image content for restored transcripts.
	User mo.Option[model.Message]
	// Model contains a restored terminal model response.
	Model mo.Option[ModelResponse]
	// ToolResult contains a restored terminal tool result.
	ToolResult mo.Option[agent.ToolResult]
}

// SessionEntryKind identifies restored transcript ownership.
type SessionEntryKind uint8

const (
	// SessionEntryUser is one restored user text item.
	SessionEntryUser SessionEntryKind = iota + 1
	// SessionEntryModel is one restored model response.
	SessionEntryModel
	// SessionEntryToolResult is one restored terminal tool result.
	SessionEntryToolResult
)
