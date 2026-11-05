package run

import "github.com/n-r-w/glyph/host/internal/domain/agent"

// Abort accepts cancellation of the matching active run without waiting for its work to settle.
func (s *Service) Abort(runID string, cause error) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state.Status != StatusRunning || s.state.RunID.OrEmpty() != runID {
		return agent.ErrRunNotActive
	}
	// Cancellation keeps its first cause, so accepted sources have their own run-local lifetime.
	s.abortSources = append(s.abortSources, cause)
	s.cancelRun(cause)
	return nil
}
