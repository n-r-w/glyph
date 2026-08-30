package sessiontree

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

// completionFailureError exposes one configured-completion classification in tests.
type completionFailureError struct {
	// code is the stable configured-selection failure code.
	code string
}

// Error implements error.
func (failure completionFailureError) Error() string { return failure.code }

// SelectionCode exposes the configured-selection failure code.
func (failure completionFailureError) SelectionCode() string { return failure.code }

// TestNavigateSummaryFailuresNeverCommit verifies selection, credential, model, and cancellation failures preserve active state.
func TestNavigateSummaryFailuresNeverCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		failure  error
		cancel   bool
		expected error
	}{
		{name: "model unavailable", failure: completionFailureError{code: selectionCodeNotFound}, cancel: false, expected: sessionnavigation.ErrModelUnavailable},
		{name: "reasoning unavailable", failure: completionFailureError{code: selectionCodeReasoningUnsupported}, cancel: false, expected: sessionnavigation.ErrModelUnavailable},
		{name: "credential unavailable", failure: completionFailureError{code: selectionCodeCredentialUnavailable}, cancel: false, expected: sessionnavigation.ErrCredentialUnavailable},
		{name: "model failed", failure: errors.New("provider failed"), cancel: false, expected: sessionnavigation.ErrModelFailed},
		{name: "canceled", failure: context.Canceled, cancel: true, expected: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one abandoned path and a configured completion that fails before commit.
			controller := gomock.NewController(t)
			active := NewMockActiveSession(controller)
			models := NewMockModelCompleter(controller)
			handlers := NewMockHandlerRunner(controller)
			selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
			active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()))
			active.EXPECT().SessionID().Return("session")
			models.EXPECT().Selection().Return(selection)
			handlers.EXPECT().Handlers(HandlerKindRequest).Return(nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			models.EXPECT().CompleteConfigured(gomock.Any(), selection, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ model.Selection, _ string, _ []agent.HistoryEntry) (model.Response, error) {
					if test.cancel {
						cancel()
					}
					return model.Response{}, test.failure
				},
			)
			service := New(active, models, handlers)

			// Act by requesting built-in summarization.
			_, err := service.NavigateTree(ctx, sessionnavigation.Request{
				TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeSummarize,
				CustomFocus: mo.None[string](),
			})

			// Assert the classified terminal error is returned without any commit call.
			require.ErrorIs(t, err, test.expected)
		})
	}
}
