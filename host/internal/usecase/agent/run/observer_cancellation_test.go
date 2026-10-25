//go:build !integration

package run

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestOwningObserverCancellationAbortsRun verifies pure owning cancellation retains aborted semantics before dispatch.
func TestOwningObserverCancellationAbortsRun(t *testing.T) {
	t.Parallel()
	// Arrange cancellation while the first lifecycle event is being delivered.
	controller := gomock.NewController(t)
	events := NewMockEventSink(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events.EXPECT().
		Deliver(ctx, gomock.Any()).
		DoAndReturn(func(context.Context, agent.Event) error { cancel(); return context.Canceled })
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		require.Equal(t, agent.EventAgentEnd, event.Type)
		require.Equal(
			t,
			agent.RunOutcomeAborted,
			event.Agent.OrElse(
				agent.RunSummary{Outcome: 0, AddedHistory: nil, ErrorMessage: mo.None[string]()},
			).Outcome,
		)
		return nil
	})
	service := newTestService(
		t,
		testInstructions,
		testModelDescriptor,
		model.ReasoningChoiceHigh,
		provider,
		tools,
		events,
	)

	// Act through the actual run entry point.
	result, err := service.Run(ctx, runcontrol.Request{RunID: "owner-cancel", UserText: "request"})

	// Assert cancellation aborts without a provider call and settlement remains possible.
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, agent.RunOutcomeAborted, result.Outcome)
	require.NoError(t, service.Settle("owner-cancel"))
	require.Equal(t, StatusIdle, service.State().Status)
}
