// Package model defines provider-neutral model messages, responses, and streaming content.
package model

// ProviderID identifies one model provider.
type ProviderID string

// ID identifies one provider model.
type ID string

// Descriptor describes one model selected during Host startup.
type Descriptor struct {
	Provider         ProviderID
	Model            ID
	ToolCapabilities ToolCapabilities
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
	Text      string
	MediaType string
	Data      []byte
}

// TextMessage creates one text-only user message.
func TextMessage(text string) Message {
	return Message{Content: []InputContent{{
		Kind: InputContentText, Text: text, MediaType: "", Data: nil,
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
	// ContentReasoning contains a provider-visible reasoning summary.
	ContentReasoning
	// ContentProviderContext contains opaque provider-owned bytes.
	ContentProviderContext
	// ContentToolCall contains one provider-neutral tool request.
	ContentToolCall
)

// Content is one ordered response content block.
type Content struct {
	Kind            ContentKind
	Text            string
	Final           bool
	ProviderContext ProviderContext
	ToolCall        ToolCall
}

// ProviderContext preserves provider-owned bytes without interpretation.
type ProviderContext struct {
	ProviderID ProviderID
	Payload    []byte
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
	Value  any
	Prefix string
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

// Diagnostic contains safe typed provider or runtime failure information.
type Diagnostic struct {
	Code    string
	Message string
}

// Response is one finalized ordered model response.
type Response struct {
	Content       []Content
	Outcome       Outcome
	ErrorMessage  string
	Provider      ProviderID
	Model         ID
	ResponseModel *ID
	ResponseID    string
	Usage         Usage
	Diagnostics   []Diagnostic
}
