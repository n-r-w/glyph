//go:build !integration

package run

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestCancellationBeforeFirstAppendReportsSettlement verifies a zero-history terminal transition belongs to its run.
func TestCancellationBeforeFirstAppendReportsSettlement(t *testing.T) {
	t.Parallel()
	// Arrange cancellation during start delivery, with no history append or provider call expected.
	controller := gomock.NewController(t)
	events := NewMockEventSink(controller)
	history := NewMockHistoryStore(controller)
	history.EXPECT().Snapshot().Return(nil).AnyTimes()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, event agent.Event) error {
		require.Equal(t, agent.EventAgentStart, event.Type)
		cancel()
		return ctx.Err()
	})
	events.EXPECT().
		Deliver(gomock.Any(), gomock.Any()).
		DoAndReturn(func(terminal context.Context, event agent.Event) error {
			require.NoError(t, terminal.Err())
			require.Equal(t, agent.EventAgentEnd, event.Type)
			require.Empty(t, event.Agent.MustGet().AddedHistory)
			return nil
		})
	service := New(testInstructions, NewMockModelRuntime(controller), NewMockToolRuntime(controller), events, history)

	// Act by finishing the canceled run, then attempting another begin before settlement.
	result, err := service.Run(ctx, runcontrol.Request{RunID: "canceled", UserText: "request"})
	require.ErrorIs(t, err, context.Canceled)
	rejected, beginErr := service.Run(t.Context(), runcontrol.Request{RunID: "other", UserText: "request"})

	// Assert only the finished run requires settlement, and failed begin preserves that run's state.
	require.True(t, result.SettlementRequired)
	require.Equal(t, StatusAwaitingSettlement, service.State().Status)
	require.ErrorIs(t, beginErr, ErrRunActive)
	require.False(t, rejected.SettlementRequired)
	require.Equal(t, "canceled", service.State().RunID.MustGet())
	require.NoError(t, service.Settle("canceled"))
	require.Equal(t, StatusIdle, service.State().Status)
}
