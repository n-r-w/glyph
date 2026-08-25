package run

import (
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
	Position   int
	Content    model.Content
	Message    model.Response
	Preview    model.ToolCallPreview
	ToolCall   model.ToolCall
	Progress   tool.Progress
	ToolResult agent.ToolResult
	Turn       TurnSummary
	Agent      AgentSummary
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
	ErrorMessage string
}

// newEvent creates a complete event whose optional payloads have explicit zero values.
func emptyModelResponse() model.Response {
	return model.Response{
		Content: nil, Outcome: 0, ErrorMessage: "", Provider: "", Model: "", ResponseModel: nil, ResponseID: "",
		Usage: model.Usage{
			InputTokens: 0, OutputTokens: 0, CachedInputTokens: 0,
			CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
		},
		Diagnostics: nil,
	}
}

func newEvent(eventType EventType, runID string) Event {
	return Event{
		Type:     eventType,
		RunID:    runID,
		Position: 0,
		Content: model.Content{
			Kind: 0, Text: "", Final: false,
			ProviderContext: model.ProviderContext{
				Source:  model.ProviderContextSource{ProviderID: "", API: "", Model: "", CompatibilityKey: ""},
				Payload: nil,
			},
			ToolCall: model.ToolCall{ID: "", Name: "", Arguments: nil},
		},
		Message: emptyModelResponse(),
		Preview: model.ToolCallPreview{
			CallID: "", Name: "", Position: 0, Provisional: false, Fields: nil,
		},
		ToolCall:   model.ToolCall{ID: "", Name: "", Arguments: nil},
		Progress:   tool.Progress{Channel: 0, Content: ""},
		ToolResult: agent.ToolResult{CallID: "", ToolName: "", Contents: nil, IsError: false},
		Turn: TurnSummary{
			Response:    emptyModelResponse(),
			ToolResults: nil,
		},
		Agent: AgentSummary{Outcome: 0, AddedHistory: nil, ErrorMessage: ""},
	}
}
