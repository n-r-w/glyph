//go:build integration

package codex

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestCodexWireCallbacks retains observer failure alongside acquired Codex HTTP diagnostics.
func TestCodexWireCallbacks(t *testing.T) {
	t.Parallel()
	// Arrange the real Codex serializer, credential resolution and diagnostic transport.
	ctrl := gomock.NewController(t)
	credentials := NewMockCredentials(ctrl)
	token := testJWT(t, map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	credentials.EXPECT().
		Load().
		Return(testCredentialPayload(t, token, "refresh", "account", time.Now().Add(time.Hour)), true, nil)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "wire-final", r.Header.Get("X-Wire"))
		w.Header().Set("X-Request-Id", "codex-diagnostic")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"codex acquired rate limit"}}`))
	}))
	defer server.Close()
	observed := false
	source := errors.New("codex observer failure: unexpected EOF")
	var acquired error
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
	driver := newDriver(testConfig(), credentials, NewMockInteraction(ctrl), testProviderOptions(server))
	request := modelexecution.ProviderRequest{
		Instructions:    "instructions",
		Model:           testModelDescriptor("gpt-test"),
		ReasoningChoice: model.ReasoningChoiceOff,
		History:         nil,
		Tools:           nil,
		Wire:            mo.Some(callbacks),
	}
	// Act through the real raw adapter boundary.
	err := driver.Stream(t.Context(), request, func(modelexecution.StreamEvent) error { return nil })
	// Assert the observer ran and retained its complete source independently of SDK classification.
	require.Error(t, err)
	require.Equal(t, 1, calls)
	require.True(t, observed)
	require.ErrorIs(t, acquired, source)
	require.Contains(t, err.Error(), "codex acquired rate limit")
}
