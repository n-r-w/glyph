//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// parentGuardSuite owns the synchronous parent cancellation scenario.
type parentGuardSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestManualCommitGuardRejectsCanceledParentWithLiveChild checks cancellation before append.
func TestManualCommitGuardRejectsCanceledParentWithLiveChild(t *testing.T) {
	// Arrange parallel execution of the persistence-guard suite.
	t.Parallel()
	// Act through parent cancellation with a live admitted child.
	// Assert synchronous persistence rejection and joined child release in TestScenario.
	suite.Run(t, &parentGuardSuite{Suite: suite.Suite{}})
}

// TestScenario checks the actual parent cause at the guard while the child remains live.
func (s *parentGuardSuite) TestScenario() {
	t := s.T()
	// Arrange a live child whose parent is canceled immediately before the persistence guard.
	service, policy, owner := newManualPolicyOperationFixture(t)
	cause := errors.New("original parent canceled before append")
	parent, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	candidate := session.CompactionEntry{
		Summary:          "summary",
		FirstKeptEntryID: "leaf",
		Source: session.CompactionSource{
			ExtensionID: mo.Some(owner.ExtensionID),
			Requests:    nil,
		},

		Details: mo.None[[]byte](),
	}
	childReleased := false
	appended := false
	sessions := service.compactionSessions.(*MockCompactionSessions)
	sessions.EXPECT().CommitReplacement(gomock.Any(), gomock.Any(), candidate, gomock.Any(), gomock.Any()).DoAndReturn(
		func(child context.Context, snapshot CompactionSnapshot, value session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
			cancel(cause)
			require.NoError(t, child.Err())
			unlock, err := guard()
			if err != nil {
				require.ErrorIs(t, err, cause)
				return session.Entry{}, err
			}
			defer unlock()
			appended = true
			record(newManualTestReceipt(value))
			return newManualTestReceipt(value), nil
		})
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (PolicyDisposition, error) {
			release, err := service.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, invocation.LifecycleID)
			if err != nil {
				return PolicyReplaced, err
			}
			_, err = service.CommitPolicyReplacement(
				t.Context(),
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			release(err)
			childReleased = true
			return PolicyReplaced, err
		})
	// Act through the owning manual operation.
	result, err := service.CompactManualPolicy(parent, modelexecution.ProviderRequest{}, mo.None[string]())
	// Assert no durable replacement, complete parent cause, and admitted-child release before settlement.
	require.ErrorIs(t, err, cause)
	require.True(t, result.Committed.IsNone())
	require.False(t, appended)
	require.True(t, childReleased)
}
