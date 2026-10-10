//go:build !integration

// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestScenario checks cancellation, joining, and independent post-commit causes.
func (s *manualPolicyOwnsNestedTeardownSuite) TestScenario() {
	t := s.T()
	// scenario contains the nested teardown outcome under test.
	for _, scenario := range []struct {
		// name identifies the teardown scenario.
		name string
		// source contains an independent nested failure.
		source error
	}{
		{name: "expected cancellation", source: nil},
		{name: "independent failure", source: errors.New("nested teardown failed: complete source")},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Arrange a committed operation with nested work still active when the policy returns.
				service, policy, owner := newManualPolicyOperationFixture(t)
				// sessions supplies replacement persistence behavior.
				sessions := service.compactionSessions.(*MockCompactionSessions)
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
				sessions.EXPECT().
					CommitReplacement(gomock.Any(), gomock.Any(), candidate, gomock.Any(), gomock.Any()).
					DoAndReturn(
						func(ctx context.Context, snapshot CompactionSnapshot, value session.CompactionEntry, guard ContextCommitGuard, record func(session.Entry)) (session.Entry, error) {
							record(receipt)
							return receipt, nil
						},
					)
				// joined signals completion of the nested worker.
				joined := make(chan struct{})
				policy.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, target PolicyOwner, bound extension.Context, invocation PolicyInvocation) (bool, error) {
						// err contains the replacement commit failure.
						if _, err := service.CommitPolicyReplacement(
							ctx,
							owner.ExtensionID,
							owner.RuntimeID,
							invocation.OperationID,
							candidate,
						); err != nil {
							return false, err
						}
						// release and err contain child admission ownership and its failure.
						release, err := service.BeginCompactionChild(
							owner.ExtensionID,
							owner.RuntimeID,
							invocation.LifecycleID,
						)
						if err != nil {
							return false, err
						}
						// childContext and unlink contain parent-linked cancellation and its release.
						childContext, unlink := service.LinkCompactionChild(ctx, invocation.LifecycleID)
						go func() {
							<-childContext.Done()
							// cause retains the worker outcome before it releases its linkage.
							cause := errors.Join(childContext.Err(), scenario.source)
							unlink()
							close(joined)
							// Release parent ownership only after all observed worker work is complete.
							release(cause)
						}()
						return false, nil
					},
				)

				// Act by completing the owning policy while its nested worker is active.
				result, err := service.CompactManualPolicy(
					t.Context(),
					modelexecution.ProviderRequest{},
					mo.None[string](),
				)

				// Assert that the parent joins the child and distinguishes expected cancellation from failure.
				select {
				case <-joined:
				default:
					assert.Fail(t, "parent returned before its nested worker ended")
				}
				require.Equal(t, mo.Some(receipt), result.Committed)
				if scenario.source == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, scenario.source)
					require.ErrorContains(t, err, scenario.source.Error())
				}
			})
		})
	}
}
