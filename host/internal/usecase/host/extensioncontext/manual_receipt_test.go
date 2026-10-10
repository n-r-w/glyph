//go:build !integration

package extensioncontext

import (
	"context"
	"errors"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks persistence reporting and single replacement.
func (s *manualPolicyRetainsReceiptAfterPolicyFailureSuite) TestScenario() {
	t := s.T()
	// Arrange an owner whose response fails after durable persistence and failed publication.
	controller := gomock.NewController(t)
	// runtime supplies the captured runtime identity.
	runtime := NewMockRuntimeState(controller)
	// sessions supplies the active session identity.
	sessions := NewMockSessionState(controller)
	// policy supplies the failing policy invocation.
	policy := NewMockPolicyRuntime(controller)
	policy.EXPECT().SnapshotCompactionHandlers().Return(contextcompaction.HandlerSet{}).AnyTimes()
	// compactionSessions supplies snapshot and persistence behavior.
	compactionSessions := NewMockCompactionSessions(controller)
	// identity identifies the captured session incarnation.
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	// owner identifies the registered policy handler and runtime.
	owner := PolicyOwner{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "manual"}
	runtime.EXPECT().ContextRuntime(owner.ExtensionID).Return(owner.RuntimeID, true)
	sessions.EXPECT().ContextSession().Return(identity)
	policy.EXPECT().SnapshotCompactionPolicies().Return([]PolicyOwner{owner})
	// snapshot contains the captured branch for replacement.
	snapshot := CompactionSnapshot{
		Identity: identity, ActiveLeafID: mo.Some("leaf"), Entries: nil,
		History: nil, Previous: mo.None[session.CompactionEntry](),
	}
	compactionSessions.EXPECT().CaptureCompaction().Return(snapshot)
	// candidate contains the proposed replacement.
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
	// receipt contains the durable replacement entry.
	receipt := newManualTestReceipt(candidate)
	// publicationCause is the acquired delivery failure.
	publicationCause := errors.New("publication failed: complete writer source")
	// policyCause is the acquired policy transport failure.
	policyCause := errors.New("policy result lost: complete transport source")
	compactionSessions.EXPECT().
		CommitReplacement(gomock.Any(), snapshot, candidate, gomock.Any(), gomock.Any()).
		DoAndReturn(
			func(ctx context.Context, snapshot CompactionSnapshot, candidate session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
				record(receipt)
				return receipt, publicationCause
			},
		)
	// service owns the manual operation under test.
	service := New(runtime, sessions)
	service.policyRuntime = policy
	service.compactionSessions = compactionSessions
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (bool, error) {
			// committed and err contain the durable receipt and delivery failure.
			committed, err := service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			require.Equal(t, receipt, committed)
			require.ErrorIs(t, err, publicationCause)
			// secondErr contains the repeated replacement rejection.
			_, secondErr := service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			require.ErrorContains(t, secondErr, "replacement already requested")
			return false, policyCause
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
