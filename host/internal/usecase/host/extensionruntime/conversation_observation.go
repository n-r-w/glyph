package extensionruntime

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// ObserveConversation invokes the policy owner's handler in the captured runtime.
func (s *Service) ObserveConversation(
	ctx context.Context,
	policy extensioncontext.PolicyOwner,
	binding extension.Context,
	invocation extensioncontext.ConversationObservation,
) error {
	owner, available := s.beginOperation(policy.ExtensionID, policy.RuntimeID)
	if !available {
		return fmt.Errorf(
			"%w: conversation policy %q generation %q is unavailable",
			ErrExtensionUnavailable,
			policy.ExtensionID,
			policy.RuntimeID,
		)
	}
	err := owner.state.runtime.ObserveConversation(ctx, policy.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, err)
	return err
}
