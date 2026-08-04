package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"google.golang.org/grpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

const (
	appUIHelperEnvironment   = "GLYPH_APP_UI_HELPER"
	appUITraceEnvironment    = "GLYPH_APP_UI_TRACE"
	appUITerminalEnvironment = "GLYPH_APP_UI_TERMINAL"
	appUIBehaviorEnvironment = "GLYPH_APP_UI_BEHAVIOR"
	appUIPTYInnerEnvironment = "GLYPH_APP_PTY_INNER"
)

// appUIService records initialization and terminates through one quit command.
type appUIService struct {
	uipb.UnimplementedUIServiceServer
}

// TestUIPluginHelperProcess serves the fake UI when this test binary is a child process.
func TestUIPluginHelperProcess(t *testing.T) {
	t.Parallel()

	if os.Getenv(appUIHelperEnvironment) == "serve" {
		uisdk.Serve(&appUIService{
			UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		})
	}
}

// GetCapabilities declares a non-terminal fake UI for application composition tests.
func (*appUIService) GetCapabilities(
	_ context.Context,
	_ *uipb.GetCapabilitiesRequest,
) (*uipb.GetCapabilitiesResponse, error) {
	controlsTerminal := os.Getenv(appUITerminalEnvironment) == "1"
	if os.Getenv(appUIBehaviorEnvironment) == "snapshot" {
		_ = os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(strconv.Itoa(os.Getpid())), 0o600)
	}
	return &uipb.GetCapabilitiesResponse{ControlsTerminal: controlsTerminal}, nil
}

// Open records the first frame and sends the authoritative quit command.
func (*appUIService) Open(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	frame, err := stream.Recv()
	if err != nil {
		return err
	}
	initialization := frame.GetInitialization()
	startupText := make([]string, 0, len(initialization.GetStartupContent()))
	for _, content := range initialization.GetStartupContent() {
		startupText = append(startupText, content.GetText())
	}
	trace := fmt.Sprintf(
		"%d\n%s\n%s\n",
		os.Getpid(), initialization.GetSelectedUiId(), strings.Join(startupText, "\n"),
	)
	if err := os.WriteFile(os.Getenv(appUITraceEnvironment), []byte(trace), 0o600); err != nil {
		return err
	}
	if os.Getenv(appUITerminalEnvironment) == "1" {
		terminalFile, terminalErr := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if terminalErr != nil {
			return terminalErr
		}
		if _, terminalErr = term.MakeRaw(terminalFile.Fd()); terminalErr != nil {
			return terminalErr
		}
		_, terminalErr = terminalFile.WriteString(
			ansi.SetMode(ansi.ModeAltScreenSaveCursor, ansi.ModeBracketedPaste) + ansi.HideCursor,
		)
		if terminalErr != nil {
			return terminalErr
		}
	}
	if os.Getenv(appUIBehaviorEnvironment) == "crash" {
		os.Exit(23)
	}
	return stream.Send(&uipb.OpenResponse{Content: &uipb.OpenResponse_Quit{Quit: &uipb.QuitCommand{}}})
}

// TestRunWithPathsIgnoresActiveUIAndFailsWithoutCredentials verifies headless-only concrete composition.
func TestRunWithPathsIgnoresActiveUIAndFailsWithoutCredentials(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, `defaultProvider: openai-codex
defaultModel: gpt-test
activeUI: UI__DO_NOT_TOUCH
`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeHeadless,
		Headless:           headless.Command{UserText: "request", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &stdout, &stderr)

	require.ErrorContains(t, err, "sign-in required")
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "[info] headless")
	assert.Contains(t, stderr.String(), "[info] extensions: none")
	assert.NotContains(t, stderr.String(), "ui-do-not-touch")
}

// TestRunWithPathsRejectsInvalidExplicitExtensionDirectory verifies invocation override failure.
func TestRunWithPathsRejectsInvalidExplicitExtensionDirectory(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	missingDirectory := filepath.Join(t.TempDir(), "missing-extensions")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeHeadless,
		Headless:           headless.Command{UserText: "request", ExtensionDirectory: missingDirectory},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	assert.ErrorContains(t, err, "explicit extension directory")
}

// TestRunWithPathsReportsUnreadableDefaultDirectory verifies unreadable defaults remain startup diagnostics.
func TestRunWithPathsReportsUnreadableDefaultDirectory(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	extensionDirectory := filepath.Join(paths.Directory, "plugins", "extension")
	require.NoError(t, os.MkdirAll(extensionDirectory, 0o700))
	require.NoError(t, os.Chmod(extensionDirectory, 0o000))
	t.Cleanup(func() { require.NoError(t, os.Chmod(extensionDirectory, 0o700)) })
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeHeadless,
		Headless:           headless.Command{UserText: "request", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: "", UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "sign-in required")
	assert.Contains(t, stderr.String(), "[extension:error]")
	assert.Contains(t, stderr.String(), "[info] extensions: none")
}

// TestRunWithPathsUIReportsAutomaticSelectionWarnings preserves structured failed-selection diagnostics.
func TestRunWithPathsUIReportsAutomaticSelectionWarnings(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "no compatible UI plugin is available")
	assert.Contains(t, stderr.String(), "[warning] excluded UI broken-ui at "+brokenPath+":")
	assert.Contains(t, stderr.String(), "start UI \"broken-ui\"")
}

// TestRunWithPathsUIReportsSelectionWarningsBeforeExtensionStartupFailure preserves pending diagnostics.
func TestRunWithPathsUIReportsSelectionWarningsBeforeExtensionStartupFailure(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	writeUIExecutable(t, uiDirectory, "Valid_UI")
	missingExtensionDirectory := filepath.Join(t.TempDir(), "missing-extensions")
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: missingExtensionDirectory, UIDirectory: uiDirectory, UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "explicit extension directory")
	warning := "[warning] excluded UI broken-ui at " + brokenPath + ":"
	assert.Equal(t, 1, strings.Count(stderr.String(), warning))
	assert.Contains(t, stderr.String(), "start UI \"broken-ui\"")
}

// TestRunWithPathsUIKeepsSelectionWarningsInInitialization prevents duplicate terminal diagnostics.
func TestRunWithPathsUIKeepsSelectionWarningsInInitialization(t *testing.T) {
	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	writeUIExecutable(t, uiDirectory, "Valid_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: "", UIDirectory: uiDirectory, UIID: "",
	}, &bytes.Buffer{}, &stderr)

	require.NoError(t, err)
	trace, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	warning := "excluded UI broken-ui at " + brokenPath + ":"
	assert.Contains(t, string(trace), warning)
	assert.NotContains(t, stderr.String(), warning)
}

// TestRunWithPathsUIUsesSelectedStreamAndCleansProcess verifies real UI process composition.
func TestRunWithPathsUIUsesSelectedStreamAndCleansProcess(t *testing.T) {
	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Fake_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "fake-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	trace, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(trace)), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "fake-ui", lines[1])
	assert.Contains(t, lines[2], "UI fake-ui")
	processID, err := strconv.Atoi(lines[0])
	require.NoError(t, err)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
	logPayload, err := os.ReadFile(paths.LogFile)
	require.NoError(t, err)
	assert.Contains(t, string(logPayload), "starting UI Glyph application")
}

// TestRunWithPathsUITerminalSnapshotFailureStopsBeforeOpen verifies terminal capture is a startup gate.
func TestRunWithPathsUITerminalSnapshotFailureStopsBeforeOpen(t *testing.T) {
	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Terminal_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUITerminalEnvironment, "1")
	t.Setenv(appUIBehaviorEnvironment, "snapshot")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "terminal-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	require.ErrorContains(t, err, "capture selected UI terminal")
	payload, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(string(payload))
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}

// TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement verifies abnormal stream authority.
func TestRunWithPathsUIProcessCrashTerminatesWithoutReplacement(t *testing.T) {
	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Crash_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "crash")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "crash-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	require.ErrorContains(t, err, "receive UI command")
	trace, readErr := os.ReadFile(tracePath)
	require.NoError(t, readErr)
	processID, parseErr := strconv.Atoi(strings.Split(strings.TrimSpace(string(trace)), "\n")[0])
	require.NoError(t, parseErr)
	require.ErrorIs(t, syscall.Kill(processID, 0), syscall.ESRCH)
}

// TestTerminalRecoveryPTY proves normal and os.Exit(23) recovery against a real Darwin PTY.
func TestTerminalRecoveryPTY(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"normal", "crash"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			command := exec.CommandContext(
				t.Context(), "/usr/bin/script", "-q", "/dev/null",
				os.Args[0], "-test.run=^TestTerminalRecoveryPTYInner$",
			)
			command.Env = append(os.Environ(), appUIPTYInnerEnvironment+"="+mode)
			output, err := command.CombinedOutput()
			require.NoError(t, err, string(output))
			assert.Contains(t, string(output), "PASS")
		})
	}
}

// TestTerminalRecoveryPTYInner mutates and verifies one controlling-terminal lifecycle.
func TestTerminalRecoveryPTYInner(t *testing.T) {
	t.Parallel()

	mode := os.Getenv(appUIPTYInnerEnvironment)
	if mode == "" {
		return
	}
	terminalFile, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, terminalFile.Close()) })
	originalState := terminalState(t, terminalFile)
	paths := testPaths(t, "defaultProvider: openai-codex\ndefaultModel: gpt-test\n")
	uiDirectory := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	writeConfiguredUIExecutable(t, uiDirectory, "Terminal_UI", tracePath, mode)

	runErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode:     cli.ModeUI,
		Headless: headless.Command{UserText: "", ExtensionDirectory: ""}, ExtensionDirectory: "",
		UIDirectory: uiDirectory, UIID: "terminal-ui",
	}, &bytes.Buffer{}, &bytes.Buffer{})
	if mode == "crash" {
		require.Error(t, runErr)
	} else {
		require.NoError(t, runErr)
	}
	assert.Equal(t, normalizeTerminalState(originalState), normalizeTerminalState(terminalState(t, terminalFile)))
}

// terminalState reads the exact controlling-terminal termios representation.
func terminalState(t *testing.T, terminalFile *os.File) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "stty", "-g")
	command.Stdin = terminalFile
	output, err := command.Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(output))
}

// normalizeTerminalState ignores macOS's transient PENDIN bit after successful restoration.
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

// writeUIExecutable creates one executable wrapper around the current test binary.
func writeUIExecutable(t *testing.T, directory, name string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=serve exec %q -test.run=^TestUIPluginHelperProcess$\n",
		appUIHelperEnvironment,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o755))
}

// writeConfiguredUIExecutable embeds one isolated PTY helper configuration.
func writeConfiguredUIExecutable(t *testing.T, directory, name, tracePath, mode string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=serve %s=%q %s=1 %s=%q exec %q -test.run=^TestUIPluginHelperProcess$\n",
		appUIHelperEnvironment,
		appUITraceEnvironment,
		tracePath,
		appUITerminalEnvironment,
		appUIBehaviorEnvironment,
		mode,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o755))
}

// testPaths creates one owner-only Glyph data directory and strict settings fixture.
func testPaths(t *testing.T, settingsContent string) persistence.Paths {
	t.Helper()
	directory := filepath.Join(t.TempDir(), ".glyph")
	require.NoError(t, os.Mkdir(directory, 0o700))
	settingsPath := filepath.Join(directory, "settings.yaml")
	require.NoError(t, os.WriteFile(settingsPath, []byte(settingsContent), 0o600))
	logsDirectory := filepath.Join(directory, "logs")
	return persistence.Paths{
		Directory: directory, SettingsFile: settingsPath,
		CredentialsFile: filepath.Join(directory, "credentials.json"),
		LogsDirectory:   logsDirectory, LogFile: filepath.Join(logsDirectory, "glyph.log"),
	}
}
