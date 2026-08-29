package compatible

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// textStreamEvent creates one text-like content lifecycle event.
func textStreamEvent(
	eventKind run.StreamEventKind,
	position int,
	contentKind model.ContentKind,
	text string,
	delta mo.Option[string],
) run.StreamEvent {
	return run.StreamEvent{
		Kind:     eventKind,
		Position: mo.Some(position),
		Content: mo.Some(model.Content{
			Kind:            contentKind,
			Text:            mo.Some(text),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		}),
		Delta:    delta,
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.None[model.Response](),
	}
}

// terminalStreamEvent creates one terminal event carrying the authoritative response.
func terminalStreamEvent(kind run.StreamEventKind, response model.Response) run.StreamEvent {
	return run.StreamEvent{
		Kind:     kind,
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](),
		Response: mo.Some(response),
	}
}

// toolCallEndStreamEvent creates one finalized tool-call event.
func toolCallEndStreamEvent(position int, call model.ToolCall) run.StreamEvent {
	return run.StreamEvent{
		Kind:     run.StreamEventToolCallEnd,
		Position: mo.Some(position),
		Content:  mo.None[model.Content](),
		Delta:    mo.None[string](),
		Preview:  mo.None[model.ToolCallPreview](),
		ToolCall: mo.Some(call),
		Response: mo.None[model.Response](),
	}
}
