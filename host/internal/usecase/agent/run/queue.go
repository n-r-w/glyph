package run

import (
	"fmt"

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

// SetQueueMode selects how many pending messages of one kind a delivery point gives the model. It applies from the
// next delivery point, also while a run is active, and holds for every later run of the process.
func (s *Service) SetQueueMode(mode agent.QueueMode) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.queueMode = mode
}

// takeSteer removes the pending steer messages that the queue mode delivers after the tools of a turn finish;
// followUp messages stay pending.
func (s *Service) takeSteer() []model.Message {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.popSteer()
}

// takeQueuedOrComplete removes the pending steer messages that the queue mode delivers after a final model
// response, or the pending followUp messages when no steer message is pending. When neither is pending, it moves
// the run out of the running state under the same lock, so Enqueue either queues before this check or fails.
func (s *Service) takeQueuedOrComplete() []model.Message {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	messages := s.popSteer()
	if len(messages) == 0 {
		messages = s.popFollowUp()
	}
	if len(messages) == 0 {
		s.state.Status = StatusAwaitingSettlement
	}
	return messages
}

// popSteer removes the steer messages that the queue mode delivers; the caller holds the mutex.
func (s *Service) popSteer() []model.Message {
	delivered, rest := popQueued(s.steering, s.queueMode)
	s.steering = rest
	return delivered
}

// popFollowUp removes the followUp messages that the queue mode delivers; the caller holds the mutex.
func (s *Service) popFollowUp() []model.Message {
	delivered, rest := popQueued(s.followUps, s.queueMode)
	s.followUps = rest
	return delivered
}

// popQueued returns the messages of a queue that one delivery point delivers in the mode and the remaining queue.
func popQueued(queue []model.Message, mode agent.QueueMode) (delivered, rest []model.Message) {
	if len(queue) == 0 || mode == agent.QueueModeAll {
		return queue, nil
	}
	return queue[:1], queue[1:]
}
