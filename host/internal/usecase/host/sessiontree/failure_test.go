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

// logicalModelRequestError exposes provider-neutral logical identity and all acquired causes in tests.
type logicalModelRequestError struct {
	// code is the stable logical failure code.
	code string
	// cause contains every acquired model-request cause.
	cause error
}

// Error exposes the complete acquired failure text.
func (failure *logicalModelRequestError) Error() string { return failure.cause.Error() }

// FailureCode exposes the provider-neutral logical failure code.
func (failure *logicalModelRequestError) FailureCode() string { return failure.code }

// Unwrap preserves every acquired model-request cause.
func (failure *logicalModelRequestError) Unwrap() error { return failure.cause }

// TestNavigateSummaryFailuresNeverCommit verifies selection, logical, model, and cancellation failures preserve state.
func TestNavigateSummaryFailuresNeverCommit(t *testing.T) {
	t.Parallel()

	providerCause := errors.New("provider failed")
	handlerCause := errors.New("retry handler failed")
	earlierCause := errors.New("earlier attempt failed")
	logicalFailure := &logicalModelRequestError{
		code: "EXTENSION_FAILED", cause: errors.Join(providerCause, handlerCause, earlierCause),
	}
	tests := []struct {
		name     string
		failure  error
		cancel   bool
		expected error
	}{
		{
			name:     "model unavailable during concurrent cancellation",
			failure:  modelRequestFailureError{code: selectionCodeNotFound},
			cancel:   true,
			expected: ErrModelUnavailable,
		},
		{
			name:     "reasoning unavailable during concurrent cancellation",
			failure:  modelRequestFailureError{code: selectionCodeReasoningUnsupported},
			cancel:   true,
			expected: ErrModelUnavailable,
		},
		{
			name:     "credential unavailable during concurrent cancellation",
			failure:  modelRequestFailureError{code: selectionCodeCredentialUnavailable},
			cancel:   true,
			expected: ErrCredentialUnavailable,
		},
		{
			name:     "logical failure during concurrent navigation cancellation",
			failure:  logicalFailure,
			cancel:   true,
			expected: ErrModelFailed,
		},
		{
			name:     "independent model failure during concurrent cancellation",
			failure:  errors.New("uncategorized provider failure"),
			cancel:   true,
			expected: ErrModelFailed,
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
			modelSelection := NewMockModelSelection(controller)
			handlers := NewMockRuntime(controller)
			service := New(active, modelSelection, models, handlers)
			selection := model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			active.EXPECT().Tree().Return(navigationTree(t, time.Unix(1, 0).UTC()))
			active.EXPECT().SessionID().Return("session")
			modelSelection.EXPECT().ActiveSelection().Return(selection)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			models.EXPECT().RequestConfigured(
				gomock.Any(), selection, gomock.Any(), gomock.Any(), gomock.Any(),
			).DoAndReturn(
				func(
					_ context.Context,
					_ model.Selection,
					_ string,
					_ []agent.HistoryEntry,
					_ func(int64, int64, time.Duration, string) error,
				) (model.Response, error) {
					if test.cancel {
						cancel()
					}
					return model.Response{}, test.failure
				},
			)

			// Act by requesting built-in summarization.
			_, err := navigateTreeForTest(t, service, ctx, NavigationRequest{
				TargetEntryID: "user", SummaryMode: SummaryModeSummarize,
				CustomFocus: mo.None[string](),
			})

			// Assert the public navigation category and original source identity survive without a commit call.
			require.ErrorIs(t, err, test.expected)
			require.ErrorIs(t, err, test.failure)
			if test.failure == logicalFailure {
				var retained ModelRequestFailure
				require.ErrorAs(t, err, &retained)
				require.Equal(t, logicalFailure.FailureCode(), retained.FailureCode())
				require.ErrorIs(t, err, providerCause)
				require.ErrorIs(t, err, handlerCause)
				require.ErrorIs(t, err, earlierCause)
			}
		})
	}
}
