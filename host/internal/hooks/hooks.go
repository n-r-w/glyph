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
	History []agent.HistoryEntry
}

// Request contains copied serialized provider request values.
type Request struct {
	Provider model.ProviderID
	Model    model.ID
	Payload  []byte
	Headers  Header
}

// Response contains copied provider response metadata without its body.
type Response struct {
	Provider model.ProviderID
	Model    model.ID
	Status   int
	Headers  Header
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

// HookError is the safe terminal classification for one internal hook failure.
type HookError struct {
	Stage Stage
}

// Error returns a safe error without the handler error or transformed values.
func (failure HookError) Error() string {
	return "internal hook failed at " + string(failure.Stage) + " stage"
}
