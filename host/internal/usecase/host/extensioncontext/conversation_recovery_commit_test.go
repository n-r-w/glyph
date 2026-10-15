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

// TestCapturedOverflowCommitFailures checks authoritative commit sources beside serialized policy reports.
func TestCapturedOverflowCommitFailures(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name        string
		committed   bool
		persistence bool
		guard       bool
		observer    bool
		category    modelexecution.FailureCategory
	}{
		{name: "structural", committed: false, persistence: false, guard: false, observer: false, category: modelexecution.FailureCompactionFailed},
		{name: "publication", committed: true, persistence: false, guard: false, observer: false, category: modelexecution.FailureInternal},
		{name: "persistence", committed: false, persistence: true, guard: false, observer: false, category: modelexecution.FailurePersistenceUnavailable},
		{name: "commit guard", committed: false, persistence: false, guard: true, observer: false, category: modelexecution.FailureExtensionFailed},
		{name: "outcome observer", committed: true, persistence: false, guard: false, observer: true, category: modelexecution.FailureExtensionFailed},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a commit boundary with independent source identity and a serialized policy report.
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
			observer := contextcompaction.Handler{
				ExtensionID: owner.ExtensionID,
				RuntimeID:   owner.RuntimeID,
				HandlerID:   "success",
			}
			var successes []contextcompaction.Handler
			if scenario.observer {
				successes = []contextcompaction.Handler{observer}
			}
			policy.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{
				Requests: nil, Generators: nil, Results: nil, Successes: successes, Failures: nil,
			}).Times(1)
			snapshot := CompactionSnapshot{
				Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil, History: nil,
				Previous: mo.None[session.CompactionEntry](),
			}
			compactions.EXPECT().CaptureCompaction().Return(snapshot).Times(1)
			candidate := session.CompactionEntry{
				Summary:          "summary",
				FirstKeptEntryID: "leaf",
				Source: session.CompactionSource{
					ExtensionID: mo.Some(owner.ExtensionID),
					Model:       mo.None[session.BranchSummaryModelSource](),
				},
				EstimatedCost: mo.None[session.EstimatedCost](),
				Details:       mo.None[[]byte](),
			}
			receipt := newManualTestReceipt(candidate)
			source := errors.New("commit source failed\nsource β")
			commitCause := error(source)
			if scenario.observer {
				commitCause = nil
				policy.EXPECT().
					ObserveCompactionSuccess(gomock.Any(), observer, gomock.Any(), gomock.Any()).
					Return(source)
			}
			if scenario.persistence {
				commitCause = errors.Join(source, session.ErrPersistenceUnavailable)
			}
			if scenario.guard {
				runtime.EXPECT().BeginContextCommit(owner.ExtensionID, owner.RuntimeID).Return(nil, source)
			}
			compactions.EXPECT().
				CommitReplacement(gomock.Any(), snapshot, candidate, gomock.Any(), gomock.Any()).
				DoAndReturn(
					func(_ context.Context, _ CompactionSnapshot, _ session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
						if scenario.guard {
							_, err := guard()
							return session.Entry{}, err
						}
						if scenario.committed {
							record(receipt)
							return receipt, commitCause
						}
						return session.Entry{}, commitCause
					})
			service := New(runtime, sessions)
			service.BindPolicy(policy, compactions, 0)
			serialized := errors.New("policy serialized commit failure\nsource γ")
			policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
				func(ctx context.Context, _ PolicyOwner, _ extension.Context, input PolicyInvocation) (PolicyDisposition, error) {
					_, err := service.CommitPolicyReplacement(
						ctx,
						owner.ExtensionID,
						owner.RuntimeID,
						input.OperationID,
						candidate,
					)
					if scenario.observer {
						require.NoError(t, err)
						return PolicyReplaced, nil
					}
					require.ErrorIs(t, err, source)
					return PolicyReplaced, serialized
				})
			captured, err := service.CaptureConversation()
			require.NoError(t, err)

			// Act through the same captured policy and independent receipt boundary.
			result, err := captured.MustGet().
				RecoverOverflow(t.Context(), modelexecution.ProviderRequest{}, errors.New("provider overflow"))

			// Assert commit sources retain precedence and every local identity without another outgoing check.
			require.ErrorIs(t, err, source)
			require.Contains(t, err.Error(), source.Error())
			if scenario.observer {
				require.NotErrorIs(t, err, serialized)
			} else {
				require.ErrorIs(t, err, serialized)
				require.Contains(t, err.Error(), serialized.Error())
			}
			failure, present := errors.AsType[modelexecution.ContextPreparationFailure](err)
			require.True(t, present)
			require.Equal(t, string(scenario.category), failure.CompactionFailureCode())
			require.Equal(t, scenario.committed, result.Committed)
			require.Empty(t, service.compactionOperations)
		})
	}
}
