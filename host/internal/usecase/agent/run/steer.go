package run

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// CheckSteer returns the ID of the running run that a steer message targets, or fails with agent.ErrRunNotActive
// while no run is running, so Host can skip input processing for a message that no run can accept.
func (s *Service) CheckSteer() (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if s.state.Status != StatusRunning {
		return "", agent.ErrRunNotActive
	}
	return s.state.RunID.OrEmpty(), nil
}

// Steer queues a message for the run that CheckSteer returned; that run appends it to history at its next delivery
// point: after the tools of the current turn finish, or after a final model response. It fails with
// agent.ErrRunNotActive unless that run is still running.
func (s *Service) Steer(runID string, message model.Message) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state.Status != StatusRunning || s.state.RunID.OrEmpty() != runID {
		return agent.ErrRunNotActive
	}
	s.steering = append(s.steering, message.Clone())
	return nil
}

// takeSteer removes the oldest pending steer message after the tools of a turn finish.
func (s *Service) takeSteer() mo.Option[model.Message] {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.popSteer()
}

// takeSteerOrComplete removes the oldest pending steer message after a final model response. When none is
// pending, it moves the run out of the running state under the same lock, so Steer either queues before this
// check or fails.
func (s *Service) takeSteerOrComplete() mo.Option[model.Message] {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	message := s.popSteer()
	if message.IsAbsent() {
		s.state.Status = StatusAwaitingSettlement
	}
	return message
}

// popSteer removes the oldest pending steer message; the caller holds the mutex.
func (s *Service) popSteer() mo.Option[model.Message] {
	if len(s.steering) == 0 {
		return mo.None[model.Message]()
	}
	// Queue mode one-at-a-time delivers one message at each delivery point.
	message := s.steering[0]
	s.steering = s.steering[1:]
	return mo.Some(message)
}
