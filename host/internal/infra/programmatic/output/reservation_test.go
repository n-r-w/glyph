//go:build !integration

package output

import (
	"testing"

	"github.com/stretchr/testify/require"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestReservationRejectsCompetingRuns checks the output owner keeps one authoritative correlation.
func TestReservationRejectsCompetingRuns(t *testing.T) {
	t.Parallel()
	// Arrange an output owner before application work starts.
	output := New()
	require.True(t, output.Reserve("first-operation", "first-run"))

	// Act with a competing reservation and cleanup for a different run.
	require.False(t, output.Reserve("second-operation", "second-run"))
	output.CancelPrepared("second-run")

	// Assert only cleanup for the reserved run permits another operation.
	require.Equal(t, "first-operation", output.ActiveOperation())
	output.CancelPrepared("first-run")
	require.True(t, output.Reserve("second-operation", "second-run"))
	require.Equal(t, "second-operation", output.ActiveOperation())
}

// TestProgressUnbindingKeepsCorrelationUntilSettlement checks reporter cleanup does not own run settlement.
func TestProgressUnbindingKeepsCorrelationUntilSettlement(t *testing.T) {
	t.Parallel()
	// Arrange an admitted run and its operation reporter binding.
	output := New()
	require.True(t, output.Reserve("operation", "run"))
	unbind := output.BindProgress("run", operation.Reporter[controller.OperationProgress]{})

	// Act by releasing the reporter before the settlement callback.
	unbind()

	// Assert the settlement transition, not reporter cleanup, releases correlation.
	require.Equal(t, "operation", output.ActiveOperation())
	require.NoError(t, output.DeliverSettled(t.Context(), "run"))
	require.Empty(t, output.ActiveOperation())
}
