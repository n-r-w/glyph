package sessions

import (
	"encoding/json/jsontext"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

const (
	// recordTypeSession identifies the immutable session header.
	recordTypeSession = "session"
	// recordTypeSessionInfo identifies a session-information mutation.
	recordTypeSessionInfo = "session_info"
	// recordTypeUser identifies a persisted user message.
	recordTypeUser = "user"
	// recordTypeModel identifies a persisted model response.
	recordTypeModel = "model"
	// recordTypeToolResult identifies a persisted tool result.
	recordTypeToolResult = "tool_result"
	// recordTypeExtension identifies a model-hidden extension entry.
	recordTypeExtension = "extension"
	// recordTypeBranchSummary identifies a persisted branch summary.
	recordTypeBranchSummary = "branch_summary"
	// mutationTypeEntry identifies an entry mutation envelope.
	mutationTypeEntry = "entry"
	// mutationTypeNavigation identifies a navigation mutation envelope.
	mutationTypeNavigation = "navigation"
	// mutationTypeLabel identifies a label mutation envelope.
	mutationTypeLabel = "label"
)

// headerRecord is the immutable first JSONL record that binds a file to one project.
type headerRecord struct {
	// Type must be "session".
	Type string `json:"type"`
	// Version selects the strict record schema.
	Version int `json:"version"`
	// ID is the opaque session identifier.
	ID string `json:"id"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// CWD is the canonical project path.
	CWD string `json:"cwd"`
}

// informationRecord stores one normalized user-assigned name change.
type informationRecord struct {
	// Type must be "session_info".
	Type string `json:"type"`
	// ID uniquely identifies this append.
	ID string `json:"id"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// Name is already normalized by the use case.
	Name string `json:"name"`
}

// userRecord stores ordered provider-neutral user content.
type userRecord struct {
	// Type must be "user".
	Type string `json:"type"`
	// ID uniquely identifies this append.
	ID string `json:"id"`
	// ParentID identifies the preceding tree entry or the implicit root when absent.
	ParentID mo.Option[string] `json:"parentId"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// Message contains ordered provider-neutral user content.
	Message *messageRecord `json:"message"`
}

type messageRecord struct {
	// Content contains ordered user-message blocks.
	Content []inputContentRecord `json:"content"`
}

type inputContentRecord struct {
	// Kind identifies the content payload.
	Kind model.InputContentKind `json:"kind"`
	// Text contains user text for text content.
	Text *string `json:"text,omitzero"`
	// MediaType identifies the format of image content.
	MediaType *string `json:"mediaType,omitzero"`
	// Data contains encoded image bytes.
	Data jsontext.Value `json:"data,omitzero"`
}

// modelRecord stores one provider-neutral terminal model response.
type modelRecord struct {
	// Type must be "model".
	Type string `json:"type"`
	// ID uniquely identifies this append.
	ID string `json:"id"`
	// ParentID identifies the preceding tree entry or the implicit root when absent.
	ParentID mo.Option[string] `json:"parentId"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// Response contains one terminal model response.
	Response modelResponseRecord `json:"response"`
	// EstimatedCost contains persisted model response cost.
	EstimatedCost *estimatedCostRecord `json:"estimatedCost,omitzero"`
}

type estimatedCostRecord struct {
	// Input contains uncached input token cost.
	Input *float64 `json:"input"`
	// Output contains output token cost.
	Output *float64 `json:"output"`
	// CacheRead contains cached input token cost.
	CacheRead *float64 `json:"cacheRead"`
	// CacheWrite contains cache creation token cost.
	CacheWrite *float64 `json:"cacheWrite"`
	// Total contains the sum of all cost buckets.
	Total *float64 `json:"total"`
}

type modelResponseRecord struct {
	// Content contains ordered finalized response blocks.
	Content []modelContentRecord `json:"content"`
	// Outcome identifies why the response ended.
	Outcome model.Outcome `json:"outcome"`
	// ErrorMessage contains a terminal failure message.
	ErrorMessage *string `json:"errorMessage,omitzero"`
	// Provider identifies the provider used for the request.
	Provider *string `json:"provider,omitzero"`
	// Model identifies the configured model used for the request.
	Model *string `json:"model,omitzero"`
	// ResponseModel identifies the model reported by the provider.
	ResponseModel *string `json:"responseModel,omitzero"`
	// ResponseID identifies the response in the provider system.
	ResponseID *string `json:"responseId,omitzero"`
	// Usage contains provider-reported token accounting.
	Usage *usageRecord `json:"usage,omitzero"`
	// Diagnostics contains typed provider failure details.
	Diagnostics []diagnosticRecord `json:"diagnostics"`
}

type diagnosticRecord struct {
	// Code identifies the diagnostic type.
	Code string `json:"code"`
	// Message contains diagnostic details.
	Message string `json:"message"`
}

type modelContentRecord struct {
	// Kind identifies the response content payload.
	Kind model.ContentKind `json:"kind"`
	// Text contains text, refusal, or reasoning content.
	Text *string `json:"text,omitzero"`
	// ProviderContext contains opaque reasoning replay state.
	ProviderContext *providerContextRecord `json:"providerContext,omitzero"`
	// ToolCall contains a finalized tool request.
	ToolCall *toolCallRecord `json:"toolCall,omitzero"`
}

type providerContextRecord struct {
	// ProviderID identifies the provider that produced the context.
	ProviderID string `json:"providerId"`
	// API identifies the provider request contract.
	API string `json:"api"`
	// Model identifies the model that produced the context.
	Model string `json:"model"`
	// CompatibilityKey identifies the replay compatibility contract.
	CompatibilityKey *string `json:"compatibilityKey,omitzero"`
	// Payload contains opaque provider-owned replay data.
	Payload []byte `json:"payload"`
}

type toolCallRecord struct {
	// ID identifies the tool call within the model response.
	ID string `json:"id"`
	// Name identifies the requested tool.
	Name string `json:"name"`
	// Arguments contains finalized tool input.
	Arguments map[string]any `json:"arguments"`
}

type usageRecord struct {
	// InputTokens contains uncached input tokens.
	InputTokens int64 `json:"inputTokens"`
	// OutputTokens contains output tokens including reasoning tokens.
	OutputTokens int64 `json:"outputTokens"`
	// CachedInputTokens contains cache-read input tokens.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// CacheWriteTokens contains cache creation input tokens.
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	// ReasoningTokens contains the reasoning subset of OutputTokens.
	ReasoningTokens int64 `json:"reasoningTokens"`
	// TotalTokens contains the sum of disjoint input and output buckets.
	TotalTokens int64 `json:"totalTokens"`
}

type toolResultRecord struct {
	// Type must be "tool_result".
	Type string `json:"type"`
	// ID uniquely identifies this append.
	ID string `json:"id"`
	// ParentID identifies the preceding tree entry or the implicit root when absent.
	ParentID mo.Option[string] `json:"parentId"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// Result contains one terminal tool result.
	Result toolResultValue `json:"result"`
}

type toolResultValue struct {
	// CallID identifies the model-requested tool call.
	CallID string `json:"callId"`
	// ToolName identifies the executed tool.
	ToolName string `json:"toolName"`
	// Contents contains ordered terminal result blocks.
	Contents []toolResultContentRecord `json:"contents"`
	// IsError reports whether tool execution failed.
	IsError bool `json:"isError"`
}

type toolResultContentRecord struct {
	// Kind identifies the result content payload.
	Kind tool.ResultContentKind `json:"kind"`
	// Text contains terminal text output.
	Text *string `json:"text,omitzero"`
	// MediaType identifies the image output format.
	MediaType *string `json:"mediaType,omitzero"`
	// Data contains encoded image bytes.
	Data jsontext.Value `json:"data,omitzero"`
}

// extensionRecord stores compact extension-owned JSON without interpreting it.
type extensionRecord struct {
	// Type must be "extension".
	Type string `json:"type"`
	// ID uniquely identifies this append.
	ID string `json:"id"`
	// ParentID identifies the preceding tree entry or the implicit root when absent.
	ParentID mo.Option[string] `json:"parentId"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// ExtensionID identifies the extension that owns the entry.
	ExtensionID string `json:"extensionId"`
	// EntryType identifies the extension-defined entry kind.
	EntryType string `json:"entryType"`
	// Data contains extension-owned JSON.
	Data jsontext.Value `json:"data"`
}

// branchSummaryRecord stores one abandoned-branch summary entry.
type branchSummaryRecord struct {
	// Type must be "branch_summary".
	Type string `json:"type"`
	// ID uniquely identifies this entry.
	ID string `json:"id"`
	// ParentID identifies the navigation destination or the implicit root when absent.
	ParentID mo.Option[string] `json:"parentId"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// Summary contains the persisted model-generated text.
	Summary string `json:"summary"`
	// FirstEntryID identifies the first abandoned entry.
	FirstEntryID string `json:"firstEntryId"`
	// LastEntryID identifies the last abandoned entry.
	LastEntryID string `json:"lastEntryId"`
	// Provider identifies the configured summary provider.
	Provider string `json:"provider"`
	// Model identifies the configured summary model.
	Model string `json:"model"`
	// ReasoningChoice identifies the configured reasoning behavior.
	ReasoningChoice model.ReasoningChoice `json:"reasoningChoice"`
	// Usage contains normalized usage when reported.
	Usage *sessionUsageRecord `json:"usage,omitzero"`
	// EstimatedCost contains persisted summary cost when calculable.
	EstimatedCost *estimatedCostRecord `json:"estimatedCost,omitzero"`
}

// sessionUsageRecord stores normalized usage field names used by session accounting.
type sessionUsageRecord struct {
	// InputTokens contains uncached input tokens.
	InputTokens int64 `json:"inputTokens"`
	// OutputTokens contains output tokens including reasoning tokens.
	OutputTokens int64 `json:"outputTokens"`
	// CacheReadTokens contains cache-read tokens.
	CacheReadTokens int64 `json:"cacheReadTokens"`
	// CacheWriteTokens contains cache creation tokens.
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	// ReasoningTokens contains the reasoning subset of output tokens.
	ReasoningTokens int64 `json:"reasoningTokens"`
	// TotalTokens contains the sum of disjoint token buckets.
	TotalTokens int64 `json:"totalTokens"`
}

// mutationRecord stores exactly one aggregate mutation.
type mutationRecord struct {
	// Type identifies the selected mutation payload.
	Type string `json:"type"`
	// Entry contains one complete tree entry.
	Entry *jsontext.Value `json:"entry,omitzero"`
	// Navigation contains one active-leaf change.
	Navigation *navigationRecord `json:"navigation,omitzero"`
	// Label contains one label change.
	Label *labelRecord `json:"label,omitzero"`
	// SessionInfo contains one session-information change.
	SessionInfo *sessionInfoRecord `json:"sessionInfo,omitzero"`
}

// navigationRecord stores a destination and optional embedded summary entry.
type navigationRecord struct {
	// DestinationID identifies an entry or the implicit root when absent.
	DestinationID mo.Option[string] `json:"destinationId"`
	// BranchSummary contains an optional encoded branch-summary entry.
	BranchSummary *jsontext.Value `json:"branchSummary,omitzero"`
}

// labelRecord stores the latest label state for one entry.
type labelRecord struct {
	// TargetID identifies the labeled entry.
	TargetID string `json:"targetId"`
	// Label contains the replacement label or clears it when empty.
	Label string `json:"label"`
}

// sessionInfoRecord stores normalized session metadata.
type sessionInfoRecord struct {
	// Name contains the normalized session name.
	Name string `json:"name"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
}

// recordType reads only the discriminator used to select the current record shape.
type recordType struct {
	// Type identifies the session header or session information record shape.
	Type string `json:"type"`
}
