//go:build integration

package run_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestCoreRetainsConcurrentProviderTimeoutIdentityWithAttemptsRemaining verifies typed Host failure reaches Core.
func TestCoreRetainsConcurrentProviderTimeoutIdentityWithAttemptsRemaining(t *testing.T) {
	t.Parallel()

	// Arrange Agent Core with real logical execution, one earlier timeout, and one concurrent final timeout.
	controller := gomock.NewController(t)
	catalog := modelexecution.NewMockCatalogResolver(controller)
	provider := modelexecution.NewMockProviderAttempt(controller)
	conversation := modelexecution.NewMockConversationContext(controller)
	retryOutput := modelexecution.NewMockRetryOutput(controller)
	tools := agentrun.NewMockToolRuntime(controller)
	events := agentrun.NewMockEventSink(controller)
	historyStore := agentrun.NewMockHistoryStore(controller)
	descriptor := model.Descriptor{
		Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 128000, MaxTokens: 16000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default: model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}
	selection := model.Selection{
		Provider: descriptor.Provider, Model: descriptor.Model, ReasoningChoice: model.ReasoningChoiceOff,
	}
	binding := modelexecution.CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}
	catalog.EXPECT().ActiveBinding().Return(binding)
	catalog.EXPECT().ResolveBinding(selection).Return(binding, nil)
	firstCause := fmt.Errorf("earlier provider timeout: %w", context.DeadlineExceeded)
	lastCause := fmt.Errorf("final provider timeout: %w", context.DeadlineExceeded)
	ctx, cancel := context.WithCancel(t.Context())
	gomock.InOrder(
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(
			&modelexecution.ProviderFailureError{
				Classification: modelexecution.ProviderFailureTransient,
				RetryDelay:     mo.None[time.Duration](), Cause: firstCause,
			},
		),
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, modelexecution.ProviderRequest, modelexecution.StreamHandler) error {
				cancel()
				return &modelexecution.ProviderFailureError{
					Classification: modelexecution.ProviderFailureTransient,
					RetryDelay:     mo.None[time.Duration](), Cause: lastCause,
				}
			},
		),
	)
	retryOutput.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	tools.EXPECT().Tools().Return(nil)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	var history []agent.HistoryEntry
	historyStore.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
		return slices.Clone(history)
	}).AnyTimes()
	historyStore.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, entry.Clone())
			return nil
		},
	).AnyTimes()
	execution := modelexecution.New(catalog, conversation, modelexecution.RetryPolicy{
		Enabled: true, MaxRetries: 2, Delays: []time.Duration{0, 0}, MaxProviderDelay: 30 * time.Second,
	}, nil, retryOutput)
	core := agentrun.New("", execution, tools, events, historyStore)

	// Act through the complete Agent Core run boundary.
	result, err := core.Run(ctx, runcontrol.Request{RunID: "typed-timeout", UserText: "request"})

	// Assert Core reports a failed INTERNAL logical result and retains every acquired cause.
	var logical *modelexecution.LogicalFailureError
	require.ErrorAs(t, err, &logical)
	assert.Equal(t, modelexecution.FailureCategory("INTERNAL"), logical.Category)
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, firstCause.Error())
	require.ErrorContains(t, err, lastCause.Error())
	require.Len(t, history, 2)
	assert.Equal(t, model.OutcomeFailed, history[1].Model.OrEmpty().Outcome.MustGet())
}

// TestCoreDoesNotRestoreDiscardedAttemptAfterRetryProgressFailure verifies reset content stays attempt-local.
func TestCoreDoesNotRestoreDiscardedAttemptAfterRetryProgressFailure(t *testing.T) {
	t.Parallel()

	// Arrange real logical execution with partial content, an accepted retry, and independent progress failure.
	controller := gomock.NewController(t)
	catalog := modelexecution.NewMockCatalogResolver(controller)
	provider := modelexecution.NewMockProviderAttempt(controller)
	conversation := modelexecution.NewMockConversationContext(controller)
	retryOutput := modelexecution.NewMockRetryOutput(controller)
	tools := agentrun.NewMockToolRuntime(controller)
	events := agentrun.NewMockEventSink(controller)
	historyStore := agentrun.NewMockHistoryStore(controller)
	descriptor := model.Descriptor{
		Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 128000, MaxTokens: 16000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default: model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
	}
	selection := model.Selection{
		Provider: descriptor.Provider, Model: descriptor.Model, ReasoningChoice: model.ReasoningChoiceOff,
	}
	binding := modelexecution.CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}
	catalog.EXPECT().ActiveBinding().Return(binding)
	catalog.EXPECT().ResolveBinding(selection).Return(binding, nil)
	providerCause := errors.New("temporary provider failure")
	deliveryCause := errors.New("retry progress delivery failed")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ modelexecution.ProviderRequest, handle modelexecution.StreamHandler) error {
			require.NoError(t, handle(modelexecution.StreamEvent{
				Kind: modelexecution.StreamEventContentStart, Position: mo.Some(0),
				Content: mo.Some(model.Content{
					Kind: model.ContentText, Text: mo.Some(""), Final: false,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			require.NoError(t, handle(modelexecution.StreamEvent{
				Kind: modelexecution.StreamEventTextDelta, Position: mo.Some(0),
				Content: mo.Some(model.Content{
					Kind: model.ContentText, Text: mo.Some("discarded"), Final: false,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}),
				Delta: mo.Some("discarded"), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			require.NoError(t, handle(modelexecution.StreamEvent{
				Kind: modelexecution.StreamEventError, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(model.Response{
					Content: []model.Content{{
						Kind: model.ContentText, Text: mo.Some("discarded"), Final: true,
						ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
					}},
					Outcome: mo.Some(model.OutcomeFailed), ErrorMessage: mo.Some("discarded failure"),
					Provider: mo.Some(selection.Provider), Model: mo.Some(selection.Model),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
			}))
			return &modelexecution.ProviderFailureError{
				Classification: modelexecution.ProviderFailureTransient,
				RetryDelay:     mo.None[time.Duration](), Cause: providerCause,
			}
		},
	)
	retryOutput.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(deliveryCause)
	tools.EXPECT().Tools().Return(nil)
	var delivered []agent.Event
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, event agent.Event) error {
			delivered = append(delivered, event)
			return nil
		},
	).AnyTimes()
	var history []agent.HistoryEntry
	historyStore.EXPECT().Snapshot().DoAndReturn(func() []agent.HistoryEntry {
		return slices.Clone(history)
	}).AnyTimes()
	historyStore.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, entry agent.HistoryEntry) error {
			history = append(history, entry.Clone())
			return nil
		},
	).AnyTimes()
	execution := modelexecution.New(catalog, conversation, modelexecution.RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, retryOutput)
	core := agentrun.New("", execution, tools, events, historyStore)

	// Act through Agent Core and the actual result returned by backoff.
	result, err := core.Run(t.Context(), runcontrol.Request{RunID: "progress-failure", UserText: "request"})

	// Assert the typed failure keeps both causes while discarded terminal content never returns after reset.
	var logical *modelexecution.LogicalFailureError
	require.ErrorAs(t, err, &logical)
	assert.Equal(t, modelexecution.FailureInternal, logical.Category)
	require.ErrorIs(t, err, providerCause)
	require.ErrorIs(t, err, deliveryCause)
	assert.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	resetObserved := false
	for _, event := range delivered {
		if event.Type == agent.EventResponseReset {
			resetObserved = true
		}
		if response, present := event.Message.Get(); resetObserved && present {
			assert.NotContains(t, response.Text(), "discarded")
		}
	}
	require.True(t, resetObserved)
	for _, entry := range history {
		if response, present := entry.Model.Get(); present {
			assert.NotContains(t, response.Text(), "discarded")
		}
	}
}
