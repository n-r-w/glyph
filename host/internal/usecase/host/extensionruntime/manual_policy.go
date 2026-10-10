// Package extensionruntime owns extension process registration, invocation, and availability.
package extensionruntime

import (
	"context"
	"fmt"
	"slices"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
)

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
) (bool, error) {
	// owner contains the captured generation reservation; available indicates admission success.
	owner, available := s.beginOperation(policy.ExtensionID, policy.RuntimeID)
	if !available {
		return false, fmt.Errorf(
			"%w: compaction policy %q generation %q is unavailable",
			ErrExtensionUnavailable,
			policy.ExtensionID,
			policy.RuntimeID,
		)
	}
	// canceled and err contain the policy cancellation and invocation outcomes.
	canceled, err := owner.state.runtime.InvokeCompactionPolicy(ctx, policy.HandlerID, binding, invocation)
	s.finishAndReport(ctx, owner, err)
	return canceled, err
}
