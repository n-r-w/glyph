//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
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
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	policy.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{}).AnyTimes()
	compactionSessions := NewMockCompactionSessions(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "captured-generation", HandlerID: "manual"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
	sessions.EXPECT().ContextSession().Return(identity)
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
	capturedHistory := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("captured history")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	snapshot := CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil,
		History: capturedHistory, Previous: mo.None[session.CompactionEntry](),
	}
	compactionSessions.EXPECT().CaptureCompaction().Return(snapshot)
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
	service := New(runtime, sessions)
	service.policyRuntime = policy
	service.compactionSessions = compactionSessions

	// Act through manual preparation with the captured conversation selection and manual guidance.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{
		Instructions: "conversation instructions", Model: model.Descriptor{}, ReasoningChoice: model.ReasoningChoiceOff,
		History: nil, Tools: nil,
	}, mo.Some("manual guidance"))

	// Assert that the captured owner's complete cause survives without replacement or recapture.
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, cause.Error())
	require.True(t, result.Committed.IsNone())
}
