//go:build !integration

package runcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// TestCoordinatorOrdersTerminalSettlement verifies settlement completes before reservation release, including failures.
func TestCoordinatorOrdersTerminalSettlement(t *testing.T) {
	t.Parallel()
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "failed"}[failed], func(t *testing.T) {
			t.Parallel()
			// Arrange a prepared execution with ordered Core and client settlement expectations.
			controller := gomock.NewController(t)
			executor := NewMockExecutor(controller)
			delivery := NewMockSettledDelivery(controller)
			gate := NewMockGate(controller)
			var runErr, settleErr, deliveryErr error
			outcome := agent.RunOutcomeCompleted
			if failed {
				outcome = agent.RunOutcomeFailed
				runErr = errors.New("first append failed with complete source detail")
				settleErr = errors.New("Core settlement failed with complete source detail")
				deliveryErr = errors.New("client settlement failed with complete source detail")
			}
			sequence := make([]string, 0, 3)
			gate.EXPECT().TryAcquire().Return(func() { sequence = append(sequence, "release") }, true)
			gomock.InOrder(
				executor.EXPECT().Run(gomock.Any(), Request{RunID: "prepared", UserText: "request"}).Return(
					Result{Outcome: outcome, SettlementRequired: true}, runErr),
				executor.EXPECT().Settle("prepared").DoAndReturn(func(string) error {
					sequence = append(sequence, "Core settlement")
					return settleErr
				}),
				delivery.EXPECT().
					DeliverSettled(gomock.Any(), "prepared").
					DoAndReturn(func(ctx context.Context, _ string) error {
						require.NoError(t, ctx.Err())
						sequence = append(sequence, "client and observers")
						return deliveryErr
					}),
			)
			coordinator := newCoordinator(executor, delivery, func() (string, error) { return "prepared", nil }, gate)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// Act through execution after acceptance-time identifier allocation.
			runID, err := coordinator.PrepareRun()
			require.NoError(t, err)
			actual, err := coordinator.RunPrepared(ctx, runID, "request")

			// Assert all terminal attempts finish on an uncanceled context before gate release.
			assert.Equal(t, outcome, actual)
			assert.Equal(t, []string{"Core settlement", "client and observers", "release"}, sequence)
			if failed {
				require.ErrorIs(t, err, runErr)
				require.ErrorIs(t, err, settleErr)
				require.ErrorIs(t, err, deliveryErr)
				require.ErrorContains(t, err, runErr.Error())
				require.ErrorContains(t, err, settleErr.Error())
				require.ErrorContains(t, err, deliveryErr.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestCoordinatorSkipsSettlementWhenRunNeverBegins verifies a failed begin cannot settle another run.
func TestCoordinatorSkipsSettlementWhenRunNeverBegins(t *testing.T) {
	t.Parallel()
	// Arrange execution rejection without a settlement transition.
	controller := gomock.NewController(t)
	executor := NewMockExecutor(controller)
	delivery := NewMockSettledDelivery(controller)
	gate := NewMockGate(controller)
	released := 0
	gate.EXPECT().TryAcquire().Return(func() { released++ }, true)
	beginErr := errors.New("another run is active")
	executor.EXPECT().Run(gomock.Any(), gomock.Any()).Return(Result{}, beginErr)
	coordinator := NewCoordinator(executor, delivery, gate)

	// Act through the one-shot run entry point.
	_, err := coordinator.Run(t.Context(), "request")

	// Assert the reservation releases without any Core or client settlement call.
	require.ErrorIs(t, err, beginErr)
	require.Equal(t, 1, released)
}

// TestGenerateRunIDProducesUniqueNonemptyValues verifies Host-owned run identifiers.
func TestGenerateRunIDProducesUniqueNonemptyValues(t *testing.T) {
	t.Parallel()
	// Arrange two independent allocations.
	first, err := generateRunID()
	require.NoError(t, err)
	// Act by allocating the next identifier.
	second, err := generateRunID()
	require.NoError(t, err)
	// Assert both identifiers are usable and distinct.
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second)
}
