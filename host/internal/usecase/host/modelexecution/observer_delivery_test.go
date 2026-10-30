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

	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestObserverDeliveryFailurePreventsProviderRetry verifies failed lifecycle delivery cannot become provider recovery.
func TestObserverDeliveryFailurePreventsProviderRetry(t *testing.T) {
	t.Parallel()
	// Arrange a provider that reports both a callback failure and an independent transient failure.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)
	output := NewMockRetryOutput(controller)
	handlers := NewMockRetryHandlers(controller)
	resets, progress, retries := 0, 0, 0
	output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, RetryProgress) error {
		progress++
		return nil
	}).AnyTimes()
	handler := RetryHandler{ExtensionID: "healthy", RuntimeID: "runtime", HandlerID: "retry"}
	handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{handler})
	handlers.EXPECT().HandleRetry(gomock.Any(), handler, gomock.Any()).DoAndReturn(
		func(context.Context, RetryHandler, RetryInvocation) (RetryAction, error) {
			retries++
			return RetryAction{Kind: RetryActionPreserve, Decision: mo.None[RetryDecision]()}, nil
		},
	).AnyTimes()
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog.EXPECT().
		ActiveBinding().
		Return(CatalogBinding{Model: modelDescriptor(selection), ReasoningChoice: selection.ReasoningChoice, Provider: provider})
	observerCause := errors.New("observer complete original cause")
	providerCause := errors.New("provider independent original cause")
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			callbackErr := handle(
				StreamEvent{
					Kind:     StreamEventTextDelta,
					Position: mo.Some(0),
					Content: mo.Some(
						model.Content{
							Kind:            model.ContentText,
							Text:            mo.Some("text"),
							Final:           false,
							ProviderContext: mo.None[model.ProviderContext](),
							ToolCall:        mo.None[model.ToolCall](),
						},
					),
					Delta:    mo.Some("text"),
					Preview:  mo.None[model.ToolCallPreview](),
					ToolCall: mo.None[model.ToolCall](),
					Response: mo.None[model.Response](),
				},
			)
			return errors.Join(
				callbackErr,
				&ProviderFailureError{
					Classification: ProviderFailureTransient,
					RetryDelay:     mo.None[time.Duration](),
					Cause:          providerCause,
				},
			)
		})
	service := New(
		catalog,
		RetryPolicy{
			Enabled:          true,
			MaxRetries:       1,
			Delays:           []time.Duration{time.Nanosecond},
			MaxProviderDelay: time.Second,
		},
		handlers,
		output,
		newTestConversationObservations(t),
	)

	// Act through the actual logical stream with failed observer delivery.
	err := streamLogical(t.Context(), service,
		agentrun.ContextRequest{
			Instructions: "",
			History:      nil,
			Tools:        nil,
		},
		func(event agentrun.StreamEvent) error {
			if event.Kind == agentrun.StreamEventTextDelta {
				return observerCause
			}
			if event.Kind == agentrun.StreamEventResponseReset {
				resets++
			}
			return nil
		},
	)

	// Assert both causes remain reachable without another provider attempt.
	require.ErrorIs(t, err, observerCause)
	require.ErrorIs(t, err, providerCause)
	require.Zero(t, resets)
	require.Zero(t, progress)
	require.Zero(t, retries)
}
