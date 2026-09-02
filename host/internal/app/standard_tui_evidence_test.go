//go:build !integration

package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStandardTUIEvidenceRejectsMissingBusyConfirmationAndWrongRestartCount verifies incomplete or corrupted restart
// transcripts fail validation.
func TestStandardTUIEvidenceRejectsMissingBusyConfirmationAndWrongRestartCount(t *testing.T) {
	t.Parallel()

	// Arrange complete and incomplete transcripts for busy resume and restart evidence.
	activeID := "active-id"
	complete := strings.Join([]string{
		"user: active history", "assistant: Request complete.", "user: blocked request",
		"Session ID: " + activeID, "Name: <absent>", "Sessions:",
		"  active history | 2026-08-27T00:00:00Z | 3 messages",
		"> restart session | 2026-08-27T00:00:00Z | 7 messages",
		"Selector: Up/Down navigate | Enter confirm | Escape cancel",
		"Session status: Session replacement is unavailable: another operation is active",
	}, "\n")
	require.NoError(t, validateBusyPreservation(complete, activeID))
	require.NoError(t, validateRestartRow(complete))

	preConfirmation := strings.Replace(
		complete, "Session status: Session replacement is unavailable: another operation is active", "", 1,
	)
	// Act by validating a transcript before busy-state confirmation.
	err := validateBusyPreservation(preConfirmation, activeID)

	// Assert missing confirmation and an incorrect persisted message count are rejected.
	require.EqualError(t, err, "busy redraw did not occur after the rejection")
	wrongCount := strings.Replace(complete, "7 messages", "0 messages", 1)
	err = validateRestartRow(wrongCount)
	require.EqualError(t, err, "restart selector did not show restart session with 7 messages")
}
