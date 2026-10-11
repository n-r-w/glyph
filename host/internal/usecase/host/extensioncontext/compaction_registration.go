// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// ValidateCompactionHandlers converts the admitted compaction capability group in registration order.
func (s *Service) ValidateCompactionHandlers(
	registration startup.PendingRegistration,
) ([]startup.AcceptedHandler, error) {
	accepted := make([]startup.AcceptedHandler, 0, len(registration.Handlers))
	for _, handler := range registration.Handlers {
		accepted = append(accepted, startup.AcceptedHandler{ID: handler.ID, Kind: handler.Kind})
	}
	return accepted, nil
}
