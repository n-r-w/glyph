//go:build integration

package app

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controllerprogrammatic "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"

	programmaticsocket "github.com/n-r-w/glyph/host/internal/infra/programmatic/socket"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
)

// TestMalformedRequestIsRejectedWithoutClosingStream verifies per-request protocol failure mapping.
func (testSuite *ProgrammaticAppSuite) TestMalformedRequestIsRejectedWithoutClosingStream() {
	t := testSuite.T()

	// Arrange a Programmatic stream and a request without an operation identifier.
	fixture := startProgrammaticFixture(t, testPaths(t, codexSettings("")))
	defer fixture.closeOwner(t)
	invalid := userRequest("", "missing operation identifier")

	// Act by sending the malformed request and then a valid query.
	require.NoError(t, fixture.stream.Send(invalid))
	rejected, err := fixture.stream.Recv()
	require.NoError(t, err)
	state := completeProgrammaticRequest(t, fixture, runStateRequest("state"))

	// Assert exact rejection mapping and continued stream use.
	assert.True(t, rejected.GetEvent().HasRejected())
	assert.Equal(t, "INVALID_ARGUMENT", rejected.GetEvent().GetRejected().GetCode())
	assert.Equal(t, "programmatic operation identifier is required", rejected.GetEvent().GetRejected().GetMessage())
	assert.True(t, state.HasRunState())
}

// TestClientDisconnectWaitsForActiveWork verifies transport-loss cleanup joins Host-owned work.
func (testSuite *ProgrammaticAppSuite) TestClientDisconnectWaitsForActiveWork() {
	t := testSuite.T()

	// Arrange provider work that observes cancellation but cannot stop until the test permits it.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requests := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	providerCanceled := make(chan struct{}, 1)
	allowProviderStop := make(chan struct{}, 1)
	providerStopped := make(chan struct{}, 1)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			if requests.Add(1) != 1 {
				return nil, errors.New("unexpected dependent provider request")
			}
			providerStarted <- struct{}{}
			<-request.Context().Done()
			providerCanceled <- struct{}{}
			<-allowProviderStop
			providerStopped <- struct{}{}
			return nil, request.Context().Err()
		},
	)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	fixture := startProgrammaticFixture(t, paths)
	t.Cleanup(func() {
		select {
		case allowProviderStop <- struct{}{}:
		default:
		}
		fixture.cancel()
		_ = fixture.connection.Close()
	})
	require.NoError(t, fixture.stream.Send(userRequest("disconnect-active", "wait for disconnect")))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "disconnect-active" && response.GetEvent().HasRunning() {
			break
		}
	}
	<-providerStarted

	// Act by dropping the real client transport while Host work remains active.
	require.NoError(t, fixture.connection.Close())
	<-providerCanceled

	// Assert process completion waits until provider work stops.
	select {
	case runErr := <-fixture.result:
		require.Failf(t, "process returned before provider work stopped", "error: %v", runErr)
	default:
	}
	allowProviderStop <- struct{}{}
	<-providerStopped
	runErr := <-fixture.result
	require.Error(t, runErr)
	assert.Equal(t, int32(1), requests.Load())
	fixture.assertStdout(t)
	_, err := os.Lstat(fixture.socketPath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

// TestServeFailureReturnsNonzero verifies that an independent server failure changes the process result.
func (testSuite *ProgrammaticAppSuite) TestServeFailureReturnsNonzero() {
	t := testSuite.T()
	delivery := hostprogrammatic.NewDelivery()
	coordinator := hostprogrammatic.NewMockCoordinator(gomock.NewController(t))
	session := hostprogrammatic.New(
		coordinator, nil,
		func() agentrun.State {
			return agentrun.State{
				Status:          agentrun.StatusIdle,
				RunID:           mo.None[string](),
				PartialResponse: mo.None[model.Response](),
				ToolPreviews:    nil,
			}
		},
		func() []agent.HistoryEntry { return nil },
		nil, delivery,
	)
	controller := controllerprogrammatic.New(t.Context(), session)
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	require.NoError(t, socketService.Listener.Close())

	runErr := runProgrammaticServer(t.Context(), server, socketService, controller.Completions())
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "serve Programmatic Control")
	require.NoError(t, socketService.Close())
}

// TestTransportCompletionReturnsNonzero verifies app handling of an independent send failure.
func (testSuite *ProgrammaticAppSuite) TestTransportCompletionReturnsNonzero() {
	t := testSuite.T()
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	sendErr := status.Error(codes.ResourceExhausted, "send failed")
	completions := make(chan controllerprogrammatic.SessionCompletion, 1)
	results := make(chan error, 1)
	go func() {
		results <- runProgrammaticServer(t.Context(), server, socketService, completions)
	}()
	connection, err := grpc.NewClient(
		"unix://"+socketService.Path(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	connection.Connect()
	for state := connection.GetState(); state != connectivity.Ready; state = connection.GetState() {
		require.True(t, connection.WaitForStateChange(t.Context(), state))
	}
	completions <- controllerprogrammatic.SessionCompletion{
		Cause: controllerprogrammatic.SessionCompletionTransportFailure,
		Err:   sendErr,
	}

	runErr := <-results
	require.Error(t, runErr)
	require.ErrorIs(t, runErr, sendErr)
	require.Same(t, sendErr, runErr)
	assert.Equal(t, codes.ResourceExhausted, status.Code(runErr))
	require.NoError(t, connection.Close())
	require.NoError(t, socketService.Close())
}

// TestSocketCleanupFailureReturnsNonzero verifies that cleanup errors change the process result.
func (testSuite *ProgrammaticAppSuite) TestSocketCleanupFailureReturnsNonzero() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixture(t, paths)
	require.NoError(t, os.Remove(fixture.socketPath))
	require.NoError(t, os.Mkdir(fixture.socketPath, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(fixture.socketPath, "keep"), []byte("keep"), 0o600))

	require.NoError(t, fixture.stream.CloseSend())
	runErr := <-fixture.result
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "directory not empty")
	fixture.assertStdout(t)
	require.NoError(t, fixture.connection.Close())
	fixture.cancel()
}

// TestAutomaticSocketDirectoryIsRemoved verifies process-owned directory cleanup.
func (testSuite *ProgrammaticAppSuite) TestAutomaticSocketDirectoryIsRemoved() {
	// Arrange a fixture with an automatically allocated socket directory.
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixtureAtPath(t, paths, t.TempDir(), "")
	directory := filepath.Dir(fixture.socketPath)

	// Act by closing the owning programmatic stream.
	fixture.closeOwner(t)
	_, err := os.Stat(directory)

	// Assert shutdown removes the automatically created directory.
	require.ErrorIs(t, err, os.ErrNotExist)
}
