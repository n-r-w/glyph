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
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestServiceCanceledRetryProgressDoesNotRestoreResetResponse verifies discarded attempt content stays discarded.
func TestServiceCanceledRetryProgressDoesNotRestoreResetResponse(t *testing.T) {
	t.Parallel()

	// Arrange one partial failed attempt and cancellation from Host retry-progress delivery after reset.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	observer := NewMockConversationContext(controller)
	retryOutput := NewMockRetryOutput(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().ResolveBinding(selection).Return(CatalogBinding{
		Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider,
	}, nil)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventTextDelta, Position: mo.Some(0),
				Content: mo.Some(model.Content{
					Kind: model.ContentText, Text: mo.Some("discarded"), Final: false,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}),
				Delta: mo.Some("discarded"), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.None[model.Response](),
			}))
			require.NoError(t, handle(StreamEvent{
				Kind: StreamEventError, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(model.OutcomeFailed), ErrorMessage: mo.Some("discarded failure"),
					Provider: mo.Some(selection.Provider), Model: mo.Some(selection.Model),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.None[model.Usage](), Diagnostics: nil,
				}),
			}))
			return &ProviderFailureError{
				Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
				Cause: errors.New("temporary provider failure"),
			}
		},
	)
	ctx, cancel := context.WithCancel(t.Context())
	retryOutput.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, RetryProgress) error {
			cancel()
			return context.Canceled
		},
	)
	service := New(catalog, observer, RetryPolicy{
		Enabled: true, MaxRetries: 1, Delays: []time.Duration{time.Second}, MaxProviderDelay: 30 * time.Second,
	}, nil, retryOutput)
	var kinds []agentrun.StreamEventKind

	// Act through the logical agent stream.
	err := service.Stream(ctx, agentrun.ModelRequest{
		Instructions: "", Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice,
		History: nil, Tools: nil,
	}, func(event agentrun.StreamEvent) error {
		kinds = append(kinds, event.Kind)
		return nil
	})

	// Assert cancellation returns no retained terminal response after the successful reset.
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, []agentrun.StreamEventKind{
		agentrun.StreamEventTextDelta,
		agentrun.StreamEventResponseReset,
	}, kinds)
}
