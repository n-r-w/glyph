//go:build integration

package compatible

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireStreamSecrets retains successful-response secret knowledge through both compatible stream decoders.
func TestWireStreamSecrets(t *testing.T) {
	t.Parallel()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		t.Run(string(api), func(t *testing.T) {
			t.Parallel()
			// Arrange successful safe observation followed by an SSE provider error echoing response credentials.
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Set-Cookie", "session=cookie-private; Path=/; HttpOnly; SameSite=Lax")
				w.Header().Set("X-API-Key", "response-auth-private")
				w.Header().Set("X-Secret-Echo", "cookie-private response-auth-private")
				_, _ = fmt.Fprint(
					w,
					"data: "+`{"error":{"message":"complete provider stream failure account=ordinary-account `+
						`Set-Cookie: session=cookie-private; Path=/; HttpOnly; SameSite=Lax `+
						`auth=response-auth-private","type":"invalid_request_error"}}`+"\n\n",
				)
			}))
			defer server.Close()
			driver, err := New(Config{
				ProviderID:                 "compatible",
				BaseURL:                    server.URL,
				API:                        api,
				Models:                     map[model.ID]API{"demo": api},
				ReasoningFormats:           nil,
				ReasoningCompatibilityKeys: nil,
				APIKey:                     expectAPIKey(t, "synthetic-api-token", nil, 1),
			})
			require.NoError(t, err)
			observed := false
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
				Response: func(_ context.Context, r modelexecution.WireResponse) error {
					observed = true
					assert.Equal(t, int64(http.StatusOK), r.Status)
					assert.NotContains(t, r.Headers, "Set-Cookie")
					assert.NotContains(t, r.Headers, "X-Api-Key")
					assert.Equal(t, []string{"[REDACTED] [REDACTED]"}, r.Headers["X-Secret-Echo"])
					return nil
				},
				Failed: func(err error) { t.Errorf("successful observer must not report middleware failure: %v", err) },
			})
			var terminal model.Response
			// Act through the actual adapter, SDK stream decoder and terminal delivery.
			err = driver.Stream(t.Context(), request, func(event modelexecution.StreamEvent) error {
				if response, present := event.Response.Get(); present {
					terminal = response
				}
				return nil
			})
			// Assert one dispatch preserves normal provider failure and all non-secret cookie context.
			require.Error(t, err)
			assert.True(t, observed)
			assert.Equal(t, 1, calls)
			failure, provider := errors.AsType[*modelexecution.ProviderFailureError](err)
			require.True(t, provider)
			assert.Equal(t, modelexecution.ProviderFailureNonRetryable, failure.Classification)
			for _, text := range []string{err.Error(), terminal.ErrorMessage.OrEmpty()} {
				assert.Contains(t, text, "complete provider stream failure account=ordinary-account")
				assert.Contains(t, text, "Set-Cookie: session=[REDACTED]; Path=/; HttpOnly; SameSite=Lax")
				assert.Contains(t, text, "auth=[REDACTED]")
				assert.NotContains(t, text, "cookie-private")
				assert.NotContains(t, text, "response-auth-private")
			}
		})
	}
}
