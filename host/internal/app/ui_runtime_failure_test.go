//go:build integration

package app

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
)

// TestRunWithPathsSessionRootFailureStopsBeforeProgrammaticControl verifies a blocked session root prevents provider
// and RPC startup.
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

// TestRunWithPathsProjectDirectoryFailureStopsBeforeUIInitialization verifies a blocked project path prevents provider
// and UI startup.
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

// TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement verifies abnormal stream authority.
func TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement(t *testing.T) {
	// Arrange a selected UI process that exits immediately after opening its stream.
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Crash_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "crash")

	// Act by running UI mode against the crashing selected process.
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

	// Assert the crash terminates UI mode and no replacement UI is started.
	require.Error(t, err)
	require.ErrorContains(t, err, "execute UI session")
	require.ErrorContains(t, err, "error reading from server: EOF")
	trace, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(strings.Split(strings.TrimSpace(string(trace)), "\n")[0])
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}
