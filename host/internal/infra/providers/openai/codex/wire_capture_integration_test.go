//go:build integration

package codex

import (
	"errors"
	"net/http"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestWireRejectionCaptureSecrets retains header secrecy when rejection capture returns no response to the SDK.
func TestWireRejectionCaptureSecrets(t *testing.T) {
	t.Parallel()
	// Arrange rejected metadata followed by acquired read and cleanup failures.
	readCause := errors.New("complete read cause account=failure-account session=cookie-private")
	cleanup := errors.New("complete cleanup cause account=failure-account session=cookie-private")
	ctrl := gomock.NewController(t)
	body := NewMockIOReadCloser(ctrl)
	body.EXPECT().Read(gomock.Any()).Return(0, readCause)
	body.EXPECT().Close().Return(cleanup)
	transport := NewMockHTTPRoundTripper(ctrl)
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(r *http.Request) (*http.Response, error) {
		response := newReceivedDiagnosticResponse(r, http.StatusTooManyRequests, body)
		response.Header.Set("Set-Cookie", "session=cookie-private; Path=/")
		return response, nil
	})
	var terminal model.Response
	// Act through the real transport capture and final provider completion without middleware handlers.
	err := newTransportFailureDriver(t, transport).Stream(t.Context(), modelexecution.ProviderRequest{
		Instructions: "instructions", Model: testModelDescriptor("gpt-test"), ReasoningChoice: model.ReasoningChoiceOff,
		History: nil, Tools: nil, Wire: mo.None[modelexecution.WireCallbacks](),
	}, func(event modelexecution.StreamEvent) error {
		if response, present := event.Response.Get(); present {
			terminal = response
		}
		return nil
	})
	// Assert original failure identities, rejection status and complete safe diagnostic text remain.
	require.ErrorIs(t, err, readCause)
	require.ErrorIs(t, err, cleanup)
	for _, text := range []string{err.Error(), terminal.ErrorMessage.OrEmpty()} {
		assert.Contains(t, text, "HTTP 429 Too Many Requests")
		assert.Contains(t, text, "complete read cause account=failure-account session=[REDACTED]")
		assert.Contains(t, text, "complete cleanup cause account=failure-account session=[REDACTED]")
		assert.NotContains(t, text, "cookie-private")
	}
}
