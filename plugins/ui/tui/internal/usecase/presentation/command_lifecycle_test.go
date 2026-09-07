//go:build !integration

package presentation

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestForegroundCancelTargetSurvivesConcurrentCommands preserves first-foreground policy at its application owner.
func TestForegroundCancelTargetSurvivesConcurrentCommands(t *testing.T) {
	t.Parallel()
	for name, foreground := range map[string]CommandKind{
		"submit": CommandSubmit, "navigation": CommandNavigateSessionTree,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one foreground operation and overlapping navigation and information work.
			service := newTestModel(t, AvailabilityRunning, nil)
			host := NewMockHost(gomock.NewController(t))
			service.host = host
			first := service.prepareCommand(emptyCommand(foreground))
			firstID := first.(*preparedDispatch).identifier
			host.EXPECT().Send(firstID, gomock.Any(), "").Return(nil)
			service.Complete(first.Execute())
			for _, kind := range []CommandKind{CommandNavigateSessionTree, CommandGetSessionInfo} {
				work := service.prepareCommand(emptyCommand(kind))
				host.EXPECT().Send(gomock.Any(), gomock.Any(), "").Return(nil)
				service.Complete(work.Execute())
			}
			host.EXPECT().Send(gomock.Any(), emptyCommand(CommandStop), firstID).Return(nil)

			// Act by preparing and executing Stop after the overlapping operations.
			stop := service.prepareCommand(emptyCommand(CommandStop))
			result := stop.Execute()
			service.Complete(result)

			// Assert Stop retains the first foreground target and does not replace it.
			require.NoError(t, result.Err)
			require.Equal(t, firstID, service.foreground)
		})
	}
}

// TestTerminalBeforeAcknowledgementReleasesForeground preserves correlation without retaining foreground ownership.
func TestTerminalBeforeAcknowledgementReleasesForeground(t *testing.T) {
	t.Parallel()
	// Arrange a dispatch whose terminal notification can overtake its event-loop acknowledgement.
	service := newTestModel(t, AvailabilityIdle, nil)
	command := emptyCommand(CommandSubmit)
	command.Text = mo.Some("hello")
	work := service.prepareCommand(command)
	result := work.Execute()
	require.NoError(t, result.Err)

	// Act by consuming terminal data before the command result returns to the event loop.
	require.NoError(t, service.Notify(plugininput.Notification{
		FailureCode: "",
		Kind:        plugininput.NotificationCompleted,
		OperationID: result.ID,
		Payload:     mo.Some(plugininput.NewPayload(plugininput.PayloadSettled)),
		Failure:     nil,
	}))

	// Assert terminal input releases foreground immediately but retains the outstanding acknowledgement.
	require.Empty(t, service.foreground)
	require.Contains(t, service.pending, result.ID)
	service.Complete(result)
	require.NotContains(t, service.pending, result.ID)
	require.Equal(t, mo.Some("hello"), service.model.state.Transcript[0].Text)
	require.Equal(t, mo.Some(true), service.model.state.Settled)
}

// TestFailedDispatchReleasesPendingState preserves the draft without waiting for a terminal event that cannot arrive.
func TestFailedDispatchReleasesPendingState(t *testing.T) {
	t.Parallel()
	// Arrange a draft whose SDK send fails.
	cause := errors.New("SDK queue unavailable")
	service := newTestModel(t, AvailabilityIdle, func(Command) error { return cause })
	service.Key(tuiinput.Key{Code: 0, Text: "draft", Mod: 0})
	work := service.Key(testKey(tuiinput.KeyEnter))

	// Act by returning the failed background I/O result to its serialized owner.
	result := work.Execute()
	require.ErrorIs(t, result.Err, cause)
	service.Complete(result)

	// Assert there is no leaked correlation or foreground target and the exact draft remains.
	require.Empty(t, service.pending)
	require.Empty(t, service.foreground)
	require.Equal(t, "draft", string(service.model.input))
	require.Contains(t, service.model.state.Transcript[0].Text.OrEmpty(), cause.Error())
}

// TestQuitAcknowledgementNeedsNoOperationTerminal completes connection work without leaking an operation item.
func TestQuitAcknowledgementNeedsNoOperationTerminal(t *testing.T) {
	t.Parallel()
	// Arrange a local Quit command whose transport closes the connection.
	service := newTestModel(t, AvailabilityIdle, nil)
	work := service.Key(tuiinput.Key{Code: 'q', Text: "", Mod: tuiinput.ModCtrl})

	// Act by acknowledging connection closure without an operation terminal event.
	quit := service.Complete(work.Execute())

	// Assert the event loop receives its Quit decision and retains no pending command.
	require.True(t, quit)
	require.Empty(t, service.pending)
}

// TestCompletionWithoutPayloadReleasesOperation preserves terminal cleanup without inventing a display update.
func TestCompletionWithoutPayloadReleasesOperation(t *testing.T) {
	t.Parallel()
	// Arrange accepted authentication work, which has no completed display payload.
	service := newTestModel(t, AvailabilityIdle, nil)
	work := service.prepareCommand(emptyCommand(CommandRetryAuthentication))
	result := work.Execute()
	service.Complete(result)
	require.Contains(t, service.pending, result.ID)
	before := service.model.state.Clone()

	// Act by consuming a terminal notification with explicitly absent payload data.
	err := service.Notify(plugininput.Notification{
		FailureCode: "",
		Kind:        plugininput.NotificationCompleted, OperationID: result.ID,
		Payload: mo.None[plugininput.Payload](), Failure: nil,
	})

	// Assert terminal state is released while projection state stays intact.
	require.NoError(t, err)
	require.Empty(t, service.pending)
	require.Equal(t, before, service.model.state)
}

// TestPreparedWorkDoesNotMutateApplicationState keeps background I/O outside serialized transitions.
func TestPreparedWorkDoesNotMutateApplicationState(t *testing.T) {
	t.Parallel()
	// Arrange a prepared submission with its application state still awaiting acknowledgement.
	service := newTestModel(t, AvailabilityIdle, nil)
	service.Key(tuiinput.Key{Code: 0, Text: "hello", Mod: 0})
	work := service.Key(testKey(tuiinput.KeyEnter))
	before := service.model.state.Clone()

	// Act by executing only the immutable prepared work.
	result := work.Execute()

	// Assert only a later event-loop acknowledgement changes draft and transcript state.
	require.NoError(t, result.Err)
	require.Equal(t, before, service.model.state)
	require.Equal(t, "hello", string(service.model.input))
	require.True(t, service.model.emitting)
	service.Complete(result)
	require.Empty(t, service.model.input)
	require.False(t, service.model.emitting)
}
