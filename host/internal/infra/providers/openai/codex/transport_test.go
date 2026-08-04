package codex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTripResult carries one asynchronous non-error body check.
type roundTripResult struct {
	response *http.Response
	err      error
}

// TestErrorCaptureTransportCapturesAndRestoresFailedBody verifies bounded SDK-visible error bodies.
func TestErrorCaptureTransportCapturesAndRestoresFailedBody(t *testing.T) {
	t.Parallel()

	body := `{"detail":"provider detail"}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(writer, body)
	}))
	t.Cleanup(server.Close)
	transport := newErrorCaptureTransport(server.Client().Transport)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)

	require.NoError(t, err)
	restored, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, body, string(restored))
	assert.Equal(t, []byte(body), transport.ErrorBody())
	assert.Equal(t, "provider detail", providerErrorDetail(transport.ErrorBody()))
}

// TestErrorCaptureTransportDoesNotBufferSuccess verifies streaming bodies remain unread before return.
func TestErrorCaptureTransportDoesNotBufferSuccess(t *testing.T) {
	t.Parallel()

	headersSent := make(chan struct{})
	releaseBody := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseBody) }) }
	t.Cleanup(release)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		close(headersSent)
		<-releaseBody
		_, _ = io.WriteString(writer, "streamed-success")
	}))
	t.Cleanup(server.Close)
	transport := newErrorCaptureTransport(server.Client().Transport)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	returned := make(chan roundTripResult, 1)
	go func() {
		//nolint:bodyclose // Main test goroutine closes the body after proving RoundTrip returns first.
		response, roundTripErr := transport.RoundTrip(request)
		returned <- roundTripResult{response: response, err: roundTripErr}
	}()
	<-headersSent

	var result roundTripResult
	select {
	case result = <-returned:
	case <-time.After(2 * time.Second):
		release()
		result = <-returned
		require.Fail(t, "non-error response body was buffered before RoundTrip returned")
	}
	release()

	require.NoError(t, result.err)
	body, err := io.ReadAll(result.response.Body)
	require.NoError(t, err)
	require.NoError(t, result.response.Body.Close())
	assert.Equal(t, "streamed-success", string(body))
	assert.Nil(t, transport.ErrorBody())
}

// TestErrorCaptureTransportBoundsBodyAndDetail verifies memory and user-visible limits.
func TestErrorCaptureTransportBoundsBodyAndDetail(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", maxProviderErrorBody+100)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, oversized)
	}))
	t.Cleanup(server.Close)
	transport := newErrorCaptureTransport(server.Client().Transport)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)

	require.NoError(t, err)
	assert.Len(t, transport.ErrorBody(), maxProviderErrorBody)
	require.NoError(t, response.Body.Close())
	assert.Len(t, []rune(boundedDetail(strings.Repeat("界", maxProviderErrorDetail+20))), maxProviderErrorDetail)
}

// TestProviderErrorDetailRecognizesApprovedShapes verifies both backend JSON formats.
func TestProviderErrorDetailRecognizesApprovedShapes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "direct", providerErrorDetail([]byte(`{"detail":"direct"}`)))
	assert.Equal(t, "nested", providerErrorDetail([]byte(`{"error":{"message":"nested"}}`)))
	assert.Empty(t, providerErrorDetail([]byte(`{"unknown":"value"}`)))
	assert.Empty(t, providerErrorDetail([]byte(`not-json`)))
}
