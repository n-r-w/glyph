//go:build integration

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	headlessoutput "github.com/n-r-w/glyph/host/internal/infra/headless"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
)

const (
	// oneShotDataEnvironment selects the child fixture and its isolated persistence directory.
	oneShotDataEnvironment = "GLYPH_TEST_ONESHOT_DATA"
	// oneShotProcessFilter selects only the one-shot child entry point.
	oneShotProcessFilter = "-test.run=^TestOneShotProcess$"
)

// newOneShotCommand prepares the one-shot child process with inherited environment and isolated persistence.
func newOneShotCommand(t *testing.T, paths persistence.Paths, extensions, text string) *exec.Cmd {
	t.Helper()
	command := exec.CommandContext(t.Context(), os.Args[0], oneShotProcessFilter,
		"--", "run", "--extension-dir", extensions, text)
	command.Env = append(os.Environ(), oneShotDataEnvironment+"="+paths.Directory)
	return command
}

// TestOneShotProcess runs the assembled one-shot application and exposes its outcome through process status.
func TestOneShotProcess(t *testing.T) {
	t.Parallel()
	dataDirectory := os.Getenv(oneShotDataEnvironment)
	if dataDirectory == "" {
		return
	}

	// Arrange the explicit child paths and the production CLI parser and renderer.
	paths := persistence.Paths{
		Directory:       dataDirectory,
		SettingsFile:    filepath.Join(dataDirectory, "settings.yaml"),
		CredentialsFile: filepath.Join(dataDirectory, "credentials.json"),
		LogsDirectory:   filepath.Join(dataDirectory, "logs"),
		LogFile:         filepath.Join(dataDirectory, "logs", "glyph.log"),
	}
	separator := slices.Index(os.Args, "--")
	require.NotEqual(t, -1, separator)
	command, err := cli.Parse(os.Args[separator+1:])
	require.NoError(t, err)

	// Act through the production application composition with isolated persistence.
	runErr := runWithPaths(t.Context(), paths, command, os.Stdout, os.Stderr)

	// Assert the outcome at the parent boundary through the actual child exit status and output.
	if runErr != nil {
		require.NoError(t, headlessoutput.NewRenderer(os.Stdout, os.Stderr).WriteError(runErr))
		os.Exit(1)
	}
	os.Exit(0)
}
