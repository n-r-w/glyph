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

// TestCodexAcquiredDiagnosticRedaction preserves rejection diagnostics and identity while removing credentials.
func TestCodexAcquiredDiagnosticRedaction(t *testing.T) {
	t.Parallel()
	for _, configured := range []bool{false, true} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("configured=%t/canceled=%t", configured, canceled), func(t *testing.T) {
				t.Parallel()
				// Arrange real credential resolution, diagnostic capture and a failing response observer.
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ctrl := gomock.NewController(t)
				credentials := NewMockCredentials(ctrl)
				const account = "ordinary-account-review"
				token := testJWT(t, map[string]any{
					"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": account},
				})
				credentials.EXPECT().Load().Return(
					testCredentialPayload(t, token, "refresh", account, time.Now().Add(time.Hour)), true, nil,
				)
				calls := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					assert.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
					w.Header().Set("Set-Cookie", "session=synthetic-session-secret; Path=/")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = fmt.Fprintf(
						w,
						`{"error":{"message":"complete provider detail token=%s account=%s session=synthetic-session-secret"}}`,
						token,
						account,
					)
				}))
				defer server.Close()
				source := fmt.Errorf("complete observer source token=%s account=%s: unexpected EOF", token, account)
				callbacks := modelexecution.WireCallbacks{
					Headers: func(
						_ context.Context, h modelexecution.WireHeaders, _ func(modelexecution.WireHeaders) error,
					) (modelexecution.WireHeaders, error) {
						return h, nil
					},
					Body: func(_ context.Context, b []byte, _ func([]byte) error) (modelexecution.WireBodyResult, error) {
						return modelexecution.WireBodyResult{Body: b, Replaced: false}, nil
					},
					Response: func(context.Context, modelexecution.WireResponse) error {
						if canceled {
							cancel()
						}
						return source
					},
					Failed: nil,
				}
				driver := newDriver(testConfig(), credentials, NewMockInteraction(ctrl), testProviderOptions(server))
				var terminal model.Response
				var err error
				// Act through configured execution or the raw adapter's terminal callback.
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
					execution := modelexecution.New(catalog, modelexecution.RetryPolicy{
						Enabled: true, MaxRetries: 2, Delays: []time.Duration{0}, MaxProviderDelay: time.Second,
					}, nil, nil, nil)
					require.NoError(t, execution.BindWireMiddleware(owner))
					_, err = execution.RequestConfigured(ctx, model.Selection{}, "instructions", nil, nil)
				} else {
					err = driver.Stream(ctx, modelexecution.ProviderRequest{
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
				}
				// Assert full non-secret causes, account identifiers and status survive with one dispatch.
				require.Error(t, err)
				require.Equal(t, 1, calls)
				require.ErrorIs(t, err, source)
				if canceled {
					require.ErrorIs(t, err, context.Canceled)
				}
				if configured {
					failure, ok := errors.AsType[*modelexecution.LogicalFailureError](err)
					require.True(t, ok)
					require.Equal(t, modelexecution.FailureExtensionFailed, failure.Category)
				} else {
					assertCodexSafeDiagnostics(t, terminal.ErrorMessage.OrEmpty(), token, account)
				}
				assertCodexSafeDiagnostics(t, err.Error(), token, account)
			})
		}
	}
}

// assertCodexSafeDiagnostics checks complete non-secret acquired text at a caller-visible boundary.
func assertCodexSafeDiagnostics(t *testing.T, text, token, account string) {
	t.Helper()
	assert.Contains(t, text, "complete observer source token=[REDACTED] account="+account+": unexpected EOF")
	assert.Contains(t, text, "complete provider detail token=[REDACTED] account="+account)
	assert.Contains(t, text, "HTTP 429 Too Many Requests")
	assert.NotContains(t, text, token)
	assert.NotContains(t, text, "synthetic-session-secret")
	assert.Contains(t, text, "session=[REDACTED]")
}
