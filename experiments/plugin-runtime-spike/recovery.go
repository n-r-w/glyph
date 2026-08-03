package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// terminalRecovery is the Host safety guard for one terminal UI lifecycle.
type terminalRecovery struct {
	terminal *os.File
	state    *term.State
	restored bool
}

// captureTerminalRecovery snapshots the controlling terminal before the UI stream opens.
func captureTerminalRecovery() (*terminalRecovery, error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open terminal recovery guard: %w", err)
	}
	state, err := term.GetState(terminal.Fd())
	if err != nil {
		closeErr := terminal.Close()
		return nil, errors.Join(fmt.Errorf("capture terminal state: %w", err), closeErr)
	}
	return &terminalRecovery{terminal: terminal, state: state}, nil
}

// restore disables terminal UI modes, restores termios, and closes the guard descriptor.
func (r *terminalRecovery) restore() error {
	if r == nil || r.restored {
		return nil
	}
	r.restored = true

	_, modeErr := io.WriteString(r.terminal, terminalResetSequence())
	if modeErr != nil {
		modeErr = fmt.Errorf("reset terminal modes: %w", modeErr)
	}
	stateErr := term.Restore(r.terminal.Fd(), r.state)
	if stateErr != nil {
		stateErr = fmt.Errorf("restore terminal state: %w", stateErr)
	}
	closeErr := r.terminal.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close terminal recovery guard: %w", closeErr)
	}
	return errors.Join(modeErr, stateErr, closeErr)
}

// terminalResetSequence disables the modes that Bubble Tea can leave active after process death.
func terminalResetSequence() string {
	return ansi.ResetModeSynchronizedOutput +
		ansi.ResetModifyOtherKeys +
		ansi.KittyKeyboard(0, 1) +
		ansi.ResetModeMouseX10 +
		ansi.ResetModeMouseNormal +
		ansi.ResetModeMouseHighlight +
		ansi.ResetModeMouseButtonEvent +
		ansi.ResetModeMouseAnyEvent +
		ansi.ResetModeMouseExtUtf8 +
		ansi.ResetModeMouseExtSgr +
		ansi.ResetModeMouseExtUrxvt +
		ansi.ResetModeMouseExtSgrPixel +
		ansi.ResetModeFocusEvent +
		ansi.ResetModeBracketedPaste +
		ansi.ResetModeUnicodeCore +
		ansi.ResetModeAltScreenSaveCursor +
		ansi.SetModeTextCursorEnable +
		"\r\n"
}
