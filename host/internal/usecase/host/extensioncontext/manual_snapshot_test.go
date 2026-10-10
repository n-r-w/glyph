//go:build !integration

package extensioncontext

import (
	"context"
	"errors"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks captured history and runtime retention on failure.
func (s *manualPolicyUsesOneCapturedSessionSuite) TestScenario() {
	t := s.T()
	// Arrange a captured owner and history distinct from earlier caller input.
	controller := gomock.NewController(t)
	// runtime supplies the captured runtime identity.
	runtime := NewMockRuntimeState(controller)
	// sessions supplies the active session identity.
	sessions := NewMockSessionState(controller)
	// policy supplies the captured policy invocation.
	policy := NewMockPolicyRuntime(controller)
	policy.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{}).AnyTimes()
	// compactionSessions supplies snapshot capture behavior.
	compactionSessions := NewMockCompactionSessions(controller)
	// identity identifies the captured session incarnation.
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	// owner identifies the captured policy handler and runtime.
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "captured-generation", HandlerID: "manual"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
	sessions.EXPECT().ContextSession().Return(identity)
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
	// capturedHistory contains session history at admission.
	capturedHistory := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("captured history")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	// snapshot contains the captured branch and model history.
	snapshot := CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil,
		History: capturedHistory, Previous: mo.None[session.CompactionEntry](),
	}
	compactionSessions.EXPECT().CaptureCompaction().Return(snapshot)
	// cause is the captured runtime transport failure.
	cause := errors.New("captured runtime became unavailable: original transport cause")
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, actualOwner PolicyOwner, bound extension.Context, invocation PolicyInvocation) (bool, error) {
			require.Equal(t, owner, actualOwner)
			require.Equal(t, owner.RuntimeID, bound.RuntimeInstanceID)
			require.NotEmpty(t, invocation.OperationID)
			require.Equal(t, snapshot, invocation.Snapshot)
			require.Equal(t, mo.Some("manual guidance"), invocation.Instructions)
			return false, cause
		},
	)
	// service owns the manual operation under test.
	service := New(runtime, sessions)
	service.policyRuntime = policy
	service.compactionSessions = compactionSessions

	// Act through manual preparation with obsolete history in the caller's model input.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{
		Instructions: "conversation instructions", Model: model.Descriptor{}, ReasoningChoice: model.ReasoningChoiceOff,
		History: nil, Tools: nil,
	}, mo.Some("manual guidance"))

	// Assert that the captured owner's complete cause survives without replacement or recapture.
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
	require.True(t, result.Committed.IsNone())
}
