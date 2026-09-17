//go:build integration

package modelselection

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
)

// TestSelectionOwnerCancellationTerminatesObserverAndReleasesAdmission verifies committed observer liveness.
func TestSelectionOwnerCancellationTerminatesObserverAndReleasesAdmission(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange one committed selection whose cooperative observer blocks until cancellation or test cleanup.
		gomockController := gomock.NewController(t)
		catalog := NewMockCatalog(gomockController)
		publisher := NewMockPublisher(gomockController)
		observer := NewMockObserver(gomockController)
		delivery := NewMockDelivery[controller.OperationProgress, controller.Response](gomockController)
		preceding := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
		committed := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
		catalog.EXPECT().ResolveReasoning(committed.ReasoningChoice).Return(committed, nil)
		catalog.EXPECT().ValidateSelection(gomock.Any(), committed).Return(nil)
		catalog.EXPECT().CommitSelection(gomock.Any(), committed).Return(preceding, committed, nil)
		publisher.EXPECT().PublishSelection(committed).Return(nil, nil)
		observerStarted := make(chan struct{})
		cleanupObserver := make(chan struct{})
		observer.EXPECT().ObserveSelection(gomock.Any(), ObservationKindReasoning, gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ ObservationKind, _ SelectionChange) []Issue {
				close(observerStarted)
				var observerErr error
				select {
				case <-ctx.Done():
					observerErr = context.Cause(ctx)
				case <-cleanupObserver:
					observerErr = errors.New("observer required test cleanup instead of cancellation")
				}
				return []Issue{{
					ExtensionID: "extension", HandlerID: "observer",
					Code: IssueCodeObserverError, Err: observerErr,
				}}
			},
		)
		service := New(catalog, publisher)
		service.BindObserver(observer)
		runOutput := hostprogrammatic.NewMockRunOutput(gomockController)
		runOutput.EXPECT().ActiveOperation().Return("")
		initiator := hostprogrammatic.New(nil, nil, nil, nil, nil, nil, runOutput, service)
		prepared, err := initiator.Prepare(
			t.Context(), reasoningSelectionCommand("selection", committed.ReasoningChoice),
		)
		require.NoError(t, err)
		terminal := make(chan operation.Outcome[controller.Response], 1)
		acknowledgements := operation.NewWriter(func(struct{}) error { return nil })
		writerResult := make(chan error, 1)
		go func() { writerResult <- acknowledgements.Run(t.Context()) }()
		acceptedAck, err := acknowledgements.EnqueueAcknowledged(struct{}{}, nil)
		require.NoError(t, err)
		terminalAck, err := acknowledgements.EnqueueAcknowledged(struct{}{}, nil)
		require.NoError(t, err)
		delivery.EXPECT().Accepted("selection").Return(acceptedAck, nil)
		delivery.EXPECT().Running("selection").Return(nil)
		delivery.EXPECT().Terminal("selection", gomock.Any()).DoAndReturn(
			func(_ string, outcome operation.Outcome[controller.Response]) (*operation.Acknowledgement, error) {
				terminal <- outcome
				return terminalAck, nil
			},
		)
		owner := operation.NewOwner(t.Context(), delivery)
		require.NoError(t, owner.Start("selection", func() (
			operation.Prepared[controller.OperationProgress, controller.Response], error,
		) {
			return prepared, nil
		}))
		<-observerStarted
		cancelResult := make(chan operation.TerminalState, 1)
		cancelErr := make(chan error, 1)
		go func() {
			state, cancellationErr := owner.CancelAndWait(t.Context(), "selection")
			cancelResult <- state
			cancelErr <- cancellationErr
		}()

		// Act by waiting until cancellation either terminates work or leaves all work durably blocked.
		synctest.Wait()
		completedBeforeCleanup := false
		var canceledState operation.TerminalState
		var cancellationErr error
		select {
		case canceledState = <-cancelResult:
			cancellationErr = <-cancelErr
			completedBeforeCleanup = true
		default:
		}
		runOutput.EXPECT().ActiveOperation().Return("")
		catalog.EXPECT().ResolveReasoning(preceding.ReasoningChoice).Return(preceding, nil).AnyTimes()
		next, nextErr := initiator.Prepare(
			t.Context(), reasoningSelectionCommand("next-selection", preceding.ReasoningChoice),
		)
		if next != nil {
			next.Release()
		}

		// Release a broken implementation only after observing its blocked operation and retained reservation.
		close(cleanupObserver)
		synctest.Wait()
		if !completedBeforeCleanup {
			canceledState = <-cancelResult
			cancellationErr = <-cancelErr
		}
		owner.Wait()
		outcome := <-terminal
		acknowledgements.Close()
		require.NoError(t, <-writerResult)

		// Assert cancellation itself terminates the observer, preserves completion, and releases admission.
		assert.True(t, completedBeforeCleanup)
		assert.Equal(t, operation.TerminalStateCompleted, canceledState)
		require.NoError(t, cancellationErr)
		require.NoError(t, nextErr)
		assert.Equal(t, operation.TerminalStateCompleted, outcome.State())
		require.ErrorIs(t, outcome.SourceError(), context.Canceled)
		result, present := outcome.Result()
		require.True(t, present)
		assert.Equal(t, committed, result.Selection.MustGet())
		require.Len(t, result.SelectionIssues, 1)
		assert.Equal(t, controller.OperationIssueObserverError, result.SelectionIssues[0].Code)
	})
}

// reasoningSelectionCommand creates one complete Programmatic reasoning-selection command.
func reasoningSelectionCommand(operationID string, choice model.ReasoningChoice) controller.Command {
	return controller.Command{
		OperationID: operationID, Kind: controller.CommandSelectReasoningChoice,
		UserText: mo.None[string](), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](),
		ReasoningChoice: mo.Some(choice), SessionID: mo.None[session.ID](), SessionName: mo.None[string](),
		TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary,
		CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	}
}
