package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=run

// ModelRequest contains projected history and the available tool catalog.
type ModelRequest struct {
	Instructions    string
	Model           model.Descriptor
	ReasoningChoice model.ReasoningChoice
	History         []agent.HistoryEntry
	Tools           []tool.Descriptor
}

// RuntimeSelection is one immutable provider request snapshot.
type RuntimeSelection struct {
	Model           model.Descriptor
	ReasoningChoice model.ReasoningChoice
	Provider        ModelProvider
}

// ModelRuntime supplies the active selection immediately before a provider request.
type ModelRuntime interface {
	Current() RuntimeSelection
}

// StreamEventKind identifies one provider-neutral model stream transition.
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

// StreamEvent is one ordered provider-neutral model stream transition.
type StreamEvent struct {
	Kind     StreamEventKind
	Position mo.Option[int]
	Content  mo.Option[model.Content]
	Delta    mo.Option[string]
	Preview  mo.Option[model.ToolCallPreview]
	ToolCall mo.Option[model.ToolCall]
	Response mo.Option[model.Response]
}

// StreamHandler consumes model stream transitions in provider order.
type StreamHandler func(event StreamEvent) error

// applyStreamEvent applies one semantic stream transition to partial response state.
func applyStreamEvent(partial *model.Response, event StreamEvent) error {
	if outcome, present := partial.Outcome.Get(); present && outcome != 0 {
		return errors.New("model stream already terminated")
	}
	if event.Kind == StreamEventDone || event.Kind == StreamEventError {
		return applyTerminalStreamEvent(partial, event.Response)
	}
	position, hasPosition := event.Position.Get()
	if !hasPosition || position < 0 {
		return fmt.Errorf("model stream position %d is invalid", position)
	}
	switch event.Kind {
	case StreamEventContentStart:
		return applyContentStart(partial, position, event.Content)
	case StreamEventTextDelta, StreamEventContentEnd:
		return applyContentUpdate(partial, position, event)
	case StreamEventDone, StreamEventError:
		return errors.New("terminal model stream event reached content handling")
	case StreamEventToolCallStart, StreamEventToolCallDelta, StreamEventToolCallEnd:
		return errors.New("tool-call stream event reached content handling")
	}
	return nil
}

// applyTerminalStreamEvent replaces partial state only after all streamed content is closed.
func applyTerminalStreamEvent(partial *model.Response, responseOption mo.Option[model.Response]) error {
	response, hasResponse := responseOption.Get()
	if !hasResponse {
		return errors.New("terminal model stream event requires an outcome")
	}
	outcome, hasOutcome := response.Outcome.Get()
	if !hasOutcome || outcome == 0 {
		return errors.New("terminal model stream event requires an outcome")
	}
	for position := range partial.Content {
		content := &partial.Content[position]
		if isStreamedContent(content.Kind) && !content.Final {
			return fmt.Errorf("model content %d is still active", position)
		}
	}
	*partial = response
	return nil
}

// applyContentStart allocates and starts one typed streamed content position.
func applyContentStart(
	partial *model.Response,
	position int,
	contentOption mo.Option[model.Content],
) error {
	eventContent, hasContent := contentOption.Get()
	if !hasContent {
		return fmt.Errorf("model content %d has no start payload", position)
	}
	for len(partial.Content) <= position {
		partial.Content = append(partial.Content, model.Content{})
	}
	content := &partial.Content[position]
	if content.Kind != 0 {
		return fmt.Errorf("model content %d already started", position)
	}
	if !isStreamedContent(eventContent.Kind) {
		return fmt.Errorf("model content %d has unsupported stream kind %d", position, eventContent.Kind)
	}
	*content = model.Content{
		Kind:            eventContent.Kind,
		Text:            mo.Some(""),
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}
	return nil
}

// applyContentUpdate appends one present delta or closes one active content position.
func applyContentUpdate(partial *model.Response, position int, event StreamEvent) error {
	eventContent, hasContent := event.Content.Get()
	if !hasContent {
		return fmt.Errorf("model content %d has no stream payload", position)
	}
	if position >= len(partial.Content) {
		return fmt.Errorf("model content %d is not active", position)
	}
	content := &partial.Content[position]
	if !isStreamedContent(content.Kind) || content.Final ||
		(eventContent.Kind != 0 && eventContent.Kind != content.Kind) {
		return fmt.Errorf("model content %d is not active", position)
	}
	if event.Kind == StreamEventContentEnd {
		content.Final = true
		return nil
	}
	delta, hasDelta := event.Delta.Get()
	if !hasDelta {
		return fmt.Errorf("model content %d has no text delta", position)
	}
	text, hasText := content.Text.Get()
	if !hasText {
		return fmt.Errorf("model content %d has no accumulated text", position)
	}
	content.Text = mo.Some(text + delta)
	return nil
}

//nolint:gocyclo // The explicit branches validate the closed tool-call event lifecycle.
func applyToolCallStreamEvent(previews map[string]model.ToolCallPreview, event StreamEvent) error {
	switch event.Kind {
	case StreamEventToolCallStart:
		preview, hasPreview := event.Preview.Get()
		position, hasPosition := event.Position.Get()
		if !hasPreview || !hasPosition || position != preview.Position || preview.CallID == "" || preview.Name == "" ||
			preview.Position < 0 || !preview.Provisional {
			return errors.New("tool-call start requires provisional identity")
		}
		if _, exists := previews[preview.CallID]; exists {
			return fmt.Errorf("tool call %q already started", preview.CallID)
		}
		preview.Fields = clonePreviewFields(preview.Fields)
		previews[preview.CallID] = preview
	case StreamEventToolCallDelta:
		preview, hasPreview := event.Preview.Get()
		position, hasPosition := event.Position.Get()
		active, exists := previews[preview.CallID]
		if !hasPreview || !hasPosition || position != preview.Position || !exists || preview.Name != active.Name ||
			preview.Position != active.Position || !preview.Provisional {
			return fmt.Errorf("tool call %q is not active", preview.CallID)
		}
		preview.Fields = clonePreviewFields(preview.Fields)
		previews[preview.CallID] = preview
	case StreamEventToolCallEnd:
		toolCall, hasToolCall := event.ToolCall.Get()
		position, hasPosition := event.Position.Get()
		active, exists := previews[toolCall.ID]
		if !hasToolCall || !hasPosition || !exists || toolCall.Name != active.Name || position != active.Position {
			return fmt.Errorf("tool call %q is not active", toolCall.ID)
		}
		delete(previews, toolCall.ID)
	case StreamEventContentStart, StreamEventTextDelta, StreamEventContentEnd,
		StreamEventDone, StreamEventError:
		return fmt.Errorf("event kind %d is not a tool-call stream event", event.Kind)
	}
	return nil
}

// isStreamedContent reports whether one content kind supports ordered text deltas.
func isStreamedContent(kind model.ContentKind) bool {
	return kind == model.ContentText || kind == model.ContentRefusal || kind == model.ContentReasoning
}

// ModelProvider streams one provider-neutral response.
type ModelProvider interface {
	Stream(ctx context.Context, request ModelRequest, handle StreamHandler) error
}

// ToolRuntime exposes the Host tool catalog and execution gateway.
type ToolRuntime interface {
	Tools() []tool.Descriptor
	Execute(
		ctx context.Context,
		call model.ToolCall,
		handleProgress tool.ProgressHandler,
	) (agent.ToolResult, error)
}

// EventSink receives Agent Core lifecycle events synchronously.
type EventSink interface {
	Deliver(ctx context.Context, event Event) error
}
