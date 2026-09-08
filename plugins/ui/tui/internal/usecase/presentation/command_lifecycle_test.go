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

// TestTerminalBeforeAcknowledgementReleasesForeground preserves causal transcript order and newer drafts.
func TestTerminalBeforeAcknowledgementReleasesForeground(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"settled", "failure", "model"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			for _, lateResult := range []string{"success", "failure"} {
				t.Run(lateResult, func(t *testing.T) {
					t.Parallel()
					// Arrange a submitted draft whose Host output overtakes its local dispatch result.
					service := newTestModel(t, AvailabilityIdle, nil)
					service.Key(tuiinput.Key{Code: 0, Text: "hello", Mod: 0})
					work := service.Key(testKey(tuiinput.KeyEnter))
					result := work.Execute()
					require.NoError(t, result.Err)
					notification := plugininput.Notification{
						FailureCode: "", Kind: plugininput.NotificationCompleted, OperationID: result.ID,
						Payload: mo.Some(plugininput.NewPayload(plugininput.PayloadSettled)), Failure: nil,
					}
					if scenario == "failure" {
						notification.Kind = plugininput.NotificationFailed
						notification.Failure = errors.New("Host rejected submission")
						notification.Payload = mo.None[plugininput.Payload]()
					}

					// Act by consuming content-bearing progress or terminal data before acknowledgement.
					if scenario == "model" {
						require.NoError(t, service.Notify(plugininput.Notification{
							FailureCode: "",
							Kind:        plugininput.NotificationProgress,
							OperationID: result.ID,
							Failure:     nil,
							Payload: mo.Some(plugininput.AgentPayload(plugininput.AgentUpdate{
								Kind:             plugininput.AgentModelEnd,
								Position:         mo.None[int](),
								ModelContentKind: mo.None[plugininput.ModelContentKind](),
								ModelResponseContent: []plugininput.ModelResponseContent{{
									Kind: plugininput.ModelContentText, Text: mo.Some("model reply"),
								}},
								ToolCallID: mo.None[string](),
								ToolName:   mo.None[string](),
								Status:     mo.None[string](),
								Stream:     mo.None[plugininput.OutputStream](),
								Text:       mo.None[string](),
								Contents:   mo.None[[]plugininput.Content](),
								ErrorText:  mo.None[string](),
								ExitCode:   mo.None[int](),
								Failure:    mo.None[bool](),
								ToolCall:   mo.None[plugininput.ToolCallState](),
							})),
						}))
						require.Equal(t, mo.Some("hello"), service.model.state.Transcript[0].Text)
						require.Equal(t, mo.Some("model reply"), service.model.state.Transcript[1].Text)
						require.Equal(t, result.ID, service.foreground)
					}
					require.NoError(t, service.Notify(notification))

					// Assert the submitted line precedes output while terminal input releases foreground.
					require.NotEmpty(t, service.model.state.Transcript)
					require.Equal(t, mo.Some("hello"), service.model.state.Transcript[0].Text)
					if scenario == "failure" {
						require.Equal(t, mo.Some(notification.Failure.Error()), service.model.state.Transcript[1].Text)
					}
					require.Empty(t, service.foreground)
					require.Contains(t, service.pending, result.ID)
					before := service.model.state.Clone()
					service.Key(tuiinput.Key{Code: 0, Text: "new draft", Mod: 0})
					require.Equal(t, "new draft", string(service.model.input))
					if lateResult == "failure" {
						result.Err = errors.New("late dispatch diagnostic")
					}
					service.Complete(result)
					require.NotContains(t, service.pending, result.ID)
					require.Equal(t, "new draft", string(service.model.input))
					require.Equal(t, len("new draft"), service.model.cursor)
					if result.Err == nil {
						require.Equal(t, before.Transcript, service.model.state.Transcript)
					} else {
						require.Len(t, service.model.state.Transcript, len(before.Transcript)+1)
						require.Equal(t, before.Transcript, service.model.state.Transcript[:len(before.Transcript)])
						require.Contains(
							t,
							service.model.state.Transcript[len(before.Transcript)].Text.OrEmpty(),
							result.Err.Error(),
						)
					}
				})
			}
		})
	}
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
