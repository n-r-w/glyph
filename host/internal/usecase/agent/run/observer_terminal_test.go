//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestEndObserverFailureOverridesAbort verifies an independent terminal observer failure survives an earlier abort.
func TestEndObserverFailureOverridesAbort(t *testing.T) {
	t.Parallel()
	// Arrange an aborted provider response followed by independently failed agent-end delivery.
	controller := gomock.NewController(t)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	cause := errors.New("agent end independent full failure")
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(streamResult(emptyModelResponse(model.OutcomeAborted), context.Canceled))
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventAgentEnd {
			return cause
		}
		return nil
	}).AnyTimes()
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		tools,
		events,
	)

	// Act through Core after the provider has already chosen an aborted outcome.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "end-abort", UserText: "go"})

	// Assert the independent failure remains Failed beside the original abort cause.
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, service.Settle("end-abort"))
}

// TestToolEndObserverFailureKeepsClientResult verifies a failed observer cannot suppress committed tool history delivery.
func TestToolEndObserverFailureKeepsClientResult(t *testing.T) {
	t.Parallel()
	// Arrange a completed first tool, a later forbidden tool, and a failing tool-end observation.
	controller := gomock.NewController(t)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	first := model.ToolCall{ID: "first", Name: "first", Arguments: testToolCallArguments(`{}`)}
	second := model.ToolCall{ID: "second", Name: "second", Arguments: testToolCallArguments(`{}`)}
	response := model.Response{
		Content:       []model.Content{testCallItem(first), testCallItem(second)},
		Outcome:       mo.Some(model.OutcomeToolUse),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	cause := errors.New("tool end observer full original failure")
	clientCause := errors.New("client tool history delivery full failure")
	tools.EXPECT().Tools().Return(nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(response, nil))
	tools.EXPECT().
		Execute(gomock.Any(), first, gomock.Any()).
		Return(agent.ToolResult{CallID: first.ID, ToolName: first.Name, Contents: tool.TextContents("completed effect"), IsError: false}, nil)
	resultDelivered := false
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		if event.Type == agent.EventToolExecutionEnd {
			return cause
		}
		if event.Type == agent.EventToolResult {
			resultDelivered = true
			return clientCause
		}
		return nil
	}).AnyTimes()
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		tools,
		events,
	)

	// Act through Core's run entry point.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "tool-end", UserText: "go"})

	// Assert mandatory result delivery and completed effects survive alongside both independent failures.
	require.True(t, resultDelivered)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, clientCause)
	require.Len(t, service.History(), 3)
}
