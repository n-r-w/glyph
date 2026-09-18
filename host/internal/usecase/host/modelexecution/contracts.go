// Package modelexecution owns Host logical model execution and raw provider attempts.
package modelexecution

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//go:generate go tool mockgen -source=contracts.go -destination=contracts_mock.go -package=modelexecution

// ProviderRequest contains one immutable raw provider-attempt request.
type ProviderRequest struct {
	// Instructions contains the system prompt.
	Instructions string
	// Model contains the immutable configured model snapshot.
	Model model.Descriptor
	// ReasoningChoice identifies the selected reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// History contains owned provider-neutral conversation history.
	History []agent.HistoryEntry
	// Tools contains the available model-callable tool catalog.
	Tools []tool.Descriptor
}

// StreamEventKind identifies one raw provider stream transition.
type StreamEventKind uint8

const (
	// StreamEventContentStart starts one text content block.
	StreamEventContentStart StreamEventKind = iota + 1
	// StreamEventTextDelta appends text to an active content block.
	StreamEventTextDelta
	// StreamEventContentEnd finalizes one text content block.
	StreamEventContentEnd
	// StreamEventToolCallStart starts one provisional function call.
	StreamEventToolCallStart
	// StreamEventToolCallDelta replaces one provisional function-call preview.
	StreamEventToolCallDelta
	// StreamEventToolCallEnd carries exact final function-call arguments.
	StreamEventToolCallEnd
	// StreamEventDone carries one successful terminal response.
	StreamEventDone
	// StreamEventError carries one failed or aborted terminal response.
	StreamEventError
)

// StreamEvent is one ordered raw provider stream transition.
type StreamEvent struct {
	// Kind identifies the stream transition and active payload.
	Kind StreamEventKind
	// Position identifies the response content block order.
	Position mo.Option[int]
	// Content contains one typed model content block.
	Content mo.Option[model.Content]
	// Delta contains an exact model text fragment.
	Delta mo.Option[string]
	// Preview contains provisional tool call state.
	Preview mo.Option[model.ToolCallPreview]
	// ToolCall contains one finalized tool request.
	ToolCall mo.Option[model.ToolCall]
	// Response contains the authoritative terminal response.
	Response mo.Option[model.Response]
}

// StreamHandler consumes raw provider stream transitions in provider order.
type StreamHandler func(event StreamEvent) error

// ProviderAttempt executes exactly one raw provider request.
type ProviderAttempt interface {
	// Stream executes one attempt and emits provider-neutral semantic events.
	Stream(ctx context.Context, request ProviderRequest, handle StreamHandler) error
}

// ConversationContext observes completed agent calls and later owns context preparation.
type ConversationContext interface {
	// ObserveCompletedConversation records one delivered terminal conversation response.
	ObserveCompletedConversation(request ProviderRequest, response model.Response)
}

// CatalogBinding binds one validated selection to its raw provider attempt.
type CatalogBinding struct {
	// Model contains the immutable configured model descriptor.
	Model model.Descriptor
	// ReasoningChoice identifies the validated reasoning behavior.
	ReasoningChoice model.ReasoningChoice
	// Provider executes one raw provider attempt.
	Provider ProviderAttempt
}

// CatalogResolver resolves active and explicit selections without exposing catalog storage.
type CatalogResolver interface {
	// ActiveBinding returns an atomic snapshot of the active model binding.
	ActiveBinding() CatalogBinding
	// ResolveBinding validates one exact selection without credential preflight.
	ResolveBinding(selection model.Selection) (CatalogBinding, error)
	// ResolveConfiguredBinding validates one exact selection and performs credential preflight.
	ResolveConfiguredBinding(ctx context.Context, selection model.Selection) (CatalogBinding, error)
}
