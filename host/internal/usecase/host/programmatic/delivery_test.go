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
				Final:    false,
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
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventAgentStart,
				RunID:      "run",
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventAgentStart,
			},
		},
		{
			name: "turn start",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventTurnStart,
				RunID:      "run",
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventTurnStart,
			},
		},
		{
			name: "message start",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventMessageStart,
				RunID:      "run",
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventMessageStart,
			},
		},
		{
			name: "content start",
			event: run.Event{
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventContentStart,
				RunID:      "run",
				Position:   2,
				Content: model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentReasoning,
					Text:            mo.Some(""),
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventModelContentStart,
				ModelContent: controller.ModelContent{
					Text:     "",
					Kind:     controller.ModelContentReasoning,
					Position: 2,
				},
			},
		},
		{
			name: "text delta",
			event: run.Event{
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventTextDelta,
				RunID:      "run",
				Position:   3,
				Content: model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentRefusal,
					Text:            mo.Some("no"),
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventModelTextDelta,
				ModelContent: controller.ModelContent{
					Kind:     controller.ModelContentRefusal,
					Position: 3,
					Text:     "no",
				},
			},
		},
		{
			name: "content end",
			event: run.Event{
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventContentEnd,
				RunID:      "run",
				Position:   4,
				Content: model.Content{
					Final:           false,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some(""),
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventModelContentEnd,
				ModelContent: controller.ModelContent{
					Text:     "",
					Kind:     controller.ModelContentText,
					Position: 4,
				},
			},
		},
		{
			name: "tool call start",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolCallStart,
				RunID:      "run",
				Preview: model.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []model.ToolCallPreviewField{
						{
							Prefix: "",
							Name:   "null",
							Kind:   model.ToolCallPreviewFieldComplete,
							Value:  nil,
						},
						{
							Prefix: "",
							Name:   "arg",
							Kind:   model.ToolCallPreviewFieldComplete,
							Value:  map[string]any{"nested": []any{"value"}},
						},
					},
				},
			},
			expected: controller.AgentEvent{
				CorrelationID: "",
				RunID:         "",
				ModelContent:  controller.ModelContent{},
				FinalToolCall: controller.FinalToolCall{},
				ToolExecution: controller.ToolExecution{},
				ToolProgress:  controller.ToolProgress{},
				ToolResult:    controller.ToolResult{},
				ModelResponse: controller.ModelResponse{},
				Turn:          controller.TurnSummary{},
				Agent:         controller.AgentSummary{},
				Type:          controller.AgentEventToolCallStart,
				ToolCallPreview: controller.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []controller.ToolCallPreviewField{
						{
							Prefix: "",
							Name:   "null",
							Kind:   controller.ToolCallPreviewFieldComplete,
							Value:  nil,
						},
						{
							Prefix: "",
							Name:   "arg",
							Kind:   controller.ToolCallPreviewFieldComplete,
							Value:  map[string]any{"nested": []any{"value"}},
						},
					},
				},
			},
		},
		{
			name: "tool call delta",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolCallDelta,
				RunID:      "run",
				Preview: model.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []model.ToolCallPreviewField{
						{
							Value:  nil,
							Name:   "arg",
							Kind:   model.ToolCallPreviewFieldPrefix,
							Prefix: "",
						},
					},
				},
			},
			expected: controller.AgentEvent{
				CorrelationID: "",
				RunID:         "",
				ModelContent:  controller.ModelContent{},
				FinalToolCall: controller.FinalToolCall{},
				ToolExecution: controller.ToolExecution{},
				ToolProgress:  controller.ToolProgress{},
				ToolResult:    controller.ToolResult{},
				ModelResponse: controller.ModelResponse{},
				Turn:          controller.TurnSummary{},
				Agent:         controller.AgentSummary{},
				Type:          controller.AgentEventToolCallDelta,
				ToolCallPreview: controller.ToolCallPreview{
					CallID:      "call",
					Name:        "tool",
					Position:    5,
					Provisional: true,
					Fields: []controller.ToolCallPreviewField{
						{
							Value:  nil,
							Name:   "arg",
							Kind:   controller.ToolCallPreviewFieldPrefix,
							Prefix: "",
						},
					},
				},
			},
		},
		{
			name: "tool call end",
			event: run.Event{
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolCallEnd,
				RunID:      "run",
				Position:   5,
				ToolCall: model.ToolCall{
					ID:        "call",
					Name:      "tool",
					Arguments: map[string]any{"arg": "value"},
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventToolCallEnd,
				FinalToolCall: controller.FinalToolCall{
					CallID:    "call",
					Name:      "tool",
					Position:  5,
					Arguments: map[string]any{"arg": "value"},
				},
			},
		},
		{
			name: "message end",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventMessageEnd,
				RunID:      "run",
				Message:    response,
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventMessageEnd,
				ModelResponse:   mapModelResponse(response),
			},
		},
		{
			name: "tool execution start",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolExecutionStart,
				RunID:      "run",
				ToolCall: model.ToolCall{
					Arguments: nil,
					ID:        "call",
					Name:      "tool",
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventToolExecutionStart,
				ToolExecution: controller.ToolExecution{
					CallID:   "call",
					ToolName: "tool",
				},
			},
		},
		{
			name: "tool execution update",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolExecutionUpdate,
				RunID:      "run",
				Progress: tool.Progress{
					Channel: tool.ProgressChannelStdout,
					Content: "line",
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventToolExecutionUpdate,
				ToolProgress: controller.ToolProgress{
					Channel: controller.ProgressChannelStdout,
					Content: "line",
				},
			},
		},
		{
			name: "tool execution end",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolExecutionEnd,
				RunID:      "run",
				ToolResult: toolResult,
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventToolExecutionEnd,
				ToolResult:      mapToolResult(toolResult),
			},
		},
		{
			name: "tool result",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				Turn:       run.TurnSummary{},
				Agent:      run.AgentSummary{},
				Type:       run.EventToolResult,
				RunID:      "run",
				ToolResult: toolResult,
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventToolResult,
				ToolResult:      mapToolResult(toolResult),
			},
		},
		{
			name: "turn end",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Agent:      run.AgentSummary{},
				Type:       run.EventTurnEnd,
				RunID:      "run",
				Turn: run.TurnSummary{
					Response:    response,
					ToolResults: []agent.ToolResult{toolResult},
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Agent:           controller.AgentSummary{},
				Type:            controller.AgentEventTurnEnd,
				Turn: controller.TurnSummary{
					Response:    mapModelResponse(response),
					ToolResults: []controller.ToolResult{mapToolResult(toolResult)},
				},
			},
		},
		{
			name: "agent end",
			event: run.Event{
				Position:   0,
				Content:    model.Content{},
				Message:    model.Response{},
				Preview:    model.ToolCallPreview{},
				ToolCall:   model.ToolCall{},
				Progress:   tool.Progress{},
				ToolResult: agent.ToolResult{},
				Turn:       run.TurnSummary{},
				Type:       run.EventAgentEnd,
				RunID:      "run",
				Agent: run.AgentSummary{
					AddedHistory: nil,
					Outcome:      agent.RunOutcomeFailed,
					ErrorMessage: "failed",
				},
			},
			expected: controller.AgentEvent{
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    controller.ModelContent{},
				ToolCallPreview: controller.ToolCallPreview{},
				FinalToolCall:   controller.FinalToolCall{},
				ToolExecution:   controller.ToolExecution{},
				ToolProgress:    controller.ToolProgress{},
				ToolResult:      controller.ToolResult{},
				ModelResponse:   controller.ModelResponse{},
				Turn:            controller.TurnSummary{},
				Type:            controller.AgentEventAgentEnd,
				Agent: controller.AgentSummary{
					Outcome:      controller.RunOutcomeFailed,
					ErrorMessage: "failed",
				},
			},
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

	err := delivery.DeliverAgent(
		t.Context(),
		run.Event{
			Position:   0,
			Content:    model.Content{},
			Message:    model.Response{},
			Preview:    model.ToolCallPreview{},
			ToolCall:   model.ToolCall{},
			Progress:   tool.Progress{},
			ToolResult: agent.ToolResult{},
			Turn:       run.TurnSummary{},
			Agent:      run.AgentSummary{},
			Type:       run.EventAgentStart,
			RunID:      "other",
		},
	)

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
