package app

import (
	"bytes"

	"fmt"

	"os"
	"os/exec"
	"path/filepath"

	"strconv"
	"strings"

	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

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
	paths := testPaths(t, codexSettings(""))
	uiDirectory := t.TempDir()
	tracePath := filepath.Join(t.TempDir(), "ui-trace")
	writeConfiguredUIExecutable(t, uiDirectory, "Terminal_UI", tracePath, mode)

	runErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI,
		Headless: headless.Command{
			UserText:           "",
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        uiDirectory,
		UIID:               "terminal-ui",
		SocketPath:         "",
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
