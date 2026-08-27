package ui

import (
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestFrameConstructorsSetOnlySelectedPayload verifies every Host frame alternative.
func TestFrameConstructorsSetOnlySelectedPayload(t *testing.T) {
	t.Parallel()

	initialization := domainui.Initialization{}
	lifecycle := domainui.Lifecycle{}
	selection := domainui.ModelSelection{}
	frames := []domainui.Frame{
		initializationFrame(initialization),
		lifecycleFrame(lifecycle),
		authorizationFrame(""),
		informationFrame(""),
		errorFrame("", false),
		modelSelectionChangedFrame(selection),
	}
	expected := [][]bool{
		{true, false, false, false, false, false},
		{false, true, false, false, false, false},
		{false, false, true, false, false, false},
		{false, false, false, true, false, false},
		{false, false, false, true, true, false},
		{false, false, false, false, false, true},
	}
	for index, frame := range frames {
		actual := []bool{
			frame.Initialization.IsSome(),
			frame.Lifecycle.IsSome(),
			frame.AuthorizationURL.IsSome(),
			frame.Text.IsSome(),
			frame.RetryAuthentication.IsSome(),
			frame.ModelSelection.IsSome(),
		}
		assert.Equal(t, expected[index], actual)
	}
	assert.Equal(t, mo.Some(""), frames[2].AuthorizationURL)
	assert.Equal(t, mo.Some(""), frames[3].Text)
	assert.Equal(t, mo.Some(false), frames[4].RetryAuthentication)
}

// TestDeliveryReportsRuntimeFailure sends one safe identity-bearing error frame.
func TestDeliveryReportsRuntimeFailure(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	channel.EXPECT().Send(domainui.Frame{
		SessionEntries:      nil,
		Kind:                domainui.FrameError,
		Initialization:      mo.None[domainui.Initialization](),
		Lifecycle:           mo.None[domainui.Lifecycle](),
		AuthorizationURL:    mo.None[string](),
		Text:                mo.Some("extension crashed-plugin unavailable: extension process exited"),
		RetryAuthentication: mo.Some(false),
		ModelSelection:      mo.None[domainui.ModelSelection](),
		SessionInfo:         mo.None[session.Info](),
		Sessions:            nil,
	})

	err := NewDelivery(channel).ReportRuntimeFailure(t.Context(), tool.RuntimeFailure{
		PluginID:  "crashed-plugin",
		Condition: tool.RuntimeUnavailableProcessExited,
	})

	require.NoError(t, err)
}

// TestDeliveryMapsTypedTextLifecycle verifies typed content identity, position, and text.
func TestDeliveryMapsTypedTextLifecycle(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	delivered := make([]domainui.Frame, 0, 3)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		delivered = append(delivered, frame)
		return nil
	}).Times(3)
	service := NewDelivery(channel)

	for _, event := range []run.Event{
		{
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
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventContentStart,
			RunID:      "run",
			Position:   mo.Some(2),
		},
		{
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventTextDelta,
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
		{
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
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventContentEnd,
			RunID:      "run",
			Position:   mo.Some(2),
		},
	} {
		require.NoError(t, service.DeliverAgent(t.Context(), event))
	}

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
	assert.Equal(t, domainui.ModelContentStart, startContent.Type)
	assert.Equal(t, domainui.ModelContentTextDelta, deltaContent.Type)
	assert.Equal(t, 2, deltaContent.Position)
	assert.Equal(t, mo.Some("delta"), deltaContent.Text)
	assert.Equal(t, domainui.ModelContentEnd, endContent.Type)
}

func TestDeliveryMapsToolCallPreviewAndFinalArguments(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	delivered := make([]domainui.Frame, 0, 2)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		delivered = append(delivered, frame)
		return nil
	}).Times(2)
	service := NewDelivery(channel)
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
	require.NoError(
		t,
		service.DeliverAgent(
			t.Context(),
			run.Event{
				Content:    mo.None[model.Content](),
				Message:    mo.None[model.Response](),
				ToolCall:   mo.None[model.ToolCall](),
				Progress:   mo.None[tool.Progress](),
				ToolResult: mo.None[agent.ToolResult](),
				Turn:       mo.None[run.TurnSummary](),
				Agent:      mo.None[run.AgentSummary](),
				Type:       run.EventToolCallDelta,
				RunID:      "run",
				Position:   mo.Some(2),
				Preview:    mo.Some(preview),
			},
		),
	)
	arguments := map[string]any{"options": map[string]any{"items": []any{"first"}}}
	require.NoError(
		t,
		service.DeliverAgent(t.Context(), run.Event{
			Content:    mo.None[model.Content](),
			Message:    mo.None[model.Response](),
			Preview:    mo.None[model.ToolCallPreview](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent:      mo.None[run.AgentSummary](),
			Type:       run.EventToolCallEnd,
			RunID:      "run",
			Position:   mo.Some(2),
			ToolCall: mo.Some(model.ToolCall{
				ID:        "call-1",
				Name:      "read",
				Arguments: arguments,
			}),
		}),
	)

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

	channel := NewMockChannel(gomock.NewController(t))
	actualModel := model.ID("gpt-actual")
	var delivered domainui.Frame
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		delivered = frame
		return nil
	})
	event := run.Event{
		Position: mo.None[int](),
		Content:  mo.None[model.Content](),
		Preview:  mo.None[model.ToolCallPreview](),
		Type:     run.EventMessageEnd,
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
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
	}

	err := NewDelivery(channel).DeliverAgent(t.Context(), event)

	require.NoError(t, err)
	assert.Equal(t, domainui.FrameLifecycle, delivered.Kind)
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
	assert.Equal(t, []domainui.ModelResponseContent{
		{
			Kind: domainui.ModelContentKindReasoning,
			Text: "hidden reasoning",
		},
		{
			Kind: domainui.ModelContentKindText,
			Text: "visible text",
		},
		{
			Kind: domainui.ModelContentKindRefusal,
			Text: "cannot help",
		},
	}, mappedResponse.Content)
	assert.Equal(t, []domainui.ModelDiagnostic{{
		Code:    "recovered_output",
		Message: "safe diagnostic",
	}}, mappedResponse.Diagnostics)
	assert.True(t, lifecycle.Outcome.IsNone())
}

// TestDeliveryPreservesAgentThenSettlementOrder verifies Host settlement remains a separate final lifecycle item.
func TestDeliveryPreservesAgentThenSettlementOrder(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	frames := make([]domainui.Frame, 0, 2)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		frames = append(frames, frame)
		return nil
	}).Times(2)
	delivery := NewDelivery(channel)

	require.NoError(
		t,
		delivery.DeliverAgent(t.Context(), run.Event{
			Position:   mo.None[int](),
			Content:    mo.None[model.Content](),
			Preview:    mo.None[model.ToolCallPreview](),
			Type:       run.EventAgentEnd,
			RunID:      "run-1",
			Message:    mo.None[model.Response](),
			ToolCall:   mo.None[model.ToolCall](),
			Progress:   mo.None[tool.Progress](),
			ToolResult: mo.None[agent.ToolResult](),
			Turn:       mo.None[run.TurnSummary](),
			Agent: mo.Some(run.AgentSummary{
				Outcome:      agent.RunOutcomeCompleted,
				AddedHistory: nil,
				ErrorMessage: mo.None[string](),
			}),
		}),
	)
	require.NoError(t, delivery.DeliverSettled(t.Context(), "run-1"))

	require.Len(t, frames, 2)
	agentLifecycle, present := frames[0].Lifecycle.Get()
	require.True(t, present)
	settledLifecycle, present := frames[1].Lifecycle.Get()
	require.True(t, present)
	assert.Equal(t, domainui.LifecycleAgentEnd, agentLifecycle.Type)
	assert.Equal(t, domainui.LifecycleAgentSettled, settledLifecycle.Type)
}

// TestCloneResultContentsClonesImageBytesInsideOption verifies lifecycle frames do not share mutable image data.
func TestCloneResultContentsClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	original := []tool.ResultContent{{
		Kind: tool.ResultContentImage,
		Text: mo.None[string](),
		Image: mo.Some(tool.ResultImage{
			MediaType: "image/png",
			Data:      []byte{1, 2, 3},
		}),
	}}
	cloned := cloneResultContents(original)
	image, ok := cloned[0].Image.Get()
	require.True(t, ok)
	image.Data[0] = 9

	assert.Equal(t, byte(1), original[0].Image.OrEmpty().Data[0])
}

// TestMapUIModelEventRejectsMalformedResponseContent verifies projection errors are returned.
func TestMapUIModelEventRejectsMalformedResponseContent(t *testing.T) {
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

	err := mapUIModelEvent(event, &domainui.Lifecycle{})

	require.Error(t, err)
}

// TestDeliveryRejectsMissingSelectedPayload verifies malformed lifecycle variants do not reach the UI channel.
func TestDeliveryRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	delivery := NewDelivery(channel)
	err := delivery.DeliverAgent(t.Context(), run.Event{
		Type:       run.EventContentStart,
		RunID:      "run",
		Position:   mo.Some(0),
		Content:    mo.None[model.Content](),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[run.TurnSummary](),
		Agent:      mo.None[run.AgentSummary](),
	})

	require.ErrorContains(t, err, "requires content")
}
