package run

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// lifecycleContent projects public text deltas and explicit empty content boundaries for observers.
func (event StreamEvent) lifecycleContent() mo.Option[model.Content] {
	content, present := event.Content.Get()
	if !present {
		return mo.None[model.Content]()
	}
	text := event.Delta
	if text.IsNone() && content.Kind != model.ContentToolCall {
		text = mo.Some("")
	}
	return mo.Some(
		model.Content{
			Kind:            content.Kind,
			Text:            text,
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	)
}
