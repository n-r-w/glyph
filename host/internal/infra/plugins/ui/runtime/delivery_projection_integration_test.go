//go:build integration

package runtime

import (
	"testing"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// testContentLifecycleEvent creates one text content lifecycle event.
func testContentLifecycleEvent(kind agent.EventType) agent.Event {
	return agent.Event{
		Content: mo.Some(model.Content{
			Kind:            model.ContentText,
			Text:            mo.None[string](),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		}),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
		Type:       kind,
		RunID:      "run",
		Position:   mo.Some(2),
	}
}

// TestDeliveryMapsTypedTextLifecycle verifies typed content identity, position, and text.
func TestDeliveryMapsTypedTextLifecycle(t *testing.T) {
	t.Parallel()
	// Arrange a delivery channel and start, delta, and end model content events.

	service, channel := testProgressOutput(t)
	delivered := make([]controllerui.Frame, 0, 3)
	channel.EXPECT().Progress("operation", gomock.Any()).DoAndReturn(func(_ string, frame controllerui.Frame) error {
		delivered = append(delivered, frame)
		return nil
	}).Times(3)

	// Act by delivering the ordered model content lifecycle.
	for _, event := range []agent.Event{
		testContentLifecycleEvent(agent.EventContentStart),
		{
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[agent.TurnSummary](),
			Agent:      mo.None[agent.RunSummary](),
			Type:       agent.EventTextDelta,
			RunID:      "run",
			Position:   mo.Some(2),
			Content: mo.Some(model.Content{
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
				Kind:            model.ContentText,
				Text:            mo.Some("delta"),
			}),
		},
		testContentLifecycleEvent(agent.EventContentEnd),
	} {
		require.NoError(t, service.DeliverAgent(t.Context(), event))
	}

	// Assert all three frames preserve content type, position, and delta text.
	startLifecycle, present := delivered[0].Lifecycle.Get()
	require.True(t, present)
	startContent, present := startLifecycle.ModelContent.Get()
	require.True(t, present)
	deltaLifecycle, present := delivered[1].Lifecycle.Get()
	require.True(t, present)
	deltaContent, present := deltaLifecycle.ModelContent.Get()
	require.True(t, present)
	endLifecycle, present := delivered[2].Lifecycle.Get()
	require.True(t, present)
	endContent, present := endLifecycle.ModelContent.Get()
	require.True(t, present)
	assert.Equal(t, controllerui.ModelContentStart, startContent.Type)
	assert.Equal(t, controllerui.ModelContentTextDelta, deltaContent.Type)
	assert.Equal(t, 2, deltaContent.Position)
	assert.Equal(t, mo.Some("delta"), deltaContent.Text)
	assert.Equal(t, controllerui.ModelContentEnd, endContent.Type)
}

// TestDeliveryMapsToolCallPreviewAndFinalArguments verifies delivery maps tool call preview and final arguments.
func TestDeliveryMapsToolCallPreviewAndFinalArguments(t *testing.T) {
	t.Parallel()
	// Arrange preview and final tool-call events with nested mutable arguments.

	service, channel := testProgressOutput(t)
	delivered := make([]controllerui.Frame, 0, 2)
	channel.EXPECT().Progress("operation", gomock.Any()).DoAndReturn(func(_ string, frame controllerui.Frame) error {
		delivered = append(delivered, frame)
		return nil
	}).Times(2)

	preview := model.ToolCallPreview{
		CallID:      "call-1",
		Name:        "read",
		Position:    2,
		Provisional: true,
		Fields: []model.ToolCallPreviewField{
			{
				Value:  mo.None[any](),
				Name:   "path",
				Kind:   model.ToolCallPreviewFieldPrefix,
				Prefix: mo.Some("fi"),
			},
			{
				Value:  mo.Some[any](map[string]any{"items": []any{"first"}}),
				Name:   "options",
				Kind:   model.ToolCallPreviewFieldComplete,
				Prefix: mo.None[string](),
			},
		},
	}
	// Act by delivering the preview followed by the finalized tool call.
	require.NoError(
		t,
		service.DeliverAgent(
			t.Context(),
			agent.Event{
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[agent.TurnSummary](),
				Agent:      mo.None[agent.RunSummary](),
				Type:       agent.EventToolCallDelta,
				RunID:      "run",
				Position:   mo.Some(2),
				Preview:    mo.Some(preview),
			},
		),
	)
	arguments := map[string]any{"options": map[string]any{"items": []any{"first"}}}
	require.NoError(
		t,
		service.DeliverAgent(t.Context(), agent.Event{
			Content:    mo.None[model.Content](),
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[agent.TurnSummary](),
			Agent:      mo.None[agent.RunSummary](),
			Type:       agent.EventToolCallEnd,
			RunID:      "run",
			Position:   mo.Some(2),
			ToolCall: mo.Some(model.ToolCall{
				ID:        "call-1",
				Name:      "read",
				Arguments: arguments,
			}),
		}),
	)

	// Assert both frames preserve values and clone nested argument ownership.
	previewLifecycle, present := delivered[0].Lifecycle.Get()
	require.True(t, present)
	mappedPreview, present := previewLifecycle.ToolCallPreview.Get()
	require.True(t, present)
	finalLifecycle, present := delivered[1].Lifecycle.Get()
	require.True(t, present)
	finalCall, present := finalLifecycle.FinalToolCall.Get()
	require.True(t, present)
	require.Equal(t, mo.Some("fi"), mappedPreview.Fields[0].Prefix)
	require.True(t, mappedPreview.Provisional)
	previewValue, present := mappedPreview.Fields[1].Value.Get()
	require.True(t, present)
	previewValue.(map[string]any)["items"].([]any)[0] = "changed"
	finalCall.Arguments["options"].(map[string]any)["items"].([]any)[0] = "changed"
	originalPreviewValue, present := preview.Fields[1].Value.Get()
	require.True(t, present)
	assert.Equal(t, "first", originalPreviewValue.(map[string]any)["items"].([]any)[0])
	assert.Equal(t, "first", arguments["options"].(map[string]any)["items"].([]any)[0])
}

// TestDeliveryFiltersProviderContextFromMessageEnd verifies opaque provider data cannot cross the UI boundary.
func TestDeliveryFiltersProviderContextFromMessageEnd(t *testing.T) {
	t.Parallel()
	// Arrange a finalized model response with visible content and opaque provider context.

	delivery, channel := testProgressOutput(t)
	actualModel := model.ID("gpt-actual")
	var delivered controllerui.Frame
	channel.EXPECT().Progress("operation", gomock.Any()).DoAndReturn(func(_ string, frame controllerui.Frame) error {
		delivered = frame
		return nil
	})
	event := agent.Event{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Preview:  mo.None[model.ToolCallPreview](),
		Type:     agent.EventMessageEnd,
		RunID:    "run-1",
		Message: mo.Some(model.Response{
			ErrorMessage: mo.None[string](),
			Content: []model.Content{
				{
					ToolCall: mo.None[model.ToolCall](),
					Kind:     model.ContentReasoning,
					Text:     mo.Some("hidden reasoning"),
					Final:    true,
					ProviderContext: mo.Some(
						model.ProviderContext{
							Source: model.ProviderContextSource{
								API:              "",
								Model:            "",
								CompatibilityKey: mo.None[string](),
								ProviderID:       "secret-provider",
							},
							Payload: []byte("encrypted-secret"),
						},
					),
				},
				{
					Final:           true,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentText,
					Text:            mo.Some("visible text"),
				},
				{
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
					Kind:            model.ContentRefusal,
					Text:            mo.Some("cannot help"),
					Final:           true,
				},
				{
					ProviderContext: mo.Some(model.ProviderContext{
						Source: model.ProviderContextSource{
							API: "responses", Model: "gpt-test", CompatibilityKey: mo.None[string](),
							ProviderID: "secret-provider",
						},
						Payload: []byte("opaque-only"),
					}),
					ToolCall: mo.None[model.ToolCall](),
					Kind:     model.ContentReasoning,
					Text:     mo.None[string](),
					Final:    true,
				},
			},
			Outcome: mo.Some(model.OutcomeStop),
			Provider: mo.Some(
				model.ProviderID("openai-codex"),
			),
			Model:         mo.Some(model.ID("gpt-test")),
			ResponseModel: mo.Some(actualModel),
			ResponseID:    mo.Some("resp-1"),
			Usage: mo.Some(model.Usage{
				InputTokens:       10,
				OutputTokens:      7,
				CachedInputTokens: 4,
				CacheWriteTokens:  1,
				ReasoningTokens:   3,
				TotalTokens:       17,
			}),
			Diagnostics: []model.Diagnostic{{
				Code:    "recovered_output",
				Message: "safe diagnostic",
			}},
		}),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
	}

	// Act by delivering the finalized model response to the UI channel.
	err := delivery.DeliverAgent(t.Context(), event)

	// Assert visible content survives while opaque provider context is removed.
	require.NoError(t, err)
	assert.Equal(t, controllerui.FrameLifecycle, delivered.Kind)
	lifecycle, present := delivered.Lifecycle.Get()
	require.True(t, present)
	mappedResponse, present := lifecycle.ModelResponse.Get()
	require.True(t, present)
	assert.Equal(t, "visible textcannot help", mappedResponse.Text)
	assert.NotContains(t, mappedResponse.Text, "encrypted-secret")
	assert.Equal(t, mo.Some("stop"), mappedResponse.Outcome)
	assert.Equal(t, mo.Some("openai-codex"), mappedResponse.Provider)
	assert.Equal(t, mo.Some("gpt-test"), mappedResponse.Model)
	assert.Equal(t, mo.Some("gpt-actual"), mappedResponse.ResponseModel)
	assert.Equal(t, mo.Some("resp-1"), mappedResponse.ResponseID)
	usage, present := mappedResponse.Usage.Get()
	require.True(t, present)
	assert.Equal(t, int64(17), usage.TotalTokens)
	assert.Equal(t, []controllerui.ModelResponseContent{
		{
			Kind: controllerui.ModelContentKindReasoning,
			Text: "hidden reasoning", ToolCall: mo.None[controllerui.FinalToolCall](),
		},
		{
			Kind: controllerui.ModelContentKindText,
			Text: "visible text", ToolCall: mo.None[controllerui.FinalToolCall](),
		},
		{
			Kind: controllerui.ModelContentKindRefusal,
			Text: "cannot help", ToolCall: mo.None[controllerui.FinalToolCall](),
		},
	}, mappedResponse.Content)
	assert.Equal(t, []controllerui.ModelDiagnostic{{
		Code:    "recovered_output",
		Message: "safe diagnostic",
	}}, mappedResponse.Diagnostics)
	assert.True(t, lifecycle.Outcome.IsNone())
}

// TestDeliveryLeavesSettlementToOperationTerminal verifies progress does not duplicate terminal completion.
func TestDeliveryLeavesSettlementToOperationTerminal(t *testing.T) {
	t.Parallel()
	// Arrange delivery observations for AgentEnd progress and later settlement.

	delivery, channel := testProgressOutput(t)
	frames := make([]controllerui.Frame, 0, 1)
	channel.EXPECT().Progress("operation", gomock.Any()).DoAndReturn(func(_ string, frame controllerui.Frame) error {
		frames = append(frames, frame)
		return nil
	})

	// Act by delivering AgentEnd and then invoking settlement.
	require.NoError(
		t,
		delivery.DeliverAgent(t.Context(), agent.Event{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Preview:    mo.None[model.ToolCallPreview](),
			Type:       agent.EventAgentEnd,
			RunID:      "run-1",
			Message:    mo.None[model.Response](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[agent.TurnSummary](),
			Agent: mo.Some(agent.RunSummary{
				Outcome:      agent.RunOutcomeCompleted,
				AddedHistory: nil,
				ErrorMessage: mo.None[string](),
			}),
		}),
	)
	require.NoError(t, delivery.DeliverSettled(t.Context(), "run-1"))

	// Assert AgentEnd produces one lifecycle frame and settlement adds no duplicate frame.
	require.Len(t, frames, 1)
	agentLifecycle, present := frames[0].Lifecycle.Get()
	require.True(t, present)
	assert.Equal(t, controllerui.LifecycleAgentEnd, agentLifecycle.Type)
}

// TestMapUIModelEventRejectsMalformedResponseContent verifies projection errors are returned.
func TestMapUIModelEventRejectsMalformedResponseContent(t *testing.T) {
	t.Parallel()
	// Arrange a MessageEnd event whose selected content has no required text.
	event := agent.Event{
		Type:       agent.EventMessageEnd,
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
		Turn:  mo.None[agent.TurnSummary](),
		Agent: mo.None[agent.RunSummary](),
	}

	// Act by projecting the malformed response through mapUIModelEvent.
	err := mapUIModelEvent(event, &controllerui.Lifecycle{})

	// Assert projection returns the missing-content error.
	require.Error(t, err)
}

// TestDeliveryRejectsMissingSelectedPayload verifies malformed lifecycle variants do not reach the UI channel.
func TestDeliveryRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()
	// Arrange a content-start event without its selected content payload.

	delivery, _ := testProgressOutput(t)

	// Act by delivering the malformed content-start event.
	err := delivery.DeliverAgent(t.Context(), agent.Event{
		Type:       agent.EventContentStart,
		RunID:      "run",
		Position:   mo.Some(0),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
	})

	// Assert delivery returns the payload error without sending a frame.
	require.ErrorContains(t, err, "requires content")
}
