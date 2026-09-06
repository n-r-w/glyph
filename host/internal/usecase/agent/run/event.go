package run

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// newEvent creates a lifecycle event without a selected payload.
func newEvent(eventType agent.EventType, runID string) agent.Event {
	return agent.Event{
		Type:       eventType,
		RunID:      runID,
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
	}
}
