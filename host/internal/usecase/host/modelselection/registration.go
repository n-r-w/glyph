package modelselection

import (
	"errors"
	"fmt"
	"strings"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

var _ startup.SelectionRegistrar = (*Service)(nil)

// ValidateSelectionHandlers validates selection declarations without mutating registration state.
func (s *Service) ValidateSelectionHandlers(
	registration startup.PendingRegistration,
) ([]startup.AcceptedHandler, error) {
	accepted := make([]startup.AcceptedHandler, 0, len(registration.Handlers))
	for _, handler := range registration.Handlers {
		if !handler.Present || strings.TrimSpace(handler.ID) == "" {
			return nil, errors.New("handler ID is empty")
		}
		if _, valid := selectionHandlerKind(handler.Kind); !valid {
			return nil, fmt.Errorf("handler %q has unknown kind %d", handler.ID, handler.Kind)
		}
		accepted = append(accepted, startup.AcceptedHandler{ID: handler.ID, Kind: handler.Kind})
	}
	return accepted, nil
}

// CommitSelectionHandlers publishes declarations only after startup accepts complete registrations.
func (s *Service) CommitSelectionHandlers(registrations []startup.AcceptedRegistration) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, registration := range registrations {
		for _, handler := range registration.Handlers {
			kind, valid := selectionHandlerKind(handler.Kind)
			if !valid {
				continue
			}
			s.handlers = append(s.handlers, registeredHandler{
				Handler: Handler{ExtensionID: registration.ID, HandlerID: handler.ID}, Kind: kind,
			})
		}
	}
}

// selectionHandlerKind maps one startup declaration to selection policy.
func selectionHandlerKind(kind startup.RawHandlerKind) (HandlerKind, bool) {
	switch kind {
	case startup.RawHandlerKindModelSelection:
		return HandlerKindModel, true
	case startup.RawHandlerKindReasoningSelection:
		return HandlerKindReasoning, true
	case startup.RawHandlerKindUnspecified,
		startup.RawHandlerKindSessionBeforeTreeRequest,
		startup.RawHandlerKindSessionBeforeTreeResult,
		startup.RawHandlerKindSessionTree,
		startup.RawHandlerKindAgentStart,
		startup.RawHandlerKindAgentEnd,
		startup.RawHandlerKindAgentSettled,
		startup.RawHandlerKindTurnStart,
		startup.RawHandlerKindTurnEnd,
		startup.RawHandlerKindMessageStart,
		startup.RawHandlerKindMessageUpdate,
		startup.RawHandlerKindMessageEnd,
		startup.RawHandlerKindToolExecutionStart,
		startup.RawHandlerKindToolExecutionUpdate,
		startup.RawHandlerKindToolExecutionEnd,
		startup.RawHandlerKindModelSelectionObserver,
		startup.RawHandlerKindReasoningSelectionObserver:
		return 0, false
	default:
		return 0, false
	}
}
