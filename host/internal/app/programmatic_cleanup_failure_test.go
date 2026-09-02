//go:build integration

package app

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestOwnerClosurePersistenceFailurePreservesContext verifies Programmatic cleanup returns storage details.
func (testSuite *ProgrammaticAppSuite) TestOwnerClosurePersistenceFailurePreservesContext() {
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
		programmaticRequest(
			request,
		).SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("cleanup durable")}.Build())
	}).GetSessionInfo().GetInfo()
	privateUserText := "private cleanup user"
	require.NoError(t, fixture.stream.Send(userRequest("cleanup-failure", privateUserText)))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "cleanup-failure" && response.GetEvent().HasRunning() {
			break
		}
	}
	<-providerStarted
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o400))
	t.Cleanup(func() { _ = os.Chmod(named.GetStoragePath(), 0o600) })

	// Act by closing the owner while terminal aborted-model persistence targets the read-only active file.
	require.NoError(t, fixture.stream.CloseSend())
	runErr := <-fixture.result
	require.NoError(t, runErr)
	fixture.assertClosed(t)
	require.NoError(t, os.Chmod(named.GetStoragePath(), 0o600))
	stored, err := os.ReadFile(named.GetStoragePath())
	require.NoError(t, err)
	var records []programmaticCleanupLogRecord
	decoder := jsontext.NewDecoder(bytes.NewReader(logs.Bytes()))
	for {
		var record programmaticCleanupLogRecord
		if decodeErr := json.UnmarshalDecode(decoder, &record); errors.Is(decodeErr, io.EOF) {
			break
		} else {
			require.NoError(t, decodeErr)
		}
		records = append(records, record)
	}

	// Assert joined cancellation, durable user state, and internal failure diagnostics.
	select {
	case <-providerCanceled:
	default:
		require.Fail(t, "process returned before provider cancellation completed")
	}
	assert.NotContains(t, logs.String(), privateUserText)
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
	assert.Contains(t, diagnostic.Error, "open session file")
	assert.True(t, strings.Contains(strings.ToLower(diagnostic.Error), "permission") ||
		strings.Contains(strings.ToLower(diagnostic.Error), "operation not permitted"),
		"unexpected persistence error %q", diagnostic.Error,
	)
}
