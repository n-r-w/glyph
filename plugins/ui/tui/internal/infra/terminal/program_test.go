//go:build integration

package terminal

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestRuntimeEmitsSubmittedTerminalInput verifies real framework input reaches prepared application work.
func TestRuntimeEmitsSubmittedTerminalInput(t *testing.T) {
	t.Parallel()
	// Arrange the real runtime and application with a captured Host command.
	controller := gomock.NewController(t)
	host := presentation.NewMockHost(controller)
	emitted := make(chan presentation.Command, 1)
	host.EXPECT().Send(gomock.Any(), gomock.Any(), "").DoAndReturn(
		func(_ string, command presentation.Command, _ string) error { emitted <- command; return nil },
	)
	host.EXPECT().StopDispatch()
	application, input, output := initializedRuntime(t, controller, host)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- application.Run(ctx) }()
	<-output.written

	// Act by submitting Unicode input through the real terminal pipe.
	_, err := io.WriteString(input, "héllo🙂\r")
	require.NoError(t, err)
	command := <-emitted
	cancel()

	// Assert input remains exact and the program joins cancellation and terminal cleanup.
	require.Equal(t, presentation.CommandSubmit, command.Kind)
	require.Equal(t, "héllo🙂", command.Text.OrEmpty())
	require.ErrorIs(t, <-done, context.Canceled)
}

// TestRuntimeUsesSuppliedTerminalIO verifies local Quit and supplied terminal resources.
func TestRuntimeUsesSuppliedTerminalIO(t *testing.T) {
	t.Parallel()
	// Arrange the real runtime with Host close expectations for a local Quit.
	controller := gomock.NewController(t)
	host := presentation.NewMockHost(controller)
	host.EXPECT().Send(gomock.Any(), gomock.Any(), "").DoAndReturn(
		func(_ string, command presentation.Command, _ string) error {
			require.Equal(t, presentation.CommandQuit, command.Kind)
			return nil
		},
	)
	host.EXPECT().CloseConnection(gomock.Any()).Return(nil)
	host.EXPECT().StopDispatch()
	application, input, output := initializedRuntime(t, controller, host)
	done := make(chan error, 1)
	go func() { done <- application.Run(t.Context()) }()
	<-output.written

	// Act by writing the documented Quit control key to supplied input.
	_, err := input.Write([]byte{17})
	require.NoError(t, err)

	// Assert local shutdown joins and supplied output contains the rendered snapshot.
	require.NoError(t, <-done)
	require.NotEmpty(t, output.String())
}

// initializedRuntime binds production input and state owners to isolated Host and terminal resources.
func initializedRuntime(
	t *testing.T, controller *gomock.Controller, host presentation.Host,
) (*presentation.Service, *io.PipeWriter, *notifyingWriter) {
	t.Helper()
	input, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	output := newNotifyingWriter()
	device := NewMockDevice(controller)
	files := NewMockFiles(controller)
	device.EXPECT().Open().Return(files, nil)
	files.EXPECT().Input().Return(input)
	files.EXPECT().Output().Return(output)
	files.EXPECT().Close().DoAndReturn(input.Close)
	source := NewMockNotificationSource(controller)
	source.EXPECT().ReceiveNotification(gomock.Any()).DoAndReturn(
		func(ctx context.Context) (*uisdk.Notification, error) { <-ctx.Done(); return nil, context.Cause(ctx) },
	)
	runtime := NewRuntime(device)
	application := presentation.New(host, runtime, runtime)
	runtime.BindInput(tuiinput.New(application), plugininput.New(application), source)
	work, err := application.PrepareInitialize(plugininput.Initialization{
		Availability: plugininput.AvailabilityIdle, Startup: nil, Models: nil,
		Selection: plugininput.ModelSelection{}, Session: plugininput.SessionInfo{},
	})
	require.NoError(t, err)
	require.NoError(t, work.Run(t.Context()))
	work.Release()
	return application, writer, output
}
