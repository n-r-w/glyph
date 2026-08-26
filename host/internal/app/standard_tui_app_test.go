package app

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	standardTUIHostTimeout              = 30 * time.Second
	standardTUIHostJoinTimeout          = 5 * time.Second
)

// TestStandardTUIHostSmoke verifies terminal input and rendered Host output through the real standard TUI.
func TestStandardTUIHostSmoke(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY acceptance runs on Darwin arm64")
	}

	paths := testPaths(t, codexSettings(""))
	accessToken := semanticAccessToken(t, "account")
	require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(fmt.Sprintf(`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`, accessToken)), 0o600))
	uiDirectory := buildStandardTUIExecutable(t)
	extensionDirectory := buildToolsExecutable(t)

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
	testsupporttui.Write(t, input, "/name restart session")
	observer.WaitNext(t, "/name restart session|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Updated:")
	testsupporttui.Write(t, input, "/name")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "restart session")
	testsupporttui.Write(t, input, "read input.txt")
	observer.WaitNext(t, "read input.txt|")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, string([]byte{17}))

	// The next idle screen proves that Host reopened the same data directory with a new TUI process.
	observer.WaitNext(t, "Status: Idle")
	testsupporttui.Write(t, input, "/name")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Usage: /name <value>")
	testsupporttui.Write(t, input, "/resume")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Sessions:")
	observer.WaitNext(t, "restart session")
	testsupporttui.Write(t, input, "\x1b[13u")
	// Clearing all selector and information rows proves that resume replaced the empty startup transcript.
	observer.WaitNext(t, "\x1b[4M")
	// /session renders every Host-confirmed lifecycle field after replacement.
	testsupporttui.Write(t, input, "/session")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Session ID:")
	observer.WaitNext(t, "Name: restart session")
	observer.WaitNext(t, "Working directory:")
	observer.WaitNext(t, "Storage path:")
	observer.WaitNext(t, "Created:")
	observer.WaitNext(t, "Updated:")
	testsupporttui.Write(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	runErr := waiter.Wait(ptyContext)
	copyErr := outputWaiter.Wait(ptyContext)
	require.NoError(t, copyErr)
	require.NoError(t, runErr, observer.String())
	assert.Contains(t, observer.String(), "user: read input.txt")
	assert.Contains(t, observer.String(), "assistant: Request complete.")
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
	requestCount := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = deterministicCodexTransport{requestCount: requestCount}
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
	assert.Equal(t, int32(2), requestCount.Load())
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
