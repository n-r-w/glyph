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
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceConcurrentNonRetryableFailureKeepsCategoryAndAllCauses verifies cancellation cannot preempt classification.
func TestServiceConcurrentNonRetryableFailureKeepsCategoryAndAllCauses(t *testing.T) {
	t.Parallel()

	// Arrange a transient first attempt and a concurrent non-retryable failure and cancellation on the second.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	firstCause := errors.New("first transient failure")
	secondCause := errors.New("second non-retryable failure")
	ctx, cancel := context.WithCancel(t.Context())
	gomock.InOrder(
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
			Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: firstCause,
		}),
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, ProviderRequest, StreamHandler) error {
				cancel()
				return &ProviderFailureError{
					Classification: ProviderFailureNonRetryable,
					RetryDelay:     mo.None[time.Duration](),
					Cause:          secondCause,
				}
			},
		),
	)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)

	// Act through one configured request.
	_, err := service.RequestConfigured(ctx, selection, "", nil, nil)

	// Assert classification wins and every acquired cause remains inspectable.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureModelFailed, failure.Category)
	require.ErrorIs(t, err, firstCause)
	require.ErrorIs(t, err, secondCause)
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceConcurrentProviderTimeoutKeepsExhaustionAndAllCauses verifies typed source identity precedes purity.
func TestServiceConcurrentProviderTimeoutKeepsExhaustionAndAllCauses(t *testing.T) {
	t.Parallel()

	// Arrange one handled transient and a final provider timeout acquired with caller cancellation.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	firstCause := errors.New("first transient failure")
	ctx, cancel := context.WithCancel(t.Context())
	gomock.InOrder(
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
			Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: firstCause,
		}),
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, ProviderRequest, StreamHandler) error {
				cancel()
				return &ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
					Cause: context.DeadlineExceeded,
				}
			},
		),
	)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)

	// Act through one configured request.
	_, err := service.RequestConfigured(ctx, selection, "", nil, nil)

	// Assert typed exhaustion and the prior failure, provider timeout, and caller cancellation all survive.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureRetryExhausted, failure.Category)
	require.ErrorIs(t, err, firstCause)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceConcurrentTransientExhaustionKeepsCategoryAndAllCauses verifies cancellation cannot hide exhaustion.
func TestServiceConcurrentTransientExhaustionKeepsCategoryAndAllCauses(t *testing.T) {
	t.Parallel()

	// Arrange one handled transient and a final transient acquired with caller cancellation.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	firstCause := errors.New("first transient failure")
	lastCause := errors.New("last transient failure")
	ctx, cancel := context.WithCancel(t.Context())
	gomock.InOrder(
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
			Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: firstCause,
		}),
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, ProviderRequest, StreamHandler) error {
				cancel()
				return &ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: lastCause,
				}
			},
		),
	)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)

	// Act through one configured request.
	_, err := service.RequestConfigured(ctx, selection, "", nil, nil)

	// Assert exhausted retry classification and every acquired cause remain inspectable.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureRetryExhausted, failure.Category)
	require.ErrorIs(t, err, firstCause)
	require.ErrorIs(t, err, lastCause)
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceCancellationPreservesIndependentDeliveryFailure verifies cancellation cannot hide callback failure.
func TestServiceCancellationPreservesIndependentDeliveryFailure(t *testing.T) {
	t.Parallel()

	// Arrange a provider that emits content as caller cancellation arrives.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	ctx, cancel := context.WithCancel(t.Context())
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			cancel()
			return handle(StreamEvent{
				Kind: StreamEventTextDelta, Position: mo.Some(0),
				Content: mo.Some(model.Content{
					Kind: model.ContentText, Text: mo.Some("partial"), Final: false,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}),
				Delta: mo.Some("partial"), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			})
		},
	)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Second}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)
	independent := errors.New("deliver model content")

	// Act while the logical stream callback has an independent failure.
	err := service.Stream(ctx, agentrun.ModelRequest{
		Instructions: "", Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice,
		History: nil, Tools: nil,
	}, func(agentrun.StreamEvent) error { return independent })

	// Assert caller cancellation and the independent delivery failure both survive.
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, independent)
}

// TestServiceCustomCancellationDuringRetryDelayPreservesCause verifies backoff returns the caller's exact cause.
func TestServiceCustomCancellationDuringRetryDelayPreservesCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Arrange one transient failure and a custom cancellation cause before replacement.
		controller := gomock.NewController(t)
		catalog := NewMockCatalogResolver(controller)
		provider := NewMockProviderAttempt(controller)
		selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
		catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
			Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
		}, nil)
		transientCause := errors.New("handled transient failure")
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
			Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: transientCause,
		})
		service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
			Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Hour}, MaxProviderDelay: 30 * time.Second,
		}, nil, nil)
		ctx, cancel := context.WithCancelCause(t.Context())
		cancellationCause := errors.New("owning operation stopped")

		// Act by canceling with an explicit cause before backoff starts the replacement attempt.
		response, err := service.RequestConfigured(
			ctx,
			selection,
			"",
			nil,
			func(int64, int64, time.Duration, string) error {
				cancel(cancellationCause)
				return nil
			},
		)

		// Assert the exact cancellation cause survives and intermediate response state is absent.
		require.ErrorIs(t, err, cancellationCause)
		assert.NotErrorIs(t, err, transientCause)
		assert.True(t, response.Outcome.IsNone())
	})
}

// TestServiceCancellationDuringRetryProgressIsPureAbort verifies output cancellation stays an abort.
func TestServiceCancellationDuringRetryProgressIsPureAbort(t *testing.T) {
	t.Parallel()

	// Arrange one transient failure and caller cancellation reported by progress delivery.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	transientCause := errors.New("handled transient failure")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: transientCause,
	})
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Hour}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())

	// Act by returning pure cancellation from the configured-operation progress boundary.
	_, err := service.RequestConfigured(ctx, selection, "", nil, func(int64, int64, time.Duration, string) error {
		cancel()
		return context.Canceled
	})

	// Assert handled retry failures stay diagnostic-only for a pure caller abort.
	require.ErrorIs(t, err, context.Canceled)
	assert.NotErrorIs(t, err, transientCause)
}

// TestServiceCancellationDuringRetryDelayStopsReplacementAttempt verifies cancelable production delay.
func TestServiceCancellationDuringRetryDelayStopsReplacementAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Arrange one transient failure and cancellation immediately after retry progress.
		controller := gomock.NewController(t)
		catalog := NewMockCatalogResolver(controller)
		provider := NewMockProviderAttempt(controller)
		selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
		catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
			Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
		}, nil)
		transientCause := errors.New("temporary source failure")
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(&ProviderFailureError{
			Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: transientCause,
		})
		service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
			Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Hour}, MaxProviderDelay: 30 * time.Second,
		}, nil, nil)
		ctx, cancel := context.WithCancel(t.Context())

		// Act by canceling after progress and before the replacement attempt.
		_, err := service.RequestConfigured(ctx, selection, "", nil, func(int64, int64, time.Duration, string) error {
			cancel()
			return nil
		})

		// Assert pure cancellation ends execution while the handled transient remains diagnostic-only.
		require.ErrorIs(t, err, context.Canceled)
		assert.NotErrorIs(t, err, transientCause)
	})
}

// TestExecuteUntypedTimeoutKeepsInternalIdentity verifies an acquired local timeout is not a pure cancellation.
func TestExecuteUntypedTimeoutKeepsInternalIdentity(t *testing.T) {
	t.Parallel()

	// Arrange one provider adapter failure that uses a cancellation sentinel without caller cancellation.
	controller := gomock.NewController(t)
	provider := NewMockProviderAttempt(controller)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(context.DeadlineExceeded)
	service := New(NewMockCatalogResolver(controller), NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)
	// Act through the shared logical execution state machine.
	_, err := service.execute(
		t.Context(), provider, ProviderRequest{}, nil, nil, errors.New("terminal response missing"),
		"terminal response missing",
	)

	// Assert the sentinel remains a cause under provider-neutral INTERNAL identity.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureInternal, failure.Category)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestExecuteRetryProgressTimeoutKeepsInternalIdentity verifies delivery timeout cannot erase its acquired causes.
func TestExecuteRetryProgressTimeoutKeepsInternalIdentity(t *testing.T) {
	t.Parallel()

	// Arrange a transient provider timeout followed by a retry-progress delivery timeout.
	controller := gomock.NewController(t)
	provider := NewMockProviderAttempt(controller)
	providerCause := errors.New("provider transient failure")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: providerCause,
	})
	service := New(NewMockCatalogResolver(controller), NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)
	// Act with a delivery callback that returns a cancellation sentinel while the caller remains active.
	_, err := service.execute(
		t.Context(), provider, ProviderRequest{}, nil, func(RetryProgress) error {
			return context.DeadlineExceeded
		}, errors.New("terminal response missing"), "terminal response missing",
	)

	// Assert INTERNAL identity retains both the provider and progress-delivery failures.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureInternal, failure.Category)
	require.ErrorIs(t, err, providerCause)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestServiceDisabledRetryIgnoresProviderDelayDuringConcurrentCancellation verifies disabled-policy precedence.
func TestServiceDisabledRetryIgnoresProviderDelayDuringConcurrentCancellation(t *testing.T) {
	t.Parallel()

	for name, concurrentCancellation := range map[string]bool{
		"normal path":                false,
		"concurrent caller canceled": true,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange the same disabled policy and transient HTTP 429 failure for normal and concurrent paths.
			controller := gomock.NewController(t)
			catalog := NewMockCatalogResolver(controller)
			provider := NewMockProviderAttempt(controller)
			selection := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
			}
			catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
				Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
			}, nil)
			providerCause := errors.New("HTTP 429 Too Many Requests")
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, ProviderRequest, StreamHandler) error {
					if concurrentCancellation {
						cancel()
					}
					return &ProviderFailureError{
						Classification: ProviderFailureTransient,
						RetryDelay:     mo.Some(60 * time.Second),
						Cause:          providerCause,
					}
				},
			)
			service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
				Enabled: false, MaxRetries: 3, Delays: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
				MaxProviderDelay: 30 * time.Second,
			}, nil, nil)

			// Act through the configured-request boundary that owns the normal and concurrent classifications.
			_, err := service.RequestConfigured(ctx, selection, "", nil, nil)

			// Assert disabling repeats keeps MODEL_FAILED and all causes without applying the delay maximum.
			var failure *LogicalFailureError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, FailureModelFailed, failure.Category)
			require.ErrorIs(t, err, providerCause)
			if concurrentCancellation {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}
