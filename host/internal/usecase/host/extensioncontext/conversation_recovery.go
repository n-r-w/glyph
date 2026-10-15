package extensioncontext

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// RecoverOverflow obtains replacement history from the captured conversation policy.
func (c *capturedConversation) RecoverOverflow(
	ctx context.Context,
	input modelexecution.ProviderRequest,
	source error,
) (modelexecution.ConversationPreparation, error) {
	prepared, err := c.prepare(ctx, input, PolicyPurposeOverflow, mo.Some(source.Error()))
	if err == nil {
		return prepared, nil
	}
	category := selectPolicyOperationCategory(err)
	if !prepared.Committed && category.IsNone() && isPolicyOwningCancellation(ctx, err) {
		return prepared, err
	}
	return prepared, &overflowPreparationError{
		category: category.OrElse(modelexecution.FailureCompactionFailed), cause: err,
	}
}
