package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	testsupporttui "github.com/n-r-w/glyph/internal/testsupport/tui"
)

const (
	standardTUIHostInnerEnvironment     = "GLYPH_STANDARD_TUI_HOST_INNER"
	standardTUIHostUIEnvironment        = "GLYPH_STANDARD_TUI_HOST_UI_DIRECTORY"
	standardTUIHostExtensionEnvironment = "GLYPH_STANDARD_TUI_HOST_EXTENSION_DIRECTORY"
	standardTUIHostDataEnvironment      = "GLYPH_STANDARD_TUI_HOST_DATA_DIRECTORY"
	standardTUIHostControlEnvironment   = "GLYPH_STANDARD_TUI_HOST_CONTROL_SOCKET"
	standardTUIHostTimeout              = 30 * time.Second
	standardTUIHostJoinTimeout          = 5 * time.Second
)

// TestStandardTUIEvidenceRejectsClearedBusyStateAndWrongRestartCount verifies incomplete or corrupted restart transcripts fail validation.
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

func validateRestartRow(output string) error {
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "restart session") && strings.Contains(line, "7 messages") {
			return nil
		}
	}
	return errors.New("restart selector did not show restart session with 7 messages")
}

func requestStandardTUIControl(t *testing.T, socketPath string, command byte) {
	t.Helper()
	connection, err := new(net.Dialer).DialContext(t.Context(), "unix", socketPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, connection.Close()) }()
	_, err = connection.Write([]byte{command})
	require.NoError(t, err)
	response, err := io.ReadAll(connection)
	require.NoError(t, err)
	require.Equal(t, "ok", string(response))
}

func lastSessionID(output string) string {
	const prefix = "Session ID: "
	position := strings.LastIndex(output, prefix)
	if position < 0 {
		return ""
	}
	fields := strings.Fields(output[position+len(prefix):])
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// TestStandardTUIHostSmoke verifies recovery, cost states, and continuation through a real TUI restart.
func TestStandardTUIHostSmoke(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY acceptance runs on Darwin arm64")
	}

	// Arrange persistent paths, credentials, real UI and extension executables, control socket, and PTY.
	paths := testPaths(t, pricedRestartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(nil, `{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken), 0o600))
	uiDirectory := buildStandardTUIExecutable(t)
	extensionDirectory := buildToolsExecutable(t)
	controlDirectory, err := os.MkdirTemp("/tmp", "glyph-tui-control-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(controlDirectory)) })
	controlSocketPath := filepath.Join(controlDirectory, "control.sock")

	ptyContext, cancelPTY := context.WithTimeout(t.Context(), standardTUIHostTimeout)
	t.Cleanup(cancelPTY)
	// Cleanup owns wrapper cancellation because testing cancels t.Context before cleanup runs.
	wrapperContext, cancelWrapper := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(
		wrapperContext, "/usr/bin/script", "-q", "/dev/null",
		os.Args[0], "-test.run=^TestStandardTUIHostSmokeInner$",
	)
	command.Env = append(
		os.Environ(),
		standardTUIHostInnerEnvironment+"=1",
		standardTUIHostUIEnvironment+"="+uiDirectory,
		standardTUIHostExtensionEnvironment+"="+extensionDirectory,
		standardTUIHostDataEnvironment+"="+paths.Directory,
		standardTUIHostControlEnvironment+"="+controlSocketPath,
		"TERM=xterm-256color",
	)
	testsupporttui.ConfigureProcessGroup(command)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	observer := testsupporttui.NewOutputObserver(ptyContext)
	command.Stderr = observer
	// Act by driving create, busy resume, restart, explicit resume, rendering, and continuation interactions.
	require.NoError(t, command.Start())
	waiter := testsupporttui.NewCommandWaiter(command)
	outputWaiter := testsupporttui.NewOutputWaiter(observer, output)
	testsupporttui.RegisterProcessGroupCleanup(t.Context(), t, testsupporttui.ProcessGroupCleanup{
		Cancel:        cancelWrapper,
		Input:         input,
		Command:       command,
		CommandWaiter: waiter,
		OutputWaiter:  outputWaiter,
		Timeout:       standardTUIHostJoinTimeout,
	})

	observer.WaitNext(t, "Status: Idle")
	observer.WaitNext(t, "Request: |")
	testsupporttui.Write(t, input, string([]byte{16}))
	observer.WaitNext(t, "openai-codex / selected-model / low")
	testsupporttui.Write(t, input, "\x1b[Z")
	observer.WaitNext(t, "high")
	testsupporttui.Write(t, input, "/name restart session")
	observer.WaitNext(t, "/name restart session|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Updated:")
	testsupporttui.Write(t, input, "/name")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "restart session")
	emptyInitialCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID:")
	observer.WaitNext(t, "Messages: 0 user, 0 model, 0 tool results, 0 total")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	assert.NotContains(t, observer.StringFrom(emptyInitialCheckpoint), "openai-codex/selected-model:")
	restartID := lastSessionID(observer.String())
	require.NotEmpty(t, restartID)
	testsupporttui.Write(t, input, "read input.txt")
	observer.WaitNext(t, "read input.txt|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+restartID)
	observer.WaitNext(t, "Name: restart session")
	observer.WaitNext(t, "Working directory:")
	observer.WaitNext(t, "Storage path:")
	observer.WaitNext(t, "Created:")
	observer.WaitNext(t, "Updated:")
	observer.WaitNext(t, "Messages: 1 user, 2 model, 1 tool results, 4 total")
	observer.WaitNext(t, "Tool calls: 1")
	observer.WaitNext(t, "Tokens: 14 input, 8 output, 4 cache read, 2 cache write, 28 total")
	observer.WaitNext(t, "Reasoning tokens: 6, included in output")
	observer.WaitNext(t, "Estimated cost: $0.000050")
	observer.WaitNext(t, "openai-codex/selected-model: $0.000050")

	// Create a second active session so the stored restart target remains one complete tool turn.
	testsupporttui.Write(t, input, "/new")
	observer.WaitNext(t, "/new|")
	newCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitForOutputAfter(t, newCheckpoint)
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: <absent>")
	observer.WaitNext(t, "Messages: 0 user, 0 model, 0 tool results, 0 total")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	activeID := lastSessionID(observer.String())
	require.NotEmpty(t, activeID)
	appendFullContentFixtureWithUsage(t, paths, restartID, mo.Some(model.Usage{
		InputTokens: 7, OutputTokens: 4, CachedInputTokens: 2,
		CacheWriteTokens: 1, ReasoningTokens: 3, TotalTokens: 14,
	}), mo.Some(session.EstimatedCost{
		Input: 0.000007, Output: 0.000008, CacheRead: 0.000006, CacheWrite: 0.000004, Total: 0.000025,
	}))
	testsupporttui.Write(t, input, "active history")
	observer.WaitNext(t, "active history|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+activeID)
	observer.WaitNext(t, "Name: <absent>")
	observer.WaitNext(t, "Messages: 1 user, 1 model, 0 tool results, 2 total")
	observer.WaitNext(t, "Tool calls: 0")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Reasoning tokens: 0, included in output")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	observer.WaitNext(t, "openai-codex/selected-model: $0.000000")

	testsupporttui.Write(t, input, "blocked request")
	observer.WaitNext(t, "blocked request|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Running")
	// Re-render exact active identity after the checkpoint, then attempt the busy resume.
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+activeID)
	observer.WaitNext(t, "Messages: 2 user, 1 model, 0 tool results, 3 total")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	observer.WaitNext(t, "openai-codex/selected-model: $0.000000")
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	// The full-content append makes the restart session the newest selected row.
	// No terminal action occurs between this checkpoint and the resume confirmation.
	busyCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session status: Session replacement is unavailable: another operation is active")
	require.Contains(t, observer.StringFrom(busyCheckpoint), "another operation is active")
	redrawCheckpoint := observer.Checkpoint()
	requestStandardTUIControl(t, controlSocketPath, 'd')
	observer.WaitNext(t, "Keys: Enter submit")
	redrawOutput := observer.StringFrom(redrawCheckpoint)
	require.NoError(t, validateBusyPreservation(redrawOutput, activeID), "%q", redrawOutput)
	testsupporttui.Write(t, input, "\x1b[27u")
	requestStandardTUIControl(t, controlSocketPath, 'r')
	observer.WaitNext(t, "Request complete.")

	// Persist and observe an unavailable-cost session before Host reconstruction.
	testsupporttui.Write(t, input, "/new")
	observer.WaitNext(t, "/new|")
	newUnavailableCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitForOutputAfter(t, newUnavailableCheckpoint)
	testsupporttui.Write(t, input, "/name unavailable cost session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Updated:")
	testsupporttui.Write(t, input, "unavailable history")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: unavailable cost session")
	observer.WaitNext(t, "Messages: 1 user, 1 model, 0 tool results, 2 total")
	observer.WaitNext(t, "Tokens: unavailable")
	observer.WaitNext(t, "Estimated cost: unavailable")
	observer.WaitNext(t, "openai-codex/selected-model: unavailable")
	unavailableID := lastSessionID(observer.String())
	require.NotEmpty(t, unavailableID)

	// Persist and observe a separate empty session with no provider-model breakdown.
	testsupporttui.Write(t, input, "/new")
	observer.WaitNext(t, "/new|")
	newEmptyCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitForOutputAfter(t, newEmptyCheckpoint)
	testsupporttui.Write(t, input, "/name empty cost session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Updated:")
	emptyBeforeCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: empty cost session")
	observer.WaitNext(t, "Messages: 0 user, 0 model, 0 tool results, 0 total")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	assert.NotContains(t, observer.StringFrom(emptyBeforeCheckpoint), "openai-codex/selected-model:")
	emptyID := lastSessionID(observer.String())
	require.NotEmpty(t, emptyID)
	restartStoragePath, workingDirectory := findSessionStoragePath(t, paths.Directory, restartID)
	recoveryFixtures := writeSessionRecoveryFixture(t, restartStoragePath, workingDirectory)

	testsupporttui.Write(t, input, string([]byte{17}))

	// The next idle screen proves that Host reopened the same data directory with a new TUI process.
	observer.WaitNext(t, "Status: Idle")
	testsupporttui.Write(t, input, string([]byte{16}))
	observer.WaitNext(t, "openai-codex / selected-model / low")
	testsupporttui.Write(t, input, "\x1b[Z")
	observer.WaitNext(t, "high")
	testsupporttui.Write(t, input, "/name")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Usage: /name <value>")
	restartCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	restartSelector := observer.StringFrom(restartCheckpoint)
	require.NoError(t, validateRestartRow(restartSelector))
	assert.Contains(t, restartSelector, "preceding tail text")
	assert.NotContains(t, restartSelector, malformedRecoveryID)
	assert.NotContains(t, restartSelector, wrongCWDRecoveryID)
	assert.NotContains(t, restartSelector, unsupportedRecoveryID)
	testsupporttui.Write(t, input, "\x1b[13u")
	// Restored tool history proves that resume replaced the empty startup transcript in stored order.
	observer.WaitNext(t, "user: read input.txt")
	observer.WaitNext(t, `[tool:status] bash (arguments) {"command":"printf tool-ok"}`)
	observer.WaitNext(t, "[tool:done] bash tool-ok")
	observer.WaitNext(t, "assistant: Request complete.")
	observer.WaitNext(t, "user: full user[image image/png, 4 bytes]after image")
	observer.WaitNext(t, "[refusal] full refusal")
	observer.WaitNext(t, "[tool:status] bash (arguments) {\"command\":\"printf full-tool\"}")
	observer.WaitNext(t, "[info] full_notice: full diagnostic")
	observer.WaitNext(t, "[tool:done] bash full tool output[image image/png, 4 bytes]")
	testsupporttui.Write(t, input, string([]byte{20}))
	observer.WaitNext(t, "reasoning: full reasoning")
	// /session renders every Host-confirmed lifecycle field after replacement.
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID:")
	observer.WaitNext(t, "Name: restart session")
	observer.WaitNext(t, "Working directory:")
	observer.WaitNext(t, "Storage path:")
	observer.WaitNext(t, "Created:")
	observer.WaitNext(t, "Updated:")
	observer.WaitNext(t, "Messages: 2 user, 3 model, 2 tool results, 7 total")
	observer.WaitNext(t, "Tool calls: 2")
	observer.WaitNext(t, "Tokens: 21 input, 12 output, 6 cache read, 3 cache write, 42 total")
	observer.WaitNext(t, "Reasoning tokens: 9, included in output")
	observer.WaitNext(t, "Estimated cost: $0.000075")
	observer.WaitNext(t, "openai-codex/selected-model: $0.000075")

	// Resume the persisted empty session and prove it still has no breakdown.
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[B")
	emptyResumeCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitForOutputAfter(t, emptyResumeCheckpoint)
	emptyAfterCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+emptyID)
	observer.WaitNext(t, "Name: empty cost session")
	observer.WaitNext(t, "Messages: 0 user, 0 model, 0 tool results, 0 total")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	assert.NotContains(t, observer.StringFrom(emptyAfterCheckpoint), "openai-codex/selected-model:")

	// Resume the unavailable-cost session and prove absence survives reconstruction.
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[B\x1b[B")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "user: unavailable history")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+unavailableID)
	observer.WaitNext(t, "Name: unavailable cost session")
	observer.WaitNext(t, "Messages: 1 user, 1 model, 0 tool results, 2 total")
	observer.WaitNext(t, "Tokens: unavailable")
	observer.WaitNext(t, "Estimated cost: unavailable")
	observer.WaitNext(t, "openai-codex/selected-model: unavailable")

	// Resume the second stored session and prove present-zero usage also survives reconstruction.
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[B\x1b[B\x1b[B")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "user: blocked request")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+activeID)
	observer.WaitNext(t, "Name: <absent>")
	observer.WaitNext(t, "Working directory:")
	observer.WaitNext(t, "Storage path:")
	observer.WaitNext(t, "Created:")
	observer.WaitNext(t, "Updated:")
	observer.WaitNext(t, "Messages: 2 user, 2 model, 0 tool results, 4 total")
	observer.WaitNext(t, "Tool calls: 0")
	observer.WaitNext(t, "Tokens: 0 input, 0 output, 0 cache read, 0 cache write, 0 total")
	observer.WaitNext(t, "Reasoning tokens: 0, included in output")
	observer.WaitNext(t, "Estimated cost: $0.000000")
	observer.WaitNext(t, "openai-codex/selected-model: $0.000000")

	// Resume the nonzero-usage session again before continuation.
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "[tool:done] bash full tool output")
	testsupporttui.Write(t, input, "continue")
	observer.WaitNext(t, "continue|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+restartID)
	observer.WaitNext(t, "Messages: 3 user, 4 model, 2 tool results, 9 total")
	observer.WaitNext(t, "Tool calls: 2")
	observer.WaitNext(t, "Tokens: unavailable")

	// Arrange a realistic runtime truncation failure without changing the readable interrupted prefix.
	require.NoError(t, os.Chmod(recoveryFixtures.interruptedPath, 0o600))
	immutable := exec.CommandContext(t.Context(), "/usr/bin/chflags", "uchg", recoveryFixtures.interruptedPath)
	require.NoError(t, immutable.Run())
	t.Cleanup(func() {
		clearCommand := exec.CommandContext(context.WithoutCancel(t.Context()), "/usr/bin/chflags", "nouchg", recoveryFixtures.interruptedPath)
		_ = clearCommand.Run()
	})

	// Act by selecting the interrupted session while its real file rejects truncate.
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[B\x1b[B\x1b[B\x1b[B")
	failureCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")

	// Assert detailed runtime-persistence text before clearing the fault and retrying successful recovery.
	observer.WaitNext(t, "Session status:")
	assert.Contains(t, observer.StringFrom(failureCheckpoint), "session persistence failed")
	clearCommand := exec.CommandContext(t.Context(), "/usr/bin/chflags", "nouchg", recoveryFixtures.interruptedPath)
	require.NoError(t, clearCommand.Run())
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "user: preceding tail text")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+recoveryFixtures.interruptedID)
	recoveredTail, err := os.ReadFile(recoveryFixtures.interruptedPath)
	require.NoError(t, err)
	assert.True(t, bytes.HasSuffix(recoveredTail, []byte{'\n'}))
	recoveredInfo, err := os.Stat(recoveryFixtures.interruptedPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), recoveredInfo.Mode().Perm())

	testsupporttui.Write(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	runErr := waiter.Wait(ptyContext)
	copyErr := outputWaiter.Wait(ptyContext)
	require.NoError(t, copyErr)
	require.NoError(t, runErr, observer.String())
	// Assert the completed PTY transcript contains every restored public content class and successful exit.
	assert.Contains(t, observer.String(), "user: read input.txt")
	assert.Contains(t, observer.String(), `[tool:status] bash (arguments) {"command":"printf tool-ok"}`)
	assert.Contains(t, observer.String(), "[tool:done] bash tool-ok")
	assert.Contains(t, observer.String(), "assistant: Request complete.")
	assert.Contains(t, observer.String(), "user: full user[image image/png, 4 bytes]after image")
	assert.Contains(t, observer.String(), "[refusal] full refusal")
	assert.Contains(t, observer.String(), "[info] full_notice: full diagnostic")
	assert.Contains(t, observer.String(), "[tool:done] bash full tool output[image image/png, 4 bytes]")
	assert.Contains(t, observer.String(), "reasoning: full reasoning")
	assert.Contains(t, observer.String(), "Session status: Session replacement is unavailable: another operation is active")
	assert.Contains(t, observer.String(), "PASS")
}

// TestStandardTUIHostSmokeInner verifies two Host runs reuse persistent state and send complete provider history.
func TestStandardTUIHostSmokeInner(t *testing.T) {
	t.Parallel()
	if os.Getenv(standardTUIHostInnerEnvironment) == "" {
		return
	}

	// Arrange the inner Host paths, real terminal, control server, and deterministic provider transport.
	testContext, cancel := context.WithTimeout(t.Context(), standardTUIHostTimeout-standardTUIHostJoinTimeout)
	t.Cleanup(cancel)
	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	testsupporttui.SetTerminalSize(t, terminalFile, 100, 44)

	dataDirectory := os.Getenv(standardTUIHostDataEnvironment)
	paths := persistence.Paths{
		Directory:       dataDirectory,
		SettingsFile:    filepath.Join(dataDirectory, "settings.yaml"),
		CredentialsFile: filepath.Join(dataDirectory, "credentials.json"),
		LogsDirectory:   filepath.Join(dataDirectory, "logs"),
		LogFile:         filepath.Join(dataDirectory, "logs", "glyph.log"),
	}
	providerRelease := make(chan struct{})
	controlListener, err := new(net.ListenConfig).Listen(
		testContext, "unix", os.Getenv(standardTUIHostControlEnvironment),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = controlListener.Close() })
	controlDone := make(chan error, 1)
	go func() {
		controlDone <- serveStandardTUIControl(testContext, controlListener, terminalFile, providerRelease)
	}()

	requestCount := &atomic.Int32{}
	lastBody := &atomic.Value{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = &blockingStandardTUITransport{
		delegate: standardTUIUsageTransport{requestCount: requestCount, lastBody: lastBody},
		release:  providerRelease, requestCount: new(atomic.Int32),
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	// Act by running two complete Host and standard TUI process cycles.
	for range 2 {
		runErr := runWithPaths(testContext, paths, cli.Command{
			Mode: cli.ModeUI,
			Headless: headless.Command{
				UserText:           "",
				ExtensionDirectory: os.Getenv(standardTUIHostExtensionEnvironment),
			},
			ExtensionDirectory: os.Getenv(standardTUIHostExtensionEnvironment),
			UIDirectory:        os.Getenv(standardTUIHostUIEnvironment),
			UIID:               "glyph-tui",
			SocketPath:         "",
		}, &bytes.Buffer{}, &bytes.Buffer{})
		require.NoError(t, runErr)
	}
	// Assert the final provider request contains restored public and private continuation content in order.
	assert.Equal(t, int32(6), requestCount.Load())
	body, ok := lastBody.Load().([]byte)
	require.True(t, ok)
	assert.Contains(t, string(body), "read input.txt")
	assert.Contains(t, string(body), "enc-restart")
	assert.Contains(t, string(body), "call-1")
	assert.Contains(t, string(body), "tool-ok")
	assert.Contains(t, string(body), "Request complete.")
	assert.Contains(t, string(body), "full user")
	assert.Contains(t, string(body), fullContentUserImageBase64)
	assert.Contains(t, string(body), "enc-full")
	assert.Contains(t, string(body), "full refusal")
	assert.Contains(t, string(body), "full-call")
	assert.Contains(t, string(body), "full tool output")
	assert.Contains(t, string(body), fullContentToolImageBase64)
	assert.NotContains(t, string(body), "full-extension")
	assert.Contains(t, string(body), "continue")
	assert.Contains(t, string(body), `"model":"selected-model"`)
	assert.Contains(t, string(body), `"effort":"high"`)
	assert.Less(t, bytes.Index(body, []byte("read input.txt")), bytes.Index(body, []byte(`"type":"function_call"`)))
	assert.Less(t, bytes.Index(body, []byte(`"type":"function_call"`)), bytes.Index(body, []byte(`"type":"function_call_output"`)))
	assert.Less(t, bytes.Index(body, []byte(`"type":"function_call_output"`)), bytes.Index(body, []byte("Request complete.")))
	assert.Less(t, bytes.Index(body, []byte("Request complete.")), bytes.Index(body, []byte("continue")))
	require.NoError(t, <-controlDone)
}

func serveStandardTUIControl(
	ctx context.Context,
	listener net.Listener,
	terminalFile *os.File,
	providerRelease chan struct{},
) error {
	defer func() { _ = listener.Close() }()
	for range 2 {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		var command [1]byte
		_, err = io.ReadFull(connection, command[:])
		if err == nil {
			switch command[0] {
			case 'd':
				resize := exec.CommandContext(ctx, "/bin/stty", "rows", "41", "columns", "101")
				resize.Stdin = terminalFile
				_, err = resize.CombinedOutput()
			case 'r':
				close(providerRelease)
			default:
				err = errors.New("unknown standard TUI control command")
			}
		}
		if err == nil {
			_, err = connection.Write([]byte("ok"))
		}
		closeErr := connection.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type standardTUIUsageTransport struct {
	requestCount *atomic.Int32
	lastBody     *atomic.Value
}

func (transport standardTUIUsageTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	transport.lastBody.Store(body)
	requestNumber := transport.requestCount.Add(1)
	responseBody := finalResponseSSE
	if requestNumber == 1 {
		responseBody = toolResponseSSE
	}
	usage := `{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}`
	if requestNumber == 1 || requestNumber == 2 {
		usage = `{"input_tokens":10,"output_tokens":4,"total_tokens":99,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":3}}`
	}
	if requestNumber != 5 && requestNumber != 6 {
		responseBody = strings.Replace(
			responseBody,
			`"status":"completed","output":[]`,
			`"status":"completed","usage":`+usage+`,"output":[]`,
			1,
		)
	}
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(responseBody)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
		Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}, nil
}

type blockingStandardTUITransport struct {
	delegate     http.RoundTripper
	release      <-chan struct{}
	requestCount *atomic.Int32
}

func (transport *blockingStandardTUITransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.requestCount.Add(1) == 4 {
		<-transport.release
	}
	return transport.delegate.RoundTrip(request)
}

// buildStandardTUIExecutable compiles the real UI command into a discoverable test directory.
func buildStandardTUIExecutable(t *testing.T) string {
	t.Helper()
	outputDirectory := t.TempDir()
	output := filepath.Join(outputDirectory, "glyph-tui")
	command := exec.CommandContext(t.Context(), "go", "build", "-o", output, "./plugins/ui/tui/cmd/glyph-tui")
	command.Dir = repoRoot(t)
	outputBytes, err := command.CombinedOutput()
	require.NoError(t, err, string(outputBytes))
	return outputDirectory
}
