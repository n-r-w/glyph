//go:build integration

package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionruntime "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/internal/errtree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestRuntimeCancellationSettlement checks that the runtime waits for the original terminal outcome
// and preserves current failure precedence in both wire orders.
func TestRuntimeCancellationSettlement(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"handler", "tool", "lifecycle", "compaction"} {
		for _, terminal := range []string{
			"canceled", "completed", "failed", "cancel-canceled", "malformed", "missing", "rejected",
		} {
			for _, order := range []string{"original-first", "cancel-first"} {
				t.Run(entry+"/"+terminal+"/"+order, func(t *testing.T) {
					t.Parallel()
					// Arrange an admitted nested Host operation and a peer waiting for cancellation.
					controller := gomock.NewController(t)
					host := NewMockHostService(controller)
					nested := NewMockHostOperation(controller)
					admitted := make(chan struct{})
					host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).Return(nested, nil)
					nested.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
						func(context.Context, *extensionsdk.HostProgressReporter) (*extensionpb.HostCompleted, error) {
							close(admitted)
							return nil, context.Canceled
						},
					)
					nested.EXPECT().Release()
					runtime := startHelperRuntime(t, settlementPeerPrefix+terminal+"-"+order)
					runtime.connection.BindHostService(host)
					_, err := runtime.Register(t.Context())
					require.NoError(t, err)
					ctx, cancel := context.WithCancel(t.Context())
					done := make(chan error, 1)
					joined := make(chan struct{})
					go func() {
						defer close(joined)
						var invocationErr error
						if entry == "handler" {
							_, invocationErr = runtime.Handle(ctx, fixtureObserverID, runtimeObserverInvocation())
						} else if entry == "lifecycle" {
							invocationErr = runtime.ObserveLifecycle(
								ctx,
								fixtureObserverID,
								newSettlementLifecycleInvocation(),
							)
						} else if entry == "compaction" {
							invocationErr = runtime.ObserveCompactionFailure(
								ctx,
								fixtureObserverID,
								runtimeTestContext(),
								extensioncontext.OutcomeInvocation{
									State:              mo.None[extensioncontext.PolicyState](),
									ParentCompactionID: mo.None[string](),
									Committed:          mo.None[session.Entry](),
									Canceled:           true,
									Error:              mo.None[string](),
								},
							)
						} else {
							_, invocationErr = runtime.Execute(
								ctx,
								fixtureToolName,
								[]byte("{}"),
								discardProgress,
								runtimeTestContext(),
							)
						}
						done <- invocationErr
					}()
					defer func() { cancel(); <-joined }()
					// Act after the nested request establishes that the original operation has started.
					<-admitted
					cancel()
					result := <-done
					// Assert that the runtime retains the original terminal or transport cause and cancellation result.
					require.ErrorIs(t, result, context.Canceled)
					if terminal != "malformed" && terminal != "missing" {
						require.NotErrorIs(t, result, extensionruntime.ErrExtensionUnavailable)
					}
					canceled := errtree.IsCurrentCancellation(
						result,
						func(leaf error) bool { return errors.Is(leaf, context.Canceled) },
					)
					require.Equal(
						t,
						terminal != "failed" && terminal != "malformed" && terminal != "missing" &&
							terminal != "rejected",
						canceled,
					)
					if terminal == "failed" {
						require.ErrorContains(t, result, "original terminal independent failure")
					} else if terminal == "rejected" {
						require.ErrorContains(t, result, "original terminal independent rejection")
					} else if terminal == "missing" {
						require.ErrorContains(t, result, "original terminal missing cause")
						require.ErrorIs(t, result, extensionruntime.ErrExtensionUnavailable)
					} else if terminal == "malformed" {
						require.ErrorIs(t, result, extensionruntime.ErrExtensionUnavailable)
					} else if terminal != "completed" {
						require.ErrorContains(t, result, "original remote cancellation diagnostic")
					}
					if terminal == "cancel-canceled" {
						require.ErrorContains(t, result, "cancellation request canceled diagnostic")
					} else {
						rejection, present := errors.AsType[*extensionsdk.RejectionError](result)
						require.True(t, present)
						expectedCode := "TARGET_NOT_ACTIVE"
						if terminal == "rejected" {
							expectedCode = "BUSY"
						}
						require.Equal(t, expectedCode, rejection.Code())
						require.ErrorContains(t, result, "original operation already settled")
						currentRejection := false
						for current := range errtree.IterateCurrentErrors(result) {
							//nolint:errorlint // The iterator visits current nodes; errors.As would also inspect diagnostics.
							if _, active := current.(*extensionsdk.RejectionError); active {
								currentRejection = true
							}
						}
						require.Equal(t, terminal == "missing" || terminal == "rejected", currentRejection)
					}
					if terminal == "malformed" || terminal == "missing" {
						return
					}
					// Assert that the settled connection still executes a later tool request.
					next, nextErr := runtime.Execute(
						t.Context(),
						fixtureToolName,
						[]byte("{}"),
						func(tool.Progress) error { return nil },
						runtimeTestContext(),
					)
					require.NoError(t, nextErr)
					require.NotEmpty(t, next.Contents)
				})
			}
		}
	}
}
