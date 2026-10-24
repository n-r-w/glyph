//go:build integration

package sessions

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestEmptyReplayRetainsMutationValidation verifies empty initialization and subsequent validation in one process.
func TestEmptyReplayRetainsMutationValidation(t *testing.T) {
	t.Parallel()
	// Arrange an empty mutation stream followed by supplied mutations targeting missing entries.
	payloads := []string{
		`{"type":"navigation","navigation":{"destinationId":"missing"}}`,
		`{"type":"label","label":{"targetId":"missing","label":"label"}}`,
	}

	// Act by initializing empty replay and applying real mutation records.
	state, err := decodeMutations(nil)

	// Assert empty state is usable and each invalid mutation remains a diagnosed failure.
	require.NoError(t, err)
	require.Empty(t, state.tree.Entries())
	require.Empty(t, state.tree.Labels())
	require.Equal(t, mo.None[string](), state.tree.ActiveLeafID())
	for _, payload := range payloads {
		_, err = decodeMutations([]byte(payload + "\n"))
		require.ErrorContains(t, err, "does not exist")
	}
	_, err = state.tree.Clone()
	require.NoError(t, err)
}
