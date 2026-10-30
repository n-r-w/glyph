//go:build integration

package codex

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

// TestWireStreamCleanupSecrets retains acquired response secrets through cleanup and concurrent cancellation.
func TestWireStreamCleanupSecrets(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprint(canceled), func(t *testing.T) {
			t.Parallel()
			// Arrange a successful observer, an SSE provider failure and a failing stream close.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cleanup := errors.New("complete cleanup account=failure-account session=cookie-private")
			terminalCause := errors.New("complete terminal cause account=failure-account session=cookie-private")
			reader := strings.NewReader(
				"data: " + `{"type":"error","code":"invalid_request_error","message":"` +
					`complete provider failure account=failure-account session=cookie-private"}` + "\n\n",
			)
			ctrl := gomock.NewController(t)
			body := NewMockIOReadCloser(ctrl)
			body.EXPECT().Read(gomock.Any()).DoAndReturn(reader.Read).AnyTimes()
			body.EXPECT().Close().DoAndReturn(func() error {
				if canceled {
					cancel()
				}
				return cleanup
			})
			transport := NewMockHTTPRoundTripper(ctrl)
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(r *http.Request) (*http.Response, error) {
				response := newReceivedDiagnosticResponse(r, http.StatusOK, body)
				response.Header.Set("Set-Cookie", "session=cookie-private; Path=/")
				return response, nil
			})
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
					return nil
				},
				Failed: func(err error) { t.Errorf("successful observer must not report middleware failure: %v", err) },
			}
			var terminal model.Response
			// Act through real stream decoding and final cleanup/cancellation composition.
			err := newTransportFailureDriver(t, transport).Stream(ctx, modelexecution.ProviderRequest{
				Instructions:    "instructions",
				Model:           testModelDescriptor("gpt-test"),
				ReasoningChoice: model.ReasoningChoiceOff,
				History:         nil,
				Tools:           nil,
				Wire:            mo.Some(callbacks),
			}, func(event modelexecution.StreamEvent) error {
				if response, present := event.Response.Get(); present {
					terminal = response
					return terminalCause
				}
				return nil
			})
			// Assert source identities and complete non-secret diagnostics remain safe at both boundaries.
			require.ErrorIs(t, err, cleanup)
			require.ErrorIs(t, err, terminalCause)
			assert.Contains(t, err.Error(), "complete terminal cause account=failure-account session=[REDACTED]")
			assert.True(t, observed)
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
			}
			for _, text := range []string{err.Error(), terminal.ErrorMessage.OrEmpty()} {
				assert.Contains(t, text, "complete provider failure account=failure-account session=[REDACTED]")
				assert.Contains(t, text, "complete cleanup account=failure-account session=[REDACTED]")
				assert.NotContains(t, text, "cookie-private")
			}
		})
	}
}
