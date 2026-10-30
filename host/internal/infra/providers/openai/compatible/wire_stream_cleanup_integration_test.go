//go:build integration

package compatible

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

//go:generate go tool mockgen -destination=wire_readcloser_mock_test.go -package=compatible -build_constraint=integration io ReadCloser
//go:generate go tool mockgen -destination=wire_roundtripper_mock_test.go -package=compatible -build_constraint=integration net/http RoundTripper

// TestWireStreamCleanupSecrets retains cleanup identity and response-secret knowledge through both compatible APIs.
func TestWireStreamCleanupSecrets(t *testing.T) {
	t.Parallel()
	for _, api := range []API{APIChatCompletions, APIResponses} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/canceled=%t", api, canceled), func(t *testing.T) {
				t.Parallel()
				// Arrange a successful observer followed by a provider stream error and failing close.
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				cleanup := errors.New("complete cleanup account=ordinary-account session=cookie-private")
				reader := strings.NewReader(
					"data: " + `{"error":{"message":"complete provider failure account=ordinary-account ` +
						`session=cookie-private","type":"invalid_request_error"}}` + "\n\n",
				)
				ctrl := gomock.NewController(t)
				body := NewMockReadCloser(ctrl)
				body.EXPECT().Read(gomock.Any()).DoAndReturn(reader.Read).AnyTimes()
				body.EXPECT().Close().DoAndReturn(func() error {
					if canceled {
						cancel()
					}
					return cleanup
				})
				transport := NewMockRoundTripper(ctrl)
				transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(r *http.Request) (*http.Response, error) {
					return &http.Response{
						Status:     "200 OK",
						StatusCode: http.StatusOK,
						Proto:      "HTTP/1.1",
						ProtoMajor: 1,
						ProtoMinor: 1,
						Header: http.Header{
							"Content-Type": []string{"text/event-stream"},
							"Set-Cookie":   []string{"session=cookie-private; Path=/"},
						},
						Body:             body,
						ContentLength:    -1,
						TransferEncoding: nil,
						Close:            false,
						Uncompressed:     false,
						Trailer:          nil,
						Request:          r,
						TLS:              nil,
					}, nil
				})
				driver, err := New(Config{
					ProviderID:                 "compatible",
					BaseURL:                    "https://stream.invalid",
					API:                        api,
					Models:                     map[model.ID]API{"demo": api},
					ReasoningFormats:           nil,
					ReasoningCompatibilityKeys: nil,
					APIKey:                     expectAPIKey(t, "api-token-private", nil, 1),
				})
				require.NoError(t, err)
				driver.httpClient.Transport = transport
				observed := false
				request := richRequest("compatible", "demo")
				request.Wire = mo.Some(newSuccessfulWireCallbacks(t, &observed))
				var terminal model.Response
				// Act through final stream cleanup and provider failure delivery.
				err = driver.Stream(ctx, request, func(event modelexecution.StreamEvent) error {
					if response, present := event.Response.Get(); present {
						terminal = response
					}
					return nil
				})
				// Assert cleanup/cancellation identities and full non-secret causes remain at both boundaries.
				require.Error(t, err)
				require.ErrorIs(t, err, cleanup)
				assert.True(t, observed)
				if canceled {
					require.ErrorIs(t, err, context.Canceled)
					assert.Contains(t, terminal.ErrorMessage.OrEmpty(), context.Canceled.Error())
				}
				_, provider := errors.AsType[*modelexecution.ProviderFailureError](err)
				assert.True(t, provider)
				for _, text := range []string{err.Error(), terminal.ErrorMessage.OrEmpty()} {
					assert.Contains(t, text, "complete provider failure account=ordinary-account session=[REDACTED]")
					assert.Contains(t, text, "complete cleanup account=ordinary-account session=[REDACTED]")
					assert.NotContains(t, text, "cookie-private")
				}
			})
		}
	}
}
