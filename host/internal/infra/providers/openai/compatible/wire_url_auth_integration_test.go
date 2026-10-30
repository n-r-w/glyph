//go:build integration

package compatible

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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

// TestWireURLAuthentication protects derived Basic credentials and preserves explicit Authorization precedence.
func TestWireURLAuthentication(t *testing.T) {
	t.Parallel()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		for _, explicit := range []bool{false, true} {
			for _, observerFails := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/explicit=%t/failure=%t", api, explicit, observerFails), func(t *testing.T) {
					t.Parallel()
					// Arrange a supported URL-authenticated endpoint and optional explicit bearer credential.
					const password = "synthetic/p@ssword"
					userinfo := url.UserPassword("synthetic-user", password)
					encoded := base64.StdEncoding.EncodeToString([]byte("synthetic-user:" + password))
					authorization := "Basic " + encoded
					key := ""
					if explicit {
						key = "explicit-api-token"
						authorization = "Bearer " + key
					}
					calls := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						assert.Equal(t, authorization, r.Header.Get("Authorization"))
						w.Header().Set("X-Auth-Echo", r.Header.Get("Authorization"))
						w.Header().Set("X-Diagnostic", "complete ordinary diagnostic account=ordinary-account")
						w.WriteHeader(http.StatusTooManyRequests)
						_, _ = w.Write([]byte(`{"error":{"message":"complete provider rejection"}}`))
					}))
					defer server.Close()
					baseURL := "http://" + userinfo.String() + "@" + server.Listener.Addr().String()
					driver, err := New(Config{
						ProviderID:                 "compatible",
						BaseURL:                    baseURL,
						API:                        api,
						Models:                     map[model.ID]API{"demo": api},
						ReasoningFormats:           nil,
						ReasoningCompatibilityKeys: nil,
						APIKey:                     expectAPIKey(t, key, nil, 1),
					})
					require.NoError(t, err)
					source := errors.New("complete observer source " + authorization + ": unexpected EOF")
					var acquired error
					var observed modelexecution.WireResponse
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
							observed = r
							if observerFails {
								return source
							}
							return nil
						},
						Failed: func(err error) { acquired = err },
					})
					// Act through the production adapter, SDK and real HTTP authentication boundary.
					err = driver.Stream(t.Context(), request, func(modelexecution.StreamEvent) error { return nil })
					// Assert authentication works once while public metadata and observer errors contain only safe details.
					require.Error(t, err)
					require.Equal(t, 1, calls)
					assert.Equal(t, int64(http.StatusTooManyRequests), observed.Status)
					assert.Equal(
						t,
						[]string{"complete ordinary diagnostic account=ordinary-account"},
						observed.Headers["X-Diagnostic"],
					)
					assert.Equal(t, []string{"[REDACTED]"}, observed.Headers["X-Auth-Echo"])
					if observerFails {
						require.ErrorIs(t, acquired, source)
						require.ErrorIs(t, err, source)
						assert.Contains(t, err.Error(), "complete observer source [REDACTED]: unexpected EOF")
					} else {
						assert.Contains(t, err.Error(), "complete provider rejection")
						assert.Contains(t, err.Error(), "429")
					}
					assert.NotContains(t, err.Error(), authorization)
					assert.NotContains(t, err.Error(), password)
					assert.NotContains(t, err.Error(), userinfo.String())
					if !observerFails {
						failure, ok := errors.AsType[*modelexecution.ProviderFailureError](err)
						require.True(t, ok)
						assert.Equal(t, modelexecution.ProviderFailureTransient, failure.Classification)
					}
				})
			}
		}
	}
}
