package contextcompaction

import (
	"errors"
	"fmt"
	"strings"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// ValidateCompactionHandlers validates one extension-local compaction capability group.
func (s *Service) ValidateCompactionHandlers(
	registration startup.PendingRegistration,
) ([]startup.AcceptedHandler, error) {
	ids := make(map[string]struct{}, len(registration.Handlers))
	accepted := make([]startup.AcceptedHandler, 0, len(registration.Handlers))
	for _, handler := range registration.Handlers {
		if !handler.Present || strings.TrimSpace(handler.ID) == "" {
			return nil, errors.New("handler ID is empty")
		}
		if handler.Kind < startup.RawHandlerKindCompactionRequest ||
			handler.Kind > startup.RawHandlerKindCompactionFailure {
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
