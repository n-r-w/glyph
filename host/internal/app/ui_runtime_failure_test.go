package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"net/http"

	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"

	hostterminal "github.com/n-r-w/glyph/host/internal/infra/terminal"
)

type runtimeFailureProcessObservation struct {
	NamingSafe        bool `json:"naming_safe"`
	FirstUserSafe     bool `json:"first_user_safe"`
	ModelSafe         bool `json:"model_safe"`
	ToolSafe          bool `json:"tool_safe"`
	ToolCompleted     bool `json:"tool_completed"`
	ResumeSafe        bool `json:"resume_safe"`
	ContentionBusy    bool `json:"contention_busy"`
	GateReleased      bool `json:"gate_released"`
	QueriesReadable   bool `json:"queries_readable"`
	IdentityPreserved bool `json:"identity_preserved"`
	Complete          bool `json:"complete"`
}

// TestRunWithPathsUIRuntimePersistenceFailurePaths verifies all runtime failures through a real UI helper process.
func TestRunWithPathsUIRuntimePersistenceFailurePaths(t *testing.T) {
	// Arrange a real Host, deterministic provider, real tool extension, and a dedicated UI helper behavior.
	if runtime.GOOS != "darwin" {
		t.Skip("runtime recovery injection uses Darwin immutable-file flags")
	}
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(
		nil,
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	), 0o600))
	uiDirectory := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "runtime-failure.json")
	writeUIExecutable(t, uiDirectory, "Runtime_Failure_UI")
	effectPath := filepath.Join(t.TempDir(), "tool-effect.txt")
	releasePath := filepath.Join(t.TempDir(), "provider-release.fifo")
	require.NoError(t, syscall.Mkfifo(releasePath, 0o600))
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "runtime-failure")
	t.Setenv(appUIRuntimeDataEnvironment, paths.Directory)
	t.Setenv(appUIRuntimeEffectEnvironment, effectPath)
	t.Setenv(appUIRuntimeReleaseEnvironment, releasePath)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = &uiRuntimeFailureTransport{
		dataDirectory: paths.Directory,
		effectPath:    effectPath,
		releasePath:   releasePath,
		requestCount:  atomic.Int32{},
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	extensionDirectory := buildToolsExecutable(t)
	command := cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: extensionDirectory},
		ExtensionDirectory: extensionDirectory, UIDirectory: uiDirectory,
		UIID: "runtime-failure-ui", SocketPath: "",
	}

	// Act by running the dedicated UI helper-process scenario.
	runErr := runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{})
	payload, readErr := os.ReadFile(tracePath)

	// Assert the helper reports complete process evidence for every approved path.
	require.NoError(t, runErr)
	require.NoError(t, readErr)
	var observation runtimeFailureProcessObservation
	require.NoError(t, json.Unmarshal(payload, &observation))
	assert.Equal(t, runtimeFailureProcessObservation{
		NamingSafe: true, FirstUserSafe: true, ModelSafe: true, ToolSafe: true,
		ToolCompleted: true, ResumeSafe: true, ContentionBusy: true, GateReleased: true,
		QueriesReadable: true, IdentityPreserved: true, Complete: true,
	}, observation)
}

// TestRunWithPathsUIRuntimeRecoveryFailureUsesPersistenceText verifies failed tail truncation is a safe runtime error.
func TestRunWithPathsUIRuntimeRecoveryFailureUsesPersistenceText(t *testing.T) {
	// Arrange the existing recovery helper, one persisted session, and an immutable interrupted-tail target.
	if runtime.GOOS != "darwin" {
		t.Skip("immutable-file recovery failure uses Darwin chflags")
	}
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(
		nil,
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	), 0o600))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Runtime_Recovery_UI")
	tracePath := filepath.Join(t.TempDir(), "runtime-recovery.json")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "session-recovery")
	command := cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "runtime-recovery-ui", SocketPath: "",
	}
	require.NoError(t, runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{}))
	partialPayload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	var partial sessionRestartObservation
	require.NoError(t, json.Unmarshal(partialPayload, &partial))
	fixtures := writeSessionRecoveryFixture(t, partial.NamedSession.StoragePath, partial.NamedSession.WorkingDirectory)
	require.NoError(t, os.Chmod(fixtures.interruptedPath, 0o600))
	immutable := exec.CommandContext(t.Context(), "/usr/bin/chflags", "uchg", fixtures.interruptedPath)
	require.NoError(t, immutable.Run())
	t.Cleanup(func() {
		clearCommand := exec.CommandContext(context.WithoutCancel(t.Context()), "/usr/bin/chflags", "nouchg", fixtures.interruptedPath)
		_ = clearCommand.Run()
	})

	// Act by asking the existing helper process to resume the interrupted immutable session.
	runErr := runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{})
	payload, readErr := os.ReadFile(tracePath)

	// Assert the helper observes exact text and the previous active identity after failed recovery.
	require.NoError(t, runErr)
	require.NoError(t, readErr)
	var observation sessionRestartObservation
	require.NoError(t, json.Unmarshal(payload, &observation))
	assert.True(t, observation.Complete)
	assert.Contains(t, observation.RuntimeFailureText, "session persistence failed")
	assert.Contains(t, observation.RuntimeFailureText, "operation not permitted")
	assert.NotEqual(t, observation.NamedSession.ID, observation.NewStartup.ID)
}

// TestRunWithPathsSessionRootFailureStopsBeforeProgrammaticControl verifies a blocked session root prevents provider and RPC startup.
//
//nolint:paralleltest // The test replaces process-global http.DefaultTransport to prove providers do not start.
func TestRunWithPathsSessionRootFailureStopsBeforeProgrammaticControl(t *testing.T) {
	// Arrange a non-directory session root, counting provider transport, and unused socket path.
	paths := testPaths(t, codexSettings(""))
	require.NoError(t, os.WriteFile(filepath.Join(paths.Directory, "sessions"), []byte("blocked"), 0o600))
	requests := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = countingFailureTransport{requests: requests}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	socketPath := filepath.Join(t.TempDir(), "glyph.sock")

	// Act by starting Programmatic Control with the blocked session root.
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeRPC, Headless: headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: "", UIID: "", SocketPath: socketPath,
	}, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert startup reports the root failure before provider traffic or socket creation.
	require.ErrorContains(t, err, "create session root")
	require.Zero(t, requests.Load())
	_, statErr := os.Stat(socketPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestRunWithPathsProjectDirectoryFailureStopsBeforeUIInitialization verifies a blocked project path prevents provider and UI startup.
func TestRunWithPathsProjectDirectoryFailureStopsBeforeUIInitialization(t *testing.T) {
	// Arrange a blocked canonical project session path, counting transport, UI executable, and trace path.
	paths := testPaths(t, codexSettings(""))
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	canonical, err := filepath.EvalSymlinks(workingDirectory)
	require.NoError(t, err)
	projectDirectoryName := sessionstore.ProjectDirectoryName(filepath.Clean(canonical))
	sessionRoot := filepath.Join(paths.Directory, "sessions")
	require.NoError(t, os.Mkdir(sessionRoot, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sessionRoot, projectDirectoryName), []byte("blocked"), 0o600))
	requests := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = countingFailureTransport{requests: requests}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Must_Not_Start_UI")
	tracePath := filepath.Join(t.TempDir(), "must-not-exist")
	t.Setenv(appUITraceEnvironment, tracePath)

	// Act by starting UI mode with the blocked project session path.
	err = runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "must-not-start-ui", SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert startup reports the project-path failure before provider traffic or UI execution.
	require.ErrorContains(t, err, "create project session directory")
	require.Zero(t, requests.Load())
	_, statErr := os.Stat(tracePath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestRunUIWithPathsTerminalSnapshotFailureStopsBeforeOpen verifies capture failure cleanup.
func TestRunUIWithPathsTerminalSnapshotFailureStopsBeforeOpen(t *testing.T) {
	// Arrange a terminal UI and a deterministic capture failure.
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Terminal_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUITerminalEnvironment, "1")
	t.Setenv(appUIBehaviorEnvironment, "snapshot")
	captureErr := errors.New("terminal snapshot unavailable")

	// Act by starting the selected UI with the failing capture dependency.
	err := runUIWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "terminal-ui",
		SocketPath:         "",
	}, func() (*hostterminal.Recovery, error) {
		return nil, captureErr
	}, &bytes.Buffer{})

	// Assert capture context survives and the selected process stops before Open.
	require.ErrorIs(t, err, captureErr)
	require.ErrorContains(t, err, "capture selected UI terminal")
	payload, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(string(payload))
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}

// TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement verifies abnormal stream authority.
func TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Crash_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "crash")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "crash-ui",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	require.ErrorContains(t, err, "receive UI command")
	trace, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(strings.Split(strings.TrimSpace(string(trace)), "\n")[0])
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}
