package run

import hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"

var _ hostprogrammatic.StateQuery = (*Service)(nil)

// RunActive reports activity without exposing history or partial-response state.
func (s *Service) RunActive() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.state.Status == StatusRunning || s.state.Status == StatusAwaitingSettlement
}
