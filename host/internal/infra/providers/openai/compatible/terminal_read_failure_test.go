//go:build integration

package compatible

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestResponsesTerminalRefusalBeforeReadFailure checks refusal classification and causes after HTTP truncation.
func TestResponsesTerminalRefusalBeforeReadFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		// name identifies the refusal classification.
		name string
		// code is the provider refusal code.
		code string
		// classification is the provider-neutral refusal identity.
		classification modelexecution.ProviderFailureClassification
	}{
		{
			name: "context overflow", code: "context_length_exceeded",
			classification: modelexecution.ProviderFailureContextOverflow,
		},
		{name: "non retryable", code: "invalid_request", classification: modelexecution.ProviderFailureNonRetryable},
	} {
		for _, delivery := range []string{"accepted", "content end failure", "terminal failure"} {
			t.Run(scenario.name+"/"+delivery, func(t *testing.T) {
				t.Parallel()
				// Arrange a received terminal refusal followed by an incomplete HTTP body.
				var requests atomic.Int64
				body := "data: " + `{"type":"response.output_text.delta",` +
					`"output_index":0,"content_index":0,"delta":"partial"}` + "\n\n" +
					fmt.Sprintf(
						"data: "+`{"type":"response.failed","response":{"id":"rejected","model":"demo",`+
							`"status":"failed","output":[],"error":{"code":%q,`+
							`"message":"provider terminal rejection full unique text"}}}`+"\n\n",
						scenario.code,
					)
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					writer.Header().Set("Content-Type", "text/event-stream")
					writer.Header().Set("Content-Length", fmt.Sprint(len(body)+100))
					_, _ = writer.Write([]byte(body))
				}))
				t.Cleanup(server.Close)
				driver, err := New(Config{
					ProviderID: "local", BaseURL: server.URL, API: APIResponses,
					Models: map[model.ID]API{"demo": ""}, APIKey: expectAPIKey(t, "", nil, 1),
					ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
				})
				require.NoError(t, err)
				deliveryCause := errors.New("independent delivery cause")
				var terminal model.Response

				// Act through the raw adapter with an optional independent delivery failure.
				err = driver.Stream(
					t.Context(),
					richRequest("local", "demo"),
					func(event modelexecution.StreamEvent) error {
						if event.Kind == modelexecution.StreamEventError {
							terminal, _ = event.Response.Get()
							if delivery == "terminal failure" {
								return deliveryCause
							}
						}
						if event.Kind == modelexecution.StreamEventContentEnd && delivery == "content end failure" {
							return deliveryCause
						}
						return nil
					},
				)

				// Assert the acquired causes, delivery-dependent classification, and single provider request.
				require.Error(t, err)
				assert.Contains(t, err.Error(), "provider terminal rejection full unique text")
				require.ErrorIs(t, err, io.ErrUnexpectedEOF)
				assert.EqualValues(t, 1, requests.Load())
				var refusal *providerResponseError
				require.ErrorAs(t, err, &refusal)
				assert.Equal(t, scenario.code, refusal.code)
				var failure *modelexecution.ProviderFailureError
				if delivery != "accepted" {
					require.ErrorIs(t, err, deliveryCause)
					assert.NotErrorAs(t, err, &failure, "delivery failure must remain outside provider classification")
				} else {
					require.ErrorAs(t, err, &failure)
					assert.Equal(t, scenario.classification, failure.Classification)
				}
				if delivery == "content end failure" {
					return
				}
				message, present := terminal.ErrorMessage.Get()
				require.True(t, present)
				assert.Contains(t, message, "provider terminal rejection full unique text")
				assert.Contains(t, message, io.ErrUnexpectedEOF.Error())
			})
		}
	}
}
