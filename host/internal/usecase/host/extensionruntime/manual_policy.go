// Package extensionruntime owns extension process registration, invocation, and availability.
package extensionruntime

import (
	"context"
	"fmt"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

// capturedPolicyUnavailableFormat identifies an unavailable captured policy generation.
const capturedPolicyUnavailableFormat = "%w: compaction policy %q generation %q is unavailable"

// SnapshotCompactionPolicies captures registered policy generations in registration order.
func (s *Service) SnapshotCompactionPolicies() []extensioncontext.PolicyOwner {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return slices.Clone(s.compactionPolicies)
}

// InvokeCompactionPolicy invokes the captured generation without replacement or fallback.
func (s *Service) InvokeCompactionPolicy(
	ctx context.Context,
	policy extensioncontext.PolicyOwner,
	binding extension.Context,
	invocation extensioncontext.PolicyInvocation,
) (extensioncontext.PolicyDisposition, error) {
	owner, available := s.beginOperation(policy.ExtensionID, policy.RuntimeID)
	if !available {
		return 0, fmt.Errorf(
			capturedPolicyUnavailableFormat,
			ErrExtensionUnavailable,
			policy.ExtensionID,
			policy.RuntimeID,
		)
	}
	disposition, err := owner.state.runtime.InvokeCompactionPolicy(ctx, policy.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, err)
	return disposition, err
}
