//go:build integration

package compatible

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireURLTransportDiagnostics preserves the non-secret username when an authenticated connection fails.
func TestWireURLTransportDiagnostics(t *testing.T) {
	t.Parallel()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		t.Run(string(api), func(t *testing.T) {
			t.Parallel()
			// Arrange an authenticated endpoint that closes before sending a response.
			const username = "ordinary-user-identifier"
			const password = "synthetic-password"
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				user, secret, ok := r.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, username, user)
				assert.Equal(t, password, secret)
				connection, _, err := w.(http.Hijacker).Hijack()
				assert.NoError(t, err)
				if err == nil {
					assert.NoError(t, connection.Close())
				}
			}))
			defer server.Close()
			baseURL := "http://" + url.UserPassword(username, password).String() + "@" + server.Listener.Addr().String()
			driver, err := New(Config{
				ProviderID: "compatible", BaseURL: baseURL, API: api, Models: map[model.ID]API{"demo": api},
				ReasoningFormats: nil, ReasoningCompatibilityKeys: nil, APIKey: expectAPIKey(t, "", nil, 1),
			})
			require.NoError(t, err)
			request := richRequest("compatible", "demo")
			request.Wire = mo.Some(modelexecution.WireCallbacks{
				Headers: func(
					_ context.Context, h modelexecution.WireHeaders, _ func(modelexecution.WireHeaders) error,
				) (modelexecution.WireHeaders, error) {
					return h, nil
				},
				Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
					return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
				},
				Response: func(context.Context, modelexecution.WireResponse) error {
					t.Error("closed connection must not invoke a response observer")
					return nil
				},
				Failed: func(err error) { t.Errorf("transport failure must not become middleware failure: %v", err) },
			})
			// Act through the real HTTP failure path.
			err = driver.Stream(t.Context(), request, func(modelexecution.StreamEvent) error { return nil })
			// Assert one dispatch retains the transport cause and ordinary identifier without its password.
			require.Error(t, err)
			assert.Equal(t, 1, calls)
			assert.Contains(t, err.Error(), username)
			assert.Contains(t, err.Error(), "EOF")
			assert.NotContains(t, err.Error(), password)
		})
	}
}
