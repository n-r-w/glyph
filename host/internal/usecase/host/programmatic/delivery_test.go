//nolint:exhaustruct // Tests set only event payload fields used by each lifecycle kind.
package programmatic

import (
	"context"
	"testing"

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

	response := model.Response{
		Content: []model.Content{
			{Kind: model.ContentText, Text: "answer", Final: true},
			{Kind: model.ContentProviderContext, ProviderContext: model.ProviderContext{ProviderID: "provider", Payload: []byte("private")}},
		},
		Outcome: model.OutcomeStop, Provider: "provider", Model: "model",
	}
	toolResult := agent.ToolResult{
		CallID: "call", ToolName: "tool",
		Contents: []tool.ResultContent{{Kind: tool.ResultContentText, Text: "output"}},
	}
	tests := []struct {
		name     string
		event    run.Event
		expected controller.AgentEvent
	}{
		{name: "agent start", event: run.Event{Type: run.EventAgentStart, RunID: "run"}, expected: controller.AgentEvent{Type: controller.AgentEventAgentStart}},
		{name: "turn start", event: run.Event{Type: run.EventTurnStart, RunID: "run"}, expected: controller.AgentEvent{Type: controller.AgentEventTurnStart}},
		{name: "message start", event: run.Event{Type: run.EventMessageStart, RunID: "run"}, expected: controller.AgentEvent{Type: controller.AgentEventMessageStart}},
		{
			name: "content start", event: run.Event{Type: run.EventContentStart, RunID: "run", Position: 2, Content: model.Content{Kind: model.ContentReasoning}},
			expected: controller.AgentEvent{Type: controller.AgentEventModelContentStart, ModelContent: controller.ModelContent{Kind: controller.ModelContentReasoning, Position: 2}},
		},
		{
			name: "text delta", event: run.Event{Type: run.EventTextDelta, RunID: "run", Position: 3, Content: model.Content{Kind: model.ContentRefusal, Text: "no"}},
			expected: controller.AgentEvent{Type: controller.AgentEventModelTextDelta, ModelContent: controller.ModelContent{Kind: controller.ModelContentRefusal, Position: 3, Text: "no"}},
		},
		{
			name: "content end", event: run.Event{Type: run.EventContentEnd, RunID: "run", Position: 4, Content: model.Content{Kind: model.ContentText}},
			expected: controller.AgentEvent{Type: controller.AgentEventModelContentEnd, ModelContent: controller.ModelContent{Kind: controller.ModelContentText, Position: 4}},
		},
		{
			name: "tool call start", event: run.Event{Type: run.EventToolCallStart, RunID: "run", Preview: model.ToolCallPreview{
				CallID: "call", Name: "tool", Position: 5, Provisional: true,
				Fields: []model.ToolCallPreviewField{
					{Name: "null", Kind: model.ToolCallPreviewFieldComplete, Value: nil},
					{Name: "arg", Kind: model.ToolCallPreviewFieldComplete, Value: map[string]any{"nested": []any{"value"}}},
				},
			}},
			expected: controller.AgentEvent{Type: controller.AgentEventToolCallStart, ToolCallPreview: controller.ToolCallPreview{
				CallID: "call", Name: "tool", Position: 5, Provisional: true,
				Fields: []controller.ToolCallPreviewField{
					{Name: "null", Kind: controller.ToolCallPreviewFieldComplete, Value: nil},
					{Name: "arg", Kind: controller.ToolCallPreviewFieldComplete, Value: map[string]any{"nested": []any{"value"}}},
				},
			}},
		},
		{
			name: "tool call delta", event: run.Event{Type: run.EventToolCallDelta, RunID: "run", Preview: model.ToolCallPreview{
				CallID: "call", Name: "tool", Position: 5, Provisional: true,
				Fields: []model.ToolCallPreviewField{{Name: "arg", Kind: model.ToolCallPreviewFieldPrefix, Prefix: ""}},
			}},
			expected: controller.AgentEvent{Type: controller.AgentEventToolCallDelta, ToolCallPreview: controller.ToolCallPreview{
				CallID: "call", Name: "tool", Position: 5, Provisional: true,
				Fields: []controller.ToolCallPreviewField{{Name: "arg", Kind: controller.ToolCallPreviewFieldPrefix, Prefix: ""}},
			}},
		},
		{
			name: "tool call end", event: run.Event{Type: run.EventToolCallEnd, RunID: "run", Position: 5, ToolCall: model.ToolCall{ID: "call", Name: "tool", Arguments: map[string]any{"arg": "value"}}},
			expected: controller.AgentEvent{Type: controller.AgentEventToolCallEnd, FinalToolCall: controller.FinalToolCall{CallID: "call", Name: "tool", Position: 5, Arguments: map[string]any{"arg": "value"}}},
		},
		{name: "message end", event: run.Event{Type: run.EventMessageEnd, RunID: "run", Message: response}, expected: controller.AgentEvent{Type: controller.AgentEventMessageEnd, ModelResponse: mapModelResponse(response)}},
		{
			name: "tool execution start", event: run.Event{Type: run.EventToolExecutionStart, RunID: "run", ToolCall: model.ToolCall{ID: "call", Name: "tool"}},
			expected: controller.AgentEvent{Type: controller.AgentEventToolExecutionStart, ToolExecution: controller.ToolExecution{CallID: "call", ToolName: "tool"}},
		},
		{
			name: "tool execution update", event: run.Event{Type: run.EventToolExecutionUpdate, RunID: "run", Progress: tool.Progress{Channel: tool.ProgressChannelStdout, Content: "line"}},
			expected: controller.AgentEvent{Type: controller.AgentEventToolExecutionUpdate, ToolProgress: controller.ToolProgress{Channel: controller.ProgressChannelStdout, Content: "line"}},
		},
		{name: "tool execution end", event: run.Event{Type: run.EventToolExecutionEnd, RunID: "run", ToolResult: toolResult}, expected: controller.AgentEvent{Type: controller.AgentEventToolExecutionEnd, ToolResult: mapToolResult(toolResult)}},
		{name: "tool result", event: run.Event{Type: run.EventToolResult, RunID: "run", ToolResult: toolResult}, expected: controller.AgentEvent{Type: controller.AgentEventToolResult, ToolResult: mapToolResult(toolResult)}},
		{
			name: "turn end", event: run.Event{Type: run.EventTurnEnd, RunID: "run", Turn: run.TurnSummary{Response: response, ToolResults: []agent.ToolResult{toolResult}}},
			expected: controller.AgentEvent{Type: controller.AgentEventTurnEnd, Turn: controller.TurnSummary{Response: mapModelResponse(response), ToolResults: []controller.ToolResult{mapToolResult(toolResult)}}},
		},
		{
			name: "agent end", event: run.Event{Type: run.EventAgentEnd, RunID: "run", Agent: run.AgentSummary{Outcome: agent.RunOutcomeFailed, ErrorMessage: "failed"}},
			expected: controller.AgentEvent{Type: controller.AgentEventAgentEnd, Agent: controller.AgentSummary{Outcome: controller.RunOutcomeFailed, ErrorMessage: "failed"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			delivery := NewDelivery()
			active := newTestActiveRun(t.Context(), delivery, "correlation", "run")
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

// TestDeliveryRejectsMismatchedRun verifies events cannot cross active correlations.
func TestDeliveryRejectsMismatchedRun(t *testing.T) {
	t.Parallel()

	delivery := NewDelivery()
	active := newTestActiveRun(t.Context(), delivery, "correlation", "active")
	require.True(t, delivery.reserve(active))

	err := delivery.DeliverAgent(t.Context(), run.Event{Type: run.EventAgentStart, RunID: "other"})

	require.Error(t, err)
	delivery.finish(active, nil)
}

func newTestActiveRun(
	ctx context.Context,
	delivery *Delivery,
	correlationID string,
	runID string,
) *activeRun {
	runContext, cancel := context.WithCancel(ctx)
	return &activeRun{
		delivery: delivery, correlationID: correlationID, runID: runID,
		runContext: runContext, cancel: cancel,
		events: make(chan controller.AgentEvent), streamDone: make(chan struct{}),
		done: make(chan struct{}), state: operationRunning,
	}
}
