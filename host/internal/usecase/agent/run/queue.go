package run

import (
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// CheckEnqueue returns the ID of the running run that a queued message targets, or fails with agent.ErrRunNotActive
// while no run is running, so Host can skip input processing for a message that no run can accept.
func (s *Service) CheckEnqueue() (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if s.state.Status != StatusRunning {
		return "", agent.ErrRunNotActive
	}
	return s.state.RunID.OrEmpty(), nil
}

// Enqueue queues a message for the run that CheckEnqueue returned; that run appends it to history at the delivery
// point of the delivery kind. It fails with agent.ErrRunNotActive unless that run is still running.
func (s *Service) Enqueue(runID string, delivery agent.MessageDelivery, message model.Message) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state.Status != StatusRunning || s.state.RunID.OrEmpty() != runID {
		return agent.ErrRunNotActive
	}
	switch delivery {
	case agent.MessageDeliverySteer:
		s.steering = append(s.steering, message.Clone())
	case agent.MessageDeliveryFollowUp:
		s.followUps = append(s.followUps, message.Clone())
	default:
		return fmt.Errorf("message delivery %s is not supported", delivery)
	}
	return nil
}

// takeSteer removes the oldest pending steer message after the tools of a turn finish; followUp messages stay
// pending.
func (s *Service) takeSteer() mo.Option[model.Message] {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.popSteer()
}

// takeQueuedOrComplete removes the oldest pending steer message after a final model response, or the oldest
// pending followUp message when no steer message is pending. When neither is pending, it moves the run out of the
// running state under the same lock, so Enqueue either queues before this check or fails.
func (s *Service) takeQueuedOrComplete() mo.Option[model.Message] {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	message := s.popSteer()
	if message.IsAbsent() {
		message = s.popFollowUp()
	}
	if message.IsAbsent() {
		s.state.Status = StatusAwaitingSettlement
	}
	return message
}

// popSteer removes the oldest pending steer message; the caller holds the mutex.
func (s *Service) popSteer() mo.Option[model.Message] {
	message, rest := popOldest(s.steering)
	s.steering = rest
	return message
}

// popFollowUp removes the oldest pending followUp message; the caller holds the mutex.
func (s *Service) popFollowUp() mo.Option[model.Message] {
	message, rest := popOldest(s.followUps)
	s.followUps = rest
	return message
}

// popOldest returns the first message of a queue and the remaining queue. Queue mode one-at-a-time delivers one
// message at each delivery point.
func popOldest(queue []model.Message) (mo.Option[model.Message], []model.Message) {
	if len(queue) == 0 {
		return mo.None[model.Message](), queue
	}
	return mo.Some(queue[0]), queue[1:]
}
