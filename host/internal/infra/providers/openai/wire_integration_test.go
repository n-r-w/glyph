//go:build integration

package openai

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestSDKWireProjection restores retained private replay and hides credentials across the real SDK boundary.
func TestSDKWireProjection(t *testing.T) {
	t.Parallel()
	// Arrange a real SDK request whose replay must stay private while public JSON is replaced.
	var received []byte
	var header string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		header = r.Header.Get("X-Test")
		w.Header().Set("Set-Cookie", "session=cookie-private; Path=/")
		w.Header().Set("X-Cookie-Echo", "cookie-private")
		w.Header().Set("X-Secret-Echo", "token-private")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"normal rate limit"}}`))
	}))
	defer server.Close()
	var observed bool
	callbacks := modelexecution.WireCallbacks{
		Headers: func(
			_ context.Context,
			h modelexecution.WireHeaders,
			_ func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			require.NotContains(t, h, "Authorization")
			require.NotContains(t, h, "Chatgpt-Account-Id")
			h["X-Test"] = []string{"changed"}
			return h, nil
		},
		Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
			require.NotContains(t, string(b), "cipher-private")
			require.NotContains(t, string(b), "reasoning-private-id")
			require.Contains(t, string(b), "$glyph_replay")
			var value map[string]any
			require.NoError(t, json.Unmarshal(b, &value))
			value["marker"] = "wire-final"
			replacement, err := json.Marshal(value)
			require.NoError(t, err)
			return modelexecution.WireBodyResult{Body: replacement, Replaced: true}, nil
		},
		Response: func(_ context.Context, r modelexecution.WireResponse) error {
			observed = true
			require.Equal(t, int64(429), r.Status)
			require.NotContains(t, r.Headers, "Set-Cookie")
			require.NotContains(t, r.Headers["X-Secret-Echo"], "token-private")
			require.NotContains(t, r.Headers["X-Cookie-Echo"], "cookie-private")
			return nil
		},
		Failed: func(err error) { t.Errorf("unexpected bridge failure: %v", err) },
	}
	client := sdk.NewClient(
		option.WithBaseURL(server.URL),
		option.WithAPIKey("token-private"),
		option.WithHeader(
			"chatgpt-account-id",
			"account-private",
		),
		option.WithMaxRetries(0),
		new(WireDiagnostics).NewWireOption(mo.Some(callbacks)),
	)
	body := map[string]any{
		"input": []any{
			map[string]any{
				"type":              "reasoning",
				"id":                "reasoning-private-id",
				"encrypted_content": "cipher-private",
				"summary":           []any{},
			},
		},
	}
	// Act through SDK serialization, middleware and HTTP error decoding.
	err := client.Post(t.Context(), "/wire", body, nil)
	// Assert successful observation preserves the ordinary provider error and final request contents.
	require.Error(t, err)
	require.Contains(t, err.Error(), "normal rate limit")
	require.True(t, observed)
	require.Equal(t, "changed", header)
	require.Contains(t, string(received), "cipher-private")
	require.Contains(t, string(received), "wire-final")
	require.NotContains(t, string(received), "$glyph_replay")
}
