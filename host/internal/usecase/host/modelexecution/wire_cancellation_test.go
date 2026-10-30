//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestWireOwningCancellationKeepsCancellation preserves cancellation when no distinct handler failure was acquired.
func TestWireOwningCancellationKeepsCancellation(t *testing.T) {
	t.Parallel()
	// Arrange a neutral adapter whose response observation is canceled by the caller.
	ctrl := gomock.NewController(t)
	owner := NewMockWireMiddleware(ctrl)
	provider := NewMockProviderAttempt(ctrl)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	owner.EXPECT().NewWireCallbacks().Return(WireCallbacks{
		Headers: func(_ context.Context, h WireHeaders, _ func(WireHeaders) error) (WireHeaders, error) { return h, nil },
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (WireBodyResult, error) {
			return WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, WireResponse) error { cancel(); return context.Canceled },
		Failed:   nil,
	})
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, request ProviderRequest, _ StreamHandler) error {
			callback, present := request.Wire.Get()
			require.True(t, present, "raw attempt must receive the neutral wire callbacks")
			err := callback.Response(ctx, WireResponse{})
			callback.Failed(err)
			return ctx.Err()
		})
	service := New(
		NewMockCatalogResolver(ctrl),
		RetryPolicy{Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second},
		nil,
		nil,
		nil,
	)
	require.NoError(t, service.BindWireMiddleware(owner))
	// Act through the shared retry and classification owner.
	_, err := service.execute(
		ctx,
		provider,
		preparedRequest{},
		nil,
		nil,
		errors.New("missing terminal"),
		"missing terminal",
		nil,
	)
	// Assert a pure owning cancellation does not acquire ordinary extension-failure meaning.
	require.ErrorIs(t, err, context.Canceled)
	failure, classified := errors.AsType[*LogicalFailureError](err)
	if classified {
		require.NotEqual(t, FailureExtensionFailed, failure.Category)
	}
}
