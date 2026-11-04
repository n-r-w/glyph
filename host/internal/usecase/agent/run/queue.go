package run

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
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

// queuedMessage retains one pending message and its sender-specific delivery data.
type queuedMessage struct {
	// sequence orders the message among all pending messages by arrival.
	sequence uint64
	// delivery identifies the delivery point of the message.
	delivery agent.MessageDelivery
	// original is the message as the client sent it, which the client gets back when the run does not deliver it.
	original model.Message
	// processed is the message after input processing, which the model receives.
	processed model.Message
	// extension contains a captured extension payload instead of client forms.
	extension mo.Option[session.QueuedExtensionMessage]
}

// toReturnedMessage converts the pending message to the form in which the client gets it back.
func (m queuedMessage) toReturnedMessage() agent.ReturnedMessage {
	return agent.ReturnedMessage{Delivery: m.delivery, Message: m.original}
}

// Enqueue queues a message for the run that CheckEnqueue returned; that run appends it to history at the delivery
// point of the delivery kind. It fails with agent.ErrRunNotActive unless that run is still running.
func (s *Service) Enqueue(runID string, delivery agent.MessageDelivery, message runcontrol.QueuedMessage) error {
	return s.enqueue(runID, queuedMessage{
		sequence: 0, delivery: delivery, original: message.Original.Clone(), processed: message.Processed.Clone(),
		extension: mo.None[session.QueuedExtensionMessage](),
	})
}

// EnqueueExtension accepts a captured extension message for the matching active run.
func (s *Service) EnqueueExtension(
	runID string,
	delivery agent.MessageDelivery,
	message session.QueuedExtensionMessage,
) error {
	return s.enqueue(runID, queuedMessage{
		sequence:  0,
		delivery:  delivery,
		original:  model.Message{},
		processed: model.Message{},
		extension: mo.Some(message),
	})
}

// enqueue atomically checks run acceptance and assigns arrival order for either sender.
func (s *Service) enqueue(runID string, message queuedMessage) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state.Status != StatusRunning || s.state.RunID.OrEmpty() != runID {
		return agent.ErrRunNotActive
	}
	s.nextSequence++
	message.sequence = s.nextSequence
	s.pending = append(s.pending, message)
	return nil
}

// deliverQueuedMessage appends a client message or delegates origin-preserving extension persistence.
func (s *Service) deliverQueuedMessage(ctx context.Context, message queuedMessage) error {
	if payload, present := message.extension.Get(); present {
		if s.extensionMessages == nil {
			return errors.New("extension message delivery is not bound")
		}
		return s.extensionMessages.DeliverExtensionMessage(ctx, payload)
	}
	return s.appendUser(ctx, message.processed)
}

// SetQueueMode selects how many pending messages of one kind a delivery point gives the model. It applies from the
// next delivery point, also while a run is active, and holds for every later run of the process.
func (s *Service) SetQueueMode(mode agent.QueueMode) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.queueMode = mode
}

// takeSteer removes the pending steer messages that the queue mode delivers after the tools of a turn finish;
// followUp messages stay pending. It removes nothing and returns the context error when the run is canceled, so
// that the run ends with the messages still pending.
func (s *Service) takeSteer(ctx context.Context) ([]queuedMessage, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.pop(agent.MessageDeliverySteer), nil
}

// takeQueuedOrComplete removes the pending steer messages that the queue mode delivers after a final model
// response, or the pending followUp messages when no steer message is pending. When neither is pending, it moves
// the run out of the running state under the same lock, so Enqueue either queues before this check or fails.
// When the run is canceled and a message is pending, it removes nothing and returns the context error, so that the
// run ends with the messages still pending.
func (s *Service) takeQueuedOrComplete(ctx context.Context) ([]queuedMessage, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := ctx.Err(); err != nil && len(s.pending) > 0 {
		return nil, err
	}
	messages := s.pop(agent.MessageDeliverySteer)
	if len(messages) == 0 {
		messages = s.pop(agent.MessageDeliveryFollowUp)
	}
	if len(messages) == 0 {
		s.state.Status = StatusAwaitingSettlement
	}
	return messages, nil
}

// pop removes the pending messages of one delivery kind that the queue mode delivers, in arrival order; the caller
// holds the mutex.
func (s *Service) pop(delivery agent.MessageDelivery) []queuedMessage {
	var delivered []queuedMessage
	rest := make([]queuedMessage, 0, len(s.pending))
	for index := range s.pending {
		message := &s.pending[index]
		if message.delivery == delivery && (s.queueMode == agent.QueueModeAll || len(delivered) == 0) {
			delivered = append(delivered, *message)
			continue
		}
		rest = append(rest, *message)
	}
	s.pending = rest
	return delivered
}

// collectReturnedMessages returns pending and popped undelivered client originals in arrival order,
// excluding extension messages, while the caller holds the mutex.
func (s *Service) collectReturnedMessages(undelivered []queuedMessage) []agent.ReturnedMessage {
	all := slices.Concat(undelivered, s.pending)
	slices.SortFunc(all, func(a, b queuedMessage) int { return cmp.Compare(a.sequence, b.sequence) })
	return lo.FilterMap(all, func(message queuedMessage, _ int) (agent.ReturnedMessage, bool) {
		return message.toReturnedMessage(), message.extension.IsNone()
	})
}
