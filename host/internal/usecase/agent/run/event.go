package run

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// EventType identifies one Agent Core lifecycle event.
type EventType uint8

const (
	// EventAgentStart starts one run.
	EventAgentStart EventType = iota + 1
	// EventTurnStart starts one model turn.
	EventTurnStart
	// EventMessageStart starts one streamed model message.
	EventMessageStart
	// EventContentStart starts one typed model content block.
	EventContentStart
	// EventTextDelta carries one model text fragment.
	EventTextDelta
	// EventContentEnd finalizes one typed model content block.
	EventContentEnd
	// EventToolCallStart starts one provisional function call.
	EventToolCallStart
	// EventToolCallDelta replaces one provisional function-call preview.
	EventToolCallDelta
	// EventToolCallEnd carries exact final function-call arguments.
	EventToolCallEnd
	// EventMessageEnd carries the complete terminal model response.
	EventMessageEnd
	// EventToolExecutionStart starts one tool call.
	EventToolExecutionStart
	// EventToolExecutionUpdate carries one tool progress fragment.
	EventToolExecutionUpdate
	// EventToolExecutionEnd carries the terminal execution result.
	EventToolExecutionEnd
	// EventToolResult records one model-visible tool-result message.
	EventToolResult
	// EventTurnEnd carries the complete turn response and results.
	EventTurnEnd
	// EventAgentEnd carries the terminal run and added history.
	EventAgentEnd
)

// Event is one synchronously delivered Agent Core lifecycle event.
type Event struct {
	Type       EventType
	RunID      string
	Position   mo.Option[int]
	Content    mo.Option[model.Content]
	Message    mo.Option[model.Response]
	Preview    mo.Option[model.ToolCallPreview]
	ToolCall   mo.Option[model.ToolCall]
	Progress   mo.Option[tool.Progress]
	ToolResult mo.Option[agent.ToolResult]
	Turn       mo.Option[TurnSummary]
	Agent      mo.Option[AgentSummary]
}

// TurnSummary is the self-contained terminal turn payload.
type TurnSummary struct {
	Response    model.Response
	ToolResults []agent.ToolResult
}

// AgentSummary is the self-contained terminal run payload.
type AgentSummary struct {
	Outcome      agent.RunOutcome
	AddedHistory []agent.HistoryEntry
	ErrorMessage mo.Option[string]
}

func newEvent(eventType EventType, runID string) Event {
	return Event{
		Type:       eventType,
		RunID:      runID,
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[TurnSummary](),
		Agent:      mo.None[AgentSummary](),
	}
}
