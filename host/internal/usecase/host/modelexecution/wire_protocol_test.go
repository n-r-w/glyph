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

// TestWireNeutralProviderAcrossCallers replaces the raw adapter without changing ordinary or configured callers.
func TestWireNeutralProviderAcrossCallers(t *testing.T) {
	t.Parallel()
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "configured"}[configured], func(t *testing.T) {
			t.Parallel()
			// Arrange a generated neutral raw adapter with no concrete-provider imports.
			ctrl := gomock.NewController(t)
			provider := NewMockProviderAttempt(ctrl)
			catalog := NewMockCatalogResolver(ctrl)
			middleware := NewMockWireMiddleware(ctrl)
			binding := CatalogBinding{
				Model:           model.Descriptor{},
				ReasoningChoice: model.ReasoningChoiceOff,
				Provider:        provider,
			}
			if configured {
				catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), gomock.Any()).Return(binding, nil)
			} else {
				catalog.EXPECT().ActiveBinding().Return(binding)
			}
			observed := false
			middleware.EXPECT().NewWireCallbacks().Return(WireCallbacks{
				Headers: func(_ context.Context, h WireHeaders, _ func(WireHeaders) error) (WireHeaders, error) {
					h["X-Neutral"] = []string{"selected"}
					return h, nil
				},
				Body: func(_ context.Context, b []byte, _ func([]byte) error) (WireBodyResult, error) {
					return WireBodyResult{Body: append(b, ' '), Replaced: true}, nil
				},
				Response: func(context.Context, WireResponse) error { observed = true; return nil },
				Failed:   nil,
			})
			provider.EXPECT().
				Stream(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, r ProviderRequest, handle StreamHandler) error {
					callbacks, present := r.Wire.Get()
					require.True(t, present, "raw attempt must receive the neutral wire callbacks")
					h, err := callbacks.Headers(ctx, WireHeaders{}, func(WireHeaders) error { return nil })
					require.NoError(t, err)
					require.Equal(t, "selected", h["X-Neutral"][0])
					body, err := callbacks.Body(ctx, []byte(`{}`), func([]byte) error { return nil })
					require.NoError(t, err)
					require.True(t, body.Replaced)
					require.Equal(t, "{} ", string(body.Body))
					require.NoError(t, callbacks.Response(ctx, WireResponse{Status: 200, Headers: nil}))
					return handle(
						StreamEvent{
							Kind:     StreamEventDone,
							Position: mo.None[int](),
							Content:  mo.None[model.Content](),
							Delta:    mo.None[string](),
							Preview:  mo.None[model.ToolCallPreview](),
							ToolCall: mo.None[model.ToolCall](),
							Response: mo.Some(model.Response{}),
						},
					)
				})
			execution := New(catalog, RetryPolicy{}, nil, nil, newTestConversationObservations(t))
			require.NoError(t, execution.BindWireMiddleware(middleware))
			// Act through the unchanged ordinary and configured caller interfaces.
			if configured {
				bindPassThroughFinalizer(t, execution)
				_, err := execution.RequestConfigured(t.Context(), model.Selection{}, "", nil, nil)
				require.NoError(t, err)
			} else {
				require.NoError(
					t,
					streamLogical(t.Context(), execution,
						agentrun.ContextRequest{
							Instructions: "",
							History:      nil,
							Tools:        nil,
						},
						func(agentrun.StreamEvent) error { return nil },
					),
				)
			}
			// Assert the neutral response observation was invoked by either caller.
			require.True(t, observed)
		})
	}
}

// TestSuccessfulWireObserverPreservesProviderRetry keeps ordinary recovery after successful response observation.
func TestSuccessfulWireObserverPreservesProviderRetry(t *testing.T) {
	t.Parallel()
	// Arrange a retryable provider source and a successful observer on each genuine attempt.
	ctrl := gomock.NewController(t)
	provider := NewMockProviderAttempt(ctrl)
	catalog := NewMockCatalogResolver(ctrl)
	middleware := NewMockWireMiddleware(ctrl)
	catalog.EXPECT().
		ResolveConfiguredBinding(gomock.Any(), gomock.Any()).
		Return(CatalogBinding{Model: model.Descriptor{}, ReasoningChoice: model.ReasoningChoiceOff, Provider: provider}, nil)
	observations := 0
	middleware.EXPECT().NewWireCallbacks().Return(WireCallbacks{
		Headers: func(_ context.Context, h WireHeaders, _ func(WireHeaders) error) (WireHeaders, error) { return h, nil },
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (WireBodyResult, error) {
			return WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, WireResponse) error { observations++; return nil },
		Failed:   nil,
	}).Times(2)
	attempts := 0
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, r ProviderRequest, handle StreamHandler) error {
			attempts++
			callbacks, present := r.Wire.Get()
			require.True(t, present, "each genuine raw attempt must receive fresh wire callbacks")
			require.NoError(t, callbacks.Response(ctx, WireResponse{Status: 429, Headers: nil}))
			if attempts == 1 {
				return &ProviderFailureError{
					Classification: ProviderFailureTransient,
					RetryDelay:     mo.None[time.Duration](),
					Cause:          errors.New("ordinary rate limit"),
				}
			}
			return handle(
				StreamEvent{
					Kind:     StreamEventDone,
					Position: mo.None[int](),
					Content:  mo.None[model.Content](),
					Delta:    mo.None[string](),
					Preview:  mo.None[model.ToolCallPreview](),
					ToolCall: mo.None[model.ToolCall](),
					Response: mo.Some(model.Response{}),
				},
			)
		}).
		Times(2)
	execution := New(
		catalog,
		RetryPolicy{Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second},
		nil,
		nil,
		nil,
	)
	require.NoError(t, execution.BindWireMiddleware(middleware))
	// Act through configured execution with successful middleware on a retryable provider response.
	bindPassThroughFinalizer(t, execution)
	_, err := execution.RequestConfigured(t.Context(), model.Selection{}, "", nil, nil)
	// Assert the normal retry policy, rather than middleware-failure suppression, controls the provider attempts.
	require.NoError(t, err)
	require.Equal(t, 2, attempts)
	require.Equal(t, 2, observations)
}
