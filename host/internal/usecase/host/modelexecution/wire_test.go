//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestWireFailureSurvivesSDKReduction retains an acquired callback cause when an adapter returns only cancellation.
func TestWireFailureSurvivesSDKReduction(t *testing.T) {
	t.Parallel()
	// Arrange a neutral raw-attempt substitute that reduces its SDK result to cancellation.
	ctrl := gomock.NewController(t)
	owner := NewMockWireMiddleware(ctrl)
	provider := NewMockProviderAttempt(ctrl)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := errors.Join(errors.New("wire observer rejected metadata"), &ProviderFailureError{
		Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](), Cause: io.ErrUnexpectedEOF,
	})
	owner.EXPECT().NewWireCallbacks().Return(WireCallbacks{
		Headers: func(_ context.Context, h WireHeaders, _ func(WireHeaders) error) (WireHeaders, error) { return h, nil },
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (WireBodyResult, error) {
			return WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, WireResponse) error { cancel(); return source },
		Failed:   nil,
	})
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, request ProviderRequest, _ StreamHandler) error {
			callbacks, present := request.Wire.Get()
			require.True(t, present, "raw attempt must receive the neutral wire callbacks")
			require.ErrorIs(t, callbacks.Response(ctx, WireResponse{Status: 429, Headers: WireHeaders{}}), source)
			return context.Canceled
		},
	)
	service := New(NewMockCatalogResolver(ctrl), RetryPolicy{
		Enabled: true, MaxRetries: 2, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
	}, nil, nil, nil)
	require.NoError(t, service.BindWireMiddleware(owner))
	// Act through the ordinary shared attempt/retry owner.
	_, err := service.execute(ctx, provider, ProviderRequest{}, nil, nil,
		errors.New("missing terminal"), "missing terminal", nil)
	// Assert the acquired middleware cause wins over cancellation and retry-like descendants.
	var failure *LogicalFailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, FailureExtensionFailed, failure.Category)
	require.ErrorIs(t, err, source)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.ErrorIs(t, err, context.Canceled)
	require.Contains(t, err.Error(), "wire observer rejected metadata")
}
