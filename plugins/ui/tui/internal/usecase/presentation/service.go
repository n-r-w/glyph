// Package presentation projects provider-neutral Host events into TUI state.
package presentation

import presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"

// Service applies Host presentation events without owning authoritative history.
type Service struct{}

// New creates a presentation projection service.
func New() *Service {
	return &Service{}
}

// Apply returns an updated presentation projection.
func (*Service) Apply(state presentationdomain.State, event presentationdomain.Event) presentationdomain.State {
	return state.Apply(event)
}
