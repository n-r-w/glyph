package programmatic

import (
	"context"
	"errors"

	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/internal/operation"
)

// runEnqueue queues the message of the command and completes when the queue accepts it or an extension fully
// handles it.
func (p *commandPrepared) runEnqueue(ctx context.Context) operation.Outcome[controller.Response] {
	delivery, deliveryPresent := p.command.Delivery.Get()
	message, messagePresent := p.command.UserMessage.Get()
	if !deliveryPresent || !messagePresent {
		return operation.Failed[controller.Response](
			controller.FailureCodeInternal,
			errors.New("enqueue command lacks its delivery kind or message after validation"),
		)
	}
	handled, err := p.service.coordinator.Enqueue(ctx, delivery, message)
	if err != nil {
		if isOperationCancellation(ctx, err) {
			return newCanceledCommandOutcome(err)
		}
		return operation.Failed[controller.Response](failureCode(err), err)
	}
	response := emptyResponse(p.command.OperationID, controller.ResponseEnqueueMessage)
	response.MessageHandled = mo.Some(handled)
	return operation.Completed(response)
}
