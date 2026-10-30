//go:build integration

package compatible

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireStreamTerminalSecrets masks response secrets in errors acquired after terminal-message preparation.
func TestWireStreamTerminalSecrets(t *testing.T) {
	t.Parallel()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		t.Run(string(api), func(t *testing.T) {
			t.Parallel()
			// Arrange successful metadata observation followed by a provider error and failing terminal delivery.
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Set-Cookie", "session=cookie-private; Path=/")
				_, _ = io.WriteString(w, "data: "+`{"error":{"message":"`+
					`complete provider failure account=ordinary-account session=cookie-private",`+
					`"type":"invalid_request_error"}}`+"\n\n")
			}))
			defer server.Close()
			driver, err := New(Config{
				ProviderID:                 "compatible",
				BaseURL:                    server.URL,
				API:                        api,
				Models:                     map[model.ID]API{"demo": api},
				ReasoningFormats:           nil,
				ReasoningCompatibilityKeys: nil,
				APIKey:                     expectAPIKey(t, "api-token-private", nil, 1),
			})
			require.NoError(t, err)
			observed := false
			request := richRequest("compatible", "demo")
			request.Wire = mo.Some(newSuccessfulWireCallbacks(t, &observed))
			terminalCause := errors.New("complete terminal cause account=ordinary-account session=cookie-private")
			var terminal model.Response
			// Act through the actual stream decoder and a terminal consumer that acquires its own error.
			err = driver.Stream(t.Context(), request, func(event modelexecution.StreamEvent) error {
				if response, present := event.Response.Get(); present {
					terminal = response
					return terminalCause
				}
				return nil
			})
			// Assert the late source identity and complete safe diagnostics remain after one dispatch.
			require.ErrorIs(t, err, terminalCause)
			assert.True(t, observed)
			assert.Equal(t, 1, calls)
			assert.Contains(t, err.Error(), "complete terminal cause account=ordinary-account session=[REDACTED]")
			for _, text := range []string{err.Error(), terminal.ErrorMessage.OrEmpty()} {
				assert.Contains(t, text, "complete provider failure account=ordinary-account session=[REDACTED]")
				assert.NotContains(t, text, "cookie-private")
			}
		})
	}
}
