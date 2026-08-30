package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStandardTUIEvidenceRejectsClearedBusyStateAndWrongRestartCount verifies incomplete or corrupted restart
// transcripts fail validation.
func TestStandardTUIEvidenceRejectsClearedBusyStateAndWrongRestartCount(t *testing.T) {
	t.Parallel()

	// Arrange complete and incomplete transcripts for busy resume and restart evidence.
	activeID := "active-id"
	complete := strings.Join([]string{
		"user: active history", "assistant: Request complete.", "user: blocked request",
		"Session ID: " + activeID, "Name: <absent>", "Request: /resume|", "Sessions:",
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

	// Assert missing confirmation and later transcript corruptions are rejected.
	require.EqualError(t, err, "busy redraw did not occur after the rejection")
	clearedEditor := strings.Replace(complete, "Request: /resume|", "Request: |", 1)
	err = validateBusyPreservation(clearedEditor, activeID)
	require.EqualError(t, err, "busy screen did not preserve the /resume editor draft")
	wrongCount := strings.Replace(complete, "7 messages", "0 messages", 1)
	err = validateRestartRow(wrongCount)
	require.EqualError(t, err, "restart selector did not show restart session with 7 messages")
}

// validateBusyPreservation verifies that a rejected resume leaves all active UI state visible.
func validateBusyPreservation(output, activeID string) error {
	if !strings.Contains(output, "Session status: Session replacement is unavailable: another operation is active") {
		return errors.New("busy redraw did not occur after the rejection")
	}
	required := []struct {
		text    string
		message string
	}{
		{text: "user: blocked request", message: "busy screen did not preserve the active user text"},
		{text: "Session ID: " + activeID, message: "busy screen did not preserve the active session ID"},
		{text: "Name: <absent>", message: "busy screen did not preserve the active session name state"},
		{text: "/resume|", message: "busy screen did not preserve the /resume editor draft"},
		{text: "Sessions:", message: "busy screen did not preserve the session selector"},
		{text: "Selector: Up/Down navigate", message: "busy screen did not preserve the open selector"},
	}
	for _, item := range required {
		if !strings.Contains(output, item.text) {
			return errors.New(item.message)
		}
	}
	if err := validateRestartRow(output); err != nil {
		return errors.New("busy screen did not preserve the restart session row")
	}
	if !strings.Contains(output, "> restart session") {
		return errors.New("busy screen did not preserve the exact selected restart session row")
	}
	return nil
}

// validateRestartRow verifies that the selector preserves the expected resumed-session history count.
func validateRestartRow(output string) error {
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "restart session") && strings.Contains(line, "7 messages") {
			return nil
		}
	}
	return errors.New("restart selector did not show restart session with 7 messages")
}
