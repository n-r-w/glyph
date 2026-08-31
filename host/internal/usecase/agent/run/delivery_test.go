//go:build !integration

package run

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// TestEndTurnPreservesRunAndDeliveryFailures verifies a failed terminal delivery retains the run cause.
func TestEndTurnPreservesRunAndDeliveryFailures(t *testing.T) {
	t.Parallel()

	// Arrange independent run and turn-delivery failures.
	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	runErr := errors.New("unique terminal run failure")
	deliveryErr := errors.New("unique turn delivery failure")
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			assert.Equal(t, EventTurnEnd, event.Type)
			return deliveryErr
		},
	)
	service := newTestService(
		t, testInstructions, testModelDescriptor, model.ReasoningChoiceHigh,
		provider, hookrunner.New(nil, nil, nil), tools, events,
	)

	// Act by ending a failed run whose turn event cannot be delivered.
	result, _, err := service.endTurn(
		t.Context(), "combined-turn-failure", model.Response{}, nil,
		agent.RunOutcomeFailed, runErr.Error(), runErr,
	)

	// Assert both independent causes remain in the returned error and terminal result.
	require.ErrorIs(t, err, runErr)
	require.ErrorIs(t, err, deliveryErr)
	assert.Contains(t, result.ErrorMessage.OrEmpty(), runErr.Error())
	assert.Contains(t, result.ErrorMessage.OrEmpty(), deliveryErr.Error())
}

// TestServiceRunEventDeliveryFailure ends the run, attempts agent_end, and starts no provider or tool work.
func TestServiceRunEventDeliveryFailure(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	deliveryErr := errors.New("Host delivery failed")
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			assert.Equal(t, EventAgentStart, event.Type)
			return deliveryErr
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event Event) error {
			assert.Equal(t, EventAgentEnd, event.Type)
			return nil
		},
	)
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

	_, err := service.Run(t.Context(), Request{RunID: "run-delivery", UserText: "hi"})

	require.ErrorIs(t, err, deliveryErr)
	assert.Equal(t, StatusAwaitingSettlement, service.State().Status)
	require.Empty(t, service.History())
	_, err = service.Run(t.Context(), Request{RunID: "blocked", UserText: "no"})
	require.ErrorIs(t, err, ErrRunActive)
}

// TestServiceRunLengthWithCalls stores error results without executing tools and continues.
func TestServiceRunLengthWithCalls(t *testing.T) {
	t.Parallel()

	provider := NewMockModelProvider(gomock.NewController(t))
	tools := NewMockToolRuntime(gomock.NewController(t))
	events := NewMockEventSink(gomock.NewController(t))
	call := model.ToolCall{ID: "length-call", Name: "read", Arguments: map[string]any{"path": "x"}}
	length := model.Response{
		Content: []model.Content{testCallItem(call)},
		Outcome: mo.Some(
			model.OutcomeLength,
		),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	stop := model.Response{
		Content:       nil,
		Outcome:       mo.Some(model.OutcomeStop),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	tools.EXPECT().Tools().Return(nil).Times(2)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(streamResult(length, nil))
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ModelRequest, update StreamHandler) error {
			require.Len(t, request.History, 3)
			assert.True(t, request.History[2].ToolResult.OrEmpty().IsError)
			return emitStream(update, stop, nil)
		},
	)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
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

	_, err := service.Run(t.Context(), Request{RunID: "run-length", UserText: "go"})

	require.NoError(t, err)
	require.Len(t, service.History(), 4)
	assert.Equal(t, "length-call", service.History()[2].ToolResult.OrEmpty().CallID)
}
