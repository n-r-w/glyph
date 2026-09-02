//go:build integration

package app

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

type headlessPersistenceFaultWriter struct {
	mutex            sync.Mutex
	sessionsRoot     string
	attempted        bool
	projectDirectory string
	err              error
}

// Write injects the directory fault after session initialization reaches the next lifecycle event.
func (writer *headlessPersistenceFaultWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.attempted {
		return len(data), nil
	}
	entries, err := os.ReadDir(writer.sessionsRoot)
	if os.IsNotExist(err) {
		return len(data), nil
	}
	if err != nil {
		writer.attempted = true
		writer.err = err
		return len(data), nil
	}
	writer.attempted = true
	if len(entries) != 1 {
		writer.err = fmt.Errorf("expected one project session directory, got %d", len(entries))
		return len(data), nil
	}
	writer.projectDirectory = filepath.Join(writer.sessionsRoot, entries[0].Name())
	writer.err = os.Chmod(writer.projectDirectory, 0o500)
	return len(data), nil
}

// TestRunWithPathsHeadlessPersistenceFailurePreservesContext verifies storage failures reach CLI output.
//
//nolint:paralleltest // The test replaces process-global HTTP transport and logger instances.
func TestRunWithPathsHeadlessPersistenceFailurePreservesContext(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("directory permission failure injection requires Darwin permission enforcement")
	}

	// Arrange the real headless composition, synchronous filesystem fault injection, and CLI stderr boundary.
	paths := testPaths(t, restartSelectionSettings())
	requests := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = newCountingFailureTransport(t, requests)
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	faultWriter := &headlessPersistenceFaultWriter{
		mutex:            sync.Mutex{},
		sessionsRoot:     filepath.Join(paths.Directory, "sessions"),
		attempted:        false,
		projectDirectory: "",
		err:              nil,
	}
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(faultWriter, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	privateUserText := "private first user provider-context extension-json"
	var stdout bytes.Buffer
	var applicationStderr bytes.Buffer

	// Act by running the first user append through the real filesystem and rendering the returned error as CLI stderr.
	runErr := runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeHeadless,
		Headless: headless.Command{
			UserText:           privateUserText,
			ExtensionDirectory: "",
		},
		ExtensionDirectory: "",
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         "",
	}, &stdout, &applicationStderr)
	require.Error(t, runErr)
	var cliStderr bytes.Buffer
	require.NoError(t, headless.NewRenderer(io.Discard, &cliStderr).WriteError(runErr))
	faultWriter.mutex.Lock()
	faultAttempted := faultWriter.attempted
	projectDirectory := faultWriter.projectDirectory
	faultErr := faultWriter.err
	faultWriter.mutex.Unlock()
	if projectDirectory != "" {
		t.Cleanup(func() { _ = os.Chmod(projectDirectory, 0o700) })
	}

	// Assert the full persistence failure reaches the returned error and CLI renderer.
	require.True(t, faultAttempted)
	require.NoError(t, faultErr)
	require.NotEmpty(t, projectDirectory)
	require.ErrorIs(t, runErr, agentrun.ErrPersistenceUnavailable)
	assert.Contains(t, runErr.Error(), "session persistence failed")
	assert.Contains(t, strings.ToLower(runErr.Error()), "permission")
	assert.Equal(t, "[error] "+runErr.Error()+"\n", cliStderr.String())
	assert.NotContains(t, cliStderr.String(), privateUserText)
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, applicationStderr.String())
	assert.Zero(t, requests.Load())
}
