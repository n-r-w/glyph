package middleware

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// Service owns accepted declarations and sequential middleware composition.
type Service struct {
	// mutex protects registration snapshots.
	mutex sync.RWMutex
	// handlers retains accepted declarations in registration order.
	handlers []Handler
	// runtime supplies exact-generation invocation mechanics.
	runtime Runtime
	// contexts issues trusted session bindings before each handler.
	contexts ContextIssuer
}

// Verify the owner implements model-execution callbacks and startup registration contracts.
var (
	_ modelexecution.WireMiddleware    = (*Service)(nil)
	_ modelexecution.ContextMiddleware = (*Service)(nil)
	_ startup.MiddlewareRegistrar      = (*Service)(nil)
)

// New connects ordinary invocation mechanics and trusted context issuance.
func New(runtime Runtime, contexts ContextIssuer) *Service {
	return &Service{mutex: sync.RWMutex{}, handlers: nil, runtime: runtime, contexts: contexts}
}

// mapMiddlewareKind maps only declarations owned by ordinary middleware composition.
func mapMiddlewareKind(raw startup.RawHandlerKind) (Kind, bool) {
	//nolint:exhaustive // Other accepted capabilities belong to separate registration owners.
	switch raw {
	case startup.RawHandlerKindProviderHeaders:
		return Headers, true
	case startup.RawHandlerKindProviderBody:
		return Body, true
	case startup.RawHandlerKindProviderResponse:
		return Response, true
	case startup.RawHandlerKindSystemInstructions:
		return SystemInstructions, true
	case startup.RawHandlerKindModelContext:
		return ModelContext, true
	default:
		return 0, false
	}
}

// ValidateMiddlewareHandlers accepts the middleware partition of a registration.
func (s *Service) ValidateMiddlewareHandlers(
	registration startup.PendingRegistration,
) ([]startup.AcceptedHandler, error) {
	result := make([]startup.AcceptedHandler, len(registration.Handlers))
	for i, handler := range registration.Handlers {
		if _, valid := mapMiddlewareKind(handler.Kind); !valid {
			return nil, fmt.Errorf("middleware handler %q has unknown kind %d", handler.ID, handler.Kind)
		}
		result[i] = startup.AcceptedHandler{ID: handler.ID, Kind: handler.Kind}
	}
	return result, nil
}

// CommitMiddlewareHandlers publishes only completely accepted registrations in order.
func (s *Service) CommitMiddlewareHandlers(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		for _, handler := range registration.Handlers {
			if kind, valid := mapMiddlewareKind(handler.Kind); valid {
				s.handlers = append(
					s.handlers,
					Handler{ExtensionID: registration.ID, HandlerID: handler.ID, Kind: kind},
				)
			}
		}
	}
}

// snapshotHandlers copies the accepted declarations in registration order.
func (s *Service) snapshotHandlers() []Handler {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return slices.Clone(s.handlers)
}

// NewWireCallbacks captures ordered handlers for one raw provider attempt.
func (s *Service) NewWireCallbacks() modelexecution.WireCallbacks {
	handlers := s.snapshotHandlers()
	return modelexecution.WireCallbacks{
		Headers: func(
			ctx context.Context,
			original modelexecution.WireHeaders,
			validate func(modelexecution.WireHeaders) error,
		) (modelexecution.WireHeaders, error) {
			return s.composeHeaders(ctx, handlers, original, validate)
		},
		Body: func(ctx context.Context, original []byte, validate func([]byte) error) (modelexecution.WireBodyResult, error) {
			return s.composeBody(ctx, handlers, original, validate)
		},
		Response: func(ctx context.Context, response modelexecution.WireResponse) error {
			return s.observeResponse(ctx, handlers, response)
		},
		Failed: nil,
	}
}

// bind issues the trusted generation and session binding for one handler invocation.
func (s *Service) bind(ctx context.Context, handler Handler) (extension.Context, error) {
	if err := ctx.Err(); err != nil {
		return extension.Context{}, err
	}
	binding, err := s.contexts.IssueContext(handler.ExtensionID)
	if err != nil {
		return extension.Context{}, fmt.Errorf("middleware handler %q context: %w", handler.HandlerID, err)
	}
	return binding, nil
}

// invoke binds one generation and exposes complete ordinary handler failures.
func (s *Service) invoke(ctx context.Context, handler Handler, input Invocation) (Action, error) {
	binding, err := s.bind(ctx, handler)
	if err != nil {
		return Action{}, err
	}
	input.Context = binding
	action, err := s.runtime.HandleWire(ctx, handler.ExtensionID, handler.HandlerID, input)
	if err != nil {
		return Action{}, fmt.Errorf("middleware handler %q: %w", handler.HandlerID, err)
	}
	return action, nil
}
