package run

import (
	"context"
	"testing"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestServiceRunStop preserves ordered history, streaming state, events, run ID, and settlement.
func TestServiceRunStop(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	descriptor := tool.Descriptor{
		Name:                "read",
		Description:         "read",
		InputSchemaJSON:     []byte(`{}`),
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
	}
	tools.EXPECT().Tools().Return([]tool.Descriptor{descriptor})
	response := model.Response{
		Content: []model.Content{
			{
				Kind:            model.ContentText,
				Text:            mo.Some("hello"),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning,
				Text: mo.Some(""),
				ProviderContext: mo.Some(
					model.ProviderContext{
						Source: model.ProviderContextSource{
							ProviderID:       "codex",
							API:              "",
							Model:            "",
							CompatibilityKey: mo.None[string](),
						},
						Payload: []byte{1, 2, 3},
					},
				),
				Final:    true,
				ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind:            model.ContentText,
				Text:            mo.Some(" world"),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		},
		Outcome: mo.Some(
			model.OutcomeStop,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			assert.Equal(t, testInstructions, request.Instructions)
			require.Len(t, request.History, 1)
			assert.Equal(t, agent.HistoryEntryUser, request.History[0].Kind)
			assert.Equal(t, []tool.Descriptor{descriptor}, request.Tools)
			require.NoError(t, emitText(update, 0, "hello"))
			require.NoError(t, emitText(update, 2, " world"))
			return emitStream(update, response, nil)
		},
	)
	delivered := make([]Event, 0, 12)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event Event) error {
		delivered = append(delivered, event)
		return nil
	}).Times(12)
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		hookrunner.New(nil, nil, nil),
		tools,
		events,
	)

	result, err := service.Run(t.Context(), Request{RunID: "run-1", UserText: "hi"})

	require.NoError(t, err)
	assert.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	assert.True(t, result.ErrorMessage.IsNone())
	require.Len(t, service.History(), 2)
	assert.Equal(t, mo.Some(response), service.History()[1].Model)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	assert.Equal(t, mo.Some("run-1"), service.State().RunID)
	assert.True(t, service.State().PartialResponse.IsNone())
	for _, event := range delivered {
		assert.Equal(t, "run-1", event.RunID)
	}
	assert.Equal(t, []EventType{
		EventAgentStart, EventTurnStart, EventMessageStart,
		EventContentStart, EventTextDelta, EventContentEnd,
		EventContentStart, EventTextDelta, EventContentEnd,
		EventMessageEnd, EventTurnEnd, EventAgentEnd,
	}, eventTypes(delivered))
	update := delivered[4]
	expectedUpdate := newEvent(EventTextDelta, "run-1")
	expectedUpdate.Position = mo.Some(0)
	expectedUpdate.Content = mo.Some(
		model.Content{
			Kind:            model.ContentText,
			Text:            mo.Some("hello"),
			Final:           false,
			ProviderContext: mo.None[model.ProviderContext](),
			ToolCall:        mo.None[model.ToolCall](),
		},
	)
	assert.Equal(t, expectedUpdate, update)
	_, err = service.Run(t.Context(), Request{RunID: "run-2", UserText: "blocked"})
	require.ErrorIs(t, err, ErrRunActive)
	require.NoError(t, service.Settle("run-1"))
	assert.Equal(t, StatusIdle, service.State().Status)
	assert.True(t, service.State().RunID.IsNone())
	assert.True(t, service.State().PartialResponse.IsNone())
}
