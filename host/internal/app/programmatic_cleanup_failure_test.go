package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const programmaticCleanupProviderMarker = "private model provider-context extension-json"

type programmaticOwnerCleanupTransport struct {
	requests *atomic.Int32
	started  chan<- struct{}
	canceled chan<- struct{}
}

type programmaticCleanupLogRecord struct {
	Message   string `json:"msg"`
	Operation string `json:"operation"`
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// RoundTrip blocks one provider request until owner cleanup cancels its context.
func (transport programmaticOwnerCleanupTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.requests.Add(1) != 1 {
		return nil, errors.New("unexpected dependent provider request")
	}
	transport.started <- struct{}{}
	<-request.Context().Done()
	transport.canceled <- struct{}{}
	return nil, errors.Join(request.Context().Err(), errors.New(programmaticCleanupProviderMarker))
}

// TestOwnerClosurePersistenceFailureIsPubliclySafe verifies Programmatic cleanup cannot return raw storage details.
func (testSuite *ProgrammaticAppSuite) TestOwnerClosurePersistenceFailureIsPubliclySafe() {
	t := testSuite.T()

	// Arrange one named real session, captured logs, and a provider request that reports cancellation completion.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requests := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	providerCanceled := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticOwnerCleanupTransport{
		requests: requests, started: providerStarted, canceled: providerCanceled,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	fixture := startProgrammaticFixture(t, paths)
	t.Cleanup(func() {
		fixture.cancel()
		_ = fixture.connection.Close()
	})
	named := sendProgrammaticCommand(t, fixture, "name-cleanup-failure", func(request *programmaticv1.OpenRequest) {
		request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("cleanup durable")}.Build())
	}).GetSessionInfo().GetInfo()
	privateUserText := "private cleanup user"
	require.NoError(t, fixture.stream.Send(userRequest("cleanup-failure", privateUserText)))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	require.True(t, accepted.GetCommandResponse().HasUserRequestAccepted())
	<-providerStarted
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o400))
	t.Cleanup(func() { _ = os.Chmod(named.GetStoragePath(), 0o600) })

	// Act by closing the owner while terminal aborted-model persistence targets the read-only active file.
	require.NoError(t, fixture.stream.CloseSend())
	runErr := <-fixture.result
	require.Error(t, runErr)
	var cliStderr bytes.Buffer
	require.NoError(t, headless.NewRenderer(io.Discard, &cliStderr).WriteError(runErr))
	fixture.assertClosed(t)
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o600))
	stored, err := os.ReadFile(named.GetStoragePath())
	require.NoError(t, err)
	var records []programmaticCleanupLogRecord
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	for {
		var record programmaticCleanupLogRecord
		if decodeErr := decoder.Decode(&record); errors.Is(decodeErr, io.EOF) {
			break
		} else {
			require.NoError(t, decodeErr)
		}
		records = append(records, record)
	}

	// Assert joined cleanup, exact public text, durable user state, removed socket, and isolated diagnostics.
	select {
	case <-providerCanceled:
	default:
		require.Fail(t, "process returned before provider cancellation completed")
	}
	require.ErrorIs(t, runErr, agentrun.ErrPersistenceUnavailable)
	assert.Equal(t, "session persistence failed", runErr.Error())
	assert.Equal(t, "[error] session persistence failed\n", cliStderr.String())
	assert.Equal(t, 1, strings.Count(cliStderr.String(), "session persistence failed"))
	assert.Equal(t, int32(1), requests.Load())
	assert.Contains(t, string(stored), privateUserText)
	assert.NotContains(t, string(stored), `"type":"model"`)
	var diagnostic *programmaticCleanupLogRecord
	for index := range records {
		if records[index].Message == "session persistence failed" {
			diagnostic = &records[index]
		}
	}
	require.NotNil(t, diagnostic)
	assert.Equal(t, "append_history", diagnostic.Operation)
	assert.Equal(t, named.GetId(), diagnostic.SessionID)
	require.NotEmpty(t, diagnostic.Error)
	privateValues := []string{
		filepath.Join(paths.Directory, "sessions"),
		filepath.Dir(named.GetStoragePath()),
		named.GetStoragePath(),
		filepath.Base(named.GetStoragePath()),
		named.GetId(),
		privateUserText,
		programmaticCleanupProviderMarker,
		"provider-context",
		"extension-json",
		diagnostic.Error,
	}
	for _, value := range privateValues {
		assert.NotContains(t, cliStderr.String(), value)
	}
	assert.NotContains(t, logs.String(), filepath.Join(paths.Directory, "sessions"))
	assert.NotContains(t, logs.String(), filepath.Dir(named.GetStoragePath()))
	assert.NotContains(t, logs.String(), filepath.Base(named.GetStoragePath()))
	assert.NotContains(t, logs.String(), privateUserText)
	assert.NotContains(t, logs.String(), programmaticCleanupProviderMarker)
	assert.NotContains(t, logs.String(), "provider-context")
	assert.NotContains(t, logs.String(), "extension-json")
	assert.NotContains(t, diagnostic.Error, filepath.Base(named.GetStoragePath()))
	for _, record := range records {
		if record.Message != "session persistence failed" {
			assert.Empty(t, record.SessionID)
			assert.NotContains(t, record.Error, named.GetId())
			assert.NotContains(t, record.Error, diagnostic.Error)
		}
	}
	assert.True(t, strings.Contains(strings.ToLower(diagnostic.Error), "permission") ||
		strings.Contains(strings.ToLower(diagnostic.Error), "operation not permitted"),
		"unexpected infrastructure cause %q", diagnostic.Error,
	)
}
