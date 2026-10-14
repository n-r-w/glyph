//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks complete diagnostics after a successful commit.
func (s *manualObserverFailureRetainsHandledPolicyCausesSuite) TestScenario() {
	t := s.T()
	// Arrange a handled child failure followed by successful persistence and a failing observer.
	observer := contextcompaction.Handler{ExtensionID: "policy", RuntimeID: "generation", HandlerID: "observer"}
	first := observer
	first.HandlerID = "handled-observer"
	handlers := contextcompaction.HandlerSet{
		Requests:   nil,
		Generators: nil,
		Results:    nil,
		Successes:  []contextcompaction.Handler{first, observer},
		Failures:   nil,
	}
	service, policy, owner := newManualPolicyOperationFixture(t, handlers)
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
	childCause := errors.New("handled projection producer cause")
	observerCause := errors.New("original observer cause")
	observerChildCause := errors.New("handled observer model cause")
	// joined signals completion of the observer child.
	joined := make(chan struct{})
	sessions := service.compactionSessions.(*MockCompactionSessions)
	sessions.EXPECT().CommitReplacement(gomock.Any(), gomock.Any(), candidate, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, snapshot CompactionSnapshot, value session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
			record(receipt)
			return receipt, nil
		})
	policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (PolicyDisposition, error) {
			release, err := service.BeginCompactionChild(owner.ExtensionID, owner.RuntimeID, invocation.LifecycleID)
			if err != nil {
				return PolicyReplaced, err
			}
			release(childCause)
			_, err = service.CommitPolicyReplacement(
				ctx,
				owner.ExtensionID,
				owner.RuntimeID,
				invocation.OperationID,
				candidate,
			)
			return PolicyReplaced, err
		})
	policy.EXPECT().ObserveCompactionSuccess(gomock.Any(), first, gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, target contextcompaction.Handler, bound extension.Context, outcome contextcompaction.OutcomeInvocation) error {
			lifetimeID := outcome.ParentCompactionID.OrEmpty()
			release, err := service.BeginCompactionChild(
				target.ExtensionID,
				target.RuntimeID,
				lifetimeID,
			)
			if err != nil {
				return err
			}
			release(observerChildCause)
			finish, err := service.BeginCompactionChild(
				target.ExtensionID,
				target.RuntimeID,
				lifetimeID,
			)
			if err != nil {
				return err
			}
			child, unlink := service.LinkCompactionChild(ctx, lifetimeID)
			go func() {
				<-child.Done()
				// Capture the child outcome before unlinking cancellation.
				childOutcome := child.Err()
				unlink()
				close(joined)
				finish(childOutcome)
			}()
			return nil
		})
	policy.EXPECT().ObserveCompactionSuccess(gomock.Any(), observer, gomock.Any(), gomock.Any()).Return(observerCause)

	// Act through the owning manual operation.
	result, err := service.CompactManualPolicy(t.Context(), modelexecution.ProviderRequest{}, mo.None[string]())

	// Assert that a later terminal failure retains every acquired cause and the independent receipt.
	require.Equal(t, mo.Some(receipt), result.Committed)
	require.ErrorIs(t, err, observerCause)
	require.ErrorIs(t, err, childCause)
	require.ErrorIs(t, err, observerChildCause)
	select {
	case <-joined:
	default:
		assert.Fail(t, "observer child was not joined before terminal return")
	}
}

// TestScenario checks a failed observer binding with a durable result.
func (s *manualUnavailableObserverRetainsCausesSuite) TestScenario() {
	t := s.T()
	// Arrange an unavailable captured observer and independent acquired causes.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	runtime.EXPECT().ContextRuntime("observer").Return("", false)
	sessions.EXPECT().
		ContextSession().
		Return(session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1})
	service := New(runtime, sessions)
	cause := errors.New("original policy RPC cause")
	handled := errors.New("handled policy model cause")
	observer := contextcompaction.Handler{ExtensionID: "observer", RuntimeID: "generation", HandlerID: "failure"}
	handlers := contextcompaction.HandlerSet{
		Requests:   nil,
		Generators: nil,
		Results:    nil,
		Successes:  nil,
		Failures:   []contextcompaction.Handler{observer},
	}
	result := modelexecution.ManualCompactionResult{Committed: mo.Some(session.Entry{}), Canceled: false}

	// Act through terminal notification settlement.
	err := service.finishPolicyOutcome(
		t.Context(),
		handlers,
		mo.None[contextcompaction.PolicyState](),
		result,
		cause,
		handled,
	)

	// Assert that binding failure supplements every acquired cause.
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, handled)
	require.ErrorContains(t, err, "issue compaction observer")
}
