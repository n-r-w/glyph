package app

import (
	"bytes"

	"os"

	"path/filepath"

	"strconv"
	"strings"

	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestRunWithPathsUICodexDefaultKeepsProviderAuthentication verifies Codex-owned startup authentication.
func TestRunWithPathsUICodexDefaultKeepsProviderAuthentication(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Codex_UI")
	tracePath := filepath.Join(t.TempDir(), "authentication-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "authentication")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "codex-ui",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	payload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	assert.Equal(t, uipb.Availability_AVAILABILITY_AUTHENTICATING.String(), string(payload))
}

// TestRunWithPathsUICompatibleDefaultSkipsCodexAuthentication verifies active-provider startup authentication.
func TestRunWithPathsUICompatibleDefaultSkipsCodexAuthentication(t *testing.T) {
	paths := testPaths(t, `defaultProvider: local
defaultModel: local-model
providers:
  openai-codex:
    type: openai-codex
    models:
      - id: codex-model
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning:
          supported: false
          choices: [off]
          default: off
  local:
    type: openai-compatible
    baseURL: http://localhost:11434/v1
    api: chat-completions
    models:
      - id: local-model
        input: [text]
        contextWindow: 131072
        maxTokens: 16384
        toolCapabilities: {}
        reasoning:
          supported: false
          choices: [off]
          default: off
`)
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Compatible_UI")
	tracePath := filepath.Join(t.TempDir(), "authentication-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	t.Setenv(appUIBehaviorEnvironment, "authentication")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "compatible-ui",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.NoError(t, err)
	payload, err := os.ReadFile(tracePath)
	require.NoError(t, err)
	assert.Equal(t, uipb.Availability_AVAILABILITY_IDLE.String(), string(payload))
}

// TestRunWithPathsIgnoresActiveUIAndFailsWithoutCredentials verifies headless-only concrete composition.
func TestRunWithPathsIgnoresActiveUIAndFailsWithoutCredentials(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings("activeUI: UI__DO_NOT_TOUCH\n"))
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           "request",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
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

	paths := testPaths(t, codexSettings(""))
	missingDirectory := filepath.Join(t.TempDir(), "missing-extensions")

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           "request",
			ExtensionDirectory: missingDirectory,
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	require.Error(t, err)
	assert.ErrorContains(t, err, "explicit extension directory")
}

// TestRunWithPathsReportsUnreadableDefaultDirectory verifies unreadable defaults remain startup diagnostics.
func TestRunWithPathsReportsUnreadableDefaultDirectory(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	extensionDirectory := filepath.Join(paths.Directory, "plugins", "extension")
	require.NoError(t, os.MkdirAll(extensionDirectory, 0o700))
	require.NoError(t, os.Chmod(extensionDirectory, 0o000))
	t.Cleanup(func() { require.NoError(t, os.Chmod(extensionDirectory, 0o700)) })
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           "request",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "sign-in required")
	assert.Contains(t, stderr.String(), "[extension:error]")
	assert.Contains(t, stderr.String(), "[info] extensions: none")
}

// TestRunWithPathsUIReportsAutomaticSelectionWarnings preserves structured failed-selection diagnostics.
func TestRunWithPathsUIReportsAutomaticSelectionWarnings(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "",
		SocketPath:         "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "no compatible UI plugin is available")
	assert.Contains(t, stderr.String(), "[warning] excluded UI broken-ui at "+brokenPath+":")
	assert.Contains(t, stderr.String(), "start UI \"broken-ui\"")
}

// TestRunWithPathsUIReportsSelectionWarningsBeforeExtensionStartupFailure preserves pending diagnostics.
func TestRunWithPathsUIReportsSelectionWarningsBeforeExtensionStartupFailure(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	writeUIExecutable(t, uiDirectory, "Valid_UI")
	missingExtensionDirectory := filepath.Join(t.TempDir(), "missing-extensions")
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: missingExtensionDirectory,
		UIDirectory:        uiDirectory,
		UIID:               "",
		SocketPath:         "",
	}, &bytes.Buffer{}, &stderr)

	require.ErrorContains(t, err, "explicit extension directory")
	warning := "[warning] excluded UI broken-ui at " + brokenPath + ":"
	assert.Equal(t, 1, strings.Count(stderr.String(), warning))
	assert.Contains(t, stderr.String(), "start UI \"broken-ui\"")
}

// TestRunWithPathsUIKeepsSelectionWarningsInInitialization prevents duplicate terminal diagnostics.
func TestRunWithPathsUIKeepsSelectionWarningsInInitialization(t *testing.T) {
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	brokenPath := filepath.Join(uiDirectory, "Broken_UI")
	require.NoError(t, os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 23\n"), 0o755))
	writeUIExecutable(t, uiDirectory, "Valid_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)
	var stderr bytes.Buffer

	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "",
		SocketPath:         "",
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
	// Arrange a selected UI helper, trace path, and application paths.
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	writeUIExecutable(t, uiDirectory, "Fake_UI")
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	t.Setenv(appUITraceEnvironment, tracePath)

	// Act by running UI mode through the selected plugin stream.
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "fake-ui",
		SocketPath:         "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert the stream completes, the plugin process exits, and structured logs describe the selection.
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
	assert.Contains(t, string(logPayload), "loading UI plugins")
	assert.Contains(t, string(logPayload), "loaded UI plugin")
	assert.Contains(t, string(logPayload), `"plugin_id":"fake-ui"`)
	assert.Contains(t, string(logPayload), `"controls_terminal":false`)
	assert.Contains(t, string(logPayload), "loading extensions")
	assert.Contains(t, string(logPayload), "loaded extensions")
}
