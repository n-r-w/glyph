//go:build !integration

package extensioncontext

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestAutomaticCapturedOwnerUnavailable checks failure against the admitted generation without recapture or fallback.
func TestAutomaticCapturedOwnerUnavailable(t *testing.T) {
	t.Parallel()
	// Arrange an owner that disappears after its logical-conversation capture.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policies := NewMockPolicyRuntime(controller)
	compactions := NewMockCompactionSessions(controller)
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "captured-generation", HandlerID: "prepare"}
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	available := true
	runtime.EXPECT().
		ContextRuntime(owner.ExtensionID).
		AnyTimes().
		DoAndReturn(func(string) (string, bool) { return owner.RuntimeID, available })
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	policies.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
	policies.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{}).Times(1)
	service := New(runtime, sessions)
	service.BindPolicy(policies, compactions, 0)
	captured, err := service.CaptureConversation()
	require.NoError(t, err)
	available = false
	// Act against the original captured object after runtime disappearance.
	result, err := captured.MustGet().Prepare(t.Context(), modelexecution.ProviderRequest{})
	// Assert preparation fails before session capture or policy dispatch and releases no new authority.
	require.Error(t, err)
	require.False(t, result.Committed)
	require.Empty(t, service.compactionOperations)
}
