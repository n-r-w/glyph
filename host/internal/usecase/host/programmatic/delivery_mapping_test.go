package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// testRunEvent creates one complete Agent Core event for delivery mapping tests.
func testRunEvent(
	eventType run.EventType,
	position mo.Option[int],
	content mo.Option[model.Content],
	message mo.Option[model.Response],
	preview mo.Option[model.ToolCallPreview],
	toolCall mo.Option[model.ToolCall],
	progress mo.Option[tool.Progress],
	toolResult mo.Option[agent.ToolResult],
	turn mo.Option[run.TurnSummary],
	agentSummary mo.Option[run.AgentSummary],
) run.Event {
	return run.Event{
		Type: eventType, RunID: "run", Position: position, Content: content, Message: message,
		Preview: preview, ToolCall: toolCall, Progress: progress, ToolResult: toolResult,
		Turn: turn, Agent: agentSummary,
	}
}

// testAgentEvent creates one complete public event for delivery mapping tests.
func testAgentEvent(
	eventType controller.AgentEventType,
	modelContent mo.Option[controller.ModelContent],
	toolCallPreview mo.Option[controller.ToolCallPreview],
	finalToolCall mo.Option[controller.FinalToolCall],
	toolExecution mo.Option[controller.ToolExecution],
	toolProgress mo.Option[controller.ToolProgress],
	toolResult mo.Option[controller.ToolResult],
	modelResponse mo.Option[controller.ModelResponse],
	turn mo.Option[controller.TurnSummary],
	agentSummary mo.Option[controller.AgentSummary],
) controller.AgentEvent {
	return controller.AgentEvent{
		Type: eventType, CorrelationID: "", RunID: "", ModelContent: modelContent,
		ToolCallPreview: toolCallPreview, FinalToolCall: finalToolCall, ToolExecution: toolExecution,
		ToolProgress: toolProgress, ToolResult: toolResult, ModelResponse: modelResponse,
		Turn: turn, Agent: agentSummary,
	}
}

// testEmptyRunEvent creates an agent event without a variant payload.
func testEmptyRunEvent(kind run.EventType, runID string) run.Event {
	event := testRunEvent(
		kind,
		mo.None[int](),
		mo.None[model.Content](),
		mo.None[model.Response](),
		mo.None[model.ToolCallPreview](),
		mo.None[model.ToolCall](),
		mo.None[tool.Progress](),
		mo.None[agent.ToolResult](),
		mo.None[run.TurnSummary](),
		mo.None[run.AgentSummary](),
	)
	event.RunID = runID
	return event
}

// testEmptyAgentEvent creates a delivery event without a variant payload.
func testEmptyAgentEvent(kind controller.AgentEventType, correlationID string, runID string) controller.AgentEvent {
	event := testAgentEvent(
		kind,
		mo.None[controller.ModelContent](),
		mo.None[controller.ToolCallPreview](),
		mo.None[controller.FinalToolCall](),
		mo.None[controller.ToolExecution](),
		mo.None[controller.ToolProgress](),
		mo.None[controller.ToolResult](),
		mo.None[controller.ModelResponse](),
		mo.None[controller.TurnSummary](),
		mo.None[controller.AgentSummary](),
	)
	event.CorrelationID = correlationID
	event.RunID = runID
	return event
}

// testModelContent creates one model-content fixture for delivery mapping.
func testModelContent(kind model.ContentKind, text string) model.Content {
	return model.Content{
		Final:           false,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
		Kind:            kind,
		Text:            mo.Some(text),
	}
}

// testModelContentAgentEvent creates one mapped model-content event fixture.
func testModelContentAgentEvent(
	kind controller.AgentEventType,
	contentKind controller.ModelContentKind,
	position int,
	text mo.Option[string],
) controller.AgentEvent {
	return testAgentEvent(
		kind,
		mo.Some(controller.ModelContent{Text: text, Kind: contentKind, Position: position}),
		mo.None[controller.ToolCallPreview](),
		mo.None[controller.FinalToolCall](),
		mo.None[controller.ToolExecution](),
		mo.None[controller.ToolProgress](),
		mo.None[controller.ToolResult](),
		mo.None[controller.ModelResponse](),
		mo.None[controller.TurnSummary](),
		mo.None[controller.AgentSummary](),
	)
}

// testToolResultRunEvent creates one run event carrying a tool result.
func testToolResultRunEvent(kind run.EventType, result agent.ToolResult) run.Event {
	return testRunEvent(kind, mo.None[int](), mo.None[model.Content](), mo.None[model.Response](),
		mo.None[model.ToolCallPreview](), mo.None[model.ToolCall](), mo.None[tool.Progress](), mo.Some(result),
		mo.None[run.TurnSummary](), mo.None[run.AgentSummary]())
}

// testToolResultAgentEvent creates one mapped event carrying a tool result.
func testToolResultAgentEvent(kind controller.AgentEventType, result agent.ToolResult) controller.AgentEvent {
	return testAgentEvent(kind, mo.None[controller.ModelContent](), mo.None[controller.ToolCallPreview](),
		mo.None[controller.FinalToolCall](), mo.None[controller.ToolExecution](), mo.None[controller.ToolProgress](),
		mo.Some(mapToolResult(result)), mo.None[controller.ModelResponse](), mo.None[controller.TurnSummary](),
		mo.None[controller.AgentSummary]())
}

// TestDeliveryMapsEveryAgentEvent verifies exhaustive transport-independent event mapping.
func TestDeliveryMapsEveryAgentEvent(t *testing.T) {
	t.Parallel()

	// Arrange every supported agent event with complete public payloads.
	response := model.Response{
		ErrorMessage:  mo.None[string](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
		Content: []model.Content{
			{
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some("answer"),
				Final:           true,
			},
			{
				Text:     mo.None[string](),
				Final:    true,
				ToolCall: mo.None[model.ToolCall](),
				Kind:     model.ContentReasoning,
				ProviderContext: mo.Some(
					model.ProviderContext{
						Source: model.ProviderContextSource{
							API:              "",
							Model:            "",
							CompatibilityKey: mo.None[string](),
							ProviderID:       "provider",
						},
						Payload: []byte("private"),
					},
				),
			},
		},
		Outcome: mo.Some(
			model.OutcomeStop,
		),
		Provider: mo.Some(model.ProviderID("provider")),
		Model:    mo.Some(model.ID("model")),
	}
	mappedResponse, err := mapModelResponseProjection(response)
	require.NoError(t, err)
	toolResult := agent.ToolResult{
		IsError:  false,
		CallID:   "call",
		ToolName: "tool",
		Contents: []tool.ResultContent{
			{
				Kind:  tool.ResultContentText,
				Text:  mo.Some("output"),
				Image: mo.None[tool.ResultImage](),
			},
		},
	}
	tests := []struct {
		name     string
		event    run.Event
		expected controller.AgentEvent
	}{
		{
			name:     "agent start",
			event:    testEmptyRunEvent(run.EventAgentStart, "run"),
			expected: testEmptyAgentEvent(controller.AgentEventAgentStart, "", ""),
		},
		{
			name:     "turn start",
			event:    testEmptyRunEvent(run.EventTurnStart, "run"),
			expected: testEmptyAgentEvent(controller.AgentEventTurnStart, "", ""),
		},
		{
			name:     "message start",
			event:    testEmptyRunEvent(run.EventMessageStart, "run"),
			expected: testEmptyAgentEvent(controller.AgentEventMessageStart, "", ""),
		},
		{
			name: "content start",
			event: testRunEvent(
				run.EventContentStart,
				mo.Some(2),
				mo.Some(testModelContent(model.ContentReasoning, "")),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testModelContentAgentEvent(
				controller.AgentEventModelContentStart,
				controller.ModelContentReasoning,
				2,
				mo.None[string](),
			),
		},
		{
			name: "text delta",
			event: testRunEvent(
				run.EventTextDelta,
				mo.Some(3),
				mo.Some(testModelContent(model.ContentRefusal, "no")),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testModelContentAgentEvent(
				controller.AgentEventModelTextDelta,
				controller.ModelContentRefusal,
				3,
				mo.Some("no"),
			),
		},
		{
			name: "content end",
			event: testRunEvent(
				run.EventContentEnd,
				mo.Some(4),
				mo.Some(testModelContent(model.ContentText, "")),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testModelContentAgentEvent(
				controller.AgentEventModelContentEnd,
				controller.ModelContentText,
				4,
				mo.None[string](),
			),
		},
		{
			name: "tool call start",
			event: testRunEvent(
				run.EventToolCallStart,
				mo.Some(0),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.Some(model.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []model.ToolCallPreviewField{
						{
							Prefix: mo.None[string](),
							Name:   "null",
							Kind:   model.ToolCallPreviewFieldComplete,
							Value:  mo.Some[any](nil),
						},
						{
							Prefix: mo.None[string](),
							Name:   "arg",
							Kind:   model.ToolCallPreviewFieldComplete,
							Value:  mo.Some[any](map[string]any{"nested": []any{"value"}}),
						},
					},
				}),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventToolCallStart,
				mo.None[controller.ModelContent](),
				mo.Some(controller.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []controller.ToolCallPreviewField{
						{
							Prefix: mo.None[string](),
							Name:   "null",
							Kind:   controller.ToolCallPreviewFieldComplete,
							Value:  mo.Some[any](nil),
						},
						{
							Prefix: mo.None[string](),
							Name:   "arg",
							Kind:   controller.ToolCallPreviewFieldComplete,
							Value:  mo.Some[any](map[string]any{"nested": []any{"value"}}),
						},
					},
				}),
				mo.None[controller.FinalToolCall](),
				mo.None[controller.ToolExecution](),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.None[controller.TurnSummary](),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name: "tool call delta",
			event: testRunEvent(
				run.EventToolCallDelta,
				mo.Some(0),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.Some(model.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []model.ToolCallPreviewField{
						{
							Value:  mo.None[any](),
							Name:   "arg",
							Kind:   model.ToolCallPreviewFieldPrefix,
							Prefix: mo.Some(""),
						},
					},
				}),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventToolCallDelta,
				mo.None[controller.ModelContent](),
				mo.Some(controller.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []controller.ToolCallPreviewField{
						{
							Value:  mo.None[any](),
							Name:   "arg",
							Kind:   controller.ToolCallPreviewFieldPrefix,
							Prefix: mo.Some(""),
						},
					},
				}),
				mo.None[controller.FinalToolCall](),
				mo.None[controller.ToolExecution](),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.None[controller.TurnSummary](),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name: "tool call end",
			event: testRunEvent(
				run.EventToolCallEnd,
				mo.Some(5),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.Some(model.ToolCall{
					ID:        "call",
					Name:      "tool",
					Arguments: map[string]any{"arg": "value"},
				}),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventToolCallEnd,
				mo.None[controller.ModelContent](),
				mo.None[controller.ToolCallPreview](),
				mo.Some(controller.FinalToolCall{
					CallID:    "call",
					Name:      "tool",
					Position:  5,
					Arguments: map[string]any{"arg": "value"},
				}),
				mo.None[controller.ToolExecution](),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.None[controller.TurnSummary](),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name: "message end",
			event: testRunEvent(
				run.EventMessageEnd,
				mo.None[int](),
				mo.None[model.Content](),
				mo.Some(response),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventMessageEnd,
				mo.None[controller.ModelContent](),
				mo.None[controller.ToolCallPreview](),
				mo.None[controller.FinalToolCall](),
				mo.None[controller.ToolExecution](),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.Some(mappedResponse),
				mo.None[controller.TurnSummary](),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name: "tool execution start",
			event: testRunEvent(
				run.EventToolExecutionStart,
				mo.None[int](),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.Some(model.ToolCall{
					Arguments: nil,
					ID:        "call",
					Name:      "tool",
				}),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventToolExecutionStart,
				mo.None[controller.ModelContent](),
				mo.None[controller.ToolCallPreview](),
				mo.None[controller.FinalToolCall](),
				mo.Some(controller.ToolExecution{
					CallID:   "call",
					ToolName: "tool",
				}),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.None[controller.TurnSummary](),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name: "tool execution update",
			event: testRunEvent(
				run.EventToolExecutionUpdate,
				mo.None[int](),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.Some(tool.Progress{
					Channel: tool.ProgressChannelStdout,
					Content: "line",
				}),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventToolExecutionUpdate,
				mo.None[controller.ModelContent](),
				mo.None[controller.ToolCallPreview](),
				mo.None[controller.FinalToolCall](),
				mo.None[controller.ToolExecution](),
				mo.Some(controller.ToolProgress{
					Channel: controller.ProgressChannelStdout,
					Content: "line",
				}),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.None[controller.TurnSummary](),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name:     "tool execution end",
			event:    testToolResultRunEvent(run.EventToolExecutionEnd, toolResult),
			expected: testToolResultAgentEvent(controller.AgentEventToolExecutionEnd, toolResult),
		},
		{
			name:     "tool result",
			event:    testToolResultRunEvent(run.EventToolResult, toolResult),
			expected: testToolResultAgentEvent(controller.AgentEventToolResult, toolResult),
		},
		{
			name: "turn end",
			event: testRunEvent(
				run.EventTurnEnd,
				mo.None[int](),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.Some(run.TurnSummary{
					Response:    response,
					ToolResults: []agent.ToolResult{toolResult},
				}),
				mo.None[run.AgentSummary](),
			),
			expected: testAgentEvent(
				controller.AgentEventTurnEnd,
				mo.None[controller.ModelContent](),
				mo.None[controller.ToolCallPreview](),
				mo.None[controller.FinalToolCall](),
				mo.None[controller.ToolExecution](),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.Some(controller.TurnSummary{
					Response:    mappedResponse,
					ToolResults: []controller.ToolResult{mapToolResult(toolResult)},
				}),
				mo.None[controller.AgentSummary](),
			),
		},
		{
			name: "agent end",
			event: testRunEvent(
				run.EventAgentEnd,
				mo.None[int](),
				mo.None[model.Content](),
				mo.None[model.Response](),
				mo.None[model.ToolCallPreview](),
				mo.None[model.ToolCall](),
				mo.None[tool.Progress](),
				mo.None[agent.ToolResult](),
				mo.None[run.TurnSummary](),
				mo.Some(run.AgentSummary{
					AddedHistory: nil,
					Outcome:      agent.RunOutcomeFailed,
					ErrorMessage: mo.Some("failed"),
				}),
			),
			expected: testAgentEvent(
				controller.AgentEventAgentEnd,
				mo.None[controller.ModelContent](),
				mo.None[controller.ToolCallPreview](),
				mo.None[controller.FinalToolCall](),
				mo.None[controller.ToolExecution](),
				mo.None[controller.ToolProgress](),
				mo.None[controller.ToolResult](),
				mo.None[controller.ModelResponse](),
				mo.None[controller.TurnSummary](),
				mo.Some(controller.AgentSummary{
					Outcome:      controller.RunOutcomeFailed,
					ErrorMessage: mo.Some("failed"),
				}),
			),
		},
	}

	// Act by delivering each event through an independently reserved active run.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			delivery := NewDelivery()
			active := newTestActiveRun(t.Context(), delivery, "correlation", "run")

			// Assert reservation and delivery preserve the expected event mapping.
			require.True(t, delivery.reserve(active))
			delivered := make(chan error)
			go func() { delivered <- delivery.DeliverAgent(t.Context(), test.event) }()

			expected := test.expected
			expected.CorrelationID = "correlation"
			expected.RunID = "run"
			assert.Equal(t, expected, <-active.Events())
			require.NoError(t, <-delivered)
			delivery.finish(active, nil)
		})
	}
}
