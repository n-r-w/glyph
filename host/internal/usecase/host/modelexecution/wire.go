package modelexecution

import (
	"context"
	"errors"
	"sync"
)

// WireHeaders contains public header names and their ordered values.
type WireHeaders map[string][]string

// Clone detaches a header collection for an ownership transfer.
func (headers WireHeaders) Clone() WireHeaders {
	result := make(WireHeaders, len(headers))
	for name, values := range headers {
		result[name] = append([]string(nil), values...)
	}
	return result
}

// WireBodyResult distinguishes replacement from preservation of the original wire bytes.
type WireBodyResult struct {
	// Body is the final safe serialized JSON when Replaced is true.
	Body []byte
	// Replaced records whether any handler replaced the safe JSON.
	Replaced bool
}

// WireResponse contains public response metadata before provider decoding.
type WireResponse struct {
	// Status is the provider response status.
	Status int64
	// Headers excludes private authentication and account state.
	Headers WireHeaders
}

// WireCallbacks defines the neutral per-attempt protocol consumed by raw adapters.
type WireCallbacks struct {
	// Headers applies ordered header transformations, using the serializer to validate each replacement.
	Headers func(context.Context, WireHeaders, func(WireHeaders) error) (WireHeaders, error)
	// Body transforms safe serialized JSON and validates each replacement through the serializer.
	Body func(context.Context, []byte, func([]byte) error) (WireBodyResult, error)
	// Response observes public metadata before provider decoding.
	Response func(context.Context, WireResponse) error
	// Failed reports confidentiality-mapped callback, validation and cleanup causes before SDK error reduction.
	Failed func(error)
}

//go:generate go tool mockgen -source=wire.go -destination=wire_mock.go -package=modelexecution

// WireMiddleware supplies fresh ordered callbacks for each raw provider attempt.
type WireMiddleware interface {
	// NewWireCallbacks captures applicable handlers for one attempt.
	NewWireCallbacks() WireCallbacks
}

// BindWireMiddleware connects the Host middleware owner before model execution starts.
func (s *Service) BindWireMiddleware(owner WireMiddleware) error {
	if owner == nil {
		return errors.New("wire middleware owner is required")
	}
	if s.wire != nil {
		return errors.New("wire middleware owner is already bound")
	}
	s.wire = owner
	return nil
}

// wireFailure retains acquired callback causes across provider SDK cancellation reduction.
type wireFailure struct {
	// mutex protects acquired callback and bridge causes.
	mutex sync.Mutex
	// cause retains callback errors before the adapter completes its confidentiality mapping.
	cause error
	// reported retains the adapter's complete non-secret error envelope and its original causes.
	reported error
}

// retain records an acquired failure while preserving previous causes.
func (failure *wireFailure) retain(err error) {
	if err == nil {
		return
	}
	failure.mutex.Lock()
	defer failure.mutex.Unlock()
	failure.cause = errors.Join(failure.cause, err)
}

// acquired returns the causes obtained before the raw attempt completed.
func (failure *wireFailure) acquired() error {
	failure.mutex.Lock()
	defer failure.mutex.Unlock()
	if failure.reported != nil {
		return failure.reported
	}
	return failure.cause
}

// report records the adapter's confidentiality-mapped envelope without exposing its private source text.
func (failure *wireFailure) report(err error) {
	if err == nil {
		return
	}
	failure.mutex.Lock()
	defer failure.mutex.Unlock()
	failure.reported = errors.Join(failure.reported, err)
}

// captureFailures retains callback failures independently of raw-adapter error returns.
func (callbacks WireCallbacks) captureFailures(failure *wireFailure) WireCallbacks {
	return WireCallbacks{
		Headers: func(ctx context.Context, headers WireHeaders, validate func(WireHeaders) error) (WireHeaders, error) {
			result, err := callbacks.Headers(ctx, headers, validate)
			failure.retain(err)
			return result, err
		},
		Body: func(ctx context.Context, body []byte, validate func([]byte) error) (WireBodyResult, error) {
			result, err := callbacks.Body(ctx, body, validate)
			failure.retain(err)
			return result, err
		},
		Response: func(ctx context.Context, response WireResponse) error {
			err := callbacks.Response(ctx, response)
			failure.retain(err)
			return err
		},
		Failed: failure.report,
	}
}
