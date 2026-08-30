package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// terminalReport records the three controlling-terminal properties under test.
type terminalReport struct {
	normalRestoration bool
	resizeDelivery    bool
	crashRestoration  bool
	originalState     string
	afterNormalState  string
	afterCrashState   string
}

// passed reports whether every terminal property matches the approved process contract.
func (r terminalReport) passed() bool {
	return r.normalRestoration && r.resizeDelivery && r.crashRestoration
}

// runTerminalChecks observes terminal state before and after normal and hard UI termination.
func runTerminalChecks(ctx context.Context, executable string) (report terminalReport, returnErr error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return terminalReport{}, fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, terminal.Close())
	}()

	originalState, err := terminalState(ctx, terminal)
	if err != nil {
		return terminalReport{}, err
	}
	report.originalState = originalState
	originalRows, originalColumns, err := terminalSizeValue(ctx, terminal)
	if err != nil {
		return terminalReport{}, err
	}
	defer func() {
		returnErr = errors.Join(
			returnErr,
			restoreTerminal(ctx, terminal, originalState, originalRows, originalColumns),
		)
	}()

	if err := runNormalTerminalSession(ctx, executable); err != nil {
		return terminalReport{}, err
	}
	afterNormal, err := terminalState(ctx, terminal)
	if err != nil {
		return terminalReport{}, err
	}
	report.afterNormalState = afterNormal
	report.normalRestoration = terminalStatesEquivalent(afterNormal, originalState)

	resized, err := runCrashingTerminalSession(
		ctx,
		executable,
		terminal,
		originalRows+1,
		originalColumns+1,
	)
	if err != nil {
		return terminalReport{}, err
	}
	report.resizeDelivery = resized
	afterCrash, err := terminalState(ctx, terminal)
	if err != nil {
		return terminalReport{}, err
	}
	report.afterCrashState = afterCrash
	report.crashRestoration = terminalStatesEquivalent(afterCrash, originalState)
	return report, nil
}

// runNormalTerminalSession proves graceful stream completion and Bubble Tea restoration.
func runNormalTerminalSession(ctx context.Context, executable string) (returnErr error) {
	runtime, err := startUI(ctx, executable, true)
	if err != nil {
		return err
	}
	if !runtime.usesTerminal {
		runtime.close()
		return fmt.Errorf("start terminal UI: capability is false")
	}
	recovery, err := captureTerminalRecovery()
	if err != nil {
		runtime.close()
		return err
	}
	defer func() {
		runtime.close()
		returnErr = errors.Join(returnErr, recovery.restore())
	}()

	stream, err := runtime.client.open(ctx)
	if err != nil {
		return err
	}
	if err := stream.expectEvent(uiEventReady, ""); err != nil {
		return err
	}
	if err := stream.expectEvent(uiEventResized, ""); err != nil {
		return err
	}
	if err := stream.send(uiCommandQuit); err != nil {
		return err
	}
	if err := stream.expectEvent(uiEventExited, ""); err != nil {
		return err
	}
	if _, err := stream.recv(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("complete normal UI stream: expected EOF, got %w", err)
	}
	return nil
}

// runCrashingTerminalSession proves resize delivery and observes state after hard process exit.
func runCrashingTerminalSession(
	ctx context.Context,
	executable string,
	terminal *os.File,
	rows int,
	columns int,
) (resized bool, returnErr error) {
	runtime, err := startUI(ctx, executable, true)
	if err != nil {
		return false, err
	}
	if !runtime.usesTerminal {
		runtime.close()
		return false, fmt.Errorf("start terminal UI: capability is false")
	}
	recovery, err := captureTerminalRecovery()
	if err != nil {
		runtime.close()
		return false, err
	}
	defer func() {
		runtime.close()
		returnErr = errors.Join(returnErr, recovery.restore())
	}()

	stream, err := runtime.client.open(ctx)
	if err != nil {
		return false, err
	}
	if err := stream.expectEvent(uiEventReady, ""); err != nil {
		return false, err
	}
	if err := stream.expectEvent(uiEventResized, ""); err != nil {
		return false, err
	}

	if err := setTerminalSize(ctx, terminal, rows, columns); err != nil {
		return false, err
	}
	processID, err := runtime.processID()
	if err != nil {
		return false, err
	}
	process, err := os.FindProcess(processID)
	if err != nil {
		return false, fmt.Errorf("find UI process %d: %w", processID, err)
	}
	if err := process.Signal(syscall.SIGWINCH); err != nil {
		return false, fmt.Errorf("signal UI resize: %w", err)
	}

	expectedSize := fmt.Sprintf("%dx%d", columns, rows)
	if err := stream.expectEvent(uiEventResized, expectedSize); err != nil {
		return false, err
	}
	if err := stream.send(uiCommandCrash); err != nil {
		return false, err
	}
	if err := stream.waitForFailure(); err != nil {
		return false, err
	}
	if err := waitForExit(ctx, runtime.exited); err != nil {
		return false, err
	}
	return true, nil
}

// waitForFailure drains pending events until hard process exit terminates the stream.
func (stream *uiStream) waitForFailure() error {
	for {
		if _, err := stream.recv(); err != nil {
			return nil
		}
	}
}

// expectEvent validates event ordering and optional event text.
func (stream *uiStream) expectEvent(expected uiEvent, expectedText string) error {
	message, err := stream.recv()
	if err != nil {
		return fmt.Errorf("receive UI event %d: %w", expected, err)
	}
	if message.event != expected {
		return fmt.Errorf("receive UI event: got %d, want %d", message.event, expected)
	}
	if expectedText != "" && message.text != expectedText {
		return fmt.Errorf("receive UI event text: got %q, want %q", message.text, expectedText)
	}
	return nil
}

// terminalStatesEquivalent compares functional state while ignoring macOS's transient PENDIN flag.
func terminalStatesEquivalent(left, right string) bool {
	return normalizeTerminalState(left) == normalizeTerminalState(right)
}

// normalizeTerminalState clears PENDIN because the kernel sets it after a successful restore.
func normalizeTerminalState(state string) string {
	parts := strings.Split(state, ":")
	for index, part := range parts {
		if !strings.HasPrefix(part, "lflag=") {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(part, "lflag="), 16, 64)
		if err != nil {
			return state
		}
		parts[index] = "lflag=" + strconv.FormatUint(value&^uint64(syscall.PENDIN), 16)
	}
	return strings.Join(parts, ":")
}

// terminalState returns the encoded termios state for exact before-and-after comparison.
func terminalState(ctx context.Context, terminal *os.File) (string, error) {
	command := exec.CommandContext(ctx, "stty", "-g")
	command.Stdin = terminal
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read terminal state: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// terminalSizeValue returns controlling-terminal rows and columns.
func terminalSizeValue(ctx context.Context, terminal *os.File) (int, int, error) {
	command := exec.CommandContext(ctx, "stty", "size")
	command.Stdin = terminal
	output, err := command.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("read terminal size: %w", err)
	}
	parts := strings.Fields(string(output))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("read terminal size: unexpected output %q", output)
	}
	rows, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("parse terminal rows %q: %w", parts[0], err)
	}
	columns, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("parse terminal columns %q: %w", parts[1], err)
	}
	return rows, columns, nil
}

// setTerminalSize changes the controlling-terminal window dimensions.
func setTerminalSize(ctx context.Context, terminal *os.File, rows, columns int) error {
	command := exec.CommandContext(
		ctx,
		"stty",
		"rows",
		strconv.Itoa(rows),
		"cols",
		strconv.Itoa(columns),
	)
	command.Stdin = terminal
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("set terminal size: %w: %s", err, output)
	}
	return nil
}

// restoreTerminal restores termios state and dimensions even when the observation fails.
func restoreTerminal(
	ctx context.Context,
	terminal *os.File,
	state string,
	rows int,
	columns int,
) error {
	stateCommand := exec.CommandContext(ctx, "stty", state)
	stateCommand.Stdin = terminal
	stateOutput, stateErr := stateCommand.CombinedOutput()
	if stateErr != nil {
		stateErr = fmt.Errorf("restore terminal state: %w: %s", stateErr, stateOutput)
	}
	return errors.Join(stateErr, setTerminalSize(ctx, terminal, rows, columns))
}
