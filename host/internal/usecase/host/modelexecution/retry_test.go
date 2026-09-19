//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceStreamResetsPartialResponseBeforeRetry verifies failed intermediate content is replaced semantically.
func TestServiceStreamResetsPartialResponseBeforeRetry(t *testing.T) {
	t.Parallel()

	// Arrange one partial transient attempt followed by one successful replacement.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	observer := NewMockConversationContext(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	attempt := 0
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(2).DoAndReturn(
		func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			attempt++
			if err := handle(StreamEvent{
				Kind: StreamEventTextDelta, Position: mo.Some(0),
				Content: mo.Some(model.Content{
					Kind: model.ContentText, Text: mo.Some("answer"), Final: false,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}), Delta: mo.Some("answer"), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}); err != nil {
				return err
			}
			if attempt == 1 {
				return &ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
					Cause: errors.New("temporary source failure"),
				}
			}
			return handle(StreamEvent{
				Kind: StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
					Provider: mo.Some(selection.Provider), Model: mo.Some(selection.Model),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
			})
		},
	)
	observer.EXPECT().ObserveCompletedConversation(gomock.Any(), gomock.Any()).Times(1)
	retryOutput := NewMockRetryOutput(controller)
	retryOutput.EXPECT().DeliverRetry(gomock.Any(), RetryProgress{
		CompletedAttempts: 1, AttemptLimit: 2, Delay: 0, Error: "temporary source failure",
	}).Return(nil)
	service := New(catalog, observer, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, retryOutput)
	var kinds []agentrun.StreamEventKind

	// Act through the Agent Core logical stream.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: selection.ReasoningChoice,
		History: nil, Tools: nil,
	}, func(event agentrun.StreamEvent) error {
		kinds = append(kinds, event.Kind)
		return nil
	})

	// Assert Core receives only reset and model output while Host output receives retry progress.
	require.NoError(t, err)
	assert.Equal(t, []agentrun.StreamEventKind{
		agentrun.StreamEventTextDelta,
		agentrun.StreamEventResponseReset,
		agentrun.StreamEventTextDelta,
		agentrun.StreamEventDone,
	}, kinds)
}

// TestServiceConfiguredRequestUsesConfiguredRetrySchedule verifies repeats exclude the initial attempt and use ordered delays.
func TestServiceConfiguredRequestUsesConfiguredRetrySchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// Arrange three transient failures followed by success under the approved default schedule.
		controller := gomock.NewController(t)
		catalog := NewMockCatalogResolver(controller)
		provider := NewMockProviderAttempt(controller)
		selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
		catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
			Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
		}, nil)
		attempt := 0
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Times(4).DoAndReturn(
			func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
				attempt++
				if attempt < 4 {
					return &ProviderFailureError{
						Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
						Cause: fmt.Errorf("transient attempt %d", attempt),
					}
				}
				return handle(StreamEvent{
					Kind: StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
					Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
					ToolCall: mo.None[model.ToolCall](), Response: mo.Some(model.Response{
						Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
						Provider: mo.Some(selection.Provider), Model: mo.Some(selection.Model),
						ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
						Usage: mo.None[model.Usage](), Diagnostics: nil,
					}),
				})
			},
		)
		service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
			Enabled: true, MaxRetries: 3,
			Delays:           []time.Duration{time.Second, 2 * time.Second, 4 * time.Second},
			MaxProviderDelay: 30 * time.Second,
		}, nil, nil)
		started := time.Now()
		var progressValues []RetryProgress

		// Act through the configured-model path using production delay scheduling.
		response, err := service.RequestConfigured(
			t.Context(), selection, "instructions", nil,
			func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error {
				progressValues = append(progressValues, RetryProgress{
					CompletedAttempts: completedAttempts, AttemptLimit: attemptLimit, Delay: delay, Error: failure,
				})
				return nil
			},
		)

		// Assert three repeats, their exact delays, and one successful terminal result.
		require.NoError(t, err)
		assert.Equal(t, model.OutcomeStop, response.Outcome.MustGet())
		assert.Equal(t, 7*time.Second, time.Since(started))
		require.Len(t, progressValues, 3)
		assert.Equal(t, []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, []time.Duration{
			progressValues[0].Delay, progressValues[1].Delay, progressValues[2].Delay,
		})
	})
}

// TestServiceFinalFailureDeliversRetainedTerminalResponse verifies only the final failed attempt reaches Core.
func TestServiceFinalFailureDeliversRetainedTerminalResponse(t *testing.T) {
	t.Parallel()

	// Arrange one non-retryable provider failure with terminal metadata and no incremental content.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	terminal := model.Response{
		Content: nil, Outcome: mo.Some(model.OutcomeFailed), ErrorMessage: mo.Some("provider rejected request"),
		Provider: mo.Some(selection.Provider), Model: mo.Some(selection.Model),
		ResponseModel: mo.Some(model.ID("reported-model")), ResponseID: mo.Some("response-id"),
		Usage: mo.Some(model.Usage{
			InputTokens: 4, OutputTokens: 1, CachedInputTokens: 0, CacheWriteTokens: 0,
			ReasoningTokens: 0, TotalTokens: 5,
		}),
		Diagnostics: []model.Diagnostic{{Code: "provider_error", Message: "complete diagnostic"}},
	}
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventError, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(terminal),
			}))
			return &ProviderFailureError{
				Classification: ProviderFailureNonRetryable, RetryDelay: mo.None[time.Duration](),
				Cause: errors.New("provider source cause"),
			}
		},
	)
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 3, Delays: []time.Duration{time.Second}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)
	var delivered []agentrun.StreamEvent

	// Act through the Agent Core stream boundary.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "", Model: descriptor, ReasoningChoice: selection.ReasoningChoice, History: nil, Tools: nil,
	}, func(event agentrun.StreamEvent) error {
		delivered = append(delivered, event)
		return nil
	})

	// Assert final terminal metadata is delivered once while the complete logical failure remains returned.
	require.ErrorContains(t, err, "provider source cause")
	require.Len(t, delivered, 1)
	assert.Equal(t, agentrun.StreamEventError, delivered[0].Kind)
	assert.Equal(t, "response-id", delivered[0].Response.MustGet().ResponseID.MustGet())
	assert.Equal(t, "complete diagnostic", delivered[0].Response.MustGet().Diagnostics[0].Message)
}

// TestServiceRejectsProviderDelayAboveMaximum verifies a source minimum is never truncated.
func TestServiceRejectsProviderDelayAboveMaximum(t *testing.T) {
	t.Parallel()

	// Arrange one transient source failure requesting a delay above the accepted maximum.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.Some(31 * time.Second),
		Cause: errors.New("provider rate limit"),
	})
	service := New(catalog, NewMockConversationContext(controller), RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Second}, MaxProviderDelay: 30 * time.Second,
	}, nil, nil)

	// Act through one logical configured request.
	_, err := service.RequestConfigured(t.Context(), selection, "", nil, nil)

	// Assert the delay-specific category and complete provider cause are both preserved.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, FailureRetryDelayExceeded, failure.Category)
	assert.ErrorContains(t, err, "provider rate limit")
	assert.ErrorContains(t, err, "exceeds configured maximum")
}

// TestServiceRetryAttemptsDeepCopyToolSchemas verifies provider mutation cannot change a later attempt.
func TestServiceRetryAttemptsDeepCopyToolSchemas(t *testing.T) {
	t.Parallel()

	// Arrange two attempts around a provider mutation of the first request schema.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	originalSchema := []byte(`{"type":"object"}`)
	gomock.InOrder(
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, request ProviderRequest, _ StreamHandler) error {
				request.Tools[0].InputSchemaJSON[0] = '!'
				return &ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
					Cause: errors.New("retry after first attempt"),
				}
			},
		),
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
				assert.Equal(t, originalSchema, request.Tools[0].InputSchemaJSON)
				return handle(successTerminalEvent(selection))
			},
		),
	)
	observer := NewMockConversationContext(controller)
	observer.EXPECT().ObserveCompletedConversation(gomock.Any(), gomock.Any())
	retryOutput := NewMockRetryOutput(controller)
	retryOutput.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	service := New(catalog, observer, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 30 * time.Second,
	}, nil, retryOutput)

	// Act through the streaming path with one tool descriptor.
	err := service.Stream(t.Context(), agentrun.ModelRequest{
		Instructions: "", Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice,
		History: nil, Tools: []tool.Descriptor{{
			Name: "read", Description: "read", InputSchemaJSON: slices.Clone(originalSchema),
			ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
		}},
	}, func(agentrun.StreamEvent) error { return nil })

	// Assert the replacement attempt and caller-owned schema remain unchanged.
	require.NoError(t, err)
	assert.Equal(t, []byte(`{"type":"object"}`), originalSchema)
}

// TestServiceConfiguredRequestPreservesProviderFailure verifies one raw failure is returned with a zero response.
func TestServiceConfiguredRequestPreservesProviderFailure(t *testing.T) {
	t.Parallel()

	// Arrange one resolved binding whose single raw attempt fails.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	providerErr := errors.New("complete provider failure")
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(providerErr)
	service := newTestService(t, catalog)

	// Act through one configured request.
	response, err := service.RequestConfigured(t.Context(), selection, "instructions", nil, nil)

	// Assert complete provider semantics and the zero response are preserved.
	require.ErrorIs(t, err, providerErr)
	assert.Contains(t, err.Error(), providerErr.Error())
	assert.Equal(t, model.Response{}, response)
}
