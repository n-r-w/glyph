//go:build !integration

package operation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFailedOutcomeRetainsAvailableResult verifies failure state, source identity, and zero-valued retained data stay independent.
func TestFailedOutcomeRetainsAvailableResult(t *testing.T) {
	t.Parallel()
	// Arrange an independent operation failure and a meaningful zero result.
	cause := errors.New("observer failed: full cause")

	// Act by constructing a failure with available operation data.
	outcome := FailedWithResult("EXTENSION_FAILED", cause, 0)

	// Assert the result remains present while terminal state and complete failure remain failed.
	result, present := outcome.Result()
	require.True(t, present)
	require.Zero(t, result)
	require.Equal(t, TerminalStateFailed, outcome.State())
	require.Equal(t, "EXTENSION_FAILED", outcome.Code())
	require.ErrorIs(t, outcome.Err(), cause)
	require.ErrorIs(t, outcome.SourceError(), cause)
}
