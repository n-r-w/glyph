//go:build integration

package programmatic

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestFailedSelectionDeliveryValidatesRetainedResult verifies typed output and complete boundary diagnoses.
func TestFailedSelectionDeliveryValidatesRetainedResult(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"selection", "mismatched-request", "missing-selection"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange an acknowledged writer and a failed operation retaining selection data.
			captured := make(chan *programmaticpb.OpenResponse, 1)
			writer, err := operation.NewWriter(func(response *programmaticpb.OpenResponse) error {
				captured <- response
				return nil
			})
			require.NoError(t, err)
			writerResult := make(chan error, 1)
			go func() { writerResult <- writer.Run(t.Context()) }()
			t.Cleanup(func() {
				writer.Close()
				<-writerResult
			})
			registry := newTargetRegistry()
			kind := CommandSelectModel
			if scenario == "mismatched-request" {
				kind = CommandGetModels
			}
			registry.add("selection", kind)
			delivery := &streamDelivery{context: t.Context(), writer: writer, registry: registry, fail: func(error) {}}
			selection := mo.Some(model.Selection{
				Provider: "provider", Model: "committed", ReasoningChoice: model.ReasoningChoiceHigh,
			})
			if scenario == "missing-selection" {
				selection = mo.None[model.Selection]()
			}
			result := Response{
				OperationID: "selection", Kind: ResponseModelSelection, State: mo.None[RunStateResult](),
				Messages: nil, Models: mo.None[ModelsResult](), Selection: selection, SelectionIssues: nil,
				SessionInfo: mo.None[session.Info](), Sessions: nil, SessionEntries: nil,
				SessionStatistics: mo.None[session.Statistics](), SessionTree: mo.None[SessionTree](),
				TreeNavigation: mo.None[TreeNavigationResult](), Replacement: mo.None[SessionReplacement](),
				Rejection: mo.None[Rejection](), CancelTargetState: mo.None[operation.TerminalState](),
				RetryPolicy: mo.None[RetryPolicy](), CompactionCanceled: mo.None[bool](),
				CompactionError: mo.None[string](), CompactionFailureCode: mo.None[string](),
			}
			cause := errors.New("complete committed selection observer failure")

			// Act through the actual Programmatic terminal delivery adapter.
			_, err = delivery.Terminal(
				"selection",
				operation.FailedWithResult(FailureCodeExtensionFailed, cause, result),
			)

			// Assert matching typed data reaches one Failed terminal and malformed data retains the failure in its diagnosis.
			if scenario == "selection" {
				require.NoError(t, err)
				failed := (<-captured).GetEvent().GetFailed()
				require.Equal(t, FailureCodeExtensionFailed, failed.GetFailure().GetCode())
				require.Equal(t, cause.Error(), failed.GetFailure().GetMessage())
				require.Equal(t, "committed", failed.GetResult().GetModelSelection().GetSelection().GetModelId())
			} else {
				require.Error(t, err)
				require.ErrorIs(t, err, cause)
				require.Contains(t, err.Error(), cause.Error())
				require.Empty(t, captured)
			}
		})
	}
}
