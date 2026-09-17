package lifecycle

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelselection"
)

// Event contains one Agent Core event or Host settlement event.
type Event struct {
	// Agent contains the Agent Core event when this is not settlement.
	Agent agent.Event
	// Settled reports that Agent contains only the settled run identifier.
	Settled bool
	// Selection contains detached committed-selection values when SelectionEvent is true.
	Selection modelselection.SelectionChange
	// SelectionEvent reports that this is a Host selection lifecycle event.
	SelectionEvent bool
	// ReasoningSelection distinguishes reasoning observation from model observation.
	ReasoningSelection bool
}

// settledSource creates a payload-free source value that carries only the settled run identity.
func settledSource(runID string) agent.Event {
	return agent.Event{
		Type:       agent.EventAgentEnd,
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
