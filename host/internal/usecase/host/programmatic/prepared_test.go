//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestPreparedOwnerCancellationStopsSummaryNavigation verifies owner cancellation stops blocked summary work without commit.
func TestPreparedOwnerCancellationStopsSummaryNavigation(t *testing.T) {
	t.Parallel()

	// Arrange an admitted navigation whose production dependency blocks until its owner context is canceled.
	navigator := NewMockNavigator(gomock.NewController(t))
	gate := NewMockGate(gomock.NewController(t))
	started := make(chan struct{})
	var committed atomic.Bool
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	navigator.EXPECT().NavigateProgrammatic(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			ctx context.Context,
			_ NavigationIntent,
			_ func(session.Tree) error,
		) (NavigationCompletion, error) {
			close(started)
			<-ctx.Done()
			return NavigationCompletion{}, ctx.Err()
		},
	)
	service := New(nil, nil, testStateQuery(t, false), nil, navigator, gate, testRunOutput(t))
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
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
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
	control := NewMockActiveSessions(gomock.NewController(t))
	gate := NewMockGate(gomock.NewController(t))
	started := make(chan struct{})
	var committed atomic.Bool
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().SetActiveName(gomock.Any(), "new name").DoAndReturn(
		func(ctx context.Context, _ string) (session.Info, error) {
			close(started)
			<-ctx.Done()
			return session.Info{}, ctx.Err()
		},
	)
	service := New(nil, nil, testStateQuery(t, false), control, nil, gate, testRunOutput(t))
	command := testProgrammaticCommand("name-cancel", controller.CommandSetSessionName)
	command.SessionName = mo.Some("new name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling the owner context after mutation work starts.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
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
	navigator := NewMockNavigator(gomock.NewController(t))
	gate := NewMockGate(gomock.NewController(t))
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	navigator.EXPECT().
		NavigateProgrammatic(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(NavigationCompletion{Committed: mo.None[NavigationCommit](), Issues: nil}, nil)
	service := New(nil, nil, testStateQuery(t, false), nil, navigator, gate, testRunOutput(t))
	command := treeCommand("domain-cancel", controller.CommandNavigateSessionTree)
	command.TargetEntryID = mo.Some("target")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()

	// Act with an active operation context.
	outcome := prepared.Run(t.Context(), operation.Reporter[controller.OperationProgress]{})

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
	control := NewMockActiveSessions(gomock.NewController(t))
	gate := NewMockGate(gomock.NewController(t))
	started := make(chan struct{})
	var committed atomic.Bool
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().SetActiveName(gomock.Any(), "committed name").DoAndReturn(
		func(ctx context.Context, _ string) (session.Info, error) {
			close(started)
			<-ctx.Done()
			committed.Store(true)
			return session.Info{}, nil
		},
	)
	service := New(nil, nil, testStateQuery(t, false), control, nil, gate, testRunOutput(t))
	command := testProgrammaticCommand("name-commit", controller.CommandSetSessionName)
	command.SessionName = mo.Some("committed name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling after work starts and allowing the successful commit to return.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
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
	control := NewMockActiveSessions(gomock.NewController(t))
	gate := NewMockGate(gomock.NewController(t))
	started := make(chan struct{})
	domainCause := errors.New("disk sync failed")
	gate.EXPECT().TryAcquire().Return(func() {}, true)
	control.EXPECT().SetActiveName(gomock.Any(), "failed name").DoAndReturn(
		func(ctx context.Context, _ string) (session.Info, error) {
			close(started)
			<-ctx.Done()
			return session.Info{}, fmt.Errorf(
				"store name: %w",
				errors.Join(session.ErrPersistenceUnavailable, domainCause),
			)
		},
	)
	service := New(nil, nil, testStateQuery(t, false), control, nil, gate, testRunOutput(t))
	command := testProgrammaticCommand("name-failure", controller.CommandSetSessionName)
	command.SessionName = mo.Some("failed name")
	prepared, err := service.Prepare(t.Context(), command)
	require.NoError(t, err)
	defer prepared.Release()
	ctx, cancel := context.WithCancel(t.Context())
	outcomeResult := make(chan operation.Outcome[controller.Response], 1)

	// Act by canceling after work starts while the independent storage failure returns.
	go func() {
		outcomeResult <- prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})
	}()
	<-started
	cancel()
	outcome := <-outcomeResult

	// Assert the independent failure keeps its terminal state, classified code, and wrapped domain cause.
	require.Equal(t, operation.TerminalStateFailed, outcome.State())
	require.Equal(t, controller.FailureCodePersistenceUnavailable, outcome.Code())
	require.ErrorIs(t, outcome.Err(), domainCause)
}

// TestRunPreparedClassifiesCancellationWithAndWithoutIndependentFailure verifies preserved errors win cancellation.
func TestRunPreparedClassifiesCancellationWithAndWithoutIndependentFailure(t *testing.T) {
	t.Parallel()

	independentErr := errors.New(strings.Repeat("界", 4001) + " complete run failure suffix...")
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
			name: "persistence failure", activeErr: agent.ErrPersistenceUnavailable,
			expectedState: operation.TerminalStateFailed,
			expectedCode:  controller.FailureCodePersistenceUnavailable, expectedCause: agent.ErrPersistenceUnavailable,
		},
		{
			name: "joined persistence failure", activeErr: errors.Join(independentErr, agent.ErrPersistenceUnavailable),
			expectedState: operation.TerminalStateFailed,
			expectedCode:  controller.FailureCodePersistenceUnavailable, expectedCause: independentErr,
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
			coordinator := NewMockCoordinator(gomock.NewController(t))
			coordinator.EXPECT().
				RunPrepared(gomock.Any(), "run", "request").
				Return(agent.RunOutcomeAborted, test.activeErr)
			prepared := &runPrepared{
				coordinator: coordinator, output: testRunOutput(t), operationID: "operation", runID: "run",
				userText: "request", started: false, release: sync.Once{},
			}
			defer prepared.Release()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			// Act after cancellation and settlement have both completed.
			outcome := prepared.Run(ctx, operation.Reporter[controller.OperationProgress]{})

			// Assert only an independent failure overrides pure cancellation.
			require.Equal(t, test.expectedState, outcome.State())
			require.Equal(t, test.expectedCode, outcome.Code())
			if test.expectedCause != nil {
				require.ErrorIs(t, outcome.Err(), test.expectedCause)
				require.Equal(t, test.activeErr.Error(), outcome.Err().Error())
			} else {
				require.NoError(t, outcome.Err())
			}
		})
	}
}
