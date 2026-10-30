//go:build integration

package codex

import (
	"context"
	"errors"
	"fmt"
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

// TestWireStreamSecrets checks successful metadata observation followed by raw and configured provider failures.
func TestWireStreamSecrets(t *testing.T) {
	t.Parallel()
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			t.Parallel()
			// Arrange a successful response with authentication headers followed by an SSE failure.
			ctrl := gomock.NewController(t)
			credentials := NewMockCredentials(ctrl)
			token := testJWT(
				t,
				map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "ordinary-account"}},
			)
			credentials.EXPECT().Load().Return(
				testCredentialPayload(t, token, "refresh", "ordinary-account", time.Now().Add(time.Hour)), true, nil,
			)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Set-Cookie", "session=cookie-private; Path=/; HttpOnly; SameSite=Lax")
				w.Header().Set("X-API-Key", "response-auth-private")
				w.Header().Set("X-Secret-Echo", "cookie-private response-auth-private")
				_, _ = fmt.Fprint(
					w,
					"data: "+`{"type":"error","code":"invalid_request_error","message":"`+
						`complete provider stream failure account=ordinary-account `+
						`Set-Cookie: session=cookie-private; Path=/; HttpOnly; SameSite=Lax `+
						`auth=response-auth-private"}`+"\n\n",
				)
			}))
			defer server.Close()
			observed := false
			callbacks := modelexecution.WireCallbacks{
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
				Failed: func(err error) { t.Errorf("provider error must not become middleware failure: %v", err) },
			}
			driver := newDriver(testConfig(), credentials, NewMockInteraction(ctrl), testProviderOptions(server))
			var terminal model.Response
			var err error
			// Act through raw terminal delivery or configured execution with retries disabled.
			if configured {
				owner := modelexecution.NewMockWireMiddleware(ctrl)
				owner.EXPECT().NewWireCallbacks().Return(callbacks)
				catalog := modelexecution.NewMockCatalogResolver(ctrl)
				catalog.EXPECT().
					ResolveConfiguredBinding(gomock.Any(), gomock.Any()).
					Return(modelexecution.CatalogBinding{
						Model: testModelDescriptor(
							"gpt-test",
						),
						ReasoningChoice: model.ReasoningChoiceOff,
						Provider:        driver,
					}, nil)
				execution := modelexecution.New(catalog, modelexecution.RetryPolicy{}, nil, nil, nil)
				require.NoError(t, execution.BindWireMiddleware(owner))
				_, err = execution.RequestConfigured(t.Context(), model.Selection{}, "instructions", nil, nil)
				failure, ok := errors.AsType[*modelexecution.LogicalFailureError](err)
				require.True(t, ok)
				assert.Equal(t, modelexecution.FailureModelFailed, failure.Category)
			} else {
				err = driver.Stream(t.Context(), modelexecution.ProviderRequest{
					Instructions:    "instructions",
					Model:           testModelDescriptor("gpt-test"),
					ReasoningChoice: model.ReasoningChoiceOff,
					History:         nil,
					Tools:           nil,
					Wire:            mo.Some(callbacks),
				}, func(event modelexecution.StreamEvent) error {
					if response, present := event.Response.Get(); present {
						terminal = response
					}
					return nil
				})
				assertSafeStreamDiagnostics(t, terminal.ErrorMessage.OrEmpty())
			}
			// Assert safe observation, one dispatch, provider identity/category and complete safe diagnostics.
			require.Error(t, err)
			assert.True(t, observed)
			assert.Equal(t, 1, calls)
			failure, provider := errors.AsType[*modelexecution.ProviderFailureError](err)
			require.True(t, provider)
			assert.Equal(t, modelexecution.ProviderFailureNonRetryable, failure.Classification)
			assertSafeStreamDiagnostics(t, err.Error())
		})
	}
}

// assertSafeStreamDiagnostics verifies full cookie context and ordinary identifiers survive credential masking.
func assertSafeStreamDiagnostics(t *testing.T, text string) {
	t.Helper()
	assert.Contains(t, text, "complete provider stream failure account=ordinary-account")
	assert.Contains(t, text, "Set-Cookie: session=[REDACTED]; Path=/; HttpOnly; SameSite=Lax")
	assert.Contains(t, text, "auth=[REDACTED]")
	assert.NotContains(t, text, "cookie-private")
	assert.NotContains(t, text, "response-auth-private")
}
