//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// modelContentAgentEvent creates one event carrying model content.
func modelContentAgentEvent(kind ModelContentKind, position int, text mo.Option[string]) AgentEvent {
	return AgentEvent{
		ModelContent:    mo.Some(ModelContent{Kind: kind, Position: position, Text: text}),
		CorrelationID:   "",
		Type:            0,
		RunID:           "",
		ToolCallPreview: mo.None[ToolCallPreview](),
		FinalToolCall:   mo.None[FinalToolCall](),
		ToolExecution:   mo.None[ToolExecution](),
		ToolProgress:    mo.None[ToolProgress](),
		ToolResult:      mo.None[ToolResult](),
		ModelResponse:   mo.None[ModelResponse](),
		Turn:            mo.None[TurnSummary](),
		Agent:           mo.None[AgentSummary](),
	}
}

// TestMapEventPreservesEveryEvent verifies every event enum and payload oneof.
func TestMapEventPreservesEveryEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typeValue AgentEventType
		payload   string
		event     AgentEvent
	}{
		{
			typeValue: AgentEventAgentStart,
			payload:   "",
			event:     AgentEvent{},
		},
		{
			typeValue: AgentEventTurnStart,
			payload:   "",
			event:     AgentEvent{},
		},
		{
			typeValue: AgentEventMessageStart,
			payload:   "",
			event:     AgentEvent{},
		},
		{
			typeValue: AgentEventModelContentStart,
			payload:   "model_content",
			event:     modelContentAgentEvent(ModelContentReasoning, 2, mo.None[string]()),
		},
		{
			typeValue: AgentEventModelTextDelta,
			payload:   "model_content",
			event:     modelContentAgentEvent(ModelContentText, 1, mo.Some("delta")),
		},
		{
			typeValue: AgentEventModelContentEnd,
			payload:   "model_content",
			event:     modelContentAgentEvent(ModelContentRefusal, 3, mo.None[string]()),
		},
		{
			typeValue: AgentEventToolCallStart,
			payload:   "tool_call_preview",
			event: AgentEvent{
				ToolCallPreview: mo.Some(maximalToolCallPreview()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolCallDelta,
			payload:   "tool_call_preview",
			event: AgentEvent{
				ToolCallPreview: mo.Some(maximalToolCallPreview()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolCallEnd,
			payload:   "final_tool_call",
			event: AgentEvent{
				FinalToolCall:   mo.Some(maximalFinalToolCall()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventMessageEnd,
			payload:   "model_response",
			event: AgentEvent{
				ModelResponse:   mo.Some(maximalModelResponse(mo.Some(""))),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolExecutionStart,
			payload:   "tool_execution",
			event: AgentEvent{
				ToolExecution: mo.Some(ToolExecution{
					CallID:   "call",
					ToolName: "tool",
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolExecutionUpdate,
			payload:   "tool_progress",
			event: AgentEvent{
				ToolProgress: mo.Some(ToolProgress{
					Channel: ProgressChannelStderr,
					Content: "progress",
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolExecutionEnd,
			payload:   "tool_result",
			event: AgentEvent{
				ToolResult:      mo.Some(maximalToolResult()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolResult,
			payload:   "tool_result",
			event: AgentEvent{
				ToolResult:      mo.Some(maximalToolResult()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventTurnEnd,
			payload:   "turn",
			event: AgentEvent{
				Turn: mo.Some(TurnSummary{
					Response:    maximalModelResponse(mo.Some("")),
					ToolResults: []ToolResult{maximalToolResult()},
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventAgentEnd,
			payload:   "agent",
			event: AgentEvent{
				Agent: mo.Some(AgentSummary{
					Outcome:      RunOutcomeFailed,
					ErrorMessage: mo.Some("failed"),
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
			},
		},
		{
			typeValue: AgentEventAgentSettled,
			payload:   "",
			event:     AgentEvent{},
		},
	}

	for _, test := range tests {
		t.Run(programmaticv1.AgentEventType(test.typeValue).String(), func(t *testing.T) {
			t.Parallel()
			test.event.Type = test.typeValue
			test.event.CorrelationID = "correlation"
			test.event.RunID = "run"
			got, err := mapEvent(test.event)
			require.NoError(t, err)
			assert.Equal(t, "correlation", got.GetCorrelationId())
			event := got.GetAgentEvent()
			assert.Equal(t, programmaticv1.AgentEventType(test.typeValue), event.GetType())
			assert.Equal(t, "run", event.GetRunId())
			if test.payload == "" {
				assert.False(t, event.HasPayload())
			} else {
				assert.Equal(t, test.payload, event.WhichPayload().String())
			}
			assertEventPayload(t, test.payload, event)
		})
	}
}
