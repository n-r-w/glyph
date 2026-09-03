//go:build !integration

package operation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestOwnerWaitsForAcceptedDeliveryBeforeRun tests the scenario where admission is visible before work starts.
func TestOwnerWaitsForAcceptedDeliveryBeforeRun(t *testing.T) {
	t.Parallel()

	// Arrange controlled delivery and prepared work.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	accepted := newAcknowledgement()
	terminal := newAcknowledgement()
	runStarted := make(chan struct{})
	acceptedCall := delivery.EXPECT().Accepted("operation-1").Return(accepted, nil)
	runningCall := delivery.EXPECT().Running("operation-1").Return(nil)
	terminalCall := delivery.EXPECT().Terminal("operation-1", gomock.Any()).DoAndReturn(
		func(_ string, outcome Outcome[string]) (*Acknowledgement, error) {
			require.Equal(t, TerminalStateCompleted, outcome.State())
			terminal.resolve(nil)
			return terminal, nil
		},
	)
	runCall := prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, Reporter[string]) Outcome[string] {
			close(runStarted)
			return Completed("done")
		},
	)
	releaseCall := prepared.EXPECT().Release()
	gomock.InOrder(acceptedCall, runningCall, runCall, releaseCall, terminalCall)
	owner := NewOwner(t.Context(), delivery)

	// Act by starting work while Accepted remains unacknowledged.
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))

	// Assert Run remains stopped until delivery acknowledgement.
	select {
	case <-runStarted:
		t.Fatal("Run started before Accepted delivery")
	default:
	}
	accepted.resolve(nil)
	<-runStarted
	owner.Wait()
}

// TestOwnerReportsProgressBeforeTerminal tests the scenario where Reporter preserves lifecycle placement.
func TestOwnerReportsProgressBeforeTerminal(t *testing.T) {
	t.Parallel()

	// Arrange automatically acknowledged lifecycle delivery.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	accepted := resolvedAcknowledgement(nil)
	terminal := resolvedAcknowledgement(nil)
	acceptedCall := delivery.EXPECT().Accepted("operation-1").Return(accepted, nil)
	runningCall := delivery.EXPECT().Running("operation-1").Return(nil)
	progressCall := delivery.EXPECT().Progress("operation-1", "half").Return(nil)
	terminalCall := delivery.EXPECT().Terminal("operation-1", gomock.Any()).Return(terminal, nil)
	runCall := prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, reporter Reporter[string]) Outcome[string] {
			require.NoError(t, reporter.Report("half"))
			return Completed("done")
		},
	)
	releaseCall := prepared.EXPECT().Release()
	gomock.InOrder(acceptedCall, runningCall, runCall, progressCall, releaseCall, terminalCall)
	owner := NewOwner(t.Context(), delivery)

	// Act by running one operation that reports progress.
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))
	owner.Wait()

	// Assert mock ordering and calls are checked by gomock.
}

// TestOwnerProgressQueueOverflowStopsWork tests a worker whose progress queue is full.
func TestOwnerProgressQueueOverflowStopsWork(t *testing.T) {
	t.Parallel()

	// Arrange running work whose first progress delivery exhausts the outbound queue.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	releaseCalls := atomic.Int32{}
	delivery.EXPECT().Accepted("operation-1").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("operation-1").Return(nil)
	delivery.EXPECT().Progress("operation-1", "blocked").Return(ErrQueueFull)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, reporter Reporter[string]) Outcome[string] {
			require.ErrorIs(t, reporter.Report("blocked"), ErrQueueFull)
			<-ctx.Done()
			return Canceled[string]()
		},
	)
	prepared.EXPECT().Release().Do(func() { releaseCalls.Add(1) })
	owner := NewOwner(t.Context(), delivery)

	// Act by starting and joining the worker after progress delivery fails.
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))
	owner.Wait()

	// Assert failure cancels the owner, releases once, and never reports terminal success.
	require.ErrorIs(t, owner.Err(), ErrQueueFull)
	require.EqualValues(t, 1, releaseCalls.Load())
}

// TestOwnerCancelAndWaitJoinsTerminalDelivery tests the scenario where cancellation completes after the target terminal event.
func TestOwnerCancelAndWaitJoinsTerminalDelivery(t *testing.T) {
	t.Parallel()

	// Arrange cancellable work and a held terminal acknowledgement.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	terminal := newAcknowledgement()
	terminalQueued := make(chan struct{})
	runStarted := make(chan struct{})
	delivery.EXPECT().Accepted("target").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("target").Return(nil)
	delivery.EXPECT().Terminal("target", gomock.Any()).DoAndReturn(
		func(_ string, outcome Outcome[string]) (*Acknowledgement, error) {
			require.Equal(t, TerminalStateCanceled, outcome.State())
			close(terminalQueued)
			return terminal, nil
		},
	)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ Reporter[string]) Outcome[string] {
			close(runStarted)
			<-ctx.Done()
			return Canceled[string]()
		},
	)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("target", preparedBy(prepared)))
	<-runStarted
	result := make(chan TerminalState, 1)
	errResult := make(chan error, 1)

	// Act by canceling the active target.
	go func() {
		state, err := owner.CancelAndWait(t.Context(), "target")
		result <- state
		errResult <- err
	}()
	<-terminalQueued

	// Assert cancellation waits for target terminal delivery, then returns its state.
	select {
	case <-result:
		t.Fatal("cancellation completed before target terminal delivery")
	default:
	}
	terminal.resolve(nil)
	require.Equal(t, TerminalStateCanceled, <-result)
	require.NoError(t, <-errResult)
	owner.Wait()
}

// TestOwnerReleasesWhenAcceptedDeliveryFails tests the scenario where failed admission delivery prevents Run and frees admission.
func TestOwnerReleasesWhenAcceptedDeliveryFails(t *testing.T) {
	t.Parallel()

	// Arrange Accepted delivery that fails asynchronously.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	deliveryErr := errors.New("accepted delivery failed")
	accepted := newAcknowledgement()
	delivery.EXPECT().Accepted("operation-1").Return(accepted, nil)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))

	// Act by failing delivery before work starts.
	accepted.resolve(deliveryErr)
	owner.Wait()

	// Assert the owner records the failure and gomock tests the scenario where Run was not called.
	require.ErrorIs(t, owner.Err(), deliveryErr)
}

// TestOwnerCloseCancelsAndJoinsWork tests the scenario where closure does not detach context-bound work.
func TestOwnerCloseCancelsAndJoinsWork(t *testing.T) {
	t.Parallel()

	// Arrange work that observes cancellation but controls when it returns.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	allowReturn := make(chan struct{})
	runStarted := make(chan struct{})
	runCanceled := make(chan struct{})
	delivery.EXPECT().Accepted("operation-1").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("operation-1").Return(nil)
	delivery.EXPECT().Terminal("operation-1", gomock.Any()).Return(resolvedAcknowledgement(nil), nil)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ Reporter[string]) Outcome[string] {
			close(runStarted)
			<-ctx.Done()
			close(runCanceled)
			<-allowReturn
			return Canceled[string]()
		},
	)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))
	<-runStarted
	closed := make(chan struct{})

	// Act by closing the owner while its operation is still returning.
	go func() {
		owner.Close()
		close(closed)
	}()
	<-runCanceled

	// Assert Close joins work instead of returning after cancellation alone.
	select {
	case <-closed:
		t.Fatal("Close returned before operation work stopped")
	default:
	}
	close(allowReturn)
	<-closed
}

// TestOwnerRejectsDuplicateActiveIdentifier tests the scenario where duplicate admission cannot replace active ownership.
func TestOwnerRejectsDuplicateActiveIdentifier(t *testing.T) {
	t.Parallel()

	// Arrange one operation held before Accepted delivery.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	first := NewMockPrepared[string, string](controller)
	accepted := newAcknowledgement()
	var duplicatePrepared atomic.Bool
	delivery.EXPECT().Accepted("operation-1").Return(accepted, nil)
	first.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("operation-1", preparedBy(first)))

	// Act by trying to reserve the same identifier again.
	err := owner.Start("operation-1", func() (Prepared[string, string], error) {
		duplicatePrepared.Store(true)
		return first, nil
	})

	// Assert duplicate validation runs before operation-specific admission.
	require.ErrorIs(t, err, ErrIdentifierInUse)
	require.False(t, duplicatePrepared.Load())
	accepted.resolve(errors.New("stop first operation"))
	owner.Wait()
}

// TestOwnerCallsReleaseExactlyOnce tests the scenario where operation cleanup has one owner.
func TestOwnerCallsReleaseExactlyOnce(t *testing.T) {
	t.Parallel()

	// Arrange a completed operation and count release calls through gomock action.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	var releases atomic.Int32
	delivery.EXPECT().Accepted("operation-1").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("operation-1").Return(nil)
	delivery.EXPECT().Terminal("operation-1", gomock.Any()).Return(resolvedAcknowledgement(nil), nil)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).Return(Completed("done"))
	prepared.EXPECT().Release().Do(func() { releases.Add(1) })
	owner := NewOwner(t.Context(), delivery)

	// Act by completing and joining the operation.
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))
	owner.Wait()

	// Assert cleanup runs exactly once.
	require.EqualValues(t, 1, releases.Load())
}

// TestOwnerReleasesClaimAfterPreparationRejection tests the scenario where rejected admission permits identifier reuse.
func TestOwnerReleasesClaimAfterPreparationRejection(t *testing.T) {
	t.Parallel()

	// Arrange one rejected preparation followed by accepted work for the same identifier.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	admissionErr := errors.New("admission rejected")
	deliveryErr := errors.New("stop accepted work")
	accepted := newAcknowledgement()
	delivery.EXPECT().Accepted("operation-1").Return(accepted, nil)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)

	// Act by rejecting preparation, then reusing the identifier successfully.
	err := owner.Start("operation-1", func() (Prepared[string, string], error) {
		return nil, admissionErr
	})
	require.ErrorIs(t, err, admissionErr)
	require.NoError(t, owner.Start("operation-1", preparedBy(prepared)))
	accepted.resolve(deliveryErr)
	owner.Wait()

	// Assert the second claim reached Accepted delivery and released its admission once.
	require.ErrorIs(t, owner.Err(), deliveryErr)
}

// TestOwnerReleasesPreparedWorkWhenContextClosesBeforeStart tests the scenario where admitted work cannot leak during cleanup.
func TestOwnerReleasesPreparedWorkWhenContextClosesBeforeStart(t *testing.T) {
	t.Parallel()

	// Arrange preparation that finishes after the connection context closes.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	connectionContext, cancelConnection := context.WithCancelCause(t.Context())
	preparing := make(chan struct{})
	allowPreparation := make(chan struct{})
	prepared.EXPECT().Release()
	owner := NewOwner(connectionContext, delivery)
	startResult := make(chan error, 1)
	go func() {
		startResult <- owner.Start("operation-1", func() (Prepared[string, string], error) {
			close(preparing)
			<-allowPreparation
			return prepared, nil
		})
	}()
	<-preparing

	// Act by closing the connection before bounded preparation returns.
	cancelConnection(errors.New("connection closed"))
	close(allowPreparation)

	// Assert Start rejects admitted work and Release runs exactly once.
	require.ErrorIs(t, <-startResult, ErrClosed)
	owner.Close()
}

// TestOwnerCancellationBeforeRunSkipsPreparedWork tests the scenario where cancellation after acceptance claim prevents Run.
func TestOwnerCancellationBeforeRunSkipsPreparedWork(t *testing.T) {
	t.Parallel()

	// Arrange Accepted delivery held until targeted cancellation is observable.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	accepted := newAcknowledgement()
	delivery.EXPECT().Accepted("target").Return(accepted, nil)
	delivery.EXPECT().Running("target").Return(nil)
	delivery.EXPECT().Terminal("target", gomock.Any()).DoAndReturn(
		func(_ string, outcome Outcome[string]) (*Acknowledgement, error) {
			require.Equal(t, TerminalStateCanceled, outcome.State())
			return resolvedAcknowledgement(nil), nil
		},
	)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("target", preparedBy(prepared)))
	canceled := observeCancellation(owner, "target")
	result := make(chan TerminalState, 1)
	errResult := make(chan error, 1)

	// Act by canceling before Accepted delivery permits Prepared.Run.
	go func() {
		state, err := owner.CancelAndWait(t.Context(), "target")
		result <- state
		errResult <- err
	}()
	<-canceled
	accepted.resolve(nil)

	// Assert Run is never called and cancellation completes after its terminal event.
	require.Equal(t, TerminalStateCanceled, <-result)
	require.NoError(t, <-errResult)
	owner.Wait()
}

// TestOwnerCompletedOutcomeWinsCancellation tests the scenario where committed completion remains terminal after cancellation.
func TestOwnerCompletedOutcomeWinsCancellation(t *testing.T) {
	t.Parallel()

	// Arrange work that observes cancellation but returns a completed outcome.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	runStarted := make(chan struct{})
	allowCompletion := make(chan struct{})
	delivery.EXPECT().Accepted("target").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("target").Return(nil)
	delivery.EXPECT().Terminal("target", gomock.Any()).DoAndReturn(
		func(_ string, outcome Outcome[string]) (*Acknowledgement, error) {
			require.Equal(t, TerminalStateCompleted, outcome.State())
			result, present := outcome.Result()
			require.True(t, present)
			require.Equal(t, "committed", result)
			return resolvedAcknowledgement(nil), nil
		},
	)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ Reporter[string]) Outcome[string] {
			close(runStarted)
			<-ctx.Done()
			<-allowCompletion
			return Completed("committed")
		},
	)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("target", preparedBy(prepared)))
	<-runStarted
	canceled := observeCancellation(owner, "target")
	result := make(chan TerminalState, 1)
	errResult := make(chan error, 1)

	// Act by canceling while work commits its completed outcome.
	go func() {
		state, err := owner.CancelAndWait(t.Context(), "target")
		result <- state
		errResult <- err
	}()
	<-canceled
	close(allowCompletion)

	// Assert the completed outcome wins and is returned by cancellation joining.
	require.Equal(t, TerminalStateCompleted, <-result)
	require.NoError(t, <-errResult)
	owner.Wait()
}

// TestOwnerFailedOutcomeWinsCancellation tests the scenario where committed failure remains terminal after cancellation.
func TestOwnerFailedOutcomeWinsCancellation(t *testing.T) {
	t.Parallel()

	// Arrange work that observes cancellation but returns a failed outcome.
	controller := gomock.NewController(t)
	delivery := NewMockDelivery[string, string](controller)
	prepared := NewMockPrepared[string, string](controller)
	runStarted := make(chan struct{})
	allowFailure := make(chan struct{})
	failureCause := errors.New("target work failed")
	delivery.EXPECT().Accepted("target").Return(resolvedAcknowledgement(nil), nil)
	delivery.EXPECT().Running("target").Return(nil)
	delivery.EXPECT().Terminal("target", gomock.Any()).DoAndReturn(
		func(_ string, outcome Outcome[string]) (*Acknowledgement, error) {
			require.Equal(t, TerminalStateFailed, outcome.State())
			require.Equal(t, "INTERNAL", outcome.Code())
			require.ErrorIs(t, outcome.Err(), failureCause)
			return resolvedAcknowledgement(nil), nil
		},
	)
	prepared.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ Reporter[string]) Outcome[string] {
			close(runStarted)
			<-ctx.Done()
			<-allowFailure
			return Failed[string]("INTERNAL", failureCause)
		},
	)
	prepared.EXPECT().Release()
	owner := NewOwner(t.Context(), delivery)
	require.NoError(t, owner.Start("target", preparedBy(prepared)))
	<-runStarted
	canceled := observeCancellation(owner, "target")
	result := make(chan TerminalState, 1)
	errResult := make(chan error, 1)

	// Act by canceling while work commits its failed outcome.
	go func() {
		state, err := owner.CancelAndWait(t.Context(), "target")
		result <- state
		errResult <- err
	}()
	<-canceled
	close(allowFailure)

	// Assert the failed outcome wins and is returned by cancellation joining.
	require.Equal(t, TerminalStateFailed, <-result)
	require.NoError(t, <-errResult)
	owner.Wait()
}

// observeCancellation wraps one owned cancellation function with a deterministic signal.
func observeCancellation[P, R any](owner *Owner[P, R], id string) <-chan struct{} {
	owner.mutex.Lock()
	defer owner.mutex.Unlock()
	owned := owner.operations[id]
	canceled := make(chan struct{})
	cancel := owned.cancel
	owned.cancel = func(cause error) {
		cancel(cause)
		close(canceled)
	}
	return canceled
}

// preparedBy returns bounded admission that succeeds with controlled work.
func preparedBy[P, R any](prepared Prepared[P, R]) func() (Prepared[P, R], error) {
	return func() (Prepared[P, R], error) { return prepared, nil }
}

// resolvedAcknowledgement constructs a completed acknowledgement for lifecycle tests.
func resolvedAcknowledgement(err error) *Acknowledgement {
	ack := newAcknowledgement()
	ack.resolve(err)
	return ack
}
