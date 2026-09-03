//go:build !integration

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

// modelRequestFailureError exposes one model request failure classification in tests.
type modelRequestFailureError struct {
	// code is the stable configured-selection failure code.
	code string
}

// Error implements error.
func (failure modelRequestFailureError) Error() string { return failure.code }

// SelectionCode exposes the configured-selection failure code.
func (failure modelRequestFailureError) SelectionCode() string { return failure.code }

// TestNavigateSummaryFailuresNeverCommit verifies selection, credential, model, and cancellation failures preserve
// active state.
func TestNavigateSummaryFailuresNeverCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		failure  error
		cancel   bool
		expected error
	}{
		{
			name:     "model unavailable",
			failure:  modelRequestFailureError{code: selectionCodeNotFound},
			cancel:   false,
			expected: sessionnavigation.ErrModelUnavailable,
		},
		{
			name:     "reasoning unavailable",
			failure:  modelRequestFailureError{code: selectionCodeReasoningUnsupported},
			cancel:   false,
			expected: sessionnavigation.ErrModelUnavailable,
		},
		{
			name:     "credential unavailable",
			failure:  modelRequestFailureError{code: selectionCodeCredentialUnavailable},
			cancel:   false,
			expected: sessionnavigation.ErrCredentialUnavailable,
		},
		{
			name:     "model failed",
			failure:  errors.New("provider failed"),
			cancel:   false,
			expected: sessionnavigation.ErrModelFailed,
		},
		{name: "canceled", failure: context.Canceled, cancel: true, expected: context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one abandoned path and a model request that fails before commit.
			controller := gomock.NewController(t)
			active := NewMockActiveSession(controller)
			models := NewMockModelRequester(controller)
			handlers := NewMockRuntime(controller)
			service := New(active, models, handlers)
			selection := model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()))
			active.EXPECT().SessionID().Return("session")
			models.EXPECT().ActiveSelection().Return(selection)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			models.EXPECT().Request(gomock.Any(), selection, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ model.Selection, _ string, _ []agent.HistoryEntry) (model.Response, error) {
					if test.cancel {
						cancel()
					}
					return model.Response{}, test.failure
				},
			)

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
