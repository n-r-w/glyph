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

// TestChangedBinding checks unavailable generations and changed session identity before dispatch.
func (s *policyGeneratorSuite) TestChangedBinding() {
	for _, scenario := range []string{
		"replaced generation", "unavailable generation", "session ID", "session incarnation",
	} {
		s.Run(scenario, func() {
			t := s.T()
			// Arrange a generator binding that changes after policy admission.
			controller := gomock.NewController(t)
			runtimeState := NewMockRuntimeState(controller)
			sessions := NewMockSessionState(controller)
			runtime := NewMockPolicyRuntime(controller)
			snapshots := NewMockCompactionSessions(controller)
			owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "policy-generation", HandlerID: "manual"}
			handler := Participant{
				ExtensionID: "generator-extension",
				RuntimeID:   "captured-generation",
				HandlerID:   "selected",
			}
			identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
			current := identity
			runtimeState.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
			generation := handler.RuntimeID
			available := true
			if scenario == "replaced generation" {
				generation = "successor-generation"
			}
			if scenario == "unavailable generation" {
				available = false
			}
			runtimeState.EXPECT().ContextRuntime(handler.ExtensionID).Return(generation, available)
			sessions.EXPECT().ContextSession().DoAndReturn(func() session.Identity { return current }).AnyTimes()
			runtime.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
			runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
				Requests:   nil,
				Generators: []Participant{handler},
				Results:    nil,
				Successes:  nil,
				Failures:   nil,
			})
			snapshots.EXPECT().CaptureCompaction().Return(CompactionSnapshot{
				Identity:     identity,
				ActiveLeafID: mo.Some("leaf"),
				Entries:      nil,
				History:      nil,
				Previous:     mo.None[session.CompactionEntry](),
			})
			service := New(runtimeState, sessions)
			service.BindPolicy(runtime, snapshots, 5*time.Minute)
			want := "captured generator participant runtime or session changed"
			if !available {
				want = "unavailable"
			}
			runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
					if scenario == "session ID" {
						current.ID = "other-session"
					}
					if scenario == "session incarnation" {
						current.Incarnation++
					}
					_, err := service.InvokePolicyGenerator(
						ctx,
						captured.ExtensionID,
						binding.RuntimeInstanceID,
						newPolicyGeneratorCommand(input.OperationID, handler, 0),
					)
					require.ErrorContains(t, err, want)
					return PolicyReplaced, err
				},
			)
			// Act through the admitted policy operation without an expectation for generator dispatch.
			result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())
			// Assert terminal binding rejection and released admission state.
			require.ErrorContains(t, err, want)
			require.True(t, result.Committed.IsNone())
			require.Empty(t, service.compactionOperations)
		})
	}
}
