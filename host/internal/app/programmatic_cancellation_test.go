//go:build integration

package app

import (
	"context"

	"errors"
	"fmt"

	"net/http"

	"strings"
	"sync/atomic"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"google.golang.org/grpc/status"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	controllerprogrammatic "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"

	programmaticsocket "github.com/n-r-w/glyph/host/internal/infra/programmatic/socket"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestOwnerCanAbortAndStartAnotherRun verifies multi-operation ownership without a process restart.
func (testSuite *ProgrammaticAppSuite) TestOwnerCanAbortAndStartAnotherRun() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{
		requestCount: requestCount,
		started:      providerStarted,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	fixture := startProgrammaticFixtureWithExtension(t, paths, buildToolsExecutable(t))
	stream := fixture.stream
	require.NoError(t, stream.Send(userRequest("c1", "first request")))
	accepted, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "c1", accepted.GetCorrelationId())
	require.Equal(t, programmaticv1.OpenResponse_CommandResponse_case, accepted.WhichContent())
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, accepted.GetCommandResponse().WhichResult())

	firstEvent, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "c1", firstEvent.GetCorrelationId())
	require.Equal(t, programmaticv1.OpenResponse_AgentEvent_case, firstEvent.WhichContent())
	// Provider transport entry proves that abort cancels an active provider request.
	<-providerStarted

	require.NoError(t, stream.Send(abortRequest("abort-c1")))
	var settled, aborted bool
	for !settled || !aborted {
		response, receiveErr := stream.Recv()
		if receiveErr != nil {
			runErr := <-fixture.result
			require.NoError(t, receiveErr, "application error: %v", runErr)
		}
		switch response.WhichContent() {
		case programmaticv1.OpenResponse_Content_not_set_case:
			require.FailNow(t, "received response without content")
		case programmaticv1.OpenResponse_AgentEvent_case:
			if response.GetCorrelationId() == "c1" && response.GetAgentEvent().GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
				settled = true
			}
		case programmaticv1.OpenResponse_CommandResponse_case:
			if response.GetCorrelationId() == "abort-c1" {
				assert.Equal(t, programmaticv1.CommandResponse_AbortCompleted_case, response.GetCommandResponse().WhichResult())
				aborted = true
			}
		}
	}

	require.NoError(t, stream.Send(runStateRequest("state-after-abort")))
	stateResponse, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "state-after-abort", stateResponse.GetCorrelationId())
	assert.Equal(t, programmaticv1.RunState_RUN_STATE_IDLE, stateResponse.GetCommandResponse().GetRunState().GetState())

	require.NoError(t, stream.Send(userRequest("c2", "second request")))
	secondAccepted, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "c2", secondAccepted.GetCorrelationId())
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, secondAccepted.GetCommandResponse().WhichResult())
	for {
		response, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		if response.GetCorrelationId() == "c2" && response.WhichContent() == programmaticv1.OpenResponse_AgentEvent_case && response.GetAgentEvent().GetType() == programmaticv1.AgentEventType_AGENT_EVENT_TYPE_AGENT_SETTLED {
			break
		}
	}

	fixture.closeOwner(t)
	assert.Equal(t, int32(2), requestCount.Load())
}

// TestOwnerClosureCancelsActiveRun verifies clean disconnect cancellation and joining.
func (testSuite *ProgrammaticAppSuite) TestOwnerClosureCancelsActiveRun() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{
		requestCount: requestCount,
		started:      providerStarted,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	fixture := startProgrammaticFixture(t, paths)
	require.NoError(t, fixture.stream.Send(userRequest("c1", "disconnect this request")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, accepted.GetCommandResponse().WhichResult())
	_, err = fixture.stream.Recv()
	require.NoError(t, err)
	<-providerStarted

	fixture.closeOwner(t)
	assert.Equal(t, int32(1), requestCount.Load())
}

// TestApplicationCancellationWinsOverStreamShutdown verifies cancellation precedence while work is active.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationWinsOverStreamShutdown() {
	t := testSuite.T()
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	requestCount := new(atomic.Int32)
	providerStarted := make(chan struct{}, 1)
	previousTransport := http.DefaultTransport
	http.DefaultTransport = programmaticTransport{
		requestCount: requestCount,
		started:      providerStarted,
	}
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	fixture := startProgrammaticFixture(t, paths)
	require.NoError(t, fixture.stream.Send(userRequest("c1", "cancel this request")))
	accepted, err := fixture.stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, programmaticv1.CommandResponse_UserRequestAccepted_case, accepted.GetCommandResponse().WhichResult())
	_, err = fixture.stream.Recv()
	require.NoError(t, err)
	<-providerStarted

	fixture.cancel()
	_ = fixture.stream.CloseSend()
	runErr := <-fixture.result
	require.ErrorIs(t, runErr, context.Canceled)
	fixture.assertClosed(t)
	assert.Equal(t, int32(1), requestCount.Load())
}

// TestApplicationCancellationRetainsBufferedProtocolCompletion verifies ready terminal sources survive arbitration.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationRetainsBufferedProtocolCompletion() {
	t := testSuite.T()

	// Arrange repeated valid canceled contexts with an already-buffered protocol completion.
	for index := range 16 {
		protocolErr := status.Error(codes.InvalidArgument, fmt.Sprintf("unique buffered protocol failure %d", index))
		cleanupErr := fmt.Errorf("unique buffered cleanup failure %d", index)
		completions := make(chan controllerprogrammatic.SessionCompletion, 1)
		completions <- controllerprogrammatic.SessionCompletion{
			Cause: controllerprogrammatic.SessionCompletionApplicationCanceled,
			Err:   errors.Join(context.Canceled, protocolErr), CleanupErr: cleanupErr,
		}
		server := grpc.NewServer(grpc.WaitForHandlers(true))
		socketService, err := programmaticsocket.New(t.Context(), "")
		require.NoError(t, err)
		canceledContext, cancel := context.WithCancel(t.Context())
		cancel()

		// Act with cancellation and completion ready before arbitration.
		runErr := runProgrammaticServer(
			canceledContext, server, socketService, completions, newIdleProgrammaticTestSession(t),
		)

		// Assert cancellation, protocol, and cleanup causes each survive once.
		require.ErrorIs(t, runErr, context.Canceled)
		require.ErrorIs(t, runErr, protocolErr)
		require.ErrorIs(t, runErr, cleanupErr)
		assert.Equal(t, 1, strings.Count(runErr.Error(), context.Canceled.Error()))
		assert.Equal(t, 1, strings.Count(runErr.Error(), protocolErr.Error()))
		assert.Equal(t, 1, strings.Count(runErr.Error(), cleanupErr.Error()))
		require.NoError(t, socketService.Close())
	}
}

// TestApplicationCancellationRetainsServeFailure verifies listener failure survives cancellation.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationRetainsServeFailure() {
	t := testSuite.T()

	// Arrange cancellation and one preloaded independent Serve result before explicit Stop.
	serveErr := errors.New("unique deterministic Serve failure")
	serveResults := make(chan error, 1)
	serveResults <- serveErr
	collector := programmaticShutdownCollector{
		completions:  make(chan controllerprogrammatic.SessionCompletion),
		serveResults: serveResults, completionRead: false, serveResultRead: false,
	}
	stopCalled := false

	// Act through the same bounded shutdown collector used by the real server path.
	runErr := collector.finish(context.Canceled, context.Canceled, nil, func() { stopCalled = true })

	// Assert cancellation and the independent Serve failure survive without scheduler delays.
	require.ErrorIs(t, runErr, context.Canceled)
	require.ErrorIs(t, runErr, serveErr)
	assert.Equal(t, 1, strings.Count(runErr.Error(), context.Canceled.Error()))
	assert.Equal(t, 1, strings.Count(runErr.Error(), serveErr.Error()))
	assert.True(t, stopCalled)
}

// TestApplicationPureCancellationIgnoresExplicitStop verifies owned Stop adds no server error.
func (testSuite *ProgrammaticAppSuite) TestApplicationPureCancellationIgnoresExplicitStop() {
	t := testSuite.T()

	// Arrange a valid canceled context with no other terminal source.
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()
	completions := make(chan controllerprogrammatic.SessionCompletion)

	// Act through pure application cancellation and explicit server Stop.
	runErr := runProgrammaticServer(
		canceledContext, server, socketService, completions, newIdleProgrammaticTestSession(t),
	)

	// Assert cancellation stays canonical and server Stop adds no sibling.
	require.ErrorIs(t, runErr, context.Canceled)
	assert.Equal(t, context.Canceled.Error(), runErr.Error())
	assert.NotContains(t, runErr.Error(), grpc.ErrServerStopped.Error())
	require.NoError(t, socketService.Close())
}

// TestApplicationCancellationDoesNotDuplicateCleanupCancellation verifies context-first cleanup deduplication.
func (testSuite *ProgrammaticAppSuite) TestApplicationCancellationDoesNotDuplicateCleanupCancellation() {
	t := testSuite.T()

	// Arrange an active operation whose cancellation returns cancellation plus one independent cleanup error.
	controller := gomock.NewController(t)
	coordinator := hostprogrammatic.NewMockCoordinator(controller)
	coordinator.EXPECT().PrepareRun().Return("run-1", nil)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	cleanupErr := errors.New("unique context-selected cleanup failure")
	runStarted := make(chan struct{})
	coordinator.EXPECT().RunPrepared(gomock.Any(), "run-1", "request").DoAndReturn(
		func(ctx context.Context, _, _ string) (agent.RunOutcome, error) {
			close(runStarted)
			<-ctx.Done()
			return agent.RunOutcomeFailed, errors.Join(ctx.Err(), cleanupErr)
		},
	)
	delivery := hostprogrammatic.NewDelivery()
	sessionService := hostprogrammatic.New(
		coordinator, nil,
		func() agentrun.State {
			return agentrun.State{
				Status: agentrun.StatusIdle, RunID: mo.None[string](),
				PartialResponse: mo.None[model.Response](), ToolPreviews: nil,
			}
		},
		func() []agent.HistoryEntry { return nil }, nil, delivery,
	)
	_, operation, err := sessionService.Handle(t.Context(), controllerprogrammatic.Command{
		CorrelationID: "c1", Kind: controllerprogrammatic.CommandUserRequest, UserText: mo.Some("request"),
		ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.None[session.ID](),
		SessionName: mo.None[string](),
	})
	require.NoError(t, err)
	require.NotNil(t, operation)
	operation.Start()
	<-runStarted
	server := grpc.NewServer(grpc.WaitForHandlers(true))
	socketService, err := programmaticsocket.New(t.Context(), "")
	require.NoError(t, err)
	completions := make(chan controllerprogrammatic.SessionCompletion)
	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through the context-selected application cancellation branch.
	runErr := runProgrammaticServer(canceledContext, server, socketService, completions, sessionService)

	// Assert cancellation and cleanup remain visible without repeating cancellation text.
	require.ErrorIs(t, runErr, context.Canceled)
	require.ErrorIs(t, runErr, cleanupErr)
	assert.Equal(t, 1, strings.Count(runErr.Error(), context.Canceled.Error()))
	assert.Equal(t, 1, strings.Count(runErr.Error(), cleanupErr.Error()))
	require.NoError(t, socketService.Close())
}
