//go:build integration

package compatible

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireCallbacksBothAPIs dispatches the neutral wire protocol through both compatible serializers.
func TestWireCallbacksBothAPIs(t *testing.T) {
	t.Parallel()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		t.Run(string(api), func(t *testing.T) {
			t.Parallel()
			// Arrange a real adapter and an observer failing on a retryable HTTP status.
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "wire-final", r.Header.Get("X-Wire"))
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"message":"normal rate limit"}}`))
			}))
			defer server.Close()
			driver, err := New(Config{
				ProviderID: "compatible",
				BaseURL:    server.URL,
				API:        api,
				Models: map[model.ID]API{
					"demo": api,
				},
				ReasoningFormats:           nil,
				ReasoningCompatibilityKeys: nil,
				APIKey:                     expectAPIKey(t, "secret", nil, 1),
			})
			require.NoError(t, err)
			observed := false
			source := errors.New("wire observer failure: unexpected EOF")
			acquired := error(nil)
			callbacks := modelexecution.WireCallbacks{
				Headers: func(
					_ context.Context,
					h modelexecution.WireHeaders,
					_ func(modelexecution.WireHeaders) error,
				) (modelexecution.WireHeaders, error) {
					h["X-Wire"] = []string{"wire-final"}
					return h, nil
				},
				Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
					return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
				},
				Response: func(context.Context, modelexecution.WireResponse) error { observed = true; return source },
				Failed:   func(err error) { acquired = err },
			}
			request := richRequest("compatible", "demo")
			request.Wire = mo.Some(callbacks)
			// Act through the raw provider port, not a transport substitute.
			err = driver.Stream(t.Context(), request, func(modelexecution.StreamEvent) error { return nil })
			// Assert one real provider dispatch and independent acquisition of the observer cause.
			require.Error(t, err)
			require.Equal(t, 1, calls)
			require.True(t, observed)
			require.ErrorIs(t, acquired, source)
		})
	}
}
