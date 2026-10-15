//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestAutomaticCancellationRetainsHandledChildCause checks acquired diagnostics when the owner cancels preparation.
func TestAutomaticCancellationRetainsHandledChildCause(t *testing.T) {
	t.Parallel()
	// Arrange a captured owner that acknowledges a child failure before canceling composition.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	compactions := NewMockCompactionSessions(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "prepare"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
	policy.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{}).Times(1)
	compactions.EXPECT().
		CaptureCompaction().
		Return(CompactionSnapshot{Identity: identity, ActiveLeafID: mo.None[string](), Entries: nil, History: nil, Previous: mo.None[session.CompactionEntry]()})
	service := New(runtime, sessions)
	service.BindPolicy(policy, compactions, 0)
	childCause := errors.New("acknowledged child failure: complete original cause")
	policy.EXPECT().
		InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ PolicyOwner, _ extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
			release, err := service.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, input.LifecycleID)
			if err != nil {
				return PolicyCanceled, err
			}
			release(childCause)
			return PolicyCanceled, nil
		})
	captured, err := service.CaptureConversation()
	require.NoError(t, err)
	// Act through automatic preparation with a live caller.
	result, err := captured.MustGet().Prepare(t.Context(), modelexecution.ProviderRequest{})
	// Assert cancellation stops dispatch without losing the earlier acquired child diagnostics.
	require.ErrorContains(t, err, "compaction policy canceled automatic preparation")
	require.ErrorIs(t, err, childCause)
	require.ErrorContains(t, err, childCause.Error())
	require.False(t, result.Committed)
	require.Empty(t, service.compactionOperations)
}
