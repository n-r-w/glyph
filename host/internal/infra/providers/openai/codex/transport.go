package codex

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// errorCaptureTransport retains provider diagnostics across the SDK boundary.
type errorCaptureTransport struct {
	// base sends provider HTTP requests.
	base http.RoundTripper
	// mu protects captured diagnostics.
	mu sync.Mutex
	// body contains one complete failed response body.
	body []byte
	// err contains the acquired HTTP transport failure.
	err error
	// response contains the received HTTP rejection metadata.
	response *http.Response
}

var _ http.RoundTripper = (*errorCaptureTransport)(nil)

// newErrorCaptureTransport wraps the SDK's underlying HTTP transport.
func newErrorCaptureTransport(base http.RoundTripper) *errorCaptureTransport {
	return &errorCaptureTransport{base: base, mu: sync.Mutex{}, body: nil, err: nil, response: nil}
}

// RoundTrip retains HTTP failures and restores failed response bodies for the SDK.
func (t *errorCaptureTransport) RoundTrip(request *http.Request) (_ *http.Response, returnErr error) {
	t.mu.Lock()
	t.body = nil
	t.err = nil
	t.response = nil
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		t.err = returnErr
		t.mu.Unlock()
	}()

	response, err := t.base.RoundTrip(request)
	if err != nil || response.StatusCode < http.StatusBadRequest || response.StatusCode > 599 {
		return response, err
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr == nil {
		t.mu.Lock()
		t.body = bytes.Clone(body)
		t.response = response
		t.mu.Unlock()
	}
	if readErr != nil {
		readErr = fmt.Errorf("capture failed Codex response: %w", readErr)
	}
	if closeErr != nil {
		closeErr = fmt.Errorf("close failed Codex response: %w", closeErr)
	}
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

// ErrorBody returns a defensive copy of the last complete failed response body.
func (t *errorCaptureTransport) ErrorBody() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return bytes.Clone(t.body)
}

// errorResponse returns borrowed rejection metadata; RoundTrip owns the original body and the SDK owns any replay.
func (t *errorCaptureTransport) errorResponse() *http.Response {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.response
}

// joinError restores an acquired transport cause absent from the SDK error.
func (t *errorCaptureTransport) joinError(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err == nil || errors.Is(err, t.err) {
		return err
	}
	return errors.Join(err, t.err)
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
