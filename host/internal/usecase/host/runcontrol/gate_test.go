//go:build !integration

package runcontrol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestPrepareRunReturnsDomainBusyError verifies rejected admission preserves the session busy category.
func TestPrepareRunReturnsDomainBusyError(t *testing.T) {
	t.Parallel()
	// Arrange an occupied gate with no execution dependencies.
	gate := NewMockGate(gomock.NewController(t))
	gate.EXPECT().TryAcquire().Return(nil, false)
	coordinator := NewCoordinator(nil, nil, gate)
	// Act by preparing while another owner holds the gate.
	_, err := coordinator.PrepareRun()
	// Assert rejection occurs before execution.
	require.ErrorIs(t, err, session.ErrBusy)
}

// TestCancelPreparedReleasesReservationWithoutStartingAgentCore verifies repeated cleanup takes the reservation once.
func TestCancelPreparedReleasesReservationWithoutStartingAgentCore(t *testing.T) {
	t.Parallel()
	// Arrange one acquired reservation without an execution expectation.
	controller := gomock.NewController(t)
	gate := NewMockGate(controller)
	executor := NewMockExecutor(controller)
	released := 0
	gate.EXPECT().TryAcquire().Return(func() { released++ }, true)
	coordinator := NewCoordinator(executor, nil, gate)
	runID, err := coordinator.PrepareRun()
	require.NoError(t, err)
	// Act by canceling twice and attempting to execute the canceled reservation.
	coordinator.CancelPrepared(runID)
	coordinator.CancelPrepared(runID)
	_, err = coordinator.RunPrepared(t.Context(), runID, "request")
	// Assert no execution starts and release occurs exactly once.
	require.Equal(t, 1, released)
	require.ErrorContains(t, err, "run is not prepared")
}

// TestPrepareRunReleasesReservationAfterIdentifierFailure verifies allocation errors do not occupy admission.
func TestPrepareRunReleasesReservationAfterIdentifierFailure(t *testing.T) {
	t.Parallel()
	// Arrange an acquired gate and a failed identifier source.
	gate := NewMockGate(gomock.NewController(t))
	released := 0
	gate.EXPECT().TryAcquire().Return(func() { released++ }, true)
	cause := errors.New("randomness source failed")
	coordinator := newCoordinator(nil, nil, func() (string, error) { return "", cause }, gate)
	// Act by preparing a run without an identifier.
	_, err := coordinator.PrepareRun()
	// Assert the full cause survives and admission releases once.
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
	require.Equal(t, 1, released)
}
