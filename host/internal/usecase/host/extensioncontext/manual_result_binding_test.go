//go:build !integration

package extensioncontext

import (
	"context"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestChangedBinding checks captured session and runtime identity before result dispatch.
func (s *policyResultSuite) TestChangedBinding() {
	for _, scenario := range []string{"generation", "unavailable", "session", "incarnation"} {
		s.Run(scenario, func() {
			t := s.T()
			// Arrange a result runtime binding that differs from the admitted snapshot.
			controller := gomock.NewController(t)
			runtimes := NewMockRuntimeState(controller)
			sessions := NewMockSessionState(controller)
			runtime := NewMockPolicyRuntime(controller)
			snapshots := NewMockCompactionSessions(controller)
			owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "policy-generation", HandlerID: "manual"}
			handler := Participant{
				ExtensionID: "result-extension",
				RuntimeID:   "result-generation",
				HandlerID:   "result",
			}
			identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
			current := identity
			generation := handler.RuntimeID
			available := scenario != "unavailable"
			if scenario == "generation" {
				generation = "successor"
			}
			runtimes.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
			runtimes.EXPECT().ContextRuntime(handler.ExtensionID).Return(generation, available)
			sessions.EXPECT().ContextSession().DoAndReturn(func() session.Identity { return current }).AnyTimes()
			runtime.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
			runtime.EXPECT().
				SnapshotCompactionHandlers().
				Return(HandlerSet{Requests: nil, Generators: nil, Results: []Participant{handler}, Successes: nil, Failures: nil})
			snapshots.EXPECT().
				CaptureCompaction().
				Return(CompactionSnapshot{Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil, History: nil, Previous: mo.None[session.CompactionEntry]()})
			service := New(runtimes, sessions)
			service.BindPolicy(runtime, snapshots, 5*time.Minute)
			want := "captured result participant runtime or session changed"
			if !available {
				want = "unavailable"
			}
			runtime.EXPECT().
				InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
					if scenario == "session" {
						current.ID = "other-session"
					}
					if scenario == "incarnation" {
						current.Incarnation++
					}
					_, err := service.InvokePolicyResult(
						ctx,
						captured.ExtensionID,
						binding.RuntimeInstanceID,
						newPolicyResultCommand(input.OperationID, handler, 0),
					)
					require.ErrorContains(t, err, want)
					return PolicyReplaced, err
				})
			// Act through admitted manual policy ownership.
			outcome, err := service.CompactManualPolicy(
				t.Context(),
				modelexecution.ProviderRequest{},
				mo.None[string](),
			)
			// Assert binding rejection without dispatch, fallback or durable mutation.
			require.ErrorContains(t, err, want)
			require.True(t, outcome.Committed.IsNone())
			require.Empty(t, service.compactionOperations)
		})
	}
}
