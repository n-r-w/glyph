package run

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=run

// ModelRequest contains projected history and the available tool catalog.
type ModelRequest struct {
	Instructions   string
	Model          model.Descriptor
	ReasoningLevel model.ReasoningLevel
	History        []agent.HistoryEntry
	Tools          []tool.Descriptor
}

// RuntimeSelection is one immutable provider request snapshot.
type RuntimeSelection struct {
	Model          model.Descriptor
	ReasoningLevel model.ReasoningLevel
	Provider       ModelProvider
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
	Position int
	Content  model.Content
	Delta    string
	Preview  model.ToolCallPreview
	ToolCall model.ToolCall
	Response model.Response
}

// StreamHandler consumes model stream transitions in provider order.
type StreamHandler func(event StreamEvent) error

// applyStreamEvent applies one semantic stream transition to partial response state.
//
//nolint:gocyclo // The branches enforce the finite semantic stream state machine.
func applyStreamEvent(partial *model.Response, event StreamEvent) error {
	if partial.Outcome != 0 {
		return errors.New("model stream already terminated")
	}
	if event.Kind == StreamEventDone || event.Kind == StreamEventError {
		if event.Response.Outcome == 0 {
			return errors.New("terminal model stream event requires an outcome")
		}
		for position, content := range partial.Content {
			if isStreamedContent(content.Kind) && !content.Final {
				return fmt.Errorf("model content %d is still active", position)
			}
		}
		*partial = event.Response
		return nil
	}
	if event.Position < 0 {
		return fmt.Errorf("model stream position %d is invalid", event.Position)
	}
	switch event.Kind {
	case StreamEventContentStart:
		for len(partial.Content) <= event.Position {
			partial.Content = append(partial.Content, model.Content{
				Kind: 0, Text: "", Final: false,
				ProviderContext: model.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
			})
		}
		content := &partial.Content[event.Position]
		if content.Kind != 0 {
			return fmt.Errorf("model content %d already started", event.Position)
		}
		if !isStreamedContent(event.Content.Kind) {
			return fmt.Errorf("model content %d has unsupported stream kind %d", event.Position, event.Content.Kind)
		}
		*content = model.Content{
			Kind: event.Content.Kind, Text: "", Final: false,
			ProviderContext: model.ProviderContext{ProviderID: "", Payload: nil},
			ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
		}
	case StreamEventTextDelta, StreamEventContentEnd:
		if event.Position >= len(partial.Content) {
			return fmt.Errorf("model content %d is not active", event.Position)
		}
		content := &partial.Content[event.Position]
		if !isStreamedContent(content.Kind) || content.Final ||
			(event.Content.Kind != 0 && event.Content.Kind != content.Kind) {
			return fmt.Errorf("model content %d is not active", event.Position)
		}
		if event.Kind == StreamEventTextDelta {
			content.Text += event.Delta
		} else {
			content.Final = true
		}
	case StreamEventDone, StreamEventError:
		return errors.New("terminal model stream event reached content handling")
	case StreamEventToolCallStart, StreamEventToolCallDelta, StreamEventToolCallEnd:
		return errors.New("tool-call stream event reached content handling")
	}
	return nil
}

//nolint:gocyclo // The explicit branches validate the closed tool-call event lifecycle.
func applyToolCallStreamEvent(previews map[string]model.ToolCallPreview, event StreamEvent) error {
	switch event.Kind {
	case StreamEventToolCallStart:
		preview := event.Preview
		if preview.CallID == "" || preview.Name == "" || preview.Position < 0 || !preview.Provisional {
			return errors.New("tool-call start requires provisional identity")
		}
		if _, exists := previews[preview.CallID]; exists {
			return fmt.Errorf("tool call %q already started", preview.CallID)
		}
		preview.Fields = clonePreviewFields(preview.Fields)
		previews[preview.CallID] = preview
	case StreamEventToolCallDelta:
		preview := event.Preview
		active, exists := previews[preview.CallID]
		if !exists || preview.Name != active.Name || preview.Position != active.Position || !preview.Provisional {
			return fmt.Errorf("tool call %q is not active", preview.CallID)
		}
		preview.Fields = clonePreviewFields(preview.Fields)
		previews[preview.CallID] = preview
	case StreamEventToolCallEnd:
		active, exists := previews[event.ToolCall.ID]
		if !exists || event.ToolCall.Name != active.Name || event.Position != active.Position {
			return fmt.Errorf("tool call %q is not active", event.ToolCall.ID)
		}
		delete(previews, event.ToolCall.ID)
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
