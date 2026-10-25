//go:build integration

package programmatic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
)

// TestCommittedCompactionTerminalCancellation verifies retained results under owning and mixed cancellation.
func TestCommittedCompactionTerminalCancellation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		independent bool
		deadline    bool
	}{
		{name: "owning cancellation", independent: false, deadline: false},
		{name: "owning deadline", independent: false, deadline: true},
		{name: "mixed failure", independent: true, deadline: false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a committed response and the context of the prepared operation's owner.
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			cause := context.Canceled
			if scenario.deadline {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithDeadline(t.Context(), time.Time{})
				t.Cleanup(deadlineCancel)
				cause = context.DeadlineExceeded
			}
			independent := errors.New("independent committed compaction failure")
			if scenario.independent {
				cause = errors.Join(cause, compactionTerminalError{cause: independent})
			}
			mockController := gomock.NewController(t)
			compactor := NewMockCompactor(mockController)
			entry := session.Entry{
				ID: compactionTerminalEntryID, ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
				Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
				EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
				Compaction: mo.Some(session.CompactionEntry{
					Summary:          "retained summary",
					FirstKeptEntryID: compactionTerminalKeptID,
					Source: session.CompactionSource{
						ExtensionID: mo.Some(compactionTerminalExtensionID),
						Requests:    nil,
					},
					Details: mo.None[[]byte](),
				}),
			}
			compactor.EXPECT().CompactProgrammatic(ctx, mo.None[string]()).Return(
				ManualCompactionResult{Committed: mo.Some(entry), Canceled: false}, cause,
			)
			output := NewMockRunOutput(mockController)
			output.EXPECT().ActiveOperation().Return("")
			service := New(nil, nil, nil, nil, nil, nil, output, nil, nil)
			require.NoError(t, service.BindCompactor(compactor))
			prepared := &commandPrepared{
				service: service,
				command: controller.Command{
					OperationID:            compactionTerminalOperationID,
					Kind:                   controller.CommandCompact,
					UserText:               mo.None[string](),
					ProviderID:             mo.None[model.ProviderID](),
					ModelID:                mo.None[model.ID](),
					ReasoningChoice:        mo.None[model.ReasoningChoice](),
					SessionID:              mo.None[session.ID](),
					SessionName:            mo.None[string](),
					TargetEntryID:          mo.None[string](),
					SummaryMode:            controller.SummaryModeNoSummary,
					CustomFocus:            mo.None[string](),
					EntryLabel:             mo.None[string](),
					RetryEnabled:           mo.None[bool](),
					CompactionInstructions: mo.None[string](),
				},
				selection: nil, release: func() {},
			}

			// Act with a real progress reporter while preserving the prepared owner's cancellation context.
			outcome := executeCompactionTerminal(t, prepared, ctx)

			// Assert a committed owning cancellation completes, while independent failure retains priority.
			expected := operation.TerminalStateCompleted
			if scenario.independent {
				expected = operation.TerminalStateFailed
				require.ErrorIs(t, outcome.Err(), independent)
				require.Equal(t, controller.FailureCodeExtensionFailed, outcome.Code())
			}
			require.Equal(t, expected, outcome.State())
			require.ErrorIs(t, outcome.SourceError(), cause)
			result, present := outcome.Result()
			require.True(t, present)
			require.Len(t, result.SessionEntries, 1)
			require.Equal(t, compactionTerminalEntryID, result.SessionEntries[0].ID)
			require.Equal(t, cause.Error(), result.CompactionError.OrEmpty())
		})
	}
}

const (
	// compactionTerminalOperationID identifies the prepared compaction under test.
	compactionTerminalOperationID = "compaction-terminal"
	// compactionTerminalEntryID identifies the actual retained marker.
	compactionTerminalEntryID = "compaction-marker"
	// compactionTerminalKeptID identifies the retained suffix boundary.
	compactionTerminalKeptID = "compaction-kept"
	// compactionTerminalExtensionID identifies the supplying policy.
	compactionTerminalExtensionID = "compaction-policy"
)

// compactionTerminalError preserves an independent categorized postcommit failure.
type compactionTerminalError struct {
	// cause is the original failure.
	cause error
}

// Error exposes the complete original error text.
func (e compactionTerminalError) Error() string { return e.cause.Error() }

// Unwrap preserves original cause identity.
func (e compactionTerminalError) Unwrap() error { return e.cause }

// CompactionFailureCode identifies the participating extension failure.
func (compactionTerminalError) CompactionFailureCode() string {
	return controller.FailureCodeExtensionFailed
}

// executeCompactionTerminal supplies a live reporter and observes the prepared public terminal.
func executeCompactionTerminal(
	t *testing.T,
	prepared *commandPrepared,
	ctx context.Context,
) operation.Outcome[controller.Response] {
	t.Helper()
	control := gomock.NewController(t)
	writer, err := operation.NewWriter(func(struct{}) error { return nil })
	require.NoError(t, err)
	writerDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(t.Context()) }()
	acknowledgement := func() *operation.Acknowledgement {
		ack, enqueueErr := writer.EnqueueAcknowledged(struct{}{}, nil)
		require.NoError(t, enqueueErr)
		return ack
	}
	delivery := operationmock.NewMockOperationDelivery[controller.OperationProgress, controller.Response](control)
	delivery.EXPECT().Accepted(compactionTerminalOperationID).Return(acknowledgement(), nil)
	delivery.EXPECT().Running(compactionTerminalOperationID).Return(nil)
	delivery.EXPECT().Progress(compactionTerminalOperationID, gomock.Any()).Return(nil)
	result := make(chan operation.Outcome[controller.Response], 1)
	delivery.EXPECT().Terminal(compactionTerminalOperationID, gomock.Any()).DoAndReturn(
		func(_ string, outcome operation.Outcome[controller.Response]) (*operation.Acknowledgement, error) {
			result <- outcome
			return acknowledgement(), nil
		})
	owner, err := operation.NewOwner[controller.OperationProgress, controller.Response](t.Context(), delivery)
	require.NoError(t, err)
	t.Cleanup(func() { owner.Close(); writer.Close(); require.ErrorIs(t, <-writerDone, context.Canceled) })
	wrapper := operationmock.NewMockOperationPrepared[controller.OperationProgress, controller.Response](control)
	wrapper.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			reporter operation.Reporter[controller.OperationProgress],
		) operation.Outcome[controller.Response] {
			return prepared.Run(ctx, reporter)
		})
	wrapper.EXPECT().Release().Do(prepared.Release)
	require.NoError(t, owner.Start(compactionTerminalOperationID,
		func() (operation.Prepared[controller.OperationProgress, controller.Response], error) {
			return wrapper, nil
		}))
	owner.Wait()
	return <-result
}
