//go:build !integration

package presentation

import (
	"context"
	"errors"
	"testing"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestNotifyCanceledDiagnostics checks that the TUI shows canceled diagnostics as information
// and releases foreground ownership.
func TestNotifyCanceledDiagnostics(t *testing.T) {
	t.Parallel()
	for _, rich := range []bool{false, true} {
		name := "plain"
		if rich {
			name = "diagnosed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange an acknowledged foreground operation and its received cancellation.
			service := newTestModel(t, AvailabilityIdle, nil)
			host := NewMockHost(gomock.NewController(t))
			service.host = host
			work := service.prepareCommand(emptyCommand(CommandCompact))
			identifier := work.(*preparedDispatch).identifier
			host.EXPECT().Send(identifier, gomock.Any(), "").Return(nil)
			dispatched := work.Execute()
			require.NoError(t, dispatched.Err)
			service.Complete(dispatched)
			before := len(service.model.state.Transcript)
			var cause error = context.Canceled
			if rich {
				cause = diagnostic.NewCurrentCause(
					context.Canceled,
					errors.New("received canceled source\nfull diagnostic text"),
				)
			}
			// Act through the notification presentation boundary.
			err := service.Notify(
				plugininput.Notification{
					Kind:        plugininput.NotificationFailed,
					OperationID: identifier,
					Payload:     mo.None[plugininput.Payload](),
					FailureCode: "",
					Failure:     cause,
				},
			)
			// Assert that Service.Notify clears terminal ownership and displays cancellation diagnostics as information.
			require.NoError(t, err)
			require.Empty(t, service.foreground)
			require.NotContains(t, service.pending, identifier)
			if rich {
				require.Len(t, service.model.state.Transcript, before+1)
				line := service.model.state.Transcript[len(service.model.state.Transcript)-1]
				require.Equal(t, LineInformation, line.Kind)
				require.Equal(t, mo.Some(cause.Error()), line.Text)
			} else {
				require.Len(t, service.model.state.Transcript, before)
			}
		})
	}
}
