package app

import (
	"bytes"
	"encoding/json"
	"errors"
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
	output           bytes.Buffer
	sessionsRoot     string
	attempted        bool
	projectDirectory string
	err              error
}

type headlessLogRecord struct {
	Message   string `json:"msg"`
	Operation string `json:"operation"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// Write captures each JSON log record and injects the directory fault before logging returns to the application.
func (writer *headlessPersistenceFaultWriter) Write(data []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	_, _ = writer.output.Write(data)
	if writer.attempted || !bytes.Contains(data, []byte(`"msg":"loading extensions"`)) {
		return len(data), nil
	}
	writer.attempted = true
	entries, err := os.ReadDir(writer.sessionsRoot)
	if err != nil {
		writer.err = err
		return len(data), nil
	}
	if len(entries) != 1 {
		writer.err = fmt.Errorf("expected one project session directory, got %d", len(entries))
		return len(data), nil
	}
	writer.projectDirectory = filepath.Join(writer.sessionsRoot, entries[0].Name())
	writer.err = os.Chmod(writer.projectDirectory, 0o500)
	return len(data), nil
}

// TestRunWithPathsHeadlessPersistenceFailureIsPubliclySafe verifies raw first-user storage details stay out of public sinks.
//
//nolint:paralleltest // The test replaces process-global HTTP transport and logger instances.
func TestRunWithPathsHeadlessPersistenceFailureIsPubliclySafe(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("directory permission failure injection requires Darwin permission enforcement")
	}

	// Arrange the real headless composition, synchronous filesystem fault injection, logs, and CLI stderr boundary.
	paths := testPaths(t, restartSelectionSettings())
	requests := &atomic.Int32{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = countingFailureTransport{requests: requests}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	faultWriter := &headlessPersistenceFaultWriter{
		mutex: sync.Mutex{}, output: bytes.Buffer{},
		sessionsRoot: filepath.Join(paths.Directory, "sessions"),
		attempted:    false, projectDirectory: "", err: nil,
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
	logs := append([]byte(nil), faultWriter.output.Bytes()...)
	faultWriter.mutex.Unlock()
	if projectDirectory != "" {
		t.Cleanup(func() { _ = os.Chmod(projectDirectory, 0o700) })
	}
	var records []headlessLogRecord
	decoder := json.NewDecoder(bytes.NewReader(logs))
	for {
		var record headlessLogRecord
		if err := decoder.Decode(&record); errors.Is(err, io.EOF) {
			break
		} else {
			require.NoError(t, err)
		}
		records = append(records, record)
	}

	// Assert exact public text, provider suppression, safe application sinks, and one isolated structured diagnostic.
	require.True(t, faultAttempted)
	require.NoError(t, faultErr)
	require.NotEmpty(t, projectDirectory)
	require.ErrorIs(t, runErr, agentrun.ErrPersistenceUnavailable)
	assert.Equal(t, "session persistence failed", runErr.Error())
	assert.Equal(t, "[error] session persistence failed\n", cliStderr.String())
	assert.Equal(t, 1, strings.Count(cliStderr.String(), "session persistence failed"))
	assert.Empty(t, stdout.String())
	assert.NotEmpty(t, applicationStderr.String())
	assert.NotContains(t, applicationStderr.String(), "session persistence failed")
	assert.Zero(t, requests.Load())
	var diagnostic *headlessLogRecord
	var applicationFailure *headlessLogRecord
	for index := range records {
		switch records[index].Message {
		case "session persistence failed":
			diagnostic = &records[index]
		case "headless Glyph application failed":
			applicationFailure = &records[index]
		}
	}
	require.NotNil(t, diagnostic)
	require.NotNil(t, applicationFailure)
	assert.Equal(t, "append_history", diagnostic.Operation)
	require.NotEmpty(t, diagnostic.SessionID)
	require.NotEmpty(t, diagnostic.Error)
	assert.Equal(t, "session persistence failed", applicationFailure.Error)
	privateValues := []string{
		filepath.Join(paths.Directory, "sessions"),
		projectDirectory,
		".jsonl",
		diagnostic.SessionID,
		privateUserText,
		"provider-context",
		"extension-json",
		diagnostic.Error,
	}
	for _, value := range privateValues {
		assert.NotContains(t, applicationFailure.Error, value)
		assert.NotContains(t, applicationStderr.String(), value)
		assert.NotContains(t, cliStderr.String(), value)
	}
	assert.NotContains(t, diagnostic.Error, filepath.Join(paths.Directory, "sessions"))
	assert.NotContains(t, diagnostic.Error, projectDirectory)
	assert.NotContains(t, diagnostic.Error, ".jsonl")
	assert.NotContains(t, string(logs), filepath.Join(paths.Directory, "sessions"))
	assert.NotContains(t, string(logs), projectDirectory)
	assert.NotContains(t, string(logs), ".jsonl")
	assert.NotContains(t, string(logs), privateUserText)
	assert.NotContains(t, string(logs), "provider-context")
	assert.NotContains(t, string(logs), "extension-json")
	for _, record := range records {
		if record.Message != "session persistence failed" {
			assert.Empty(t, record.SessionID)
			assert.NotContains(t, record.Error, diagnostic.SessionID)
			assert.NotContains(t, record.Error, diagnostic.Error)
		}
	}
	assert.True(t, strings.Contains(strings.ToLower(diagnostic.Error), "permission") ||
		strings.Contains(strings.ToLower(diagnostic.Error), "operation not permitted"))
}
