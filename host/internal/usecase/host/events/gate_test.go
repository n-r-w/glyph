//go:build !integration

package events

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestPrepareRunReturnsDomainBusyError verifies failed gate acquisition returns the session busy error.
func TestPrepareRunReturnsDomainBusyError(t *testing.T) {
	t.Parallel()

	// Arrange a coordinator whose operation gate rejects acquisition.
	tryAcquire := func() (func(), bool) { return nil, false }
	coordinator := newCoordinator(nil, nil, nil, func() (string, error) { return "unused", nil }, tryAcquire)

	// Act by preparing a run while the gate is owned.
	_, err := coordinator.PrepareRun()

	// Assert preparation returns the shared domain busy error.
	require.ErrorIs(t, err, session.ErrBusy)
}

// TestCancelPreparedReleasesReservationWithoutStartingAgentCore verifies repeated cancellation releases once
// without a run.
func TestCancelPreparedReleasesReservationWithoutStartingAgentCore(t *testing.T) {
	t.Parallel()

	// Arrange a prepared reservation with release and Agent Core execution counters.
	releaseCount := 0
	tryAcquire := func() (func(), bool) { return func() { releaseCount++ }, true }
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
		tryAcquire,
	)

	// Act by preparing the run and canceling its reservation twice.
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	coordinator.CancelPrepared(runID)
	coordinator.CancelPrepared(runID)

	// Assert cancellation releases once and never starts Agent Core.
	require.Equal(t, 1, releaseCount)
	require.Zero(t, executed)
}
