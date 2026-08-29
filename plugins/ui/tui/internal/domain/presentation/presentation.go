// Package presentation defines process-local state derived from Host UI frames.
package presentation

import (
	"time"

	"github.com/samber/mo"
)

// Availability controls which user commands the presentation may emit.
type Availability uint8

const (
	// AvailabilityUnspecified represents a missing Host availability value.
	AvailabilityUnspecified Availability = iota
	// AvailabilityChecking means the Host is checking stored authentication.
	AvailabilityChecking
	// AvailabilityAuthenticating means browser authentication is in progress.
	AvailabilityAuthenticating
	// AvailabilityAuthenticationFailed allows a manual authentication retry.
	AvailabilityAuthenticationFailed
	// AvailabilityIdle allows a new request.
	AvailabilityIdle
	// AvailabilityRunning allows a stop request.
	AvailabilityRunning
)

// ReasoningChoice identifies one configured reasoning choice.
type ReasoningChoice uint8

const (
	// ReasoningChoiceUnspecified represents a missing reasoning choice.
	ReasoningChoiceUnspecified ReasoningChoice = iota
	// ReasoningChoiceOff disables reasoning.
	ReasoningChoiceOff
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

// ModelSelection identifies the Host-confirmed active selection.
type ModelSelection struct {
	// ProviderID identifies the selected provider.
	ProviderID string
	// ModelID identifies the selected provider model.
	ModelID string
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice ReasoningChoice
}

// EventKind identifies one provider-neutral Host presentation event.
type EventKind uint8

const (
	// EventUnspecified represents a missing presentation event kind.
	EventUnspecified EventKind = iota
	// EventInitialization carries the first Host frame.
	EventInitialization
	// EventUserSubmitted records a successfully sent user request.
	EventUserSubmitted
	// EventAvailability changes command availability.
	EventAvailability
	// EventTurnStarted marks a new active turn.
	EventTurnStarted
	// EventModelDelta appends incremental model text.
	EventModelDelta
	// EventModelEnd settles one model text position.
	EventModelEnd
	// EventToolCallPreview replaces one provisional function-call preview.
	EventToolCallPreview
	// EventToolCallFinal replaces one preview with exact final arguments.
	EventToolCallFinal
	// EventToolStarted records tool identity.
	EventToolStarted
	// EventToolProgress records tool status text.
	EventToolProgress
	// EventToolOutput records tool output text.
	EventToolOutput
	// EventToolEnded records execution completion.
	EventToolEnded
	// EventToolResult records the terminal tool result.
	EventToolResult
	// EventTurnEnded records a terminal turn failure when present.
	EventTurnEnded
	// EventAgentSettled marks the agent run as settled.
	EventAgentSettled
	// EventAuthorization presents a browser authorization URL.
	EventAuthorization
	// EventInformation presents informational text.
	EventInformation
	// EventError presents error text.
	EventError
	// EventModelSelectionChanged confirms one committed selection.
	EventModelSelectionChanged
	// EventSessionList carries stored sessions.
	EventSessionList
	// EventSessionChanged replaces active-session identity.
	EventSessionChanged
	// EventSessionInformation carries active-session information.
	EventSessionInformation
)

// ModelContentKind identifies one visible model content block.
type ModelContentKind uint8

const (
	// ModelContentUnspecified represents a missing model content kind.
	ModelContentUnspecified ModelContentKind = iota
	// ModelContentText contains ordinary model text.
	ModelContentText
	// ModelContentRefusal contains model refusal text.
	ModelContentRefusal
	// ModelContentReasoning contains visible model reasoning.
	ModelContentReasoning
)

// ModelResponseContent carries one finalized visible model content block.
type ModelResponseContent struct {
	// Kind identifies the finalized content type.
	Kind ModelContentKind
	// Text contains finalized visible text.
	Text mo.Option[string]
}

// ActiveModelContent carries one streaming visible model content block.
type ActiveModelContent struct {
	// Kind identifies the streaming content type.
	Kind mo.Option[ModelContentKind]
	// Text contains accumulated visible text.
	Text mo.Option[string]
}

// OutputStream identifies readable tool output without exposing tool internals.
type OutputStream uint8

const (
	// OutputUnspecified represents a missing tool output stream.
	OutputUnspecified OutputStream = iota
	// OutputStdout identifies standard tool output.
	OutputStdout
	// OutputStderr identifies tool error output.
	OutputStderr
)

// Extension describes startup information received from the Host.
type Extension struct {
	// ID identifies the available extension.
	ID string
	// Path is the extension executable path.
	Path string
	// Tools lists model-callable tool names.
	Tools []string
}

// Content is one ordered public text or image block received from the Host.
type Content struct {
	// Text contains public text content.
	Text mo.Option[string]
	// MediaType identifies the image format.
	MediaType mo.Option[string]
	// Data contains encoded image bytes.
	Data mo.Option[[]byte]
}

// SessionInfo contains one session lifecycle snapshot.
type SessionInfo struct {
	// ID is the opaque value returned for resume commands.
	ID string
	// Name contains the user-assigned value only when NamePresent is true.
	Name string
	// NamePresent distinguishes an absent name from an explicit string value.
	NamePresent bool
	// WorkingDirectory identifies the canonical project associated with the session.
	WorkingDirectory string
	// StoragePath contains the JSONL path only when StoragePresent is true.
	StoragePath string
	// StoragePresent is false for a new session that has no persisted entries.
	StoragePresent bool
	// CreatedAt is the immutable header time.
	CreatedAt time.Time
	// UpdatedAt drives resume-list ordering and display.
	UpdatedAt time.Time
}

// TokenUsage contains disjoint token buckets rendered by /session.
type TokenUsage struct {
	// InputTokens contains uncached input tokens.
	InputTokens int64
	// OutputTokens contains output tokens including reasoning tokens.
	OutputTokens int64
	// CacheReadTokens contains cached input tokens.
	CacheReadTokens int64
	// CacheWriteTokens contains cache creation input tokens.
	CacheWriteTokens int64
	// ReasoningTokens contains the reasoning subset of OutputTokens.
	ReasoningTokens int64
	// TotalTokens is the sum of disjoint input and output buckets.
	TotalTokens int64
}

// EstimatedCost contains calculated USD cost for disjoint token buckets.
type EstimatedCost struct {
	// Input is the cost of uncached input tokens.
	Input float64
	// Output is the cost of output tokens.
	Output float64
	// CacheRead is the cost of cached input tokens.
	CacheRead float64
	// CacheWrite is the cost of cache creation input tokens.
	CacheWrite float64
	// Total is the sum of all cost buckets.
	Total float64
}

// ProviderModelCost groups persisted cost by configured provider and requested model.
type ProviderModelCost struct {
	// ProviderID identifies the configured provider.
	ProviderID string
	// ModelID identifies the requested provider model.
	ModelID string
	// EstimatedCost contains the complete persisted cost for this group.
	EstimatedCost mo.Option[EstimatedCost]
}

// SessionStatistics contains available counts and optional complete token and cost totals.
type SessionStatistics struct {
	// UserMessages counts durable user entries.
	UserMessages int
	// ModelResponses counts durable terminal model entries.
	ModelResponses int
	// ToolCalls counts finalized calls in durable model responses.
	ToolCalls int
	// ToolResults counts durable tool-result entries.
	ToolResults int
	// TotalMessages counts all client-visible terminal messages.
	TotalMessages int
	// TokenUsage contains complete token totals when available.
	TokenUsage mo.Option[TokenUsage]
	// EstimatedCost contains complete persisted cost totals when available.
	EstimatedCost mo.Option[EstimatedCost]
	// CostBreakdown groups persisted cost by provider and model.
	CostBreakdown []ProviderModelCost
}

// SessionSummary contains one selector row.
type SessionSummary struct {
	// Info supplies the row identity, name, and update time.
	Info SessionInfo
	// FirstUserText is the fallback label only when TextPresent is true.
	FirstUserText string
	// TextPresent distinguishes absent fallback text from an empty value.
	TextPresent bool
	// TotalMessages is displayed as the session message count.
	TotalMessages int64
}

// Event contains the fields used by one presentation update.
type Event struct {
	// Kind identifies the presentation update and active payload.
	Kind EventKind
	// Startup contains ordered startup lines.
	Startup []Line
	// RestoredTranscript replaces transcript state only after Host confirms session replacement.
	RestoredTranscript []Line
	// Extensions lists available extensions and their tools.
	Extensions []Extension
	// Availability contains the updated command availability.
	Availability mo.Option[Availability]
	// Position identifies the model content block order.
	Position mo.Option[int]
	// ModelContentKind identifies the model content type.
	ModelContentKind mo.Option[ModelContentKind]
	// ModelResponseContent contains finalized visible model content.
	ModelResponseContent []ModelResponseContent
	// ToolCallID identifies the active tool call.
	ToolCallID mo.Option[string]
	// ToolName identifies the active tool.
	ToolName mo.Option[string]
	// Status contains tool execution status text.
	Status mo.Option[string]
	// Stream identifies the tool output source.
	Stream mo.Option[OutputStream]
	// Text contains event-specific visible text.
	Text mo.Option[string]
	// Contents contains ordered terminal tool result blocks.
	Contents mo.Option[[]Content]
	// ErrorText contains a terminal failure message.
	ErrorText mo.Option[string]
	// ExitCode contains the tool process exit code.
	ExitCode mo.Option[int]
	// Failure reports whether the event represents failure.
	Failure mo.Option[bool]
	// ToolCall contains transient or finalized call state.
	ToolCall mo.Option[ToolCallState]
	// Models lists selectable configured models.
	Models []ConfiguredModel
	// ModelSelection contains the committed active selection.
	ModelSelection mo.Option[ModelSelection]
	// SessionInfo is present on initialization, replacement, and information events.
	SessionInfo mo.Option[SessionInfo]
	// Sessions carries ordered selector data on a list event.
	Sessions []SessionSummary
	// SessionStatistics is present only on a session-information event.
	SessionStatistics mo.Option[SessionStatistics]
}

// LineKind controls the plain prefix used to render one transcript line.
type LineKind uint8

const (
	// LineUnspecified represents a missing transcript line kind.
	LineUnspecified LineKind = iota
	// LineInformation renders informational text.
	LineInformation
	// LineError renders error text.
	LineError
	// LineWarning renders non-fatal startup exclusions.
	LineWarning
	// LineUser renders submitted user text.
	LineUser
	// LineModel renders model text.
	LineModel
	// LineRefusal renders model refusal text.
	LineRefusal
	// LineReasoning renders visible model reasoning.
	LineReasoning
	// LineToolStatus renders tool status text.
	LineToolStatus
	// LineToolStdout renders standard tool output.
	LineToolStdout
	// LineToolStderr renders tool error output.
	LineToolStderr
	// LineToolDone renders successful tool completion.
	LineToolDone
	// LineToolError renders failed tool completion.
	LineToolError
)

// Line is one readable startup or transcript entry.
type Line struct {
	// Kind controls the rendered line prefix.
	Kind LineKind
	// ToolName identifies the tool associated with the line.
	ToolName mo.Option[string]
	// Status contains tool status text.
	Status mo.Option[string]
	// Text contains rendered line text.
	Text mo.Option[string]
	// Contents contains ordered public text or image blocks.
	Contents mo.Option[[]Content]
}

// ToolCallField is one rendered argument field.
type ToolCallField struct {
	// Name identifies the argument field.
	Name string
	// Value contains a fully received JSON value.
	Value mo.Option[any]
	// Prefix contains an exact received scalar prefix.
	Prefix mo.Option[string]
}

// ToolCallState is one transient or finalized function call.
type ToolCallState struct {
	// CallID identifies the tool call.
	CallID string
	// Name identifies the requested tool.
	Name string
	// Position identifies the call order within the response.
	Position int
	// Provisional reports whether the call can still change.
	Provisional bool
	// Fields contains ordered provisional argument fields.
	Fields []ToolCallField
	// Arguments contains finalized tool input.
	Arguments map[string]any
}

// State is the TUI-owned projection of provider-neutral Host frames.
type State struct {
	// Startup contains rendered startup lines.
	Startup []Line
	// Transcript contains rendered session lines.
	Transcript []Line
	// ActiveModel contains streaming model content by response position.
	ActiveModel map[int]ActiveModelContent
	// ActiveToolCalls contains tool call state by call ID.
	ActiveToolCalls map[string]ToolCallState
	// ActiveTools contains active tool names by call ID.
	ActiveTools map[string]string
	// Availability controls accepted user commands.
	Availability mo.Option[Availability]
	// AuthorizationURL contains the pending browser OAuth URL.
	AuthorizationURL mo.Option[string]
	// Settled reports whether the active agent run has settled.
	Settled mo.Option[bool]
	// Models lists selectable configured models.
	Models []ConfiguredModel
	// ModelSelection contains the committed active selection.
	ModelSelection mo.Option[ModelSelection]
	// SessionInfo identifies the transcript owner currently shown by the TUI.
	SessionInfo mo.Option[SessionInfo]
	// Sessions retains the latest resume selector result.
	Sessions []SessionSummary
}

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
}
