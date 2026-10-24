//go:build !integration

package presentation

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestFailedSelectionAppliesCommittedState verifies retained selection, complete error, and pending settlement in either delivery order.
func TestFailedSelectionAppliesCommittedState(t *testing.T) {
	t.Parallel()
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledgement-first", true: "terminal-first"}[early], func(t *testing.T) {
			t.Parallel()
			// Arrange an admitted selection whose Host confirms a commit before reporting observer failure.
			service := newTestModel(t, AvailabilityIdle, nil)
			host := NewMockHost(gomock.NewController(t))
			service.host = host
			work := service.prepareCommand(emptyCommand(CommandSelectModel))
			identifier := work.(*preparedDispatch).identifier
			host.EXPECT().Send(identifier, gomock.Any(), "").Return(nil)
			acknowledgement := work.Execute()
			require.NoError(t, acknowledgement.Err)
			if !early {
				service.Complete(acknowledgement)
			}
			payload := plugininput.NewPayload(plugininput.PayloadSelection)
			payload.Selection = plugininput.ModelSelection{
				ProviderID:      "committed-provider",
				ModelID:         "committed-model",
				ReasoningChoice: plugininput.ReasoningChoiceHigh,
			}
			cause := errors.New("selection observer failed: complete unique cause")

			// Act through the same failed notification input used by the TUI controller.
			require.NoError(t, service.Notify(plugininput.Notification{
				Kind: plugininput.NotificationFailed, OperationID: identifier, Payload: mo.Some(payload),
				Failure: cause, FailureCode: "EXTENSION_FAILED",
			}))
			if early {
				service.Complete(acknowledgement)
			}

			// Assert Host state is applied, the complete failure is shown once, and pending work returns to idle.
			selection, present := service.model.state.ModelSelection.Get()
			require.True(t, present)
			require.Equal(t, "committed-provider", selection.ProviderID)
			require.Equal(t, "committed-model", selection.ModelID)
			require.Equal(t, ReasoningChoiceHigh, selection.ReasoningChoice)
			require.Len(t, service.model.state.Transcript, 1)
			require.Equal(t, mo.Some(cause.Error()), service.model.state.Transcript[0].Text)
			require.Empty(t, service.pending)
			require.Empty(t, service.foreground)
			require.False(t, service.model.emitting)
			require.Equal(t, mo.Some(AvailabilityIdle), service.model.state.Availability)
		})
	}
}
