package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestExecuteMapsCompletedRunToZero verifies one parsed request reaches concrete composition.
func TestExecuteMapsCompletedRunToZero(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	called := 0
	runner := func(
		_ context.Context,
		command cli.Command,
		actualStdout, actualStderr io.Writer,
	) error {
		called++
		assert.Equal(t, cli.Command{
			Mode:               cli.ModeHeadless,
			Headless:           headless.Command{UserText: "request", ExtensionDirectory: "/extensions"},
			ExtensionDirectory: "", UIDirectory: "", UIID: "", SocketPath: "",
		}, command)
		assert.Equal(t, &stdout, actualStdout)
		assert.Equal(t, &stderr, actualStderr)
		return nil
	}

	exitCode := execute(
		t.Context(),
		[]string{"run", "--extension-dir", "/extensions", "request"},
		&stdout,
		&stderr,
		runner,
	)

	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 1, called)
	assert.Empty(t, stderr.String())
}

// TestExecuteRejectsUIAndInvalidCardinalityWithUsageExit verifies startup never runs.
func TestExecuteRejectsUIAndInvalidCardinalityWithUsageExit(t *testing.T) {
	t.Parallel()

	testCases := map[string][]string{
		"UI flag":         {"run", "--ui", "glyph-tui", "request"},
		"missing request": {"run"},
		"extra request":   {"run", "first", "second"},
	}
	for name, arguments := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			called := false

			exitCode := execute(
				t.Context(), arguments, &bytes.Buffer{}, &stderr,
				func(context.Context, cli.Command, io.Writer, io.Writer) error {
					called = true
					return nil
				},
			)

			assert.Equal(t, 2, exitCode)
			assert.False(t, called)
			assert.Contains(t, stderr.String(), "[error]")
		})
	}
}

// TestExecuteWritesSafePersistenceFailure verifies the final CLI renderer preserves exact public persistence text.
func TestExecuteWritesSafePersistenceFailure(t *testing.T) {
	t.Parallel()

	// Arrange one parsed headless command whose application boundary returns the safe classified error.
	var stderr bytes.Buffer

	// Act by executing through the final CLI return and rendering boundary.
	exitCode := execute(
		t.Context(), []string{"run", "request"}, &bytes.Buffer{}, &stderr,
		func(context.Context, cli.Command, io.Writer, io.Writer) error {
			return agentrun.ErrPersistenceUnavailable
		},
	)

	// Assert the process result fails and stderr contains only the exact public persistence text.
	assert.Equal(t, 1, exitCode)
	assert.Equal(t, "[error] session persistence failed\n", stderr.String())
}

// TestExecuteMapsRuntimeAndCancellationErrorsToNonzero verifies terminal failures are rendered once.
func TestExecuteMapsRuntimeAndCancellationErrorsToNonzero(t *testing.T) {
	t.Parallel()

	testCases := map[string]error{
		"provider failure": errors.New("provider failed"),
		"cancellation":     context.Canceled,
	}
	for name, runErr := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer

			exitCode := execute(
				t.Context(), []string{"run", "request"}, &bytes.Buffer{}, &stderr,
				func(context.Context, cli.Command, io.Writer, io.Writer) error { return runErr },
			)

			assert.Equal(t, 1, exitCode)
			assert.Contains(t, stderr.String(), "[error]")
			assert.Contains(t, stderr.String(), runErr.Error())
		})
	}
}

// TestExecuteReturnsNonzeroWhenTerminalErrorCannotBeRendered verifies writer failures remain terminal.
func TestExecuteReturnsNonzeroWhenTerminalErrorCannotBeRendered(t *testing.T) {
	t.Parallel()

	closedWriter, err := os.Create(filepath.Join(t.TempDir(), "closed-stderr"))
	require.NoError(t, err)
	require.NoError(t, closedWriter.Close())

	exitCode := execute(
		t.Context(), []string{"run", "request"}, &bytes.Buffer{}, closedWriter,
		func(context.Context, cli.Command, io.Writer, io.Writer) error {
			return errors.New("provider failed")
		},
	)

	assert.Equal(t, 1, exitCode)
}
