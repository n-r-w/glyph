//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardOverflowCancellation joins nested generation before settling the canceled conversation.
func TestStandardOverflowCancellation(t *testing.T) {
	// Arrange standard recovery with pending provider work and later result participants.
	t.Setenv(standardRetainedEnvironment, "1")
	directory, _ := buildStandardOverflowFixture(t)
	resultInputs := t.TempDir()
	t.Setenv("GLYPH_REFERENCE_RESULTS", "1")
	t.Setenv("GLYPH_REFERENCE_RESULT_INPUTS", resultInputs)
	binary, err := os.ReadFile(filepath.Join(directory, "reference-observer"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "reference-results"), binary, 0o700))
	started, stopped := make(chan struct{}), make(chan struct{})
	bodies := &requestBodies{}
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			encoded, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				return nil, readErr
			}
			switch bodies.append(encoded) {
			case 2:
				return newStandardOverflowResponse("provider context limit source"), nil
			case 3:
				close(started)
				<-request.Context().Done()
				close(stopped)
				return nil, request.Context().Err()
			default:
				return runtimeFailureHTTPResponse(finalResponseSSE), nil
			}
		},
	)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-cancel-original", manualOriginalText))
	require.NoError(t, fixture.stream.Send(userRequest("overflow-cancel", manualRetainedText)))
	terminal, failures := receiveManualTerminal(fixture, "overflow-cancel")
	select {
	case <-started:
	case event := <-terminal:
		require.FailNow(t, "overflow generation did not start", "terminal: %v", event)
	case receiveErr := <-failures:
		require.NoError(t, receiveErr)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "overflow generation did not start")
	}

	// Act by canceling the public conversation while its nested summary request is pending.
	require.NoError(t, fixture.stream.Send(cancelRequest("overflow-cancel-command", "overflow-cancel")))
	select {
	case event := <-terminal:
		require.True(t, event.HasCanceled() || event.HasFailed())
	case receiveErr := <-failures:
		require.NoError(t, receiveErr)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "overflow cancellation did not settle")
	}

	// Assert joined provider work, no later participants or retry, and no durable replacement.
	select {
	case <-stopped:
	default:
		require.FailNow(t, "conversation settled before nested provider stopped")
	}
	require.Len(t, bodies.snapshot(), 3)
	captured, err := os.ReadDir(resultInputs)
	require.NoError(t, err)
	require.Empty(t, captured)
	stored := sendProgrammaticOperation(
		t,
		fixture,
		"overflow-cancel-stored",
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	for _, entry := range stored {
		require.Nil(t, entry.GetCompaction())
	}
}
