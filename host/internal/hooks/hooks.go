// Package hooks defines Host-owned internal model request transformation contracts.
package hooks

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// Stage identifies the internal hook boundary that failed.
type Stage string

const (
	// StageContext transforms provider-neutral outbound context.
	StageContext Stage = "context"
	// StageRequest transforms one serialized provider request before dispatch.
	StageRequest Stage = "request"
	// StageResponse observes one provider response before body decoding.
	StageResponse Stage = "response"
)

// Header contains copied HTTP header values without an HTTP request or response.
type Header map[string][]string

// Context contains one request-local provider-neutral history projection.
type Context struct {
	// History contains the copied provider-neutral request history.
	History []agent.HistoryEntry
}

// Request contains copied serialized provider request values.
type Request struct {
	// Provider identifies the target model provider.
	Provider model.ProviderID
	// Model identifies the target provider model.
	Model model.ID
	// Payload contains the serialized provider request body.
	Payload []byte
	// Headers contains copied provider request headers.
	Headers Header
}

// Response contains copied provider response metadata without its body.
type Response struct {
	// Provider identifies the model provider that returned the response.
	Provider model.ProviderID
	// Model identifies the requested provider model.
	Model model.ID
	// Status contains the HTTP response status code.
	Status int
	// Headers contains copied provider response headers.
	Headers Header
}

// ContextHandler transforms one request-local provider-neutral context.
type ContextHandler func(ctx context.Context, value Context) (Context, error)

// RequestHandler transforms one serialized request copy before HTTP dispatch.
type RequestHandler func(ctx context.Context, value Request) (Request, error)

// ResponseHandler observes one response metadata copy before body decoding.
type ResponseHandler func(ctx context.Context, value Response) error

// ContextRunner applies provider-neutral context handlers.
type ContextRunner interface {
	TransformContext(ctx context.Context, value Context) (Context, error)
}

// ProviderRunner applies serialized request and response handlers.
type ProviderRunner interface {
	TransformRequest(ctx context.Context, value Request) (Request, error)
	ObserveResponse(ctx context.Context, value Response) error
}

// HookError identifies the stage and cause of one internal hook failure.
type HookError struct {
	// Stage identifies the failed hook stage.
	Stage Stage
	// Cause contains the handler failure.
	Cause error
}

// Error identifies the hook stage and includes the handler failure.
func (failure HookError) Error() string {
	return "internal hook failed at " + string(failure.Stage) + " stage: " + failure.Cause.Error()
}

// Unwrap exposes the handler failure for errors.Is and errors.As.
func (failure HookError) Unwrap() error {
	return failure.Cause
}
