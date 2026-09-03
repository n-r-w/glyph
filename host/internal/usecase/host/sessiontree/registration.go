package sessiontree

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// ValidateHandlers validates one extension-local handler registration.
func (s *Service) ValidateHandlers(registration startup.PendingRegistration) ([]startup.AcceptedHandler, error) {
	ids := make(map[string]struct{}, len(registration.Handlers))
	accepted := make([]startup.AcceptedHandler, 0, len(registration.Handlers))
	for _, handler := range registration.Handlers {
		if !handler.Present || strings.TrimSpace(handler.ID) == "" {
			return nil, errors.New("handler ID is empty")
		}
		switch handler.Kind {
		case startup.RawHandlerKindSessionBeforeTreeRequest,
			startup.RawHandlerKindSessionBeforeTreeResult,
			startup.RawHandlerKindSessionTree:
		case startup.RawHandlerKindUnspecified:
			return nil, fmt.Errorf("handler %q has unknown kind %d", handler.ID, handler.Kind)
		default:
			return nil, fmt.Errorf("handler %q has unknown kind %d", handler.ID, handler.Kind)
		}
		if _, exists := ids[handler.ID]; exists {
			return nil, fmt.Errorf("handler ID %q is duplicated", handler.ID)
		}
		ids[handler.ID] = struct{}{}
		accepted = append(accepted, startup.AcceptedHandler{ID: handler.ID, Kind: handler.Kind})
	}
	return accepted, nil
}

// CommitHandlers publishes handlers from registrations accepted by every startup validator.
func (s *Service) CommitHandlers(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		for _, handler := range registration.Handlers {
			s.handlers = append(s.handlers, registeredHandler{
				Handler: Handler{ExtensionID: registration.ID, HandlerID: handler.ID},
				Kind:    acceptedHandlerKind(handler.Kind),
			})
		}
	}
}

// acceptedHandlerKind maps one previously validated startup handler kind.
func acceptedHandlerKind(kind startup.RawHandlerKind) HandlerKind {
	switch kind {
	case startup.RawHandlerKindSessionBeforeTreeRequest:
		return HandlerKindRequest
	case startup.RawHandlerKindSessionBeforeTreeResult:
		return HandlerKindResult
	case startup.RawHandlerKindSessionTree:
		return HandlerKindObserver
	case startup.RawHandlerKindUnspecified:
		return 0
	default:
		return 0
	}
}

// handlersFor returns an available handler snapshot in deterministic registration order.
func (s *Service) handlersFor(kind HandlerKind) []Handler {
	s.mutex.RLock()
	registered := slices.Clone(s.handlers)
	s.mutex.RUnlock()
	handlers := make([]Handler, 0, len(registered))
	// Keep every selected handler from one extension on the same availability decision within this snapshot.
	availability := make(map[string]bool)
	for _, candidate := range registered {
		if candidate.Kind != kind {
			continue
		}
		available, checked := availability[candidate.Handler.ExtensionID]
		if !checked {
			available = s.runtime.HandlerRuntimeAvailable(candidate.Handler.ExtensionID)
			availability[candidate.Handler.ExtensionID] = available
		}
		if available {
			handlers = append(handlers, candidate.Handler)
		}
	}
	return handlers
}
