package run

import (
	"github.com/n-r-w/glyph/host/internal/domain/agent"
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
	// EventMessageUpdate carries one text delta and position.
	EventMessageUpdate
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
	Position   int
	Delta      string
	Message    agent.ModelResponse
	ToolCall   agent.ToolCall
	Progress   tool.Progress
	ToolResult agent.ToolResult
	Turn       TurnSummary
	Agent      AgentSummary
}

// TurnSummary is the self-contained terminal turn payload.
type TurnSummary struct {
	Response    agent.ModelResponse
	ToolResults []agent.ToolResult
}

// AgentSummary is the self-contained terminal run payload.
type AgentSummary struct {
	Outcome      agent.RunOutcome
	AddedHistory []agent.HistoryEntry
	ErrorMessage string
}

// newEvent creates a complete event whose optional payloads have explicit zero values.
func newEvent(eventType EventType, runID string) Event {
	return Event{
		Type:       eventType,
		RunID:      runID,
		Position:   0,
		Delta:      "",
		Message:    agent.ModelResponse{Items: nil, Outcome: 0, ErrorMessage: ""},
		ToolCall:   agent.ToolCall{ID: "", Name: "", Arguments: nil},
		Progress:   tool.Progress{Channel: 0, Content: ""},
		ToolResult: agent.ToolResult{CallID: "", ToolName: "", Content: "", IsError: false},
		Turn: TurnSummary{
			Response:    agent.ModelResponse{Items: nil, Outcome: 0, ErrorMessage: ""},
			ToolResults: nil,
		},
		Agent: AgentSummary{Outcome: 0, AddedHistory: nil, ErrorMessage: ""},
	}
}
