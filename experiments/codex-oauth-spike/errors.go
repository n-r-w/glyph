package main

import (
	"bytes"

	"encoding/json/v2"
	"errors"
	"fmt"
	"io"

	"net/http"

	"strings"

	openai "github.com/openai/openai-go/v3"
)

// RoundTrip captures a bounded failed response body and restores it for the SDK.
func (transport *errorCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.body = nil
	transport.mu.Unlock()

	response, err := transport.base.RoundTrip(request)
	if err != nil || response.StatusCode < http.StatusBadRequest {
		return response, err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBody))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("capture failed provider response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close failed provider response: %w", closeErr)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	transport.mu.Lock()
	transport.body = bytes.Clone(body)
	transport.mu.Unlock()
	return response, nil
}

// ErrorBody returns a defensive copy of the last failed provider response.
func (transport *errorCaptureTransport) ErrorBody() []byte {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return bytes.Clone(transport.body)
}

// normalizeProviderError surfaces the backend detail shape without exposing request credentials.
func normalizeProviderError(err error, capturedBody []byte) error {
	var apiError *openai.Error
	if !errors.As(err, &apiError) {
		return err
	}
	body := []byte(apiError.RawJSON())
	if len(body) == 0 {
		body = capturedBody
	}
	detail := providerErrorDetail(body)
	if detail == "" {
		detail = apiError.Message
	}
	if len(detail) > maxProviderErrorDetail {
		detail = detail[:maxProviderErrorDetail]
	}
	return fmt.Errorf("Codex API HTTP %d: %s", apiError.StatusCode, detail)
}

// providerErrorDetail extracts common provider error shapes and preserves unknown bounded JSON.
func providerErrorDetail(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body))
	}
	if detail, ok := payload["detail"].(string); ok {
		return detail
	}
	if providerError, ok := payload["error"].(map[string]any); ok {
		if message, ok := providerError["message"].(string); ok {
			return message
		}
	}
	return strings.TrimSpace(string(body))
}
