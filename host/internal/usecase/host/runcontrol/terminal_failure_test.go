//go:build !integration

package runcontrol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// TestSettledFailureOverridesEarlierOutcome verifies terminal delivery failure fails a completed or aborted run.
func TestSettledFailureOverridesEarlierOutcome(t *testing.T) {
	t.Parallel()
	for _, outcome := range []agent.RunOutcome{agent.RunOutcomeCompleted, agent.RunOutcomeAborted} {
		t.Run(map[agent.RunOutcome]string{
			agent.RunOutcomeCompleted: "completed", agent.RunOutcomeAborted: "aborted",
		}[outcome], func(t *testing.T) {
			t.Parallel()
			// Arrange a run whose work has ended before settlement delivery fails.
			controller := gomock.NewController(t)
			executor := NewMockExecutor(controller)
			delivery := NewMockSettledDelivery(controller)
			gate := NewMockGate(controller)
			released := false
			cause := errors.New("settled observer complete original cause")
			gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
			executor.EXPECT().
				Run(gomock.Any(), gomock.Any()).
				Return(Result{Outcome: outcome, SettlementRequired: true}, nil)
			executor.EXPECT().Settle(gomock.Any()).Return(nil)
			delivery.EXPECT().DeliverSettled(gomock.Any(), gomock.Any()).Return(cause)
			coordinator := NewCoordinator(executor, delivery, gate)

			// Act through the actual completion owner.
			actual, err := coordinator.Run(t.Context(), "request")

			// Assert failure overrides the earlier outcome after cleanup releases ownership.
			require.Equal(t, agent.RunOutcomeFailed, actual)
			require.ErrorIs(t, err, cause)
			require.True(t, released)
		})
	}
}
