//go:build integration

package app

import (
	"os"
	"path/filepath"

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
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestProtocolFailureReturnsNonzero verifies that invalid input terminates the owning process as an error.
func (testSuite *ProgrammaticAppSuite) TestProtocolFailureReturnsNonzero() {
	t := testSuite.T()

	// Arrange a Programmatic stream and a request without correlation identity.
	paths := testPaths(t, codexSettings(""))
	fixture := startProgrammaticFixture(t, paths)
	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
	invalid := programmaticv1.OpenRequest_builder{
		UserRequest: programmaticv1.UserRequest_builder{
			Text: new("missing correlation"),
		}.Build(),
		CreateSession:  nil,
		ListSessions:   nil,
		ResumeSession:  nil,
		SetSessionName: nil,
		GetSessionInfo: nil, GetSessionTree: nil, NavigateSessionTree: nil,
	}.Build()
	// Act by sending the invalid protocol request and receiving stream termination.
	require.NoError(t, fixture.stream.Send(invalid))
	_, receiveErr := fixture.stream.Recv()
	require.Error(t, receiveErr)
	runErr := <-fixture.result
	// Assert the application exits nonzero with the safe protocol error.
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "correlation ID is required")
	fixture.assertClosed(t)
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

	runErr := runProgrammaticServer(t.Context(), server, socketService, controller.Completions(), session)
	require.Error(t, runErr)
	require.ErrorContains(t, runErr, "serve Programmatic Control")
	require.NoError(t, socketService.Close())
}

// TestTransportCompletionReturnsNonzero verifies app handling of an independent send failure.
func (testSuite *ProgrammaticAppSuite) TestTransportCompletionReturnsNonzero() {
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
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	sendErr := status.Error(codes.ResourceExhausted, "send failed")
	completions := make(chan controllerprogrammatic.SessionCompletion, 1)
	results := make(chan error, 1)
	go func() {
		results <- runProgrammaticServer(t.Context(), server, socketService, completions, session)
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
		Cause:      controllerprogrammatic.SessionCompletionTransportFailure,
		Err:        sendErr,
		CleanupErr: nil,
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
