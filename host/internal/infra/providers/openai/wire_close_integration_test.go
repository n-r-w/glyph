//go:build integration

package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

//go:generate go tool mockgen -destination=wire_readcloser_mock_test.go -package=openai -build_constraint=integration io ReadCloser

// TestWireObserverFailureRetainsCloseError closes unread responses and keeps independent cleanup causes.
func TestWireObserverFailureRetainsCloseError(t *testing.T) {
	t.Parallel()
	// Arrange a real SDK exchange with a generated response-body substitute that fails to close.
	ctrl := gomock.NewController(t)
	body := NewMockReadCloser(ctrl)
	closeCause := errors.New("response close full source")
	observerCause := errors.New("observer full source")
	body.EXPECT().Close().Return(closeCause)
	body.EXPECT().Read(gomock.Any()).Return(0, io.EOF).AnyTimes()
	var acquired error
	callbacks := modelexecution.WireCallbacks{
		Headers: func(
			_ context.Context,
			h modelexecution.WireHeaders,
			_ func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			return h, nil
		},
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
			return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
		},
		Response: func(context.Context, modelexecution.WireResponse) error { return observerCause },
		Failed:   func(err error) { acquired = err },
	}
	client := sdk.NewClient(
		option.WithAPIKey("secret"),
		option.WithMaxRetries(0),
		new(WireDiagnostics).NewWireOption(mo.Some(callbacks)),
		option.WithMiddleware(func(*http.Request, option.MiddlewareNext) (*http.Response, error) {
			return &http.Response{
				Status:           "",
				StatusCode:       http.StatusTooManyRequests,
				Proto:            "",
				ProtoMajor:       0,
				ProtoMinor:       0,
				Header:           nil,
				Body:             body,
				ContentLength:    0,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          nil,
				TLS:              nil,
			}, nil
		}),
	)
	// Act before SDK error decoding or body consumption.
	err := client.Post(t.Context(), "/wire", map[string]any{}, nil)
	// Assert the acquired middleware envelope keeps both independent original errors.
	require.Error(t, err)
	require.ErrorIs(t, acquired, observerCause)
	require.ErrorIs(t, acquired, closeCause)
	require.Contains(t, acquired.Error(), observerCause.Error())
	require.Contains(t, acquired.Error(), closeCause.Error())
}
