//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/n-r-w/glyph/internal/errtree"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"

	"github.com/samber/mo"
	"go.uber.org/mock/gomock"
)

// newTestConversationObservations creates a mock for conversations without a policy owner.
func newTestConversationObservations(t *testing.T) *MockConversationPolicies {
	t.Helper()
	observations := NewMockConversationPolicies(gomock.NewController(t))
	observations.EXPECT().
		CaptureConversation().
		Return(mo.None[CapturedConversation](), nil).
		AnyTimes()
	return observations
}

// TestObservationCancellationPreservesClassifiedFailure checks acquired failure precedence over owning cancellation.
func TestObservationCancellationPreservesClassifiedFailure(t *testing.T) {
	t.Parallel()
	// Arrange an acquired configured-model failure and owning cancellation with the same sentinel.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	acquired := &LogicalFailureError{Category: FailureModelFailed, Cause: context.Canceled}
	// Act through the conversation observation failure classifier.
	err := observationFailure(ctx, acquired, nil)
	// Assert that the error contains a category from the closed Glyph set and the acquired cause.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
	require.ErrorIs(t, err, acquired)
	require.ErrorIs(t, err, context.Canceled)
}

// TestObservationFailureRetainsOwnedAttempt checks provider mutation isolation and response retention after observation fails.
func TestObservationFailureRetainsOwnedAttempt(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name     string
		canceled bool
		callback error
	}{
		{name: "owner failure", canceled: false, callback: nil},
		{name: "retry then cancellation", canceled: true, callback: nil},
		{name: "independent terminal callback", canceled: true, callback: errors.New("independent callback cause")},
		{name: "classified terminal callback", canceled: true, callback: &LogicalFailureError{Category: FailureInternal, Cause: context.Canceled}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange successful provider output after optional retry and cancellation during observation.
			controller := gomock.NewController(t)
			catalog := NewMockCatalogResolver(controller)
			provider := NewMockProviderAttempt(controller)

			observations := NewMockConversationPolicies(controller)
			selection := model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceLow,
			}
			descriptor := modelDescriptor(selection)
			response := model.Response{
				Content: []model.Content{
					{
						Kind:     model.ContentReasoning,
						Text:     mo.Some("visible reasoning"),
						Final:    true,
						ToolCall: mo.None[model.ToolCall](),
						ProviderContext: mo.Some(
							model.ProviderContext{
								Source: model.ProviderContextSource{
									ProviderID:       "provider",
									API:              "responses",
									Model:            "model",
									CompatibilityKey: mo.None[string](),
								},
								Payload: []byte("private replay bytes"),
							},
						),
					},
				},
				Outcome: mo.Some(
					model.OutcomeStop,
				),
				ErrorMessage: mo.None[string](),
				Provider:     mo.Some(model.ProviderID("provider")),
				Model:        mo.Some(model.ID("model")),
				ResponseModel: mo.Some(
					model.ID("reported-model"),
				),
				ResponseID:  mo.Some("provider-response"),
				Usage:       mo.Some(model.Usage{}),
				Diagnostics: []model.Diagnostic{{Code: "provider-detail", Message: "original diagnostic"}},
			}
			catalog.EXPECT().
				ResolveBinding(selection).
				Return(CatalogBinding{Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider}, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			priorFirst := errors.New("first recovered provider cause")
			priorSecond := errors.New("second recovered provider cause")
			priorFirstTyped := &ProviderFailureError{
				Classification: ProviderFailureTransient,
				RetryDelay:     mo.None[time.Duration](),
				Cause:          priorFirst,
			}
			priorTyped := &ProviderFailureError{
				Classification: ProviderFailureTransient,
				RetryDelay:     mo.None[time.Duration](),
				Cause:          priorSecond,
			}
			observerCause := errors.New("original observer cause")
			captured := newTestPreservingCapture(t)
			observations.EXPECT().CaptureConversation().Return(mo.Some[CapturedConversation](captured), nil)
			captured.EXPECT().
				Observe(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, input ProviderRequest, actual model.Response) error {
					require.Equal(t, "original input", input.History[0].User.MustGet().Content[0].Text.MustGet())
					require.Equal(t, response, actual)
					if scenario.canceled {
						cancel()
						return context.Cause(ctx)
					}
					return observerCause
				})
			calls := 0
			provider.EXPECT().
				Stream(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().
				DoAndReturn(func(_ context.Context, input ProviderRequest, handle StreamHandler) error {
					calls++
					if scenario.canceled && calls == 1 {
						return priorFirstTyped
					}
					if scenario.canceled && calls == 2 {
						return priorTyped
					}
					input.History[0].User.MustGet().Content[0].Text = mo.Some("provider mutation")
					return handle(
						StreamEvent{
							Kind:     StreamEventDone,
							Position: mo.None[int](),
							Content:  mo.None[model.Content](),
							Delta:    mo.None[string](),
							Preview:  mo.None[model.ToolCallPreview](),
							ToolCall: mo.None[model.ToolCall](),
							Response: mo.Some(response),
						},
					)
				})
			retry := disabledRetryPolicy()
			if scenario.canceled {
				retry = RetryPolicy{Enabled: true, MaxRetries: 2, Delays: []time.Duration{0, 0}, MaxProviderDelay: 0}
			}
			output := NewMockRetryOutput(controller)
			output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			service := New(catalog, retry, nil, output, observations)
			var terminal agentrun.StreamEvent
			// Act through the model-execution service's conversation stream.
			err := service.Stream(
				ctx,
				agentrun.ModelRequest{
					Instructions:    "",
					Model:           descriptor,
					ReasoningChoice: selection.ReasoningChoice,
					History:         []agent.HistoryEntry{textHistoryEntry("original input")},
					Tools:           nil,
				},
				func(event agentrun.StreamEvent) error {
					if event.Kind == agentrun.StreamEventError {
						terminal = event
						return scenario.callback
					}
					return nil
				},
			)
			// Assert the failed observation retains the complete response, present-zero usage, and private replay data.
			if scenario.canceled {
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorIs(t, err, priorFirst)
				require.ErrorIs(t, err, priorSecond)
				retained, present := errors.AsType[*ProviderFailureError](err)
				require.True(t, present)
				require.ErrorContains(t, err, priorTyped.Error())
				require.Same(t, priorFirstTyped, retained)
				require.ErrorIs(t, err, priorTyped)
				require.Contains(t, err.Error(), priorFirst.Error())
				require.Contains(t, err.Error(), priorSecond.Error())
				canceled := errtree.IsCurrentCancellation(
					err,
					func(cause error) bool { return errors.Is(cause, context.Canceled) },
				)
				require.Equal(t, scenario.callback == nil, canceled)
				if scenario.callback != nil {
					require.ErrorIs(t, err, scenario.callback)
				}
				require.Equal(t, 3, calls)
			} else {
				require.ErrorIs(t, err, observerCause)
				failure, classified := errors.AsType[*LogicalFailureError](err)
				require.True(t, classified)
				require.Equal(t, FailureExtensionFailed, failure.Category)
				require.Equal(t, 1, calls)
			}
			require.Equal(t, agentrun.StreamEventError, terminal.Kind)
			require.Equal(t, response, terminal.Response.MustGet())
		})
	}
}
