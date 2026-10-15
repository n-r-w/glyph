//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks persistence reporting and single replacement.
func (s *manualPolicyRetainsReceiptAfterPolicyFailureSuite) TestScenario() {
	t := s.T()
	// Arrange an owner whose response fails after durable persistence and failed publication.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	policy := NewMockPolicyRuntime(controller)
	policy.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{}).AnyTimes()
	compactionSessions := NewMockCompactionSessions(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "manual"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
	sessions.EXPECT().ContextSession().Return(identity)
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
	snapshot := CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil,
		History: nil, Previous: mo.None[session.CompactionEntry](),
	}
	compactionSessions.EXPECT().CaptureCompaction().Return(snapshot)
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
	publicationCause := errors.New("publication failed: complete writer source")
	policyCause := errors.New("policy result lost: complete transport source")
	compactionSessions.EXPECT().
		CommitReplacement(gomock.Any(), snapshot, candidate, gomock.Any(), gomock.Any()).
		DoAndReturn(
			func(ctx context.Context, snapshot CompactionSnapshot, candidate session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
				record(receipt)
				return receipt, publicationCause
			},
		)
	service := New(runtime, sessions)
	service.policyRuntime = policy
	service.compactionSessions = compactionSessions
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (PolicyDisposition, error) {
			committed, err := service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			require.Equal(t, receipt, committed)
			require.ErrorIs(t, err, publicationCause)
			_, secondErr := service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			require.ErrorContains(t, secondErr, "replacement already requested")
			return PolicyReplaced, policyCause
		},
	)

	// Act through the parent manual operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())

	// Assert that the durable receipt and both acquired causes survive a lost policy result.
	require.Equal(t, mo.Some(receipt), result.Committed)
	require.ErrorIs(t, err, publicationCause)
	require.ErrorIs(t, err, policyCause)
	require.ErrorContains(t, err, publicationCause.Error())
	require.ErrorContains(t, err, policyCause.Error())
}
