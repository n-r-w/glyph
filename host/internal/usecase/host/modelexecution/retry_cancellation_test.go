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
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestRetryCancellationRetainsProviderCause checks both shared retry exits for ordinary and configured requests.
func TestRetryCancellationRetainsProviderCause(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"configured", "ordinary"} {
		for _, progressFails := range []bool{true, false} {
			name := "pending delay"
			if progressFails {
				name = "progress delivery"
			}
			t.Run(entry+"/"+name, func(t *testing.T) {
				t.Parallel()
				// Arrange one transient attempt and cancellation before its admitted retry.
				controller := gomock.NewController(t)
				catalog := NewMockCatalogResolver(controller)
				provider := NewMockProviderAttempt(controller)
				selection := model.Selection{
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: model.ReasoningChoiceLow,
				}
				binding := CatalogBinding{
					Model:           modelDescriptor(selection),
					ReasoningChoice: selection.ReasoningChoice,
					Provider:        provider,
				}
				if entry == "configured" {
					catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(binding, nil)
				} else {
					catalog.EXPECT().ActiveBinding().Return(binding)
				}
				source := errors.New("transient provider failure\nsource α")
				provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: source,
				})
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cancelProgress := func() error {
					cancel()
					if progressFails {
						return context.Canceled
					}
					return nil
				}
				output := NewMockRetryOutput(controller)
				var policies ConversationPolicies
				if entry == "ordinary" {
					policies = newTestConversationObservations(t)
					output.EXPECT().
						DeliverRetry(gomock.Any(), gomock.Any()).
						DoAndReturn(func(context.Context, RetryProgress) error {
							return cancelProgress()
						})
				}
				service := New(catalog, RetryPolicy{
					Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Hour}, MaxProviderDelay: time.Hour,
				}, nil, output, policies)

				// Act through the selected public model-execution entry with owning cancellation.
				var response model.Response
				var err error
				if entry == "configured" {
					bindPassThroughFinalizer(t, service)
					response, err = service.RequestConfigured(ctx, selection, "instructions", nil,
						func(int64, int64, time.Duration, string) error { return cancelProgress() })
				} else {
					err = streamLogical(ctx, service, agentrun.ContextRequest{
						Instructions: "instructions", History: nil, Tools: nil,
					}, func(agentrun.StreamEvent) error { return nil })
				}

				// Assert prior provider history remains diagnostic and the current outcome stays cancellation.
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorIs(t, err, source)
				require.Contains(t, err.Error(), source.Error())
				require.True(t, errtree.IsCurrentCancellation(err, func(cause error) bool {
					return errors.Is(cause, context.Canceled)
				}))
				require.Equal(t, model.Response{}, response)
			})
		}
	}
}

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
	service := New(catalog, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil, newTestConversationObservations(t))

	// Act through one configured request.
	bindPassThroughFinalizer(t, service)
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
	service := New(catalog, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil, newTestConversationObservations(t))

	// Act through one configured request.
	bindPassThroughFinalizer(t, service)
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
	service := New(catalog, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil, newTestConversationObservations(t))

	// Act through one configured request.
	bindPassThroughFinalizer(t, service)
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
	catalog.EXPECT().ActiveBinding().Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	})
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
	service := New(catalog, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Second}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil, newTestConversationObservations(t))
	independent := errors.New("deliver model content")

	// Act while the logical stream callback has an independent failure.
	err := streamLogical(ctx, service, agentrun.ContextRequest{
		Instructions: "", History: nil, Tools: nil,
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
		service := New(catalog, RetryPolicy{
			Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Hour}, MaxProviderDelay: 30 * time.Second,
		}, nil, nil, newTestConversationObservations(t))
		ctx, cancel := context.WithCancelCause(t.Context())
		cancellationCause := errors.New("owning operation stopped")

		// Act by canceling with an explicit cause before backoff starts the replacement attempt.
		bindPassThroughFinalizer(t, service)
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

		// Assert owning cancellation retains its custom cause and prior provider diagnostics without response state.
		require.ErrorIs(t, err, cancellationCause)
		require.ErrorIs(t, err, transientCause)
		require.Contains(t, err.Error(), cancellationCause.Error())
		require.Contains(t, err.Error(), transientCause.Error())
		require.True(t, errtree.IsCurrentCancellation(err, func(cause error) bool {
			return errors.Is(cause, context.Canceled) || errors.Is(cause, cancellationCause)
		}))
		assert.True(t, response.Outcome.IsNone())
		require.Equal(t, model.Response{}, response)
	})
}

// TestExecuteUntypedTimeoutKeepsInternalIdentity verifies an acquired local timeout is not a pure cancellation.
func TestExecuteUntypedTimeoutKeepsInternalIdentity(t *testing.T) {
	t.Parallel()

	// Arrange one provider adapter failure that uses a cancellation sentinel without caller cancellation.
	controller := gomock.NewController(t)
	provider := NewMockProviderAttempt(controller)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(context.DeadlineExceeded)
	service := New(NewMockCatalogResolver(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil, newTestConversationObservations(t))
	// Act through the shared logical execution state machine.
	_, err := service.execute(
		t.Context(), provider, preparedRequest{}, nil, nil, errors.New("terminal response missing"),
		"terminal response missing", nil,
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
	service := New(NewMockCatalogResolver(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil, newTestConversationObservations(t))
	// Act with a delivery callback that returns a cancellation sentinel while the caller remains active.
	_, err := service.execute(
		t.Context(), provider, preparedRequest{}, nil, func(RetryProgress) error {
			return context.DeadlineExceeded
		}, errors.New("terminal response missing"), "terminal response missing", nil,
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
			service := New(catalog, RetryPolicy{
				Enabled: false, MaxRetries: 3, Delays: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
				MaxProviderDelay: 30 * time.Second,
			}, nil, nil, newTestConversationObservations(t))

			// Act through the configured-request boundary that owns the normal and concurrent classifications.
			bindPassThroughFinalizer(t, service)
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
