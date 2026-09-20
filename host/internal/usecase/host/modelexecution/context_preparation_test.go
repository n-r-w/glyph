//go:build !integration

package modelexecution

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
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServicePreparesInitialConversationContext verifies only the agent stream uses the context-preparation owner.
func TestServicePreparesInitialConversationContext(t *testing.T) {
	t.Parallel()
	// Arrange one preparation that replaces the provider-visible history.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	observer := NewMockConversationContext(controller)
	preparation := NewMockContextPreparation(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	preparedHistory := []agent.HistoryEntry{textHistoryEntry("compacted")}
	preparation.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest) (ProviderRequest, error) {
			request.History = preparedHistory
			return request, nil
		},
	)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			require.Equal(t, preparedHistory, request.History)
			return emitContextPreparationSuccess(handle)
		},
	)
	observer.EXPECT().ObserveCompletedConversation(gomock.Any(), gomock.Any())
	service := New(catalog, observer, disabledRetryPolicy(), nil, nil)
	service.BindContextPreparation(preparation)

	// Act through the active-conversation stream.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: []agent.HistoryEntry{textHistoryEntry("original")}, Tools: nil,
	}, func(_ agentrun.StreamEvent) error { return nil })

	// Assert preparation completed before provider dispatch.
	require.NoError(t, err)
}

// TestServicePreservesTypedPreparationFailure verifies orchestration identity is not replaced by a generic category.
func TestServicePreservesTypedPreparationFailure(t *testing.T) {
	t.Parallel()
	// Arrange one preparation failure with a stable extension category.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	observer := NewMockConversationContext(controller)
	preparation := NewMockContextPreparation(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	preparation.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).Return(
		ProviderRequest{}, typedPreparationFailure{cause: errors.New("extension failed")},
	)
	service := New(catalog, observer, disabledRetryPolicy(), nil, nil)
	service.BindContextPreparation(preparation)

	// Act before provider dispatch.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: []agent.HistoryEntry{textHistoryEntry("oversized")}, Tools: nil,
	}, func(_ agentrun.StreamEvent) error { return nil })

	// Assert the complete cause and original category remain discoverable.
	require.ErrorContains(t, err, "extension failed")
	typed, present := errors.AsType[interface {
		error
		FailureCode() string
	}](err)
	require.True(t, present)
	require.Equal(t, "EXTENSION_FAILED", typed.FailureCode())
}

// typedPreparationFailure is one test failure with stable orchestration identity.
type typedPreparationFailure struct {
	// cause is the complete underlying failure.
	cause error
}

// Error exposes the complete failure text.
func (e typedPreparationFailure) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying cause.
func (e typedPreparationFailure) Unwrap() error { return e.cause }

// CompactionFailureCode returns the stable category through the preparation consumer contract.
func (typedPreparationFailure) CompactionFailureCode() string { return "EXTENSION_FAILED" }

// TestServiceInitialPreparationCancellationMatrix verifies pure abort and typed precedence before dispatch.
func TestServiceInitialPreparationCancellationMatrix(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name         string
		mixed        bool
		expectedCode mo.Option[string]
	}{
		{name: "pure owning cancellation", mixed: false, expectedCode: mo.None[string]()},
		{
			name: "typed failure with concurrent cancellation", mixed: true,
			expectedCode: mo.Some(string(FailureExtensionFailed)),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange initial preparation that cancels its owner before provider dispatch.
			controller := gomock.NewController(t)
			catalog := NewMockCatalogResolver(controller)
			provider := NewMockProviderAttempt(controller)
			observer := NewMockConversationContext(controller)
			preparation := NewMockContextPreparation(controller)
			output := NewMockRetryOutput(controller)
			selection := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow,
			}
			descriptor := modelDescriptor(selection)
			catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
				Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
			}, nil)
			callerCause := errors.New("user canceled initial preparation")
			independentCause := errors.New("extension failed during initial preparation")
			ctx, cancel := context.WithCancelCause(t.Context())
			preparation.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ProviderRequest) (ProviderRequest, error) {
					cancel(callerCause)
					if testCase.mixed {
						return ProviderRequest{}, typedPreparationFailure{cause: independentCause}
					}
					return ProviderRequest{}, callerCause
				},
			)
			service := New(catalog, observer, RetryPolicy{
				Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
			}, nil, output)
			service.BindContextPreparation(preparation)

			// Act through the full initial context-preparation owner.
			err := service.Stream(ctx, agentrun.ModelRequest{
				Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
				History: []agent.HistoryEntry{textHistoryEntry("oversized")}, Tools: nil,
			}, func(_ agentrun.StreamEvent) error { return nil })

			// Assert pure cancellation is untyped and mixed failure retains category and every cause.
			require.ErrorIs(t, err, callerCause)
			failure, found := errors.AsType[interface {
				error
				FailureCode() string
			}](err)
			if expected, present := testCase.expectedCode.Get(); present {
				require.True(t, found)
				require.Equal(t, expected, failure.FailureCode())
				require.ErrorIs(t, err, independentCause)
			} else {
				require.False(t, found)
				require.NotErrorIs(t, err, independentCause)
			}
		})
	}
}

// TestServiceDisabledRetryClassifiesOverflowWithBoundRecovery verifies disabled retry keeps context-limit identity.
func TestServiceDisabledRetryClassifiesOverflowWithBoundRecovery(t *testing.T) {
	t.Parallel()
	// Arrange disabled retry with production-style context preparation bound to the service.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	observer := NewMockConversationContext(controller)
	preparation := NewMockContextPreparation(controller)
	output := NewMockRetryOutput(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	preparation.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest) (ProviderRequest, error) { return request, nil },
	)
	providerCause := errors.New("provider context overflow with retry disabled")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureContextOverflow, RetryDelay: mo.None[time.Duration](), Cause: providerCause,
	})
	service := New(catalog, observer, RetryPolicy{
		Enabled: false, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
	}, nil, output)
	service.BindContextPreparation(preparation)

	// Act through the full model-execution owner.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: []agent.HistoryEntry{textHistoryEntry("oversized")}, Tools: nil,
	}, func(_ agentrun.StreamEvent) error { return nil })

	// Assert recovery is not called and overflow retains its public terminal identity and cause.
	require.ErrorIs(t, err, providerCause)
	failure, found := errors.AsType[interface {
		error
		FailureCode() string
	}](err)
	require.True(t, found)
	require.Equal(t, string(FailureContextLimit), failure.FailureCode())
}

// TestServiceOverflowRecoveryCancellationMatrix verifies pure abort and typed-failure precedence during recovery.
func TestServiceOverflowRecoveryCancellationMatrix(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name           string
		typed          bool
		independent    bool
		expectedCode   mo.Option[string]
		retainOverflow bool
	}{
		{
			name: "pure owning cancellation", typed: false, independent: false,
			expectedCode: mo.None[string](), retainOverflow: false,
		},
		{
			name: "typed cancellation terminal", typed: true, independent: false,
			expectedCode: mo.Some(string(FailureExtensionFailed)), retainOverflow: true,
		},
		{
			name: "typed failure with concurrent cancellation", typed: true, independent: true,
			expectedCode: mo.Some(string(FailureExtensionFailed)), retainOverflow: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange one overflow whose recovery cancels the owner, with an optional independent typed failure.
			controller := gomock.NewController(t)
			catalog := NewMockCatalogResolver(controller)
			provider := NewMockProviderAttempt(controller)
			observer := NewMockConversationContext(controller)
			preparation := NewMockContextPreparation(controller)
			output := NewMockRetryOutput(controller)
			selection := model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow,
			}
			descriptor := modelDescriptor(selection)
			catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
				Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
			}, nil)
			preparation.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, request ProviderRequest) (ProviderRequest, error) { return request, nil },
			)
			providerCause := errors.New("handled provider context overflow")
			provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
				Classification: ProviderFailureContextOverflow,
				RetryDelay:     mo.None[time.Duration](), Cause: providerCause,
			})
			callerCause := errors.New("user canceled overflow recovery")
			independentCause := errors.New("extension failed while cancellation arrived")
			ctx, cancel := context.WithCancelCause(t.Context())
			preparation.EXPECT().RecoverOverflow(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ ProviderRequest) (ProviderRequest, error) {
					cancel(callerCause)
					if testCase.typed {
						if testCase.independent {
							return ProviderRequest{}, typedPreparationFailure{cause: independentCause}
						}
						return ProviderRequest{}, typedPreparationFailure{cause: callerCause}
					}
					return ProviderRequest{}, callerCause
				},
			)
			service := New(catalog, observer, RetryPolicy{
				Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
			}, nil, output)
			service.BindContextPreparation(preparation)

			// Act through the complete model-execution owner.
			err := service.Stream(ctx, agentrun.ModelRequest{
				Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
				History: []agent.HistoryEntry{textHistoryEntry("oversized")}, Tools: nil,
			}, func(_ agentrun.StreamEvent) error { return nil })

			// Assert pure owner cancellation stays untyped while mixed failure retains category and every cause.
			require.ErrorIs(t, err, callerCause)
			failure, found := errors.AsType[interface {
				error
				FailureCode() string
			}](err)
			if expected, present := testCase.expectedCode.Get(); present {
				require.True(t, found)
				require.Equal(t, expected, failure.FailureCode())
				if testCase.independent {
					require.ErrorIs(t, err, independentCause)
				}
			} else {
				require.False(t, found)
			}
			if testCase.retainOverflow {
				require.ErrorIs(t, err, providerCause)
			} else {
				require.NotErrorIs(t, err, providerCause)
			}
		})
	}
}

// TestServiceRecoversOneOverflowWithinRetryAllowance verifies recovery changes context and consumes one repeat.
func TestServiceRecoversOneOverflowWithinRetryAllowance(t *testing.T) {
	t.Parallel()
	// Arrange one overflowing attempt followed by a successful changed request.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	observer := NewMockConversationContext(controller)
	preparation := NewMockContextPreparation(controller)
	output := NewMockRetryOutput(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	preparation.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest) (ProviderRequest, error) { return request, nil },
	)
	changed := []agent.HistoryEntry{textHistoryEntry("summary"), textHistoryEntry("suffix")}
	preparation.EXPECT().RecoverOverflow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest) (ProviderRequest, error) {
			request.History = changed
			return request, nil
		},
	)
	calls := 0
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			calls++
			if calls == 1 {
				return &ProviderFailureError{
					Classification: ProviderFailureContextOverflow, RetryDelay: mo.None[time.Duration](),
					Cause: errors.New("provider context overflow"),
				}
			}
			require.Equal(t, changed, request.History)
			return emitContextPreparationSuccess(handle)
		},
	)
	output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	observer.EXPECT().ObserveCompletedConversation(gomock.Any(), gomock.Any())
	service := New(catalog, observer, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
	}, nil, output)
	service.BindContextPreparation(preparation)

	// Act through one logical agent request.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: []agent.HistoryEntry{textHistoryEntry("oversized")}, Tools: nil,
	}, func(_ agentrun.StreamEvent) error { return nil })

	// Assert exactly one changed replacement attempt completed.
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

// textHistoryEntry creates one text-only provider-neutral history value.
func textHistoryEntry(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}

// emitContextPreparationSuccess completes one provider attempt.
func emitContextPreparationSuccess(handle StreamHandler) error {
	return handle(StreamEvent{
		Kind: StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](), Delta: mo.None[string](),
		Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](),
		Response: mo.Some(model.Response{
			Content:       nil,
			Outcome:       mo.Some(model.OutcomeStop),
			ErrorMessage:  mo.None[string](),
			Provider:      mo.Some(model.ProviderID("provider")),
			Model:         mo.Some(model.ID("model")),
			ResponseModel: mo.None[model.ID](),
			ResponseID:    mo.None[string](),
			Usage:         mo.None[model.Usage](),
			Diagnostics:   nil,
		}),
	})
}
