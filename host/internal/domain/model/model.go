// Package model defines provider-neutral model messages, responses, and streaming content.
package model

import "github.com/samber/mo"

// ProviderID identifies one model provider.
type ProviderID string

// ID identifies one provider model.
type ID string

// ReasoningChoice selects provider-neutral model reasoning behavior.
type ReasoningChoice string

// InputModality identifies one provider-neutral model input kind.
type InputModality string

const (
	// InputModalityText accepts text input.
	InputModalityText InputModality = "text"
	// InputModalityImage accepts image input.
	InputModalityImage InputModality = "image"
)

const (
	// ReasoningChoiceOff disables reasoning.
	ReasoningChoiceOff ReasoningChoice = "off"
	// ReasoningChoiceOn enables reasoning with the provider default.
	ReasoningChoiceOn ReasoningChoice = "on"
	// ReasoningChoiceMinimal requests minimal reasoning effort.
	ReasoningChoiceMinimal ReasoningChoice = "minimal"
	// ReasoningChoiceLow requests low reasoning effort.
	ReasoningChoiceLow ReasoningChoice = "low"
	// ReasoningChoiceMedium requests medium reasoning effort.
	ReasoningChoiceMedium ReasoningChoice = "medium"
	// ReasoningChoiceHigh requests high reasoning effort.
	ReasoningChoiceHigh ReasoningChoice = "high"
	// ReasoningChoiceXHigh requests extra-high reasoning effort.
	ReasoningChoiceXHigh ReasoningChoice = "xhigh"
	// ReasoningChoiceMax requests maximum reasoning effort.
	ReasoningChoiceMax ReasoningChoice = "max"
)

// Selection identifies one provider, model, and reasoning combination.
type Selection struct {
	// Provider identifies the selected model provider.
	Provider ProviderID
	// Model identifies the selected provider model.
	Model ID
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice ReasoningChoice
}

// ReasoningCapabilities describes one model reasoning contract.
type ReasoningCapabilities struct {
	// Supported reports whether the model supports reasoning controls.
	Supported bool
	// Choices lists supported reasoning choices in configured order.
	Choices []ReasoningChoice
	// Default is the reasoning choice used without an explicit selection.
	Default ReasoningChoice
}

// PricingTier overrides all rates when request input exceeds InputTokensAbove.
type PricingTier struct {
	// InputTokensAbove is the exclusive request input threshold for this tier.
	InputTokensAbove int64
	// Input is the USD rate for one million uncached input tokens.
	Input float64
	// Output is the USD rate for one million output tokens.
	Output float64
	// CacheRead is the USD rate for one million cached input tokens.
	CacheRead float64
	// CacheWrite is the USD rate for one million cache creation input tokens.
	CacheWrite float64
}

// Pricing contains USD rates per one million tokens and ordered request-wide tiers.
type Pricing struct {
	// Input is the base USD rate for one million uncached input tokens.
	Input float64
	// Output is the base USD rate for one million output tokens.
	Output float64
	// CacheRead is the base USD rate for one million cached input tokens.
	CacheRead float64
	// CacheWrite is the base USD rate for one million cache creation input tokens.
	CacheWrite float64
	// Tiers contains ordered request-wide rate overrides.
	Tiers []PricingTier
}

// Descriptor describes one configured model and its capabilities.
type Descriptor struct {
	// Provider identifies the model provider.
	Provider ProviderID
	// Model identifies the provider model.
	Model ID
	// Input lists accepted modalities in configured order.
	Input []InputModality
	// ContextWindow is the combined input and generated-output token capacity.
	ContextWindow int64
	// MaxTokens is the maximum generated-output token count.
	MaxTokens int64
	// ReasoningCapabilities describes supported reasoning choices.
	ReasoningCapabilities ReasoningCapabilities
	// ToolCapabilities describes provider-owned tool constraints.
	ToolCapabilities ToolCapabilities
	// Pricing contains configured token rates when available.
	Pricing mo.Option[Pricing]
}

// ToolCapabilities describes provider-neutral constrained tool support.
type ToolCapabilities struct {
	// StrictJSONSchema reports whether strict JSON Schema generation is available.
	StrictJSONSchema bool
	// Grammar describes supported constrained grammar formats.
	Grammar GrammarCapabilities
}

// GrammarCapabilities describes supported constrained grammar formats.
type GrammarCapabilities struct {
	// Lark reports whether Lark grammars are supported.
	Lark bool
	// Regex reports whether regular expression grammars are supported.
	Regex bool
}

// Message is one provider-neutral user message.
type Message struct {
	// Content contains ordered user-message blocks.
	Content []InputContent
}

// InputContentKind identifies one user-message content block.
type InputContentKind uint8

const (
	// InputContentText contains user text.
	InputContentText InputContentKind = iota + 1
	// InputContentImage contains image bytes and their media type.
	InputContentImage
)

// InputContent is one ordered user-message content block.
type InputContent struct {
	// Kind identifies the content payload.
	Kind InputContentKind
	// Text contains user text when Kind is InputContentText.
	Text mo.Option[string]
	// MediaType identifies the image format when Kind is InputContentImage.
	MediaType mo.Option[string]
	// Data contains encoded image bytes when Kind is InputContentImage.
	Data mo.Option[[]byte]
}

// TextMessage creates one text-only user message.
func TextMessage(text string) Message {
	return Message{Content: []InputContent{{
		Kind: InputContentText, Text: mo.Some(text), MediaType: mo.None[string](), Data: mo.None[[]byte](),
	}}}
}

// Outcome identifies why one model response ended.
type Outcome uint8

const (
	// OutcomeStop is a final response without automatic work.
	OutcomeStop Outcome = iota + 1
	// OutcomeToolUse requests finalized tool calls.
	OutcomeToolUse
	// OutcomeLength reached the provider response limit.
	OutcomeLength
	// OutcomeAborted records provider cancellation.
	OutcomeAborted
	// OutcomeFailed records provider failure.
	OutcomeFailed
)

// ContentKind identifies one ordered response content block.
type ContentKind uint8

const (
	// ContentText contains model text.
	ContentText ContentKind = iota + 1
	// ContentRefusal contains provider refusal text.
	ContentRefusal
	// ContentReasoning contains visible model reasoning and optional replay context.
	ContentReasoning
	// ContentToolCall contains one provider-neutral tool request.
	ContentToolCall
)

// Content is one ordered response content block.
type Content struct {
	// Kind identifies the response content payload.
	Kind ContentKind
	// Text contains text for text, refusal, and reasoning content.
	Text mo.Option[string]
	// Final reports whether streamed content is complete.
	Final bool
	// ProviderContext contains opaque replay state for reasoning content.
	ProviderContext mo.Option[ProviderContext]
	// ToolCall contains a tool request when Kind is ContentToolCall.
	ToolCall mo.Option[ToolCall]
}

// ProviderContextSource identifies the request contract that produced opaque context.
type ProviderContextSource struct {
	// ProviderID identifies the provider that produced the context.
	ProviderID ProviderID
	// API identifies the provider API contract.
	API string
	// Model identifies the model that produced the context.
	Model ID
	// CompatibilityKey identifies an optional replay compatibility contract.
	CompatibilityKey mo.Option[string]
}

// ProviderContext preserves an opaque replay payload with its source snapshot.
type ProviderContext struct {
	// Source identifies the request contract that produced Payload.
	Source ProviderContextSource
	// Payload contains opaque provider-owned replay data.
	Payload []byte
}

// ToolCall is one provider-neutral model-requested tool invocation.
type ToolCall struct {
	// ID identifies the tool call within the model response.
	ID string
	// Name identifies the requested tool.
	Name string
	// Arguments contains the finalized tool input.
	Arguments map[string]any
}

// ToolCallPreviewFieldKind identifies whether one preview field is complete or still streaming.
type ToolCallPreviewFieldKind uint8

const (
	// ToolCallPreviewFieldComplete contains one fully received JSON value.
	ToolCallPreviewFieldComplete ToolCallPreviewFieldKind = iota + 1
	// ToolCallPreviewFieldPrefix contains an exact received scalar prefix.
	ToolCallPreviewFieldPrefix
)

// ToolCallPreviewField contains one ordered provisional argument field.
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

// ToolCallPreview contains transient function-call state that must not enter history.
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

// Usage contains provider-reported token accounting.
type Usage struct {
	// InputTokens contains uncached input tokens after normalization.
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

// Diagnostic contains typed provider or runtime failure information.
type Diagnostic struct {
	// Code identifies the diagnostic type.
	Code string
	// Message contains diagnostic details.
	Message string
}

// Response is one finalized ordered model response.
type Response struct {
	// Content contains ordered finalized response blocks.
	Content []Content
	// Outcome identifies why the response ended.
	Outcome mo.Option[Outcome]
	// ErrorMessage contains a terminal provider or runtime failure message.
	ErrorMessage mo.Option[string]
	// Provider identifies the provider used for the request.
	Provider mo.Option[ProviderID]
	// Model identifies the configured model used for the request.
	Model mo.Option[ID]
	// ResponseModel identifies the model reported by the provider.
	ResponseModel mo.Option[ID]
	// ResponseID identifies the response in the provider system.
	ResponseID mo.Option[string]
	// Usage contains provider-reported token accounting.
	Usage mo.Option[Usage]
	// Diagnostics contains typed provider or runtime failure details.
	Diagnostics []Diagnostic
}
