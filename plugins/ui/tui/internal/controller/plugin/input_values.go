// Package plugin defines validated inputs consumed by the TUI application.
package plugin

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

// TranscriptKind controls the plain prefix used to render one transcript line.
type TranscriptKind uint8

const (
	// TranscriptUnspecified represents a missing transcript line kind.
	TranscriptUnspecified TranscriptKind = iota
	// TranscriptInformation renders informational text.
	TranscriptInformation
	// TranscriptError renders error text.
	TranscriptError
	// TranscriptWarning renders non-fatal startup exclusions.
	TranscriptWarning
	// TranscriptUser renders submitted user text.
	TranscriptUser
	// TranscriptModel renders model text.
	TranscriptModel
	// TranscriptRefusal renders model refusal text.
	TranscriptRefusal
	// TranscriptReasoning renders visible model reasoning.
	TranscriptReasoning
	// TranscriptBranchSummary renders one abandoned-branch summary.
	TranscriptBranchSummary
	// TranscriptToolStatus renders tool status text.
	TranscriptToolStatus
	// TranscriptToolStdout renders standard tool output.
	TranscriptToolStdout
	// TranscriptToolStderr renders tool error output.
	TranscriptToolStderr
	// TranscriptToolDone renders successful tool completion.
	TranscriptToolDone
	// TranscriptToolError renders failed tool completion.
	TranscriptToolError
)

// Transcript is one readable startup or transcript entry.
type Transcript struct {
	// Kind controls the rendered line prefix.
	Kind TranscriptKind
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
