package agent

import (
	"github.com/samber/mo"

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
	// Type identifies the lifecycle transition and active payload.
	Type EventType
	// RunID identifies the agent run.
	RunID string
	// Position identifies the response content block order.
	Position mo.Option[int]
	// Content contains one typed model content block.
	Content mo.Option[model.Content]
	// Message contains one terminal model response.
	Message mo.Option[model.Response]
	// Preview contains provisional tool call state.
	Preview mo.Option[model.ToolCallPreview]
	// ToolCall contains one finalized tool request.
	ToolCall mo.Option[model.ToolCall]
	// Progress contains one tool execution update.
	Progress mo.Option[tool.Progress]
	// ToolResult contains one terminal tool result.
	ToolResult mo.Option[ToolResult]
	// Turn contains one terminal turn summary.
	Turn mo.Option[TurnSummary]
	// Agent contains one terminal run summary.
	Agent mo.Option[RunSummary]
}

// TurnSummary is the self-contained terminal turn payload.
type TurnSummary struct {
	// Response contains the terminal model response.
	Response model.Response
	// ToolResults contains ordered terminal tool results.
	ToolResults []ToolResult
}

// RunSummary is the self-contained terminal run payload.
type RunSummary struct {
	// Outcome identifies the terminal run state.
	Outcome RunOutcome
	// AddedHistory contains entries appended by the run.
	AddedHistory []HistoryEntry
	// ErrorMessage contains a terminal failure message.
	ErrorMessage mo.Option[string]
}
