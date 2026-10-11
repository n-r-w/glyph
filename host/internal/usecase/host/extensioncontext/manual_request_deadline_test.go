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

// TestPolicyRequestDeadline checks invocation bounds and joined child causes.
func TestPolicyRequestDeadline(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"participant", "participant late success", "caller deadline", "caller cancellation"} {
		t.Run(scenario, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				// Arrange a captured participant with owned nested work.
				handler := contextcompaction.Handler{
					ExtensionID: "policy",
					RuntimeID:   "generation",
					HandlerID:   "bounded",
				}
				service, runtime, owner := newManualPolicyOperationFixture(t, contextcompaction.HandlerSet{
					Requests: []contextcompaction.Handler{
						handler,
					},
					Generators: nil,
					Results:    nil,
					Successes:  nil,
					Failures:   nil,
				})
				service.participantTimeout = 2 * time.Second
				caller := t.Context()
				callerCause := errors.New("caller cancellation source")
				want := error(context.DeadlineExceeded)
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
						time.Sleep(time.Second)
						cancel(callerCause)
					}()
					want = context.Canceled
				}
				nestedCause := errors.New("participant child cleanup source")
				childJoined := false
				runtime.EXPECT().
					InvokeCompactionRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(
						func(ctx context.Context, target RequestParticipant, bound extension.Context, input RequestInvocation) (RequestAction, error) {
							release, err := service.BeginCompactionChild(
								target.ExtensionID,
								bound.RuntimeInstanceID,
								input.ParentCompactionID.MustGet(),
							)
							require.NoError(t, err)
							go func() {
								<-ctx.Done()
								// Cleanup completes after the participant invocation returns.
								time.Sleep(time.Second)
								childJoined = true
								release(nestedCause)
							}()
							select {
							case <-ctx.Done():
								if scenario == "participant late success" {
									return RequestAction{}, nil
								}
								return RequestAction{}, context.Cause(ctx)
							case <-time.After(3 * time.Second):
								return RequestAction{}, errors.New("participant exceeded its configured deadline")
							}
						},
					)
				runtime.EXPECT().InvokeCompactionPolicy(gomock.Any(), owner, gomock.Any(), gomock.Any()).DoAndReturn(
					func(ctx context.Context, captured PolicyOwner, bound extension.Context, input PolicyInvocation) (bool, error) {
						_, err := service.InvokePolicyRequest(
							ctx,
							captured.ExtensionID,
							bound.RuntimeInstanceID,
							newPolicyRequestCommand(input.OperationID, handler, 0),
						)
						require.True(t, childJoined)
						return false, err
					},
				)
				// Act through the owning manual operation.
				result, err := service.CompactManualPolicy(caller, modelexecution.ProviderRequest{}, mo.None[string]())
				// Assert timeout or earlier caller cancellation and complete cleanup causes.
				require.ErrorIs(t, err, want)
				require.ErrorIs(t, err, nestedCause)
				if scenario == "caller deadline" || scenario == "caller cancellation" {
					require.ErrorIs(t, err, callerCause)
				}
				require.True(t, result.Committed.IsNone())
				require.Empty(t, service.compactionOperations)
			})
		})
	}
}
