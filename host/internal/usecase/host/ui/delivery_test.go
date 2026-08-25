//nolint:exhaustruct // Tests set only fields relevant to each event union.
package ui

import (
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDeliveryReportsRuntimeFailure sends one safe identity-bearing error frame.
func TestDeliveryReportsRuntimeFailure(t *testing.T) {
	t.Parallel()

	channel := NewMockChannel(gomock.NewController(t))
	channel.EXPECT().Send(domainui.Frame{
		Kind:                domainui.FrameError,
		Initialization:      domainui.Initialization{},
		Lifecycle:           domainui.Lifecycle{},
		AuthorizationURL:    "",
		Text:                "extension crashed-plugin unavailable: extension process exited",
		RetryAuthentication: false,
	})

	err := NewDelivery(channel).ReportRuntimeFailure(t.Context(), tool.RuntimeFailure{
		PluginID: "crashed-plugin", Condition: tool.RuntimeUnavailableProcessExited,
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
		{Type: run.EventContentStart, RunID: "run", Position: 2},
		{Type: run.EventTextDelta, RunID: "run", Position: 2, Content: model.Content{Kind: model.ContentText, Text: mo.Some("delta")}},
		{Type: run.EventContentEnd, RunID: "run", Position: 2},
	} {
		require.NoError(t, service.DeliverAgent(t.Context(), event))
	}

	assert.Equal(t, domainui.ModelContentStart, delivered[0].Lifecycle.ModelContent.Type)
	assert.Equal(t, domainui.ModelContentTextDelta, delivered[1].Lifecycle.ModelContent.Type)
	assert.Equal(t, 2, delivered[1].Lifecycle.ModelContent.Position)
	assert.Equal(t, "delta", delivered[1].Lifecycle.ModelContent.Text)
	assert.Equal(t, domainui.ModelContentEnd, delivered[2].Lifecycle.ModelContent.Type)
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
		CallID: "call-1", Name: "read", Position: 2, Provisional: true,
		Fields: []model.ToolCallPreviewField{{Name: "path", Kind: model.ToolCallPreviewFieldPrefix, Prefix: "fi"}},
	}
	require.NoError(t, service.DeliverAgent(t.Context(), run.Event{
		Type: run.EventToolCallDelta, RunID: "run", Position: 2, Preview: preview,
	}))
	require.NoError(t, service.DeliverAgent(t.Context(), run.Event{
		Type: run.EventToolCallEnd, RunID: "run", Position: 2,
		ToolCall: model.ToolCall{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "file.txt"}},
	}))

	require.Equal(t, "fi", delivered[0].Lifecycle.ToolCallPreview.Fields[0].Prefix)
	require.True(t, delivered[0].Lifecycle.ToolCallPreview.Provisional)
	require.Equal(t, map[string]any{"path": "file.txt"}, delivered[1].Lifecycle.FinalToolCall.Arguments)
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
		Type: run.EventMessageEnd, RunID: "run-1",
		Message: model.Response{
			Content: []model.Content{
				{
					Kind: model.ContentReasoning, Text: mo.Some("hidden reasoning"), Final: true,
					ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "secret-provider"}, Payload: []byte("encrypted-secret")}),
				},
				{
					Kind: model.ContentText, Text: mo.Some("visible text"),
				},
				{Kind: model.ContentRefusal, Text: mo.Some("cannot help"), Final: true},
			},
			Outcome:  mo.Some(model.OutcomeStop),
			Provider: mo.Some(model.ProviderID("openai-codex")), Model: mo.Some(model.ID("gpt-test")), ResponseModel: mo.Some(actualModel), ResponseID: mo.Some("resp-1"),
			Usage: mo.Some(model.Usage{
				InputTokens: 10, OutputTokens: 7, CachedInputTokens: 4,
				CacheWriteTokens: 1, ReasoningTokens: 3, TotalTokens: 17,
			}),
			Diagnostics: []model.Diagnostic{{Code: "recovered_output", Message: "safe diagnostic"}},
		},
		ToolCall:   model.ToolCall{},
		Progress:   tool.Progress{},
		ToolResult: agent.ToolResult{},
		Turn:       run.TurnSummary{},
		Agent:      run.AgentSummary{},
	}

	err := NewDelivery(channel).DeliverAgent(t.Context(), event)

	require.NoError(t, err)
	assert.Equal(t, domainui.FrameLifecycle, delivered.Kind)
	assert.Equal(t, "visible textcannot help", delivered.Lifecycle.ModelResponse.Text)
	assert.NotContains(t, delivered.Lifecycle.ModelResponse.Text, "encrypted-secret")
	assert.Equal(t, "stop", delivered.Lifecycle.ModelResponse.Outcome)
	assert.Equal(t, "openai-codex", delivered.Lifecycle.ModelResponse.Provider)
	assert.Equal(t, "gpt-test", delivered.Lifecycle.ModelResponse.Model)
	require.NotNil(t, delivered.Lifecycle.ModelResponse.ResponseModel)
	assert.Equal(t, "gpt-actual", *delivered.Lifecycle.ModelResponse.ResponseModel)
	assert.Equal(t, "resp-1", delivered.Lifecycle.ModelResponse.ResponseID)
	assert.Equal(t, int64(17), delivered.Lifecycle.ModelResponse.Usage.TotalTokens)
	assert.Equal(t, []domainui.ModelResponseContent{
		{Kind: domainui.ModelContentKindReasoning, Text: "hidden reasoning"},
		{Kind: domainui.ModelContentKindText, Text: "visible text"},
		{Kind: domainui.ModelContentKindRefusal, Text: "cannot help"},
	}, delivered.Lifecycle.ModelResponse.Content)
	assert.Equal(t, []domainui.ModelDiagnostic{{
		Code: "recovered_output", Message: "safe diagnostic",
	}}, delivered.Lifecycle.ModelResponse.Diagnostics)
	assert.Empty(t, delivered.Lifecycle.Outcome)
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

	require.NoError(t, delivery.DeliverAgent(t.Context(), run.Event{
		Type: run.EventAgentEnd, RunID: "run-1",
		Message:    model.Response{},
		ToolCall:   model.ToolCall{},
		Progress:   tool.Progress{},
		ToolResult: agent.ToolResult{},
		Turn:       run.TurnSummary{},
		Agent:      run.AgentSummary{Outcome: agent.RunOutcomeCompleted, AddedHistory: nil, ErrorMessage: ""},
	}))
	require.NoError(t, delivery.DeliverSettled(t.Context(), "run-1"))

	require.Len(t, frames, 2)
	assert.Equal(t, domainui.LifecycleAgentEnd, frames[0].Lifecycle.Type)
	assert.Equal(t, domainui.LifecycleAgentSettled, frames[1].Lifecycle.Type)
}

// TestCloneResultContentsClonesImageBytesInsideOption verifies lifecycle frames do not share mutable image data.
func TestCloneResultContentsClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	original := []tool.ResultContent{{
		Kind:  tool.ResultContentImage,
		Text:  mo.None[string](),
		Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2, 3}}),
	}}
	cloned := cloneResultContents(original)
	image, ok := cloned[0].Image.Get()
	require.True(t, ok)
	image.Data[0] = 9

	assert.Equal(t, byte(1), original[0].Image.OrEmpty().Data[0])
}
