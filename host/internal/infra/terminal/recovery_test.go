package terminal

import (
	"errors"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecoveryRetainsErrorAndRunsExactlyOnce verifies recovery remains idempotent without losing failure evidence.
func TestRecoveryRetainsErrorAndRunsExactlyOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	expectedErr := errors.New("restore failed")
	recovery := newRecovery(func() error {
		calls++
		return expectedErr
	})

	firstErr := recovery.Restore()
	secondErr := recovery.Restore()

	require.ErrorIs(t, firstErr, expectedErr)
	require.ErrorIs(t, secondErr, expectedErr)
	assert.Equal(t, 1, calls)
}

// TestResetSequenceDisablesEveryOwnedTerminalMode verifies the Host safety reset is complete.
func TestResetSequenceDisablesEveryOwnedTerminalMode(t *testing.T) {
	t.Parallel()

	sequence := resetSequence()

	for _, expected := range []string{
		ansi.ResetMode(ansi.ModeSynchronizedOutput),
		ansi.ResetModifyOtherKeys,
		ansi.KittyKeyboard(0, 1),
		ansi.ResetMode(
			ansi.ModeMouseX10,
			ansi.ModeMouseNormal,
			ansi.ModeMouseHighlight,
			ansi.ModeMouseButtonEvent,
			ansi.ModeMouseAnyEvent,
			ansi.ModeMouseExtUtf8,
			ansi.ModeMouseExtSgr,
			ansi.ModeMouseExtUrxvt,
			ansi.ModeMouseExtSgrPixel,
			ansi.ModeFocusEvent,
			ansi.ModeBracketedPaste,
			ansi.ModeUnicodeCore,
			ansi.ModeAltScreenSaveCursor,
		),
		ansi.ShowCursor,
	} {
		assert.Contains(t, sequence, expected)
	}
}
