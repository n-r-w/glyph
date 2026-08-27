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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
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

func TestStandardTUIEvidenceRejectsClearedBusyStateAndWrongRestartCount(t *testing.T) {
	t.Parallel()

	activeID := "active-id"
	complete := strings.Join([]string{
		"user: active history", "assistant: Request complete.", "user: blocked request",
		"Session ID: " + activeID, "Name: <absent>", "Request: /resume|", "Sessions:",
		"  active history | 2026-08-27T00:00:00Z | 3 messages",
		"> restart session | 2026-08-27T00:00:00Z | 4 messages",
		"Selector: Up/Down navigate | Enter confirm | Escape cancel",
		"Session status: Session replacement is unavailable.",
	}, "\n")
	require.NoError(t, validateBusyPreservation(complete, activeID))
	require.NoError(t, validateRestartRow(complete))

	preConfirmation := strings.Replace(complete, "Session status: Session replacement is unavailable.", "", 1)
	err := validateBusyPreservation(preConfirmation, activeID)
	require.EqualError(t, err, "busy redraw did not occur after the rejection")
	clearedEditor := strings.Replace(complete, "Request: /resume|", "Request: |", 1)
	err = validateBusyPreservation(clearedEditor, activeID)
	require.EqualError(t, err, "busy screen did not preserve the /resume editor draft")
	wrongCount := strings.Replace(complete, "4 messages", "0 messages", 1)
	err = validateRestartRow(wrongCount)
	require.EqualError(t, err, "restart selector did not show restart session with 4 messages")
}

func validateBusyPreservation(output, activeID string) error {
	if !strings.Contains(output, "Session status: Session replacement is unavailable.") {
		return errors.New("busy redraw did not occur after the rejection")
	}
	required := []struct {
		text    string
		message string
	}{
		{text: "user: active history", message: "busy screen did not preserve prior user text"},
		{text: "assistant: Request complete.", message: "busy screen did not preserve prior model text"},
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
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "restart session") && strings.Contains(line, "4 messages") {
			return nil
		}
	}
	return errors.New("restart selector did not show restart session with 4 messages")
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

// TestStandardTUIHostSmoke verifies terminal input and rendered Host output through the real standard TUI.
func TestStandardTUIHostSmoke(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY acceptance runs on Darwin arm64")
	}

	paths := testPaths(t, restartSelectionSettings())
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(fmt.Sprintf(`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken)), 0o600))
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
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID:")
	testsupporttui.Write(t, input, "read input.txt")
	observer.WaitNext(t, "read input.txt|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")

	// Create a second active session so the stored restart target remains one complete tool turn.
	testsupporttui.Write(t, input, "/new")
	observer.WaitNext(t, "/new|")
	newCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitForOutputAfter(t, newCheckpoint)
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Name: <absent>")
	activeID := lastSessionID(observer.String())
	require.NotEmpty(t, activeID)
	testsupporttui.Write(t, input, "active history")
	observer.WaitNext(t, "active history|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")

	testsupporttui.Write(t, input, "blocked request")
	observer.WaitNext(t, "blocked request|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Running")
	// Re-render exact active identity after the checkpoint, then attempt the busy resume.
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID: "+activeID)
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	selectionCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[B")
	observer.WaitForOutputAfter(t, selectionCheckpoint)
	// No terminal action occurs between this checkpoint and the resume confirmation.
	busyCheckpoint := observer.Checkpoint()
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session status: Session replacement is unavailable.")
	require.Contains(t, observer.StringFrom(busyCheckpoint), "Session replacement is unavailable.")
	redrawCheckpoint := observer.Checkpoint()
	requestStandardTUIControl(t, controlSocketPath, 'd')
	observer.WaitNext(t, "Keys: Enter submit")
	redrawOutput := observer.StringFrom(redrawCheckpoint)
	require.NoError(t, validateBusyPreservation(redrawOutput, activeID), "%q", redrawOutput)
	testsupporttui.Write(t, input, "\x1b[27u")
	requestStandardTUIControl(t, controlSocketPath, 'r')
	observer.WaitNext(t, "Request complete.")
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
	require.NoError(t, validateRestartRow(observer.StringFrom(restartCheckpoint)))
	testsupporttui.Write(t, input, "\x1b[B")
	testsupporttui.Write(t, input, "\x1b[13u")
	// Restored tool history proves that resume replaced the empty startup transcript in stored order.
	observer.WaitNext(t, "user: read input.txt")
	observer.WaitNext(t, `[tool:status] bash (arguments) {"command":"printf tool-ok"}`)
	observer.WaitNext(t, "[tool:done] bash tool-ok")
	observer.WaitNext(t, "assistant: Request complete.")
	// /session renders every Host-confirmed lifecycle field after replacement.
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID:")
	observer.WaitNext(t, "Name: restart session")
	observer.WaitNext(t, "Working directory:")
	observer.WaitNext(t, "Storage path:")
	observer.WaitNext(t, "Created:")
	observer.WaitNext(t, "Updated:")
	testsupporttui.Write(t, input, "continue")
	observer.WaitNext(t, "continue|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	runErr := waiter.Wait(ptyContext)
	copyErr := outputWaiter.Wait(ptyContext)
	require.NoError(t, copyErr)
	require.NoError(t, runErr, observer.String())
	assert.Contains(t, observer.String(), "user: read input.txt")
	assert.Contains(t, observer.String(), `[tool:status] bash (arguments) {"command":"printf tool-ok"}`)
	assert.Contains(t, observer.String(), "[tool:done] bash tool-ok")
	assert.Contains(t, observer.String(), "assistant: Request complete.")
	assert.Contains(t, observer.String(), "Session status: Session replacement is unavailable.")
	assert.Contains(t, observer.String(), "PASS")
}

// TestStandardTUIHostSmokeInner runs two Host instances inside the pseudo-terminal owned by the outer test.
func TestStandardTUIHostSmokeInner(t *testing.T) {
	t.Parallel()
	if os.Getenv(standardTUIHostInnerEnvironment) == "" {
		return
	}

	testContext, cancel := context.WithTimeout(t.Context(), standardTUIHostTimeout-standardTUIHostJoinTimeout)
	t.Cleanup(cancel)
	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	testsupporttui.SetTerminalSize(t, terminalFile, 100, 40)

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
		delegate: deterministicCodexTransport{requestCount: requestCount, lastBody: lastBody},
		release:  providerRelease, requestCount: new(atomic.Int32),
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

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
	assert.Equal(t, int32(5), requestCount.Load())
	body, ok := lastBody.Load().([]byte)
	require.True(t, ok)
	assert.Contains(t, string(body), "read input.txt")
	assert.Contains(t, string(body), "enc-restart")
	assert.Contains(t, string(body), "call-1")
	assert.Contains(t, string(body), "tool-ok")
	assert.Contains(t, string(body), "Request complete.")
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
