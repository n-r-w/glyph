// Package terminal provides the Host safety guard for terminal-controlling UI plugins.
package terminal

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// Recovery resets terminal modes and restores one captured termios state exactly once.
type Recovery struct {
	restoreOnce sync.Once
	restoreFunc func() error
	restoreErr  error
}

// Capture opens the controlling terminal and snapshots its current state.
func Capture() (*Recovery, error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open terminal recovery guard: %w", err)
	}
	state, err := term.GetState(terminal.Fd())
	if err != nil {
		return nil, errors.Join(fmt.Errorf("capture terminal state: %w", err), terminal.Close())
	}
	return newRecovery(func() error {
		_, modeErr := terminal.WriteString(resetSequence())
		if modeErr != nil {
			modeErr = fmt.Errorf("reset terminal modes: %w", modeErr)
		}
		stateErr := term.Restore(terminal.Fd(), state)
		if stateErr != nil {
			stateErr = fmt.Errorf("restore terminal state: %w", stateErr)
		}
		closeErr := terminal.Close()
		if closeErr != nil {
			closeErr = fmt.Errorf("close terminal recovery guard: %w", closeErr)
		}
		return errors.Join(modeErr, stateErr, closeErr)
	}), nil
}

// newRecovery creates one exactly-once recovery operation.
func newRecovery(restoreFunc func() error) *Recovery {
	return &Recovery{restoreOnce: sync.Once{}, restoreFunc: restoreFunc, restoreErr: nil}
}

// Restore attempts terminal recovery once and retains its result for every caller.
func (r *Recovery) Restore() error {
	if r == nil {
		return nil
	}
	r.restoreOnce.Do(func() {
		r.restoreErr = r.restoreFunc()
	})
	return r.restoreErr
}

// resetSequence disables terminal UI modes that can survive process termination.
func resetSequence() string {
	return ansi.ResetMode(ansi.ModeSynchronizedOutput) +
		ansi.ResetModifyOtherKeys +
		ansi.KittyKeyboard(0, 1) +
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
		) +
		ansi.ShowCursor +
		"\r\n"
}
