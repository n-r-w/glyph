package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	testsupporttui "github.com/n-r-w/glyph/internal/testsupport/tui"
)

const standardTUIRuntimeInnerEnvironment = "GLYPH_STANDARD_TUI_RUNTIME_INNER"

// TestStandardTUIHostRuntimePersistenceFailures verifies every runtime persistence path through a real TUI process.
func TestStandardTUIHostRuntimePersistenceFailures(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY acceptance runs on Darwin arm64")
	}

	// Arrange persistent paths, real executables, deterministic fault transport inputs, and a PTY wrapper.
	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(nil,
		`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
		accessToken,
	), 0o600))
	uiDirectory := buildStandardTUIExecutable(t)
	extensionDirectory := buildToolsExecutable(t)
	effectPath := filepath.Join(t.TempDir(), "tool-effect.txt")
	releasePath := filepath.Join(t.TempDir(), "provider-release.fifo")
	require.NoError(t, syscall.Mkfifo(releasePath, 0o600))
	ptyContext, cancelPTY := context.WithTimeout(t.Context(), standardTUIHostTimeout)
	t.Cleanup(cancelPTY)
	wrapperContext, cancelWrapper := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(
		wrapperContext, "/usr/bin/script", "-q", "/dev/null",
		os.Args[0], "-test.run=^TestStandardTUIHostRuntimePersistenceFailuresInner$",
	)
	command.Env = append(
		os.Environ(),
		standardTUIRuntimeInnerEnvironment+"=1",
		standardTUIHostUIEnvironment+"="+uiDirectory,
		standardTUIHostExtensionEnvironment+"="+extensionDirectory,
		standardTUIHostDataEnvironment+"="+paths.Directory,
		appUIRuntimeEffectEnvironment+"="+effectPath,
		appUIRuntimeReleaseEnvironment+"="+releasePath,
		"TERM=xterm-256color",
	)
	testsupporttui.ConfigureProcessGroup(command)
	input, err := command.StdinPipe()
	require.NoError(t, err)
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	observer := testsupporttui.NewOutputObserver(ptyContext)
	command.Stderr = observer

	// Act by launching the real Host plus glyph-tui process and selecting the deterministic runtime.
	require.NoError(t, command.Start())
	waiter := testsupporttui.NewCommandWaiter(command)
	outputWaiter := testsupporttui.NewOutputWaiter(observer, output)
	testsupporttui.RegisterProcessGroupCleanup(t.Context(), t, testsupporttui.ProcessGroupCleanup{
		Cancel: cancelWrapper, Input: input, Command: command, CommandWaiter: waiter,
		OutputWaiter: outputWaiter, Timeout: standardTUIHostJoinTimeout,
	})
	observer.WaitNext(t, "Status: Idle")
	observer.WaitNext(t, "Request: |")
	testsupporttui.Write(t, input, string([]byte{16}))
	observer.WaitNext(t, "openai-codex / selected-model / low")

	// Arrange one durable baseline, then fail naming twice while preserving active identity and query access.
	testsupporttui.Write(t, input, "/name durable runtime session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Updated:")
	durablePath, err := latestRuntimeSessionPath(paths.Directory)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(durablePath, 0o400))
	t.Cleanup(func() { _ = os.Chmod(durablePath, 0o600) })
	failedNameCommand := "/name private failed name"
	testsupporttui.Write(t, input, failedNameCommand)
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "session persistence failed")
	assert.Contains(t, observer.String(), failedNameCommand+"|")
	runtimeTUIClearInput(t, input, failedNameCommand)
	require.NoError(t, os.Chmod(durablePath, 0o600))
	blockedNameCommand := "/name blocked after restore"
	testsupporttui.Write(t, input, blockedNameCommand)
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "session persistence failed")
	assert.Contains(t, observer.String(), "blocked after restore|")
	runtimeTUIClearInput(t, input, blockedNameCommand)
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID:")
	assert.Contains(t, observer.String(), "Name: durable runtime session")
	durableID := lastSessionID(observer.String())
	require.NotEmpty(t, durableID)

	// Act on a new session whose first user record cannot create a file.
	firstID := runtimeTUINewSession(t, input, observer)
	projectDirectory := filepath.Dir(durablePath)
	require.NoError(t, os.Chmod(projectDirectory, 0o500))
	firstCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "private first user")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "session persistence failed")
	require.NoError(t, os.Chmod(projectDirectory, 0o700))
	firstOutput := observer.StringFrom(firstCheckpoint)
	assert.NotContains(t, firstOutput, "assistant:")
	assert.NotContains(t, firstOutput, "[tool:")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+firstID)

	// Act on terminal model persistence failure after the durable user append.
	runtimeTUINewSession(t, input, observer)
	modelPath := runtimeTUINameSession(t, input, observer, "model runtime failure", paths.Directory)
	modelCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "private model user")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "session persistence failed")
	modelOutput := observer.StringFrom(modelCheckpoint)
	modelFailureIndex := strings.LastIndex(modelOutput, "session persistence failed")
	require.NotEqual(t, -1, modelFailureIndex)
	assert.NotContains(t, modelOutput[modelFailureIndex:], "assistant: Request complete.")
	assert.NotContains(t, modelOutput[modelFailureIndex:], "[tool:status]")
	require.NoError(t, os.Chmod(modelPath, 0o600))

	// Act on terminal tool-result failure after one real tool invocation writes its external effect.
	runtimeTUINewSession(t, input, observer)
	toolPath := runtimeTUINameSession(t, input, observer, "tool runtime failure", paths.Directory)
	toolCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "private tool user")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "session persistence failed")
	toolOutput := observer.StringFrom(toolCheckpoint)
	assert.Contains(t, toolOutput, "[tool:status] bash (started)")
	assert.NotContains(t, toolOutput, "[tool:done]")
	effect, err := os.ReadFile(effectPath)
	require.NoError(t, err)
	assert.Equal(t, "tool-effect", string(effect))
	require.NoError(t, os.Chmod(toolPath, 0o600))

	// Arrange a blocked provider request, then prove resume contention and terminal gate release.
	runtimeTUINewSession(t, input, observer)
	contentionPath := runtimeTUINameSession(t, input, observer, "contention runtime failure", paths.Directory)
	busyCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "private contention user")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "user: private contention user")
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session status: Session replacement is unavailable: another operation is active")
	busyOutput := observer.StringFrom(busyCheckpoint)
	assert.Contains(t, busyOutput, "Selector: Up/Down navigate")
	assert.Contains(t, busyOutput, "/resume|")
	assert.Contains(t, busyOutput, "private contention user")
	require.NoError(t, signalRuntimeRelease(releasePath))
	observer.WaitNext(t, "session persistence failed")
	require.NoError(t, os.Chmod(contentionPath, 0o600))
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "user: private contention user")

	// Act on immutable interrupted-tail recovery, preserve selector state, then recover only after clearing the fault.
	workingDirectory, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, writeRuntimeInterruptedSession(t.Context(), sessionInfoObservation{
		ID: "contention", Name: "contention runtime failure", WorkingDirectory: workingDirectory,
		StoragePath: contentionPath, CreatedTime: "", UpdateTime: "",
		IDPresent: true, NamePresent: true, WorkingDirectoryPresent: true,
		StoragePathPresent: true, CreatedTimePresent: false, UpdateTimePresent: false,
	}))
	resumeCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	testsupporttui.Write(t, input, "\x1b[B\x1b[B\x1b[B\x1b[B")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "session persistence failed")
	resumeOutput := observer.StringFrom(resumeCheckpoint)
	assert.Contains(t, resumeOutput, "runtime preceding |")
	assert.Contains(t, resumeOutput, "/resume|")
	interruptedPath := filepath.Join(filepath.Dir(contentionPath), "runtime-interrupted.jsonl")
	require.NoError(t, clearImmutable(t.Context(), interruptedPath))
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "user: runtime preceding")
	testsupporttui.Write(t, input, "/name recovered writable")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: recovered writable")

	// Assert clean process completion with persistence causes and without unrelated provider data.
	testsupporttui.Write(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	runErr := waiter.Wait(ptyContext)
	copyErr := outputWaiter.Wait(ptyContext)
	require.NoError(t, copyErr)
	require.NoError(t, runErr, observer.String())
	publicOutput := observer.String()
	assert.Contains(t, publicOutput, "permission denied")
	assert.Contains(t, publicOutput, "openat ")
	assert.NotContains(t, publicOutput, "provider-context")
	assert.NotContains(t, publicOutput, "extension-json")
	assert.Contains(t, publicOutput, "PASS")
}

// TestStandardTUIHostRuntimePersistenceFailuresInner runs one real Host with deterministic runtime faults.
func TestStandardTUIHostRuntimePersistenceFailuresInner(t *testing.T) {
	t.Parallel()
	if os.Getenv(standardTUIRuntimeInnerEnvironment) == "" {
		return
	}

	// Arrange the real terminal, Host paths, and the shared deterministic runtime-failure transport.
	testContext, cancel := context.WithTimeout(t.Context(), standardTUIHostTimeout-standardTUIHostJoinTimeout)
	t.Cleanup(cancel)
	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	testsupporttui.SetTerminalSize(t, terminalFile, 100, 44)
	dataDirectory := os.Getenv(standardTUIHostDataEnvironment)
	paths := persistence.Paths{
		Directory: dataDirectory, SettingsFile: filepath.Join(dataDirectory, "settings.yaml"),
		CredentialsFile: filepath.Join(dataDirectory, "credentials.json"),
		LogsDirectory:   filepath.Join(dataDirectory, "logs"), LogFile: filepath.Join(dataDirectory, "logs", "glyph.log"),
	}
	transport := &uiRuntimeFailureTransport{
		dataDirectory: dataDirectory,
		effectPath:    os.Getenv(appUIRuntimeEffectEnvironment),
		releasePath:   os.Getenv(appUIRuntimeReleaseEnvironment),
		requestCount:  atomic.Int32{},
	}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	// Act by running the real standard TUI until the parent completes every failure and recovery interaction.
	runErr := runWithPaths(testContext, paths, cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: os.Getenv(standardTUIHostExtensionEnvironment)},
		ExtensionDirectory: os.Getenv(standardTUIHostExtensionEnvironment),
		UIDirectory:        os.Getenv(standardTUIHostUIEnvironment), UIID: "glyph-tui", SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert only the three approved provider requests occurred and no dependent request followed tool-result failure.
	require.NoError(t, runErr)
	assert.Equal(t, int32(3), transport.requestCount.Load())
}

func runtimeTUIClearInput(t *testing.T, input io.Writer, value string) {
	t.Helper()
	testsupporttui.Write(t, input, strings.Repeat("\x7f", len([]rune(value))))
}

func runtimeTUINewSession(
	t *testing.T,
	input io.Writer,
	observer *testsupporttui.OutputObserver,
) string {
	t.Helper()
	testsupporttui.Write(t, input, "/new")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "\x1b[J")
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: <absent>")
	id := lastSessionID(observer.String())
	require.NotEmpty(t, id)
	return id
}

func runtimeTUINameSession(
	t *testing.T,
	input io.Writer,
	observer *testsupporttui.OutputObserver,
	name string,
	dataDirectory string,
) string {
	t.Helper()
	testsupporttui.Write(t, input, "/name "+name)
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: "+name)
	path, err := latestRuntimeSessionPath(dataDirectory)
	require.NoError(t, err)
	return path
}
