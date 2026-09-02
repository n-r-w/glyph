//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hookrunner "github.com/n-r-w/glyph/host/internal/hooks/runner"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestPreparedOwnerCancellationStopsSummaryNavigation verifies owner cancellation stops blocked summary work without commit.
func TestPreparedOwnerCancellationStopsSummaryNavigation(t *testing.T) {
	t.Parallel()

	// Arrange an admitted navigation whose production dependency blocks until its owner context is canceled.
	control := NewMockSessionControl(gomock.NewController(t))
	started := make(chan struct{})
	var committed atomic.Bool
	control.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().Navigate(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ sessionnavigation.Request) (sessionnavigation.Result, error) {
			close(started)
			<-ctx.Done()
			return sessionnavigation.Result{}, ctx.Err()
		},
	)
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := treeCommand("summary-cancel", controller.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")
	command.SummaryMode = controller.SummaryModeSummarize
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling the owner context after navigation starts.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.AgentEvent]{})
	}()
	<-started
	cancel()
	outcome := <-outcomeResult

	// Assert cancellation is terminal and the dependency exposed no post-cancel commit.
	require.Equal(t, operation.TerminalStateCanceled, outcome.State())
	require.False(t, committed.Load())
}

// TestPreparedOwnerCancellationStopsStoredSessionMutation verifies cancellation stops blocked storage-backed mutation work.
func TestPreparedOwnerCancellationStopsStoredSessionMutation(t *testing.T) {
	t.Parallel()

	// Arrange an admitted name mutation whose storage-backed dependency blocks until cancellation.
	control := NewMockSessionControl(gomock.NewController(t))
	started := make(chan struct{})
	var committed atomic.Bool
	control.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().SetName(gomock.Any(), "new name").DoAndReturn(
		func(ctx context.Context, _ string) (session.Info, error) {
			close(started)
			<-ctx.Done()
			return session.Info{}, ctx.Err()
		},
	)
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := testProgrammaticCommand("name-cancel", controller.CommandSetSessionName)
	command.SessionName = mo.Some("new name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling the owner context after mutation work starts.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.AgentEvent]{})
	}()
	<-started
	cancel()
	outcome := <-outcomeResult

	// Assert cancellation is terminal and storage exposed no post-cancel commit.
	require.Equal(t, operation.TerminalStateCanceled, outcome.State())
	require.False(t, committed.Load())
}

// TestPreparedDomainCanceledNavigationCompletes verifies a domain cancellation remains completed without owner cancellation.
func TestPreparedDomainCanceledNavigationCompletes(t *testing.T) {
	t.Parallel()

	// Arrange admitted navigation that completes with a domain-canceled result.
	control := NewMockSessionControl(gomock.NewController(t))
	control.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().Navigate(gomock.Any(), gomock.Any()).Return(sessionnavigation.Result{
		Canceled: true, Tree: session.Tree{}, ActiveLeafID: mo.None[string](), ActiveBranch: nil,
		NextInput: mo.None[string](), Issues: nil,
	}, nil)
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := treeCommand("domain-cancel", controller.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()

	// Act with an active operation context.
	outcome := prepared.Run(t.Context(), operation.Reporter[controller.AgentEvent]{})

	// Assert the domain result remains completed and carries its canceled navigation status.
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
	response, present := outcome.Result()
	require.True(t, present)
	require.Equal(t, controller.TreeNavigationStatusCanceled, response.TreeNavigation.MustGet().Status)
}

// TestPreparedCommittedMutationWinsCancellation verifies a successful commit remains completed during cancellation.
func TestPreparedCommittedMutationWinsCancellation(t *testing.T) {
	t.Parallel()

	// Arrange a mutation that commits while owner cancellation races with its return.
	control := NewMockSessionControl(gomock.NewController(t))
	started := make(chan struct{})
	var committed atomic.Bool
	control.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().SetName(gomock.Any(), "committed name").DoAndReturn(
		func(ctx context.Context, _ string) (session.Info, error) {
			close(started)
			<-ctx.Done()
			committed.Store(true)
			return session.Info{}, nil
		},
	)
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := testProgrammaticCommand("name-commit", controller.CommandSetSessionName)
	command.SessionName = mo.Some("committed name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling after work starts and allowing the successful commit to return.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.AgentEvent]{})
	}()
	<-started
	cancel()
	outcome := <-outcomeResult

	// Assert the committed effect and completed terminal state win the race.
	require.True(t, committed.Load())
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
}

// TestPreparedIndependentMutationFailureWinsCancellation verifies cancellation does not hide a storage failure.
func TestPreparedIndependentMutationFailureWinsCancellation(t *testing.T) {
	t.Parallel()

	// Arrange a mutation that returns an independent classified failure after owner cancellation.
	control := NewMockSessionControl(gomock.NewController(t))
	started := make(chan struct{})
	control.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().SetName(gomock.Any(), "failed name").DoAndReturn(
		func(ctx context.Context, _ string) (session.Info, error) {
			close(started)
			<-ctx.Done()
			return session.Info{}, fmt.Errorf("store name: %w", session.ErrPersistenceUnavailable)
		},
	)
	service := New(nil, nil, idleStateSnapshot, emptyHistorySnapshot, control, NewDelivery())
	command := testProgrammaticCommand("name-failure", controller.CommandSetSessionName)
	command.SessionName = mo.Some("failed name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling after work starts while the independent storage failure returns.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.AgentEvent]{})
	}()
	<-started
	cancel()
	outcome := <-outcomeResult

	// Assert the independent failure keeps its terminal state and classified code.
	require.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.Equal(t, controller.FailureCodePersistenceUnavailable, outcome.Code())
}

// TestRunPreparedProgressFailureStopsTerminalDeliveryBeforeJoin verifies failed progress cannot deadlock owner joining.
func TestRunPreparedProgressFailureStopsTerminalDeliveryBeforeJoin(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange a real Agent Core run and an owner delivery that maps progress to a failed connection.
		mockController := gomock.NewController(t)
		delivery := NewDelivery()
		historyStore := agentrun.NewMockHistoryStore(mockController)
		history := make([]agent.HistoryEntry, 0, 1)
		historyStore.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
			return append([]agent.HistoryEntry(nil), history...)
		}).AnyTimes()
		historyStore.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, entry agent.HistoryEntry) error {
				history = append(history, entry.Clone())
				return nil
			},
		).AnyTimes()
		eventSink := agentrun.NewMockEventSink(mockController)
		eventSink.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(delivery.DeliverAgent).AnyTimes()
		provider := agentrun.NewMockModelProvider(mockController)
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ agentrun.ModelRequest, _ agentrun.StreamHandler) error {
				<-ctx.Done()
				return context.Cause(ctx)
			},
		).AnyTimes()
		runtime := agentrun.NewMockModelRuntime(mockController)
		runtime.EXPECT().Snapshot().Return(agentrun.RequestSnapshot{
			Model: model.Descriptor{
				Provider: "provider", Model: "model", Input: nil, ContextWindow: 0, MaxTokens: 0,
				ReasoningCapabilities: model.ReasoningCapabilities{}, ToolCapabilities: model.ToolCapabilities{},
				Pricing: mo.None[model.Pricing](),
			},
			ReasoningChoice: model.ReasoningChoiceHigh,
			Provider:        provider,
		}).AnyTimes()
		tools := agentrun.NewMockToolRuntime(mockController)
		tools.EXPECT().Tools().Return(nil).AnyTimes()
		agentCore := agentrun.New(
			"instructions", runtime, hookrunner.New(nil, nil, nil), tools, eventSink, historyStore,
		)
		coordinator := NewMockCoordinator(mockController)
		coordinator.EXPECT().PrepareRun().Return("run-progress-failure", nil)
		coordinator.EXPECT().RunPrepared(gomock.Any(), "run-progress-failure", "request").DoAndReturn(
			func(ctx context.Context, runID, userText string) (agent.RunOutcome, error) {
				result, err := agentCore.Run(ctx, agentrun.Request{RunID: runID, UserText: userText})
				return result.Outcome, err
			},
		)
		service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, delivery)
		command := testProgrammaticCommand("progress-failure", controller.CommandUserRequest)
		command.UserText = mo.Some("request")
		prepared, err := service.Prepare(t.Context(), command)
		require.NoError(t, err)
		preparedRun := prepared.(*runPrepared)

		writer := operation.NewWriter(func(string) error { return nil })
		writerResult := make(chan error, 1)
		go func() { writerResult <- writer.Run(t.Context()) }()
		connectionErr := status.Error(codes.Unavailable, "send failed")
		ownerDelivery := operation.NewMockDelivery[controller.AgentEvent, controller.Response](mockController)
		ownerDelivery.EXPECT().Accepted("progress-failure").DoAndReturn(
			func(string) (*operation.Acknowledgement, error) {
				return writer.EnqueueAcknowledged("accepted")
			},
		)
		ownerDelivery.EXPECT().Running("progress-failure").Return(nil)
		ownerDelivery.EXPECT().Progress("progress-failure", gomock.Any()).Return(connectionErr)
		owner := operation.NewOwner(t.Context(), ownerDelivery)
		require.NoError(t, owner.Start("progress-failure", func() (
			operation.Prepared[controller.AgentEvent, controller.Response], error,
		) {
			return prepared, nil
		}))

		// Act until failed progress creates the cleanup and terminal-delivery channel cycle.
		synctest.Wait()

		// Assert cleanup stopped progress first, then rescue the old behavior so RED exits without a timeout.
		streamStopped := false
		select {
		case <-preparedRun.active.streamDone:
			streamStopped = true
		default:
		}
		assert.True(t, streamStopped, "cleanup must stop progress delivery before waiting for Agent Core")
		if !streamStopped {
			delivery.mutex.Lock()
			delivery.stopStreamLocked(preparedRun.active)
			delivery.mutex.Unlock()
		}
		owner.Wait()
		writer.Close()
		require.NoError(t, <-writerResult)

		// Assert Agent Core finished, Delivery.finish closed events, and the owner kept the connection error.
		assert.Equal(t, agentrun.StatusAwaitingSettlement, agentCore.State().Status)
		select {
		case <-preparedRun.active.done:
		default:
			assert.Fail(t, "Agent Core did not reach Delivery.finish")
		}
		_, eventsOpen := <-preparedRun.active.events
		assert.False(t, eventsOpen)
		assert.ErrorIs(t, owner.Err(), connectionErr)
		assert.Equal(t, codes.Unavailable, status.Code(owner.Err()))
	})
}

// TestRunPreparedClassifiesCancellationWithAndWithoutIndependentFailure verifies preserved errors win cancellation.
func TestRunPreparedClassifiesCancellationWithAndWithoutIndependentFailure(t *testing.T) {
	t.Parallel()

	independentErr := errors.New("settlement failed")
	tests := []struct {
		name          string
		activeErr     error
		expectedState operation.TerminalState
		expectedCode  string
		expectedCause error
	}{
		{
			name: "independent failure", activeErr: independentErr,
			expectedState: operation.TerminalStateFailed,
			expectedCode:  controller.FailureCodeInternal, expectedCause: independentErr,
		},
		{
			name: "pure cancellation", activeErr: nil,
			expectedState: operation.TerminalStateCanceled, expectedCode: "", expectedCause: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a settled prepared run and a canceled owner context.
			delivery := NewDelivery()
			active := newTestActiveRun(t.Context(), delivery, "operation", "run")
			active.state = operationFinished
			active.err = test.activeErr
			close(active.events)
			close(active.done)
			defer active.cancel()
			prepared := &runPrepared{active: active, release: sync.Once{}}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// Act after cancellation and settlement have both completed.
			outcome := prepared.Run(ctx, operation.Reporter[controller.AgentEvent]{})

			// Assert only an independent failure overrides pure cancellation.
			require.Equal(t, test.expectedState, outcome.State())
			require.Equal(t, test.expectedCode, outcome.Code())
			if test.expectedCause != nil {
				require.ErrorIs(t, outcome.Err(), test.expectedCause)
			} else {
				require.NoError(t, outcome.Err())
			}
		})
	}
}
