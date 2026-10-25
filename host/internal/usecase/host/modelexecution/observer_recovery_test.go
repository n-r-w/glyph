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
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestCurrentObserverFailureStopsRecovery checks nested provider causes cannot authorize outer recovery.
func TestCurrentObserverFailureStopsRecovery(t *testing.T) {
	t.Parallel()
	for _, classification := range []ProviderFailureClassification{ProviderFailureTransient, ProviderFailureContextOverflow} {
		for _, canceled := range []bool{false, true} {
			t.Run(
				map[ProviderFailureClassification]string{
					ProviderFailureTransient: "transient/", ProviderFailureContextOverflow: "overflow/",
				}[classification]+map[bool]string{false: "active", true: "canceled"}[canceled],
				func(t *testing.T) {
					t.Parallel()
					// Arrange healthy recovery recipients around a callback with an independently classified child failure.
					mocks := gomock.NewController(t)
					provider := NewMockProviderAttempt(mocks)
					handlers := NewMockRetryHandlers(mocks)
					handler := RetryHandler{ExtensionID: "extension", RuntimeID: "runtime", HandlerID: "healthy-retry"}
					handlers.EXPECT().SnapshotRetryHandlers().Return([]RetryHandler{handler})
					retries, progress, recoveries, attempts := 0, 0, 0, 0
					handlers.EXPECT().
						HandleRetry(gomock.Any(), handler, gomock.Any()).
						DoAndReturn(func(context.Context, RetryHandler, RetryInvocation) (RetryAction, error) {
							retries++
							return RetryAction{Kind: RetryActionPreserve, Decision: mo.None[RetryDecision]()}, nil
						}).
						AnyTimes()
					providerCause := errors.New("nested provider original cause")
					childProvider := &ProviderFailureError{
						Classification: classification,
						RetryDelay:     mo.None[time.Duration](),
						Cause:          providerCause,
					}
					observerCause := &LogicalFailureError{Category: FailureExtensionFailed, Cause: childProvider}
					adapterCause := errors.New("outer provider adapter original cause")
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					response := model.Response{
						Content:       nil,
						Outcome:       mo.Some(model.OutcomeAborted),
						ErrorMessage:  mo.None[string](),
						Provider:      mo.None[model.ProviderID](),
						Model:         mo.None[model.ID](),
						ResponseModel: mo.None[model.ID](),
						ResponseID:    mo.Some("retained"),
						Usage:         mo.None[model.Usage](),
						Diagnostics:   nil,
					}
					provider.EXPECT().
						Stream(gomock.Any(), gomock.Any(), gomock.Any()).
						DoAndReturn(func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
							attempts++
							if attempts > 1 {
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
							}
							callbackErr := handle(
								StreamEvent{
									Kind:     StreamEventTextDelta,
									Position: mo.Some(0),
									Content:  mo.None[model.Content](),
									Delta:    mo.Some("text"),
									Preview:  mo.None[model.ToolCallPreview](),
									ToolCall: mo.None[model.ToolCall](),
									Response: mo.None[model.Response](),
								},
							)
							require.NoError(
								t,
								handle(
									StreamEvent{
										Kind:     StreamEventError,
										Position: mo.None[int](),
										Content:  mo.None[model.Content](),
										Delta:    mo.None[string](),
										Preview:  mo.None[model.ToolCallPreview](),
										ToolCall: mo.None[model.ToolCall](),
										Response: mo.Some(response),
									},
								),
							)
							if canceled {
								cancel()
							}
							return errors.Join(callbackErr, adapterCause)
						}).
						AnyTimes()
					service := New(
						nil,
						RetryPolicy{
							Enabled:          true,
							MaxRetries:       1,
							Delays:           []time.Duration{time.Nanosecond},
							MaxProviderDelay: time.Second,
						},
						handlers,
						nil,
						nil,
					)
					// Act through the callback boundary with a working overflow owner and retry progress recipient.
					completion, err := service.execute(
						ctx,
						provider,
						ProviderRequest{},
						func(StreamEvent) error { return observerCause },
						func(RetryProgress) error { progress++; return nil },
						errors.New("missing terminal"),
						"missing terminal",
						func(context.Context, ProviderRequest, error) (ConversationPreparation, error) {
							recoveries++
							return ConversationPreparation{History: nil, Committed: true}, nil
						},
					)
					// Assert current observer identity, adapter identity and response data survive without any recovery.
					require.ErrorIs(t, err, observerCause)
					require.ErrorIs(t, err, childProvider)
					require.ErrorIs(t, err, providerCause)
					require.ErrorIs(t, err, adapterCause)
					require.Equal(t, response, completion.response)
					require.Equal(t, 1, attempts)
					require.Zero(t, retries)
					require.Zero(t, progress)
					require.Zero(t, recoveries)
					if canceled {
						require.ErrorIs(t, err, context.Canceled)
					}
				},
			)
		}
	}
}

// TestObserverOwningCancellationRetainsDiagnostics checks callback cancellation keeps earlier recovered provider causes.
func TestObserverOwningCancellationRetainsDiagnostics(t *testing.T) {
	t.Parallel()
	// Arrange a recovered transient attempt followed by owning cancellation during callback delivery.
	controller := gomock.NewController(t)
	provider := NewMockProviderAttempt(controller)
	recovered := errors.New("recovered provider original cause")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	gomock.InOrder(
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
			Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: recovered,
		}),
		provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
				return handle(
					StreamEvent{
						Kind:     StreamEventTextDelta,
						Position: mo.Some(0),
						Content:  mo.None[model.Content](),
						Delta:    mo.Some("text"),
						Preview:  mo.None[model.ToolCallPreview](),
						ToolCall: mo.None[model.ToolCall](),
						Response: mo.None[model.Response](),
					},
				)
			},
		),
	)
	service := New(
		nil,
		RetryPolicy{
			Enabled:          true,
			MaxRetries:       1,
			Delays:           []time.Duration{time.Nanosecond},
			MaxProviderDelay: time.Second,
		},
		nil,
		nil,
		nil,
	)
	// Act while the callback cancels its owning operation.
	_, err := service.execute(
		ctx,
		provider,
		ProviderRequest{},
		func(StreamEvent) error { cancel(); return context.Canceled },
		nil,
		errors.New("missing terminal"),
		"missing terminal",
		nil,
	)
	// Assert current cancellation remains distinct from retained provider diagnostics.
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, recovered)
	require.True(
		t,
		errtree.IsCurrentCancellation(err, func(cause error) bool { return errors.Is(cause, context.Canceled) }),
	)
}
