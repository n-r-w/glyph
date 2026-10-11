//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks distinct mutation and nested-work bindings.
func (s *manualPolicySeparatesAuthorityFromLifetimeSuite) TestScenario() {
	t := s.T()
	// Arrange one admitted policy invocation.
	service, policy, owner := newManualPolicyOperationFixture(t)
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (bool, error) {
			assert.NotEmpty(t, invocation.LifecycleID)
			assert.NotEqual(t, invocation.OperationID, invocation.LifecycleID)
			mistakenRelease, err := service.BeginCompactionChild(
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
			)
			assert.ErrorContains(t, err, "compaction lifetime binding")
			if mistakenRelease != nil {
				mistakenRelease(nil)
			}
			release, err := service.BeginCompactionChild(
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.LifecycleID,
			)
			if err != nil {
				return false, errors.Join(errors.New("separate lifetime correlation is unavailable"), err)
			}
			runContext := context.WithValue(ctx, struct{}{}, "Run context value")
			childContext, unlink := service.LinkCompactionChild(runContext, invocation.LifecycleID)
			assert.Equal(t, "Run context value", childContext.Value(struct{}{}))
			assert.NoError(t, childContext.Err())
			unlink()
			release(nil)
			return true, nil
		},
	)

	// Act through the parent manual operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())

	// Assert that explicit cancellation ends the admitted parent without a replacement.
	require.NoError(t, err)
	require.True(t, result.Canceled)
	require.True(t, result.Committed.IsNone())
}

// newManualPolicyOperationFixture supplies one captured owner and session for scoped-operation tests.
func newManualPolicyOperationFixture(
	t *testing.T,
	observers ...contextcompaction.HandlerSet,
) (*Service, *MockPolicyRuntime, PolicyOwner) {
	t.Helper()
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	handlers := contextcompaction.HandlerSet{}
	if len(observers) != 0 {
		handlers = observers[0]
	}
	policy.EXPECT().SnapshotCompactionHandlers().Return(handlers)
	compactionSessions := NewMockCompactionSessions(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "manual"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
	compactionSessions.EXPECT().CaptureCompaction().Return(CompactionSnapshot{
		Identity:     identity,
		ActiveLeafID: mo.Some("leaf"),
		Entries:      nil,
		History:      nil,
		Previous:     mo.None[session.CompactionEntry](),
	})
	service := New(runtime, sessions)
	service.BindManualPolicy(policy, compactionSessions)
	return service, policy, owner
}
