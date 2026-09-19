//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestServiceOversizedProviderDelayRunsHandlersBeforeMaximumEnforcement verifies handler terminal precedence.
func TestServiceOversizedProviderDelayRunsHandlersBeforeMaximumEnforcement(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		// action is the explicit handler result when invocation succeeds.
		action RetryAction
		// handlerErr is the handler invocation failure when present.
		handlerErr error
		// category is the required terminal logical category.
		category FailureCategory
	}{
		"explicit cancellation": {
			action:     RetryAction{Kind: RetryActionCancel, Decision: mo.None[RetryDecision]()},
			handlerErr: nil, category: FailureRetryCanceled,
		},
		"handler failure": {
			action: RetryAction{}, handlerErr: errors.New("retry extension failed"),
			category: FailureExtensionFailed,
		},
		"successful composition": {
			action:     RetryAction{Kind: RetryActionPreserve, Decision: mo.None[RetryDecision]()},
			handlerErr: nil, category: FailureRetryDelayExceeded,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a provider delay above the maximum and one registered handler.
			controller := gomock.NewController(t)
			catalog := NewMockCatalogResolver(controller)
			provider := NewMockProviderAttempt(controller)
			handlers := NewMockRetryHandlers(controller)
			selection := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff,
			}
			catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
				Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
			}, nil)
			providerDelay := 31 * time.Second
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
				Classification: ProviderFailureTransient, RetryDelay: mo.Some(providerDelay),
				Cause: errors.New("provider rate limit"),
			})
			handler := RetryHandler{ExtensionID: "retry", RuntimeID: "runtime", HandlerID: "handler"}
			handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{handler})
			handlers.EXPECT().HandleRetry(gomock.Any(), handler, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
					assert.Equal(t, providerDelay, invocation.Original.Delay)
					assert.Equal(t, providerDelay, invocation.Current.Delay)
					return test.action, test.handlerErr
				},
			)
			service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
				Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Second},
				MaxProviderDelay: 30 * time.Second,
			}, handlers, nil)

			// Act through one configured request.
			_, err := service.RequestConfigured(t.Context(), selection, "", nil, nil)

			// Assert the handler terminal result wins before maximum-delay enforcement.
			var failure *LogicalFailureError
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.category, failure.Category)
		})
	}
}

// TestServiceProviderDelayIsHandlerLowerBound verifies handlers see and cannot undercut the accepted source delay.
func TestServiceProviderDelayIsHandlerLowerBound(t *testing.T) {
	t.Parallel()

	// Arrange a source delay above policy delay and a first handler that tries to undercut it.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	handlers := NewMockRetryHandlers(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	providerDelay := 5 * time.Second
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.Some(providerDelay),
		Cause: errors.New("provider throttled request"),
	})
	first := RetryHandler{ExtensionID: "one", RuntimeID: "runtime-one", HandlerID: "first"}
	second := RetryHandler{ExtensionID: "two", RuntimeID: "runtime-two", HandlerID: "second"}
	handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{first, second})
	handlers.EXPECT().HandleRetry(gomock.Any(), first, gomock.Any()).DoAndReturn(
		func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
			assert.Equal(t, providerDelay, invocation.Original.Delay)
			assert.Equal(t, providerDelay, invocation.Current.Delay)
			return RetryAction{Kind: RetryActionReplace, Decision: mo.Some(RetryDecision{
				Retryable: true, Retry: true, Delay: providerDelay - time.Second, AttemptLimit: 2,
			})}, nil
		},
	)
	handlers.EXPECT().HandleRetry(gomock.Any(), second, gomock.Any()).Times(0)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 0, Delays: nil, MaxProviderDelay: 30 * time.Second,
	}, handlers, nil)

	// Act through one configured request after the built-in attempt allowance is exhausted.
	_, err := service.RequestConfigured(t.Context(), selection, "", nil, nil)

	// Assert the invalid undercut stops composition before the next handler.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureExtensionFailed, failure.Category)
	assert.ErrorContains(t, err, "provider retry delay")
}

// TestServiceStoppedDecisionCannotUndercutProviderDelay verifies invalid stopped state never reaches later handlers.
func TestServiceStoppedDecisionCannotUndercutProviderDelay(t *testing.T) {
	t.Parallel()

	// Arrange a source delay and a first handler that clears retry with a lower delay.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	handlers := NewMockRetryHandlers(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	providerDelay := 5 * time.Second
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.Some(providerDelay),
		Cause: errors.New("provider throttled request"),
	})
	first := RetryHandler{ExtensionID: "one", RuntimeID: "runtime-one", HandlerID: "first"}
	second := RetryHandler{ExtensionID: "two", RuntimeID: "runtime-two", HandlerID: "second"}
	handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{first, second})
	handlers.EXPECT().HandleRetry(gomock.Any(), first, gomock.Any()).Return(RetryAction{
		Kind: RetryActionReplace, Decision: mo.Some(RetryDecision{
			Retryable: true, Retry: false, Delay: 0, AttemptLimit: 2,
		}),
	}, nil)
	handlers.EXPECT().HandleRetry(gomock.Any(), second, gomock.Any()).Times(0)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Second}, MaxProviderDelay: 30 * time.Second,
	}, handlers, nil)

	// Act through one configured request.
	_, err := service.RequestConfigured(t.Context(), selection, "", nil, nil)

	// Assert the invalid undercut is terminal before the next handler.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureExtensionFailed, failure.Category)
	assert.ErrorContains(t, err, "provider retry delay")
}

// TestServiceInvalidHandlerDecisionStopsComposition verifies invalid current state never reaches later handlers.
func TestServiceInvalidHandlerDecisionStopsComposition(t *testing.T) {
	t.Parallel()

	// Arrange one transient failure and a first handler that returns an invalid negative delay.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	handlers := NewMockRetryHandlers(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
		Cause: errors.New("temporary source failure"),
	})
	first := RetryHandler{ExtensionID: "one", RuntimeID: "runtime-one", HandlerID: "first"}
	second := RetryHandler{ExtensionID: "two", RuntimeID: "runtime-two", HandlerID: "second"}
	handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{first, second})
	handlers.EXPECT().HandleRetry(gomock.Any(), first, gomock.Any()).Return(RetryAction{
		Kind: RetryActionReplace,
		Decision: mo.Some(RetryDecision{
			Retryable: true, Retry: true, Delay: -time.Second, AttemptLimit: 2,
		}),
	}, nil)
	handlers.EXPECT().HandleRetry(gomock.Any(), second, gomock.Any()).Times(0)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, handlers, nil)

	// Act through one configured request.
	_, err := service.RequestConfigured(t.Context(), selection, "", nil, nil)

	// Assert invalid action is terminal and the later handler is not invoked.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureExtensionFailed, failure.Category)
	assert.ErrorContains(t, err, "negative delay")
}

// TestServiceRetryHandlersComposeOriginalAndCurrentInSnapshotOrder verifies ordered immutable decision composition.
func TestServiceRetryHandlersComposeOriginalAndCurrentInSnapshotOrder(t *testing.T) {
	t.Parallel()

	// Arrange one transient attempt and two handlers that replace then preserve the decision.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	handlers := NewMockRetryHandlers(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	first := RetryHandler{ExtensionID: "one", RuntimeID: "runtime-one", HandlerID: "first"}
	second := RetryHandler{ExtensionID: "two", RuntimeID: "runtime-two", HandlerID: "second"}
	handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{first, second})
	replacement := RetryDecision{Retryable: true, Retry: true, Delay: 0, AttemptLimit: 3}
	gomock.InOrder(
		handlers.EXPECT().HandleRetry(gomock.Any(), first, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
				assert.Equal(t, invocation.Original, invocation.Current)
				return RetryAction{Kind: RetryActionReplace, Decision: mo.Some(replacement)}, nil
			},
		),
		handlers.EXPECT().HandleRetry(gomock.Any(), second, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ RetryHandler, invocation RetryInvocation) (RetryAction, error) {
				assert.NotEqual(t, invocation.Original, invocation.Current)
				assert.Equal(t, replacement, invocation.Current)
				return RetryAction{Kind: RetryActionPreserve, Decision: mo.None[RetryDecision]()}, nil
			},
		),
	)
	attempt := 0
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			attempt++
			if attempt == 1 {
				return &ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
					Cause: errors.New("first source cause"),
				}
			}
			return handle(successTerminalEvent(selection))
		},
	)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, handlers, nil)

	// Act through one logical configured request.
	_, err := service.RequestConfigured(t.Context(), selection, "", nil, nil)

	// Assert both handlers composed before the successful replacement attempt.
	require.NoError(t, err)
}
