//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestDeadline checks deadline and caller-cancellation precedence after participant child cleanup.
func (s *policyResultSuite) TestDeadline() {
	for _, scenario := range []string{"participant", "late success", "caller deadline", "caller cancellation"} {
		s.Run(scenario, func() {
			synctest.Test(s.T(), func(t *testing.T) {
				// Arrange bounded result dispatch with an independently failing cleanup.
				handler := contextcompaction.Handler{
					ExtensionID: "policy",
					RuntimeID:   "generation",
					HandlerID:   "result",
				}
				service, runtime, owner := newManualPolicyOperationFixture(
					t,
					contextcompaction.HandlerSet{
						Requests:   nil,
						Generators: nil,
						Results:    []contextcompaction.Handler{handler},
						Successes:  nil,
						Failures:   nil,
					},
				)
				service.participantTimeout = 2 * time.Second
				caller := t.Context()
				callerCause := errors.New("result caller cancellation cause")
				want := context.DeadlineExceeded
				if scenario == "caller deadline" {
					var cancel context.CancelFunc
					caller, cancel = context.WithTimeoutCause(caller, time.Second, callerCause)
					defer cancel()
				}
				if scenario == "caller cancellation" {
					var cancel context.CancelCauseFunc
					caller, cancel = context.WithCancelCause(caller)
					defer cancel(nil)
					go func() {
						select {
						case <-time.After(time.Second):
							cancel(callerCause)
						case <-caller.Done():
						}
					}()
					want = context.Canceled
				}
				cleanupCause := errors.New("result timeout cleanup cause")
				joined := false
				runtime.EXPECT().
					InvokeCompactionResult(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, target Participant, binding extension.Context, input ResultInvocation) (ResultAction, error) {
						release, err := service.BeginCompactionChild(
							target.ExtensionID,
							binding.RuntimeInstanceID,
							input.ParentCompactionID.MustGet(),
						)
						require.NoError(t, err)
						go func() { <-ctx.Done(); time.Sleep(time.Second); joined = true; release(cleanupCause) }()
						<-ctx.Done()
						if scenario == "late success" {
							return ResultAction{}, nil
						}
						return ResultAction{}, context.Cause(ctx)
					})
				runtime.EXPECT().
					InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, captured PolicyOwner, binding extension.Context, input PolicyInvocation) (bool, error) {
						_, err := service.InvokePolicyResult(
							ctx,
							captured.ExtensionID,
							binding.RuntimeInstanceID,
							newPolicyResultCommand(input.OperationID, handler, 0),
						)
						return false, err
					})
				// Act through manual ownership under the fake clock.
				outcome, err := service.CompactManualPolicy(caller, modelexecution.ProviderRequest{}, mo.None[string]())
				// Assert failure precedence, complete acquired causes and cleanup before settlement.
				require.ErrorIs(t, err, want)
				require.ErrorIs(t, err, cleanupCause)
				if scenario == "caller deadline" || scenario == "caller cancellation" {
					require.ErrorIs(t, err, callerCause)
				}
				require.True(t, joined)
				require.True(t, outcome.Committed.IsNone())
				require.Empty(t, service.compactionOperations)
			})
		})
	}
}
