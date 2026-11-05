//go:build !integration

package runcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/internal/errtree"
)

// TestExtensionAbortChecksCapturedRun checks bypass, origin and replacement rejection at the executor boundary.
func TestExtensionAbortChecksCapturedRun(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"active", "idle", "replaced", "canceled"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			// Arrange only executor expectations, with no input-processing or operation-gate calls.
			mocks := gomock.NewController(t)
			executor := NewMockExecutor(mocks)
			coordinator := NewCoordinator(executor, NewMockInputProcessor(mocks), nil, NewMockGate(mocks))
			ctx := t.Context()
			switch state {
			case "idle":
				executor.EXPECT().CheckEnqueue().Return("", agent.ErrRunNotActive)
			default:
				executor.EXPECT().CheckEnqueue().Return("captured", nil)
				if state == "canceled" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				} else {
					executor.EXPECT().Abort("captured", gomock.Any()).DoAndReturn(func(_ string, cause error) error {
						require.ErrorIs(t, cause, context.Canceled)
						require.ErrorContains(t, cause, "bound-extension")
						// Source diagnostics must survive a simultaneous cancellation of the client operation.
						require.False(t, errtree.IsPlainCancellation(cause, func(err error) bool {
							return errors.Is(err, context.Canceled)
						}))
						if state == "replaced" {
							return agent.ErrRunNotActive
						}
						return nil
					})
				}
			}
			// Act by capturing the active identity and then requesting cancellation of only that run.
			id, err := coordinator.CheckAbort()
			if err == nil {
				err = coordinator.Abort(ctx, id, "bound-extension")
			}
			// Assert each rejection remains explicit, without falling back to a new run or operation owner.
			switch state {
			case "active":
				require.NoError(t, err)
			case "canceled":
				require.ErrorIs(t, err, context.Canceled)
			default:
				require.ErrorIs(t, err, agent.ErrRunNotActive)
			}
		})
	}
}
