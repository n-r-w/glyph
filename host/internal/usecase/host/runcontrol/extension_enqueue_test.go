//go:build !integration

package runcontrol

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestExtensionEnqueueBypassesInput checks acceptance and race rejection without any client processing.
func TestExtensionEnqueueBypassesInput(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"active", "idle", "ended"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			// Arrange Core acceptance and no input-processing expectation.
			mocks := gomock.NewController(t)
			executor := NewMockExecutor(mocks)
			input := NewMockInputProcessor(mocks)
			message := session.QueuedExtensionMessage{
				Session: session.Identity{
					ID:               "session",
					WorkingDirectory: "/project",
					Incarnation:      1,
				},
				RuntimeInstanceID: "runtime",
				Message: session.ExtensionMessage{
					ExtensionID: "extension",
					EntryType:   "kind",
					Text:        "exact",
					Visibility:  session.ClientVisibilityHidden,
				},
			}
			if state == "idle" {
				executor.EXPECT().CheckEnqueue().Return("", agent.ErrRunNotActive)
			} else {
				executor.EXPECT().CheckEnqueue().Return("run", nil)
				var cause error
				if state == "ended" {
					cause = agent.ErrRunNotActive
				}
				executor.EXPECT().EnqueueExtension("run", agent.MessageDeliveryFollowUp, message).Return(cause)
			}
			coordinator := NewCoordinator(executor, input, nil, nil)

			// Act by accepting the extension payload.
			err := coordinator.EnqueueExtension(t.Context(), agent.MessageDeliveryFollowUp, message)

			// Assert full rejection text or successful acceptance with no client handler calls.
			if state == "active" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, agent.ErrRunNotActive)
				require.ErrorContains(t, err, agent.ErrRunNotActive.Error())
			}
		})
	}
}
