package codex

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// errorCaptureTransport retains one complete failed response body for SDK error normalization.
type errorCaptureTransport struct {
	// base sends provider HTTP requests.
	base http.RoundTripper
	// mu protects body.
	mu sync.Mutex
	// body contains one complete failed response body.
	body []byte
}

var _ http.RoundTripper = (*errorCaptureTransport)(nil)

// newErrorCaptureTransport wraps the SDK's underlying HTTP transport.
func newErrorCaptureTransport(base http.RoundTripper) *errorCaptureTransport {
	return &errorCaptureTransport{base: base, mu: sync.Mutex{}, body: nil}
}

// RoundTrip captures only failed HTTP bodies and restores them for the OpenAI SDK.
func (t *errorCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.body = nil
	t.mu.Unlock()

	response, err := t.base.RoundTrip(request)
	if err != nil || response.StatusCode < http.StatusBadRequest || response.StatusCode > 599 {
		return response, err
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("capture failed Codex response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close failed Codex response: %w", closeErr)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	t.mu.Lock()
	t.body = bytes.Clone(body)
	t.mu.Unlock()
	return response, nil
}

// ErrorBody returns a defensive copy of the last complete failed response body.
func (t *errorCaptureTransport) ErrorBody() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.body)
}

// providerErrorDetail extracts the approved backend error shapes.
func providerErrorDetail(body []byte) string {
	var payload struct {
		Detail string `json:"detail"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	if strings.TrimSpace(payload.Detail) != "" {
		return payload.Detail
	}
	return payload.Error.Message
}
