//go:build integration

package app

import (
	"bytes"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestProgrammaticObserverCancellationReleasesRun verifies recovery before the first user append on an open stream.
func TestProgrammaticObserverCancellationReleasesRun(t *testing.T) {
	// Arrange an AgentStart observer blocked in its nested configured-model request, before any history append.
	t.Setenv(externalLifecycleEnvironment, "1")
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	started := make(chan struct{})
	requests := new(atomic.Int32)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(started)
			<-request.Context().Done()
			return nil, request.Context().Err()
		}
		return &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
			Header: make(http.Header), Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0,
			ContentLength: 0, TransferEncoding: nil, Close: false, Uncompressed: false,
			Trailer: nil, Request: nil, TLS: nil,
		}, nil
	}).AnyTimes()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest("blocked-observer", "first request")))
	<-started

	// Act by canceling only the admitted run, without closing the client connection.
	require.NoError(t, fixture.stream.Send(cancelRequest("cancel-observer", "blocked-observer")))
	terminals := 0
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		event := response.GetEvent()
		if response.GetOperationId() == "blocked-observer" &&
			(event.HasCanceled() || event.HasFailed() || event.HasCompleted()) {
			terminals++
		}
		if response.GetOperationId() == "cancel-observer" && event.HasCompleted() {
			break
		}
	}

	// Assert cancellation settled Core and its output association before a query and a later admitted run.
	require.Equal(t, 1, terminals)
	require.Equal(t, int32(1), requests.Load())
	state := completeObserverRecoveryRequest(t, fixture, runStateRequest("after-cancel")).GetRunState()
	require.Empty(t, state.GetActiveOperationId(), "canceled observer must clear the active run association")
	require.Equal(t, programmaticpb.RunState_RUN_STATE_IDLE, state.GetState())
	messages := completeObserverRecoveryRequest(
		t,
		fixture,
		testProgrammaticRequest("empty-history", func(request *programmaticpb.ControllerRequest) {
			request.SetGetMessages(new(programmaticpb.GetMessages))
		}),
	)
	require.Empty(t, messages.GetMessages().GetEntries())
	completeObserverRecoveryRequest(t, fixture, userRequest("later-run", "second request"))
	require.Equal(t, int32(3), requests.Load())
}

// completeObserverRecoveryRequest verifies the retained stream accepts work without a duplicate canceled-run terminal.
func completeObserverRecoveryRequest(
	t *testing.T,
	fixture *programmaticFixture,
	request *programmaticpb.OpenRequest,
) *programmaticpb.HostCompleted {
	t.Helper()
	require.NoError(t, fixture.stream.Send(request))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		event := response.GetEvent()
		if event == nil {
			continue
		}
		if event.HasCanceled() || event.HasFailed() || event.HasCompleted() {
			require.NotEqual(
				t,
				"blocked-observer",
				response.GetOperationId(),
				"canceled run must have exactly one terminal result",
			)
		}
		if response.GetOperationId() != request.GetOperationId() {
			continue
		}
		require.False(t, event.HasRejected(), "later operation must be admitted: %v", event.GetRejected())
		require.False(t, event.HasFailed(), "later operation must complete: %v", event.GetFailed())
		require.False(t, event.HasCanceled(), "later operation must not be canceled")
		if event.HasCompleted() {
			return event.GetCompleted()
		}
	}
}
