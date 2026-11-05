//go:build !integration

package runcontrol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestNextTurnEnqueueNeedsNoRun checks idle acceptance and cancellation without client input processing.
func TestNextTurnEnqueueNeedsNoRun(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "canceled"}[canceled], func(t *testing.T) {
			t.Parallel()
			// Arrange a coordinator with no active-run or input-processing expectation.
			mocks := gomock.NewController(t)
			executor := NewMockExecutor(mocks)
			message := session.QueuedExtensionMessage{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if canceled {
				cancel()
			} else {
				executor.EXPECT().EnqueueExtension("", agent.MessageDeliveryNextTurn, message).Return(nil)
			}
			coordinator := NewCoordinator(executor, NewMockInputProcessor(mocks), nil, nil)

			// Act through the extension enqueue operation.
			err := coordinator.EnqueueExtension(ctx, agent.MessageDeliveryNextTurn, message)

			// Assert only cancellation rejects the attachment.
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
