//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks successful replacement after a handled proposal rejection.
func (s *manualPolicyCanRejectItsOwnProposalSuite) TestScenario() {
	t := s.T()
	// Arrange a rejected preliminary projection and a valid final replacement.
	service, policy, owner := newManualPolicyOperationFixture(t)
	sessions := service.compactionSessions.(*MockCompactionSessions)
	candidate := session.CompactionEntry{
		Summary:          "summary",
		FirstKeptEntryID: "leaf",
		Source: session.CompactionSource{
			ExtensionID: mo.Some(owner.ExtensionID),
			Requests:    nil,
		},

		Details: mo.None[[]byte](),
	}
	receipt := newManualTestReceipt(candidate)
	rejected := errors.New("proposed boundary separates a tool call from its result")
	sessions.EXPECT().ProjectReplacement(gomock.Any(), candidate).Return(nil, rejected)
	sessions.EXPECT().CommitReplacement(gomock.Any(), gomock.Any(), candidate, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, snapshot CompactionSnapshot, value session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
			record(receipt)
			return receipt, nil
		},
	)
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (PolicyDisposition, error) {
			release, err := service.BeginCompactionChild(
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.LifecycleID,
			)
			if err != nil {
				return PolicyReplaced, err
			}
			childContext, unlink := service.LinkCompactionChild(ctx, invocation.LifecycleID)
			_, proposalErr := service.ProjectPolicyReplacement(
				childContext,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			unlink()
			release(proposalErr)
			assert.ErrorIs(t, proposalErr, rejected)
			_, err = service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			return PolicyReplaced, err
		},
	)

	// Act through the parent manual operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())

	// Assert that the extension's handled proposal rejection does not become a Host policy decision.
	require.NoError(t, err)
	require.Equal(t, mo.Some(receipt), result.Committed)
}

// newManualTestReceipt supplies a complete durable entry for operation-state tests.
func newManualTestReceipt(candidate session.CompactionEntry) session.Entry {
	return session.Entry{
		ID: "durable-replacement", ParentID: mo.Some("leaf"), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(candidate),
	}
}
