// Package model defines provider-neutral model messages, responses, and streaming content.
package model

import "github.com/samber/mo"

// ProviderID identifies one model provider.
type ProviderID string

// ID identifies one provider model.
type ID string

// ReasoningChoice selects provider-neutral model reasoning behavior.
type ReasoningChoice string

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
	Provider        ProviderID
	Model           ID
	ReasoningChoice ReasoningChoice
}

// ReasoningCapabilities describes one model reasoning contract.
type ReasoningCapabilities struct {
	Supported bool
	Choices   []ReasoningChoice
	Default   ReasoningChoice
}

// PricingTier overrides all rates when request input exceeds InputTokensAbove.
type PricingTier struct {
	InputTokensAbove int64
	Input            float64
	Output           float64
	CacheRead        float64
	CacheWrite       float64
}

// Pricing contains USD rates per one million tokens and ordered request-wide tiers.
type Pricing struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	Tiers      []PricingTier
}

// Descriptor describes one configured model and its capabilities.
type Descriptor struct {
	Provider              ProviderID
	Model                 ID
	ReasoningCapabilities ReasoningCapabilities
	ToolCapabilities      ToolCapabilities
	Pricing               mo.Option[Pricing]
}

// ToolCapabilities describes provider-neutral constrained tool support.
type ToolCapabilities struct {
	StrictJSONSchema bool
	Grammar          GrammarCapabilities
}

// GrammarCapabilities describes supported constrained grammar formats.
type GrammarCapabilities struct {
	Lark  bool
	Regex bool
}

// Message is one provider-neutral user message.
type Message struct {
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
	Kind      InputContentKind
	Text      mo.Option[string]
	MediaType mo.Option[string]
	Data      mo.Option[[]byte]
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
	Kind            ContentKind
	Text            mo.Option[string]
	Final           bool
	ProviderContext mo.Option[ProviderContext]
	ToolCall        mo.Option[ToolCall]
}

// ProviderContextSource identifies the request contract that produced opaque context.
type ProviderContextSource struct {
	ProviderID       ProviderID
	API              string
	Model            ID
	CompatibilityKey mo.Option[string]
}

// ProviderContext preserves an opaque replay payload with its source snapshot.
type ProviderContext struct {
	Source  ProviderContextSource
	Payload []byte
}

// ToolCall is one provider-neutral model-requested tool invocation.
type ToolCall struct {
	ID        string
	Name      string
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
	Name   string
	Kind   ToolCallPreviewFieldKind
	Value  mo.Option[any]
	Prefix mo.Option[string]
}

// ToolCallPreview contains transient function-call state that must not enter history.
type ToolCallPreview struct {
	CallID      string
	Name        string
	Position    int
	Provisional bool
	Fields      []ToolCallPreviewField
}

// Usage contains provider-reported token accounting.
type Usage struct {
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	ReasoningTokens   int64
	TotalTokens       int64
}

// NormalizeUsage converts provider totals into disjoint nonnegative buckets.
func NormalizeUsage(usage Usage) Usage {
	cacheRead := max(int64(0), usage.CachedInputTokens)
	cacheWrite := max(int64(0), usage.CacheWriteTokens)
	output := max(int64(0), usage.OutputTokens)
	// Provider input includes both cache buckets, so only the remainder is uncached input.
	input := max(int64(0), usage.InputTokens-cacheRead-cacheWrite)
	// Reasoning is output detail. It cannot exceed output and never increases the total.
	reasoning := min(max(int64(0), usage.ReasoningTokens), output)
	return Usage{
		InputTokens:       input,
		OutputTokens:      output,
		CachedInputTokens: cacheRead,
		CacheWriteTokens:  cacheWrite,
		ReasoningTokens:   reasoning,
		TotalTokens:       input + output + cacheRead + cacheWrite,
	}
}

// Diagnostic contains typed provider or runtime failure information.
type Diagnostic struct {
	Code    string
	Message string
}

// Response is one finalized ordered model response.
type Response struct {
	Content       []Content
	Outcome       mo.Option[Outcome]
	ErrorMessage  mo.Option[string]
	Provider      mo.Option[ProviderID]
	Model         mo.Option[ID]
	ResponseModel mo.Option[ID]
	ResponseID    mo.Option[string]
	Usage         mo.Option[Usage]
	Diagnostics   []Diagnostic
}
