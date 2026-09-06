//go:build integration

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	testsupporttui "github.com/n-r-w/glyph/internal/testsupport/tui"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// externalLifecycleEnvironment enables the public fixture's agent-start observer.
	externalLifecycleEnvironment = "GLYPH_EXTERNAL_LIFECYCLE"
	// lifecycleAppendedText is the configured-model result persisted by the observer.
	lifecycleAppendedText = "Request complete."
	// lifecycleTUIInnerEnvironment identifies the PTY-owned Host subprocess.
	lifecycleTUIInnerEnvironment = "GLYPH_LIFECYCLE_TUI_INNER"
	// lifecycleTUIDataEnvironment supplies the inner Host data directory.
	lifecycleTUIDataEnvironment = "GLYPH_LIFECYCLE_TUI_DATA"
	// lifecycleTUIUIEnvironment supplies the standard TUI executable directory.
	lifecycleTUIUIEnvironment = "GLYPH_LIFECYCLE_TUI_UI"
	// lifecycleTUIExtensionEnvironment supplies the public extension directory.
	lifecycleTUIExtensionEnvironment = "GLYPH_LIFECYCLE_TUI_EXTENSION"
	// lifecycleTUITraceEnvironment records the next Agent Core provider request.
	lifecycleTUITraceEnvironment = "GLYPH_LIFECYCLE_TUI_TRACE"
	// lifecycleTUITimeout bounds the real terminal scenario.
	lifecycleTUITimeout = 30 * time.Second
)

// TestLifecycleObserverAcrossApplicationModes verifies one public extension composes nested lifecycle work everywhere.
//
//nolint:paralleltest // The scenarios replace process-global provider HTTP transport and environment.
func TestLifecycleObserverAcrossApplicationModes(t *testing.T) {
	// Arrange one unchanged public extension executable used by each Host assembly.
	directory := buildPublicExtensionFixture(t)
	t.Setenv(externalLifecycleEnvironment, "1")
	for _, scenario := range []struct {
		name string
		mode cli.Mode
	}{
		{name: "headless", mode: cli.ModeHeadless},
		{name: "ui", mode: cli.ModeUI},
		{name: "programmatic", mode: cli.ModeRPC},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			bodies := new(requestBodies)
			transport := NewMockHTTPRoundTripper(gomock.NewController(t))
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				bodies.append(body)
				return &http.Response{
					StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
					Header: make(http.Header), Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0,
					ContentLength: 0, TransferEncoding: nil, Close: false, Uncompressed: false,
					Trailer: nil, Request: nil, TLS: nil,
				}, nil
			}).Times(2)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })

			// Act through the selected application composition.
			if scenario.mode == cli.ModeRPC {
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				completeProgrammaticRequest(t, fixture, userRequest("lifecycle", "lifecycle request"))
				models := sendProgrammaticOperation(
					t,
					fixture,
					"models-after-lifecycle",
					func(operation *programmaticpb.OpenRequest) {
						programmaticRequest(operation).SetGetModels(new(programmaticpb.GetModels))
					},
				).GetModels()
				require.Equal(t, "openai-codex", models.GetActiveSelection().GetProviderId())
				require.Equal(t, "gpt-test", models.GetActiveSelection().GetModelId())
				require.Equal(t, programmaticpb.ReasoningChoice_REASONING_CHOICE_OFF,
					models.GetActiveSelection().GetReasoningChoice())
				fixture.closeOwner(t)
			} else {
				runPublicExtensionMode(t, scenario.mode, paths, directory, "lifecycle", "lifecycle request")
			}

			// Assert the observer's nested request finished before the next Agent Core model request.
			requests := bodies.snapshot()
			require.Len(t, requests, 2)
			require.NotContains(t, string(requests[0]), lifecycleAppendedText)
			require.Contains(t, string(requests[1]), lifecycleAppendedText)
		})
	}
}

// TestLifecycleObserverThroughStandardTUI verifies the same public observer through the real TUI assembly.
//
//nolint:paralleltest // The test owns a PTY subprocess and process environment.
func TestLifecycleObserverThroughStandardTUI(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("real PTY acceptance runs on Darwin arm64")
	}

	// Arrange real standard TUI and external extension executables behind one PTY-owned Host.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	uiDirectory := buildStandardTUIExecutable(t)
	extensionDirectory := buildPublicExtensionFixture(t)
	trace := filepath.Join(t.TempDir(), "next-request.json")
	ptyContext, cancelPTY := context.WithTimeout(t.Context(), lifecycleTUITimeout)
	t.Cleanup(cancelPTY)
	wrapperContext, cancelWrapper := context.WithCancel(context.WithoutCancel(t.Context()))
	command := exec.CommandContext(wrapperContext, "/usr/bin/script", "-q", "/dev/null",
		os.Args[0], "-test.run=^TestLifecycleObserverThroughStandardTUIInner$")
	command.Env = append(os.Environ(),
		lifecycleTUIInnerEnvironment+"=1",
		lifecycleTUIDataEnvironment+"="+paths.Directory,
		lifecycleTUIUIEnvironment+"="+uiDirectory,
		lifecycleTUIExtensionEnvironment+"="+extensionDirectory,
		lifecycleTUITraceEnvironment+"="+trace,
		externalLifecycleEnvironment+"=1",
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
		Cancel: cancelWrapper, Input: input, Command: command, CommandWaiter: waiter,
		OutputWaiter: outputWaiter, Timeout: standardTUIHostJoinTimeout,
	})

	// Act by submitting one request and leaving after terminal settlement.
	observer.WaitNext(t, "Status: Idle")
	testsupporttui.Write(t, input, "lifecycle request")
	testsupporttui.Write(t, input, "\x1b[13u")
	observer.WaitNext(t, "Request complete.")
	testsupporttui.Write(t, input, string([]byte{17}))
	require.NoError(t, input.Close())
	require.NoError(t, outputWaiter.Wait(ptyContext))
	require.NoError(t, waiter.Wait(ptyContext), observer.String())

	// Assert the real TUI path let the observer append before the Agent Core request.
	body, err := os.ReadFile(trace)
	require.NoError(t, err)
	require.Contains(t, string(body), lifecycleAppendedText)
	require.Contains(t, observer.String(), "PASS")
}

// TestLifecycleObserverThroughStandardTUIInner runs Host and the standard TUI inside the PTY.
func TestLifecycleObserverThroughStandardTUIInner(t *testing.T) {
	t.Parallel()
	if os.Getenv(lifecycleTUIInnerEnvironment) == "" {
		return
	}

	// Arrange terminal state, Host paths, and two deterministic provider responses.
	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	testsupporttui.SetTerminalSize(t, terminalFile, 100, 40)
	dataDirectory := os.Getenv(lifecycleTUIDataEnvironment)
	paths := persistence.Paths{
		Directory:       dataDirectory,
		SettingsFile:    filepath.Join(dataDirectory, "settings.yaml"),
		CredentialsFile: filepath.Join(dataDirectory, "credentials.json"),
		LogsDirectory: filepath.Join(
			dataDirectory,
			"logs",
		),
		LogFile: filepath.Join(dataDirectory, "logs", "glyph.log"),
	}
	calls := 0
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			return nil, readErr
		}
		calls++
		if calls == 2 {
			if writeErr := os.WriteFile(os.Getenv(lifecycleTUITraceEnvironment), body, 0o600); writeErr != nil {
				return nil, writeErr
			}
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
			Header: make(http.Header), Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0,
			ContentLength: 0, TransferEncoding: nil, Close: false, Uncompressed: false,
			Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).Times(2)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })

	// Act through the real standard TUI application composition.
	runErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{},
		ExtensionDirectory: os.Getenv(lifecycleTUIExtensionEnvironment),
		UIDirectory:        os.Getenv(lifecycleTUIUIEnvironment), UIID: "glyph-tui", SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	require.NoError(t, runErr)
	require.Equal(t, 2, calls)
}
