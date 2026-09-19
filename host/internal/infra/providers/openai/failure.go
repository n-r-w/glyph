// Package openai contains source-level failure rules shared by the in-process OpenAI provider families.
package openai

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

const (
	// contextOverflowCode identifies OpenAI's structured context-window rejection.
	contextOverflowCode = "context_length_exceeded"
	// providerServerErrorCode identifies an SSE server failure.
	providerServerErrorCode = "server_error"
	// providerRateLimitCode identifies an SSE rate-limit failure.
	providerRateLimitCode = "rate_limit_exceeded"
)

// FailureClassification maps provider-owned HTTP, SSE, and transport identity to the neutral source class.
func FailureClassification(
	code string,
	status int,
	transientTransport bool,
) modelexecution.ProviderFailureClassification {
	if code == contextOverflowCode {
		return modelexecution.ProviderFailureContextOverflow
	}
	if code == providerServerErrorCode || code == providerRateLimitCode ||
		transientTransport || isRetryableHTTPStatus(status) {
		return modelexecution.ProviderFailureTransient
	}
	return modelexecution.ProviderFailureNonRetryable
}

// IsTransientTransportFailure identifies active-request transport failures that can succeed unchanged.
func IsTransientTransportFailure(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}

// RetryAfterDelay parses one standard Retry-After response header without applying retry policy.
func RetryAfterDelay(response *http.Response) mo.Option[time.Duration] {
	if response == nil {
		return mo.None[time.Duration]()
	}
	value := strings.TrimSpace(response.Header.Get("Retry-After"))
	if value == "" {
		return mo.None[time.Duration]()
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return mo.Some(time.Duration(seconds * float64(time.Second)))
	}
	requestedAt, err := http.ParseTime(value)
	if err != nil {
		return mo.None[time.Duration]()
	}
	delay := max(time.Until(requestedAt), 0)
	return mo.Some(delay)
}

// isRetryableHTTPStatus reports the fixed source-owned transient HTTP statuses.
func isRetryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}
