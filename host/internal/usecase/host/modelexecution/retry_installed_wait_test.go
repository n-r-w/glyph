//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestServiceCancellationInterruptsInstalledRetryWait checks owning cancellation after the worker blocks on backoff.
func TestServiceCancellationInterruptsInstalledRetryWait(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Arrange synchronous ports and a finite repeated failure so cancellation regressions terminate observably.
		const providerID, modelID = "provider", "model"
		controller := gomock.NewController(t)
		catalog := NewMockCatalogResolver(controller)
		provider := NewMockProviderAttempt(controller)
		selection := model.Selection{Provider: providerID, Model: modelID, ReasoningChoice: model.ReasoningChoiceOff}
		catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
			Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
		}, nil)
		providerCause := errors.New("handled transient failure")
		attempts := 0
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, ProviderRequest, StreamHandler) error {
				attempts++
				return &ProviderFailureError{
					Classification: ProviderFailureTransient,
					RetryDelay:     mo.None[time.Duration](),
					Cause:          providerCause,
				}
			},
		).AnyTimes()
		service := New(catalog, RetryPolicy{
			Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Hour}, MaxProviderDelay: 30 * time.Second,
		}, nil, nil, newTestConversationObservations(t))
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		cancellationCause := errors.New("owning operation stopped")
		progress := make(chan struct{}, 1)
		finished := make(chan struct{})
		var response model.Response
		var err error
		startedAt := time.Now()

		// Act in a worker whose progress callback only signals and returns to the production wait.
		go func() {
			response, err = service.RequestConfigured(ctx, selection, "", nil,
				func(int64, int64, time.Duration, string) error {
					progress <- struct{}{}
					return nil
				})
			close(finished)
		}()
		<-progress
		synctest.Wait()

		// Assert durable blocking before cancellation, without advancing the scheduled delay.
		select {
		case <-finished:
			t.Fatal("request completed before owning cancellation")
		default:
		}
		require.Equal(t, startedAt, time.Now())
		require.Equal(t, 1, attempts)

		// Act from the owning goroutine after the worker has installed its retry wait.
		cancel(cancellationCause)
		<-finished

		// Assert immediate completion, one attempt, both complete causes, and current cancellation identity.
		assert.Equal(t, time.Duration(0), time.Since(startedAt), "owning cancellation must interrupt the retry delay")
		assert.Equal(t, 1, attempts, "owning cancellation must prevent a provider repeat")
		require.ErrorIs(t, err, cancellationCause)
		require.ErrorIs(t, err, providerCause)
		assert.Contains(t, err.Error(), cancellationCause.Error())
		assert.Contains(t, err.Error(), providerCause.Error())
		assert.True(t, errtree.IsCurrentCancellation(err, func(cause error) bool {
			return errors.Is(cause, context.Canceled) || errors.Is(cause, cancellationCause)
		}))
		assert.True(t, response.Outcome.IsNone())
		assert.Equal(t, model.Response{}, response)
	})
}
