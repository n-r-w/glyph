//go:build integration

package ui

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/authentication"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	"github.com/n-r-w/glyph/internal/testsupport/operationmock"
)

// TestManualCompactionRunsThroughUIOperationOwner verifies admission, progress, completion, and release.
func TestManualCompactionRunsThroughUIOperationOwner(t *testing.T) {
	t.Parallel()
	// Arrange a production UI session and operation owner around one committed compaction result.
	controller := gomock.NewController(t)
	compactor := NewMockCompactor(controller)
	gate := NewMockGate(controller)
	released := false
	gate.EXPECT().TryAcquire().Return(func() { released = true }, true)
	committed := uiCompactionEntry()
	observerCause := uiIntegrationError{
		code: controllerui.FailureCodeExtensionFailed, cause: errors.New("observer failed after commit"),
	}
	compactor.EXPECT().CompactManual(gomock.Any(), mo.Some("preserve decisions")).Return(
		ManualCompactionResult{Committed: mo.Some(committed), Canceled: false}, observerCause,
	)
	service := NewSession(nil, nil, nil, nil, nil, nil, gate, nil, nil, nil)
	service.setOperationAvailability(AvailabilityIdle)
	require.NoError(t, service.BindCompactor(compactor))
	command := controllerui.Command{
		OperationID: "compact", Kind: controllerui.CommandCompact,
		AuthenticationMethod: authentication.MethodUnspecified, Text: mo.None[string](),
		ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[model.ReasoningChoice](), SessionID: mo.None[string](),
		SessionName: mo.None[string](), TargetEntryID: mo.None[string](),
		SummaryMode: controllerui.SummaryModeNoSummary, CustomFocus: mo.None[string](),
		EntryLabel: mo.None[string](), RetryEnabled: mo.None[bool](),
		CompactionInstructions: mo.Some("preserve decisions"),
	}

	acknowledgements := operation.NewWriter(func(struct{}) error { return nil })
	writerResult := make(chan error, 1)
	go func() { writerResult <- acknowledgements.Run(t.Context()) }()
	newAcknowledgement := func() *operation.Acknowledgement {
		acknowledgement, err := acknowledgements.EnqueueAcknowledged(struct{}{}, nil)
		require.NoError(t, err)
		return acknowledgement
	}
	delivery := operationmock.NewMockOperationDelivery[controllerui.Frame, controllerui.Frame](controller)
	delivery.EXPECT().Accepted("compact").Return(newAcknowledgement(), nil)
	delivery.EXPECT().Running("compact").Return(nil)
	delivery.EXPECT().Progress("compact", gomock.Any()).DoAndReturn(
		func(_ string, progress controllerui.Frame) error {
			require.Equal(t, controllerui.FrameCompactionProgress, progress.Kind)
			require.Equal(t, mo.Some("running"), progress.CompactionStage)
			require.True(t, progress.NextInput.IsNone())
			return nil
		},
	)
	terminal := make(chan operation.Outcome[controllerui.Frame], 1)
	delivery.EXPECT().Terminal("compact", gomock.Any()).DoAndReturn(
		func(_ string, outcome operation.Outcome[controllerui.Frame]) (*operation.Acknowledgement, error) {
			terminal <- outcome
			return newAcknowledgement(), nil
		},
	)
	owner := operation.NewOwner[controllerui.Frame, controllerui.Frame](t.Context(), delivery)
	t.Cleanup(func() {
		owner.Close()
		acknowledgements.Close()
		<-writerResult
	})

	// Act through accepted preparation and operation-owned progress delivery.
	err := owner.Start("compact", func() (operation.Prepared[controllerui.Frame, controllerui.Frame], error) {
		return service.Prepare(t.Context(), command)
	})
	require.NoError(t, err)
	owner.Wait()

	// Assert the settled committed result keeps complete categorized error text and releases the gate.
	outcome := <-terminal
	require.Equal(t, operation.TerminalStateCompleted, outcome.State())
	frame, present := outcome.Result()
	require.True(t, present)
	require.Len(t, frame.SessionEntries, 1)
	require.Equal(t, mo.Some(false), frame.CompactionCanceled)
	require.True(t, frame.NextInput.IsNone())
	require.Equal(t, observerCause.Error(), frame.CompactionError.OrEmpty())
	require.Equal(t, controllerui.FailureCodeExtensionFailed, frame.CompactionFailureCode.OrEmpty())
	require.True(t, released)
}

// uiIntegrationError keeps one stable category and complete post-commit cause.
type uiIntegrationError struct {
	// code is the stable public category.
	code string
	// cause is the complete underlying failure.
	cause error
}

// Error returns the complete underlying failure text.
func (e uiIntegrationError) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying failure.
func (e uiIntegrationError) Unwrap() error { return e.cause }

// CompactionFailureCode returns the stable direct category consumed by the UI mapping.
func (e uiIntegrationError) CompactionFailureCode() string { return e.code }

// uiCompactionEntry creates one complete committed compaction marker for public mapping.
func uiCompactionEntry() session.Entry {
	return session.Entry{
		ID: "compaction", ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
		}),
	}
}
