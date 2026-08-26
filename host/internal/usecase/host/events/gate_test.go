package events

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

func TestPrepareRunReturnsDomainBusyError(t *testing.T) {
	t.Parallel()

	gate := NewMockOperationGate(gomock.NewController(t))
	gate.EXPECT().TryAcquire().Return(nil, false)
	coordinator := newCoordinator(nil, nil, nil, func() (string, error) { return "unused", nil }, gate)

	_, err := coordinator.PrepareRun()
	require.ErrorIs(t, err, session.ErrBusy)
}

func TestCancelPreparedReleasesReservationWithoutStartingAgentCore(t *testing.T) {
	t.Parallel()

	gate := NewMockOperationGate(gomock.NewController(t))
	releaseCount := 0
	gate.EXPECT().TryAcquire().Return(func() { releaseCount++ }, true)
	executed := 0
	coordinator := newCoordinator(
		func(context.Context, run.Request) (run.Result, error) {
			executed++
			return run.Result{
				Outcome: agent.RunOutcomeCompleted, AddedHistory: nil, ErrorMessage: mo.None[string](),
			}, nil
		},
		func(string) error { return nil },
		NewDispatcher(
			func(context.Context, run.Event) error { return nil },
			func(context.Context, string) error { return nil },
		),
		func() (string, error) { return "prepared", nil },
		gate,
	)

	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	coordinator.CancelPrepared(runID)
	coordinator.CancelPrepared(runID)

	require.Equal(t, 1, releaseCount)
	require.Zero(t, executed)
}
