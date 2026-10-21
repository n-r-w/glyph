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
	return c.prepare(ctx, input, PolicyPurposeAutomatic, mo.None[string]())
}

// prepare obtains policy-accepted session history for a captured conversation binding.
func (c *capturedConversation) prepare(
	ctx context.Context,
	input modelexecution.ProviderRequest,
	purpose PolicyPurpose,
	overflowError mo.Option[string],
) (modelexecution.ConversationPreparation, error) {
	prepared := modelexecution.ConversationPreparation{History: nil, Committed: false}
	if err := c.validate(); err != nil {
		return prepared, err
	}
	snapshot := c.service.compactionSessions.CaptureCompaction()
	if snapshot.Identity.ID != c.binding.SessionID || snapshot.Identity.Incarnation != c.incarnation {
		return prepared, errors.New("captured conversation session changed before preparation")
	}
	input.History = snapshot.History.Content()
	result, disposition, err := c.service.runPolicy(
		ctx,
		c.owner,
		c.binding,
		snapshot,
		c.handlers,
		purpose,
		input,
		mo.None[string](),
		overflowError,
	)
	prepared.Committed = result.Committed.IsSome()
	if err != nil {
		return prepared, err
	}
	if disposition == PolicyPreserve {
		prepared.History = snapshot.History.Content()
		return prepared, nil
	}
	if validationErr := c.validate(); validationErr != nil {
		return prepared, validationErr
	}
	outgoing := c.service.compactionSessions.CaptureCompaction()
	if outgoing.Identity.ID != c.binding.SessionID || outgoing.Identity.Incarnation != c.incarnation {
		return prepared, errors.New("captured conversation session changed before outgoing input check")
	}
	input.History = outgoing.History.Content()
	operation := c.service.newLifetime(ctx, c.owner)
	err = c.service.policyRuntime.ValidateOutgoingInput(
		operation.context,
		c.owner,
		c.binding,
		OutgoingInput{Snapshot: outgoing, Input: input, LifecycleID: operation.lifecycleID},
	)
	err = NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, c.finishLifetime(ctx, operation, err))
	if err != nil {
		return prepared, err
	}
	prepared.History = outgoing.History.Content()
	return prepared, nil
}
