//go:build !integration

package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeFailureMessageFormatsProcessExit verifies the process-exit message keeps its public text.
func TestRuntimeFailureMessageFormatsProcessExit(t *testing.T) {
	t.Parallel()

	// Arrange one classified extension process exit.
	failure := RuntimeFailure{
		PluginID:  "crashed-plugin",
		Condition: RuntimeUnavailableProcessExited,
	}

	// Act by formatting the runtime failure.
	message, err := failure.Message()

	// Assert the existing public text and successful result are preserved.
	require.NoError(t, err)
	assert.Equal(t, "extension crashed-plugin unavailable: extension process exited", message)
}

// TestRuntimeFailureMessageRejectsUnknownCondition verifies unknown conditions keep their complete error text.
func TestRuntimeFailureMessageRejectsUnknownCondition(t *testing.T) {
	t.Parallel()

	// Arrange one runtime failure with an unknown condition.
	failure := RuntimeFailure{
		PluginID:  "crashed-plugin",
		Condition: RuntimeUnavailableCondition(42),
	}

	// Act by formatting the runtime failure.
	message, err := failure.Message()

	// Assert no partial message hides the complete classification error.
	assert.Empty(t, message)
	require.EqualError(t, err, "unknown runtime unavailability condition 42")
}
