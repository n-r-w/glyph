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
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestCanceledOverflowSelectsObserverSource checks active observer failure beside acquired child diagnostics.
func TestCanceledOverflowSelectsObserverSource(t *testing.T) {
	t.Parallel()
	// Arrange a canceled policy with a handled child failure and an independent failure observer.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	compactions := NewMockCompactionSessions(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "prepare"}
	observer := contextcompaction.Handler{
		ExtensionID: owner.ExtensionID,
		RuntimeID:   owner.RuntimeID,
		HandlerID:   "failure",
	}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true).AnyTimes()
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
	policy.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{
		Requests: nil, Generators: nil, Results: nil, Successes: nil, Failures: []contextcompaction.Handler{observer},
	}).Times(1)
	compactions.EXPECT().CaptureCompaction().Return(CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.None[string](), Entries: nil, History: nil,
		Previous: mo.None[session.CompactionEntry](),
	}).Times(1)
	service := New(runtime, sessions)
	service.BindPolicy(policy, compactions, 0)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	childCause := errors.New("handled child source\nsource β")
	observerCause := errors.New("independent observer source\nsource γ")
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ PolicyOwner, _ extension.Context, invocation PolicyInvocation) (PolicyDisposition, error) {
			release, err := service.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, invocation.LifecycleID)
			require.NoError(t, err)
			release(&PolicyOperationError{Category: modelexecution.FailureInternal, Cause: childCause})
			cancel()
			return PolicyCanceled, context.Canceled
		})
	policy.EXPECT().ObserveCompactionFailure(gomock.Any(), observer, gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ contextcompaction.Handler, _ extension.Context, outcome contextcompaction.OutcomeInvocation) error {
			require.True(t, outcome.Canceled)
			require.True(t, outcome.Committed.IsNone())
			return observerCause
		})
	captured, err := service.CaptureConversation()
	require.NoError(t, err)

	// Act through captured recovery and joined failure observation.
	result, err := captured.MustGet().
		RecoverOverflow(ctx, modelexecution.ProviderRequest{}, errors.New("provider overflow"))

	// Assert the observer supplies the active terminal source while all acquired identities remain available.
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, childCause)
	require.ErrorIs(t, err, observerCause)
	failure, present := errors.AsType[modelexecution.ContextPreparationFailure](err)
	require.True(t, present)
	require.Equal(t, string(modelexecution.FailureExtensionFailed), failure.CompactionFailureCode())
	require.False(t, result.Committed)
	require.Empty(t, service.compactionOperations)
}
