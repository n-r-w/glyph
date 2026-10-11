package codex

import (
	"encoding/json/v2"
	"strings"

	"github.com/openai/openai-go/v3"
)

// receivedRejectionError presents received diagnostics with the SDK cause identity.
type receivedRejectionError struct {
	// message contains the received rejection text.
	message string
	// cause contains the original SDK failure.
	cause error
}

// newReceivedRejectionError combines received envelope text with the original SDK failure.
func newReceivedRejectionError(cause error, apiError *openai.Error, body []byte) error {
	if len(body) == 0 || strings.Contains(cause.Error(), string(body)) {
		return cause
	}
	message := cause.Error()
	if raw := apiError.RawJSON(); raw != "" && strings.Contains(message, raw) {
		message = strings.Replace(message, raw, string(body), 1)
	} else {
		message += ": " + string(body)
	}
	return &receivedRejectionError{message: message, cause: cause}
}

// Error returns the received rejection diagnostics.
func (e *receivedRejectionError) Error() string { return e.message }

// Unwrap exposes the original SDK failure.
func (e *receivedRejectionError) Unwrap() error { return e.cause }

// providerErrorCode extracts the provider's rejection code.
func providerErrorCode(body []byte) string {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Error.Code)
}
