package extensioncontext

import (
	"context"
	"errors"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// Prepare obtains policy-accepted history for the frozen conversation binding.
func (c *capturedConversation) Prepare(
	ctx context.Context,
	input modelexecution.ProviderRequest,
) (modelexecution.ConversationPreparation, error) {
	prepared := modelexecution.ConversationPreparation{History: nil, Committed: false}
	if err := c.validate(); err != nil {
		return prepared, err
	}
	snapshot := c.service.compactionSessions.CaptureCompaction()
	if snapshot.Identity.ID != c.binding.SessionID || snapshot.Identity.Incarnation != c.incarnation {
		return prepared, errors.New("captured conversation session changed before preparation")
	}
	input.History = snapshot.History
	result, disposition, err := c.service.runPolicy(
		ctx,
		c.owner,
		c.binding,
		snapshot,
		c.handlers,
		PolicyPurposeAutomatic,
		input,
		mo.None[string](),
	)
	prepared.Committed = result.Committed.IsSome()
	if err != nil {
		return prepared, err
	}
	if disposition == PolicyPreserve {
		prepared.History = snapshot.History
		return prepared, nil
	}
	if validationErr := c.validate(); validationErr != nil {
		return prepared, validationErr
	}
	outgoing := c.service.compactionSessions.CaptureCompaction()
	if outgoing.Identity.ID != c.binding.SessionID || outgoing.Identity.Incarnation != c.incarnation {
		return prepared, errors.New("captured conversation session changed before outgoing input check")
	}
	input.History = outgoing.History
	operation := c.service.newLifetime(ctx, c.owner)
	err = c.service.policyRuntime.ValidateOutgoingInput(
		operation.context,
		c.owner,
		c.binding,
		OutgoingInput{Snapshot: outgoing, Input: input, LifecycleID: operation.lifecycleID},
	)
	err = c.finishLifetime(ctx, operation, err)
	if err != nil {
		return prepared, err
	}
	prepared.History = outgoing.History
	return prepared, nil
}
