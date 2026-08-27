package programmatic

import (
	"context"
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
			name: "agent start",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventAgentStart,
				RunID:      "run",
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventAgentStart,
			},
		},
		{
			name: "turn start",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventTurnStart,
				RunID:      "run",
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventTurnStart,
			},
		},
		{
			name: "message start",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventMessageStart,
				RunID:      "run",
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventMessageStart,
			},
		},
		{
			name: "content start",
			event: run.Event{
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventContentStart,
				RunID:      "run",
				Position:   mo.Some(2),
				Content: mo.Some(model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentReasoning,
					Text:            mo.Some(""),
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventModelContentStart,
				ModelContent: mo.Some(controller.ModelContent{
					Text:     mo.None[string](),
					Kind:     controller.ModelContentReasoning,
					Position: 2,
				}),
			},
		},
		{
			name: "text delta",
			event: run.Event{
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventTextDelta,
				RunID:      "run",
				Position:   mo.Some(3),
				Content: mo.Some(model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentRefusal,
					Text:            mo.Some("no"),
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventModelTextDelta,
				ModelContent: mo.Some(controller.ModelContent{
					Kind:     controller.ModelContentRefusal,
					Position: 3,
					Text:     mo.Some("no"),
				}),
			},
		},
		{
			name: "content end",
			event: run.Event{
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventContentEnd,
				RunID:      "run",
				Position:   mo.Some(4),
				Content: mo.Some(model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some(""),
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventModelContentEnd,
				ModelContent: mo.Some(controller.ModelContent{
					Text:     mo.None[string](),
					Kind:     controller.ModelContentText,
					Position: 4,
				}),
			},
		},
		{
			name: "tool call start",
			event: run.Event{
				Position:   mo.Some(0),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolCallStart,
				RunID:      "run",
				Preview: mo.Some(model.ToolCallPreview{
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
			},
			expected: controller.AgentEvent{
				CorrelationID: "",
				RunID:         "",
				ModelContent:  mo.None[controller.ModelContent](),
				FinalToolCall: mo.None[controller.FinalToolCall](),
				ToolExecution: mo.None[controller.ToolExecution](),
				ToolProgress:  mo.None[controller.ToolProgress](),
				ToolResult:    mo.None[controller.ToolResult](),
				ModelResponse: mo.None[controller.ModelResponse](),
				Turn:          mo.None[controller.TurnSummary](),
				Agent:         mo.None[controller.AgentSummary](),
				Type:          controller.AgentEventToolCallStart,
				ToolCallPreview: mo.Some(controller.ToolCallPreview{
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
			},
		},
		{
			name: "tool call delta",
			event: run.Event{
				Position:   mo.Some(0),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolCallDelta,
				RunID:      "run",
				Preview: mo.Some(model.ToolCallPreview{
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
			},
			expected: controller.AgentEvent{
				CorrelationID: "",
				RunID:         "",
				ModelContent:  mo.None[controller.ModelContent](),
				FinalToolCall: mo.None[controller.FinalToolCall](),
				ToolExecution: mo.None[controller.ToolExecution](),
				ToolProgress:  mo.None[controller.ToolProgress](),
				ToolResult:    mo.None[controller.ToolResult](),
				ModelResponse: mo.None[controller.ModelResponse](),
				Turn:          mo.None[controller.TurnSummary](),
				Agent:         mo.None[controller.AgentSummary](),
				Type:          controller.AgentEventToolCallDelta,
				ToolCallPreview: mo.Some(controller.ToolCallPreview{
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
			},
		},
		{
			name: "tool call end",
			event: run.Event{
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolCallEnd,
				RunID:      "run",
				Position:   mo.Some(5),
				ToolCall: mo.Some(model.ToolCall{
					ID:        "call",
					Name:      "tool",
					Arguments: map[string]any{"arg": "value"},
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventToolCallEnd,
				FinalToolCall: mo.Some(controller.FinalToolCall{
					CallID:    "call",
					Name:      "tool",
					Position:  5,
					Arguments: map[string]any{"arg": "value"},
				}),
			},
		},
		{
			name: "message end",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventMessageEnd,
				RunID:      "run",
				Message:    mo.Some(response),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventMessageEnd,
				ModelResponse:   mo.Some(mappedResponse),
			},
		},
		{
			name: "tool execution start",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolExecutionStart,
				RunID:      "run",
				ToolCall: mo.Some(model.ToolCall{
					Arguments: nil,
					ID:        "call",
					Name:      "tool",
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventToolExecutionStart,
				ToolExecution: mo.Some(controller.ToolExecution{
					CallID:   "call",
					ToolName: "tool",
				}),
			},
		},
		{
			name: "tool execution update",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolExecutionUpdate,
				RunID:      "run",
				Progress: mo.Some(tool.Progress{
					Channel: tool.ProgressChannelStdout,
					Content: "line",
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventToolExecutionUpdate,
				ToolProgress: mo.Some(controller.ToolProgress{
					Channel: controller.ProgressChannelStdout,
					Content: "line",
				}),
			},
		},
		{
			name: "tool execution end",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolExecutionEnd,
				RunID:      "run",
				ToolResult: mo.Some(toolResult),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventToolExecutionEnd,
				ToolResult:      mo.Some(mapToolResult(toolResult)),
			},
		},
		{
			name: "tool result",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolResult,
				RunID:      "run",
				ToolResult: mo.Some(toolResult),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventToolResult,
				ToolResult:      mo.Some(mapToolResult(toolResult)),
			},
		},
		{
			name: "turn end",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventTurnEnd,
				RunID:      "run",
				Turn: mo.Some(run.TurnSummary{
					Response:    response,
					ToolResults: []agent.ToolResult{toolResult},
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Agent:           mo.None[controller.AgentSummary](),
				Type:            controller.AgentEventTurnEnd,
				Turn: mo.Some(controller.TurnSummary{
					Response:    mappedResponse,
					ToolResults: []controller.ToolResult{mapToolResult(toolResult)},
				}),
			},
		},
		{
			name: "agent end",
			event: run.Event{
				Position:   mo.None[int](),
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				Preview:    mo.None[model.ToolCallPreview](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Type:       run.EventAgentEnd,
				RunID:      "run",
				Agent: mo.Some(run.AgentSummary{
					AddedHistory: nil,
					Outcome:      agent.RunOutcomeFailed,
					ErrorMessage: mo.Some("failed"),
				}),
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[controller.ModelContent](),
				ToolCallPreview: mo.None[controller.ToolCallPreview](),
				FinalToolCall:   mo.None[controller.FinalToolCall](),
				ToolExecution:   mo.None[controller.ToolExecution](),
				ToolProgress:    mo.None[controller.ToolProgress](),
				ToolResult:      mo.None[controller.ToolResult](),
				ModelResponse:   mo.None[controller.ModelResponse](),
				Turn:            mo.None[controller.TurnSummary](),
				Type:            controller.AgentEventAgentEnd,
				Agent: mo.Some(controller.AgentSummary{
					Outcome:      controller.RunOutcomeFailed,
					ErrorMessage: mo.Some("failed"),
				}),
			},
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

// TestDeliveryStopsCleanlyWhenOwnerStreamEnds verifies owner teardown is not a delivery failure.
func TestDeliveryStopsCleanlyWhenOwnerStreamEnds(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
	defer active.cancel()
	close(active.streamDone)

	require.NoError(t, delivery.emit(t.Context(), active, controller.AgentEvent{}))
}

// TestDeliveryReturnsIndependentContextCancellation verifies delivery context ownership remains unchanged.
func TestDeliveryReturnsIndependentContextCancellation(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
	defer active.cancel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, delivery.emit(ctx, active, controller.AgentEvent{}), context.Canceled)
}

// TestDeliveryRejectsMismatchedRun verifies events cannot cross active correlations.
func TestDeliveryRejectsMismatchedRun(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "active")
	require.True(t, delivery.reserve(active))

	err := delivery.DeliverAgent(
		t.Context(),
		run.Event{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventAgentStart,
			RunID:      "other",
		},
	)

	require.Error(t, err)
	delivery.finish(active, nil)
}

// TestDeliveryRejectsMissingSelectedPayload verifies malformed variants do not reach Programmatic Control.
func TestDeliveryRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
	require.True(t, delivery.reserve(active))

	err := delivery.DeliverAgent(t.Context(), run.Event{
		Type:       run.EventMessageEnd,
		RunID:      "run",
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
	})

	require.ErrorContains(t, err, "requires model response")
	delivery.finish(active, nil)
}

// TestMapProgrammaticModelEventRejectsMalformedResponseContent verifies projection errors are returned.
func TestMapProgrammaticModelEventRejectsMalformedResponseContent(t *testing.T) {
	t.Parallel()

	event := run.Event{
		Type:       run.EventMessageEnd,
		RunID:      "run",
		Position:   mo.None[int](),
		Content:    mo.None[model.Content](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Message: mo.Some(model.Response{
			Content: []model.Content{{
				Kind:            model.ContentText,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			}},
			Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
			Usage: mo.None[model.Usage](), Diagnostics: nil,
		}),
		Turn:  mo.None[run.TurnSummary](),
		Agent: mo.None[run.AgentSummary](),
	}

	err := mapProgrammaticModelEvent(event, &controller.AgentEvent{})

	require.Error(t, err)
}

func newTestActiveRun(
	ctx context.Context,
	delivery *Delivery,
	correlationID string,
	runID string,
) *activeRun {
	runContext, cancel := context.WithCancel(ctx)
	return &activeRun{
		coordinator:   nil,
		userText:      "",
		streamStopped: false,
		err:           nil,
		delivery:      delivery,
		correlationID: correlationID,
		runID:         runID,
		runContext:    runContext,
		cancel:        cancel,
		events:        make(chan controller.AgentEvent),
		streamDone:    make(chan struct{}),
		done:          make(chan struct{}),
		state:         operationRunning,
	}
}
