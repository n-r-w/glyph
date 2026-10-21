//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// TestCapturedOverflowClassifiesCurrentSources checks recovery categories without searching diagnostic history.
func TestCapturedOverflowClassifiesCurrentSources(t *testing.T) {
	t.Parallel()
	independent := errors.New("policy invocation failed\nsource β")
	for _, scenario := range []struct {
		name     string
		cause    error
		category modelexecution.FailureCategory
	}{
		{name: "invocation", cause: independent, category: modelexecution.FailureExtensionFailed},
		{name: "persistence first", cause: errors.Join(session.ErrPersistenceUnavailable, independent), category: modelexecution.FailurePersistenceUnavailable},
		{name: "persistence last", cause: errors.Join(independent, session.ErrPersistenceUnavailable), category: modelexecution.FailurePersistenceUnavailable},
		{name: "historical persistence", cause: diagnostic.NewCurrentCause(independent, session.ErrPersistenceUnavailable), category: modelexecution.FailureExtensionFailed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a failed policy invocation with current and historical source branches.
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
			compactions.EXPECT().CaptureCompaction().Return(CompactionSnapshot{
				Identity: identity, ActiveLeafID: mo.None[string](), Entries: nil, History: nil,
				Previous: mo.None[session.CompactionEntry](),
			})
			policy.EXPECT().
				InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
				Return(PolicyReplaced, scenario.cause)
			service := New(runtime, sessions)
			service.BindPolicy(policy, compactions, 0)
			captured, err := service.CaptureConversation()
			require.NoError(t, err)

			// Act through the captured recovery boundary.
			result, err := captured.MustGet().
				RecoverOverflow(t.Context(), modelexecution.ProviderRequest{}, errors.New("provider overflow"))

			// Assert only recovery gains the consumer category while retaining original source identity and text.
			require.ErrorIs(t, err, independent)
			require.Contains(t, err.Error(), scenario.cause.Error())
			failure, present := errors.AsType[modelexecution.ContextPreparationFailure](err)
			require.True(t, present)
			require.Equal(t, string(scenario.category), failure.CompactionFailureCode())
			require.False(t, result.Committed)
			require.Empty(t, service.compactionOperations)
		})
	}
}

// TestCapturedOverflowRejectsChangedBinding checks unavailable captured generations without recapture.
func TestCapturedOverflowRejectsChangedBinding(t *testing.T) {
	t.Parallel()
	for _, loseRuntime := range []bool{true, false} {
		name := "session incarnation"
		if loseRuntime {
			name = "captured runtime"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange an admitted owner whose runtime or session changes after capture.
			controller := gomock.NewController(t)
			runtime := NewMockRuntimeState(controller)
			sessions := NewMockSessionState(controller)
			policy := NewMockPolicyRuntime(controller)
			compactions := NewMockCompactionSessions(controller)
			identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
			owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "prepare"}
			available := true
			runtime.EXPECT().ContextRuntime(owner.ExtensionID).DoAndReturn(func(string) (string, bool) {
				return owner.RuntimeID, available
			}).AnyTimes()
			sessions.EXPECT().ContextSession().DoAndReturn(func() session.Identity { return identity }).AnyTimes()
			policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner}).Times(1)
			policy.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{}).Times(1)
			service := New(runtime, sessions)
			service.BindPolicy(policy, compactions, 0)
			captured, err := service.CaptureConversation()
			require.NoError(t, err)
			expected := modelexecution.FailureCompactionFailed
			if loseRuntime {
				available = false
				expected = modelexecution.FailureExtensionFailed
			} else {
				identity.Incarnation++
			}

			// Act before any replacement snapshot or policy invocation can start.
			result, err := captured.MustGet().
				RecoverOverflow(t.Context(), modelexecution.ProviderRequest{}, errors.New("provider overflow"))

			// Assert the captured binding fails with its source category and no new authority.
			require.ErrorContains(t, err, "runtime or active-session incarnation was replaced")
			failure, present := errors.AsType[modelexecution.ContextPreparationFailure](err)
			require.True(t, present)
			require.Equal(t, string(expected), failure.CompactionFailureCode())
			require.False(t, result.Committed)
			require.Empty(t, service.compactionOperations)
		})
	}
}

// TestCapturedOverflowUsesFreshSnapshot checks repeated recovery on one captured owner.
func TestCapturedOverflowUsesFreshSnapshot(t *testing.T) {
	t.Parallel()
	// Arrange one owner capture and a changed projection for each recovery invocation.
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
	service := New(runtime, sessions)
	service.BindPolicy(policy, compactions, 0)
	capture, err := service.CaptureConversation()
	require.NoError(t, err)
	input := modelexecution.ProviderRequest{
		Instructions: "frozen instructions", Model: model.Descriptor{}, ReasoningChoice: model.ReasoningChoiceOff,
		History: []agent.HistoryEntry{newAutomaticTestHistory("rejected earlier history")}, Tools: nil,
	}
	for _, text := range []string{"new snapshot", "later snapshot"} {
		snapshot := CompactionSnapshot{
			Identity:     identity,
			ActiveLeafID: mo.Some(text),
			Entries:      nil,
			History: newCapturedTestHistory(
				[]agent.HistoryEntry{newAutomaticTestHistory(text)},
			),
			Previous: mo.None[session.CompactionEntry](),
		}
		compactions.EXPECT().CaptureCompaction().Return(snapshot).Times(1)
		source := errors.New("provider rejected input\nsource α: " + text)
		policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ PolicyOwner, _ extension.Context, invocation PolicyInvocation) (PolicyDisposition, error) {
				require.Equal(t, PolicyPurposeOverflow, invocation.Purpose)
				require.Equal(t, mo.Some(source.Error()), invocation.OverflowError)
				require.Equal(t, snapshot, invocation.Snapshot)
				require.Equal(t, snapshot.History.Content(), invocation.Input.History)
				require.Equal(t, input.Instructions, invocation.Input.Instructions)
				require.Equal(t, input.Model, invocation.Input.Model)
				require.Equal(t, input.ReasoningChoice, invocation.Input.ReasoningChoice)
				return PolicyPreserve, nil
			})

		// Act through each admitted recovery on the captured object.
		prepared, recoveryErr := capture.MustGet().RecoverOverflow(t.Context(), input, source)

		// Assert preserve declines replacement without notification or a leaked lifetime.
		require.NoError(t, recoveryErr)
		require.False(t, prepared.Committed)
		require.Equal(t, snapshot.History.Content(), prepared.History)
		require.Empty(t, service.compactionOperations)
	}
}
