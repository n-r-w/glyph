package lifecycle

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// Event contains one Agent Core event or Host settlement event.
type Event struct {
	// Agent contains the Agent Core event when this is not settlement.
	Agent run.Event
	// Settled reports that Agent contains only the settled run identifier.
	Settled bool
}

// settledSource creates a payload-free source value that carries only the settled run identity.
func settledSource(runID string) run.Event {
	return run.Event{
		Type: run.EventAgentEnd, RunID: runID, Position: mo.None[int](), Content: mo.None[model.Content](),
		Message: mo.None[model.Response](), Preview: mo.None[model.ToolCallPreview](),
		ToolCall: mo.None[model.ToolCall](), Progress: mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](), Turn: mo.None[run.TurnSummary](), Agent: mo.None[run.AgentSummary](),
	}
}
