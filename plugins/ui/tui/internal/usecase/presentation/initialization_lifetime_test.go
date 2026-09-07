//go:build !integration

package presentation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestUnconsumedInitializationClosesPreparedTerminal verifies cleanup before the event loop takes ownership.
func TestUnconsumedInitializationClosesPreparedTerminal(t *testing.T) {
	t.Parallel()

	// Arrange initialized terminal resources and a cleanup failure.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	display := NewMockDisplay(controller)
	runtime.EXPECT().Open().Return(nil)
	display.EXPECT().Publish(gomock.Any())
	cause := errors.New("restore terminal failed")
	runtime.EXPECT().Close().Return(cause)
	service := New(NewMockHost(controller), display, runtime)
	work, err := service.PrepareInitialize(plugininput.Initialization{})
	require.NoError(t, err)
	require.NoError(t, work.Run(t.Context()))
	work.Release()

	// Act by closing an initialization that did not enter Run.
	err = service.Close()

	// Assert the complete cause remains detectable and cleanup is not repeated.
	require.ErrorIs(t, err, cause)
	require.EqualError(t, err, "close prepared TUI terminal: restore terminal failed")
	require.NoError(t, service.Close())
}

// TestCompletedRuntimeCanInitializeAgain preserves sequential SDK connection resource ownership.
func TestCompletedRuntimeCanInitializeAgain(t *testing.T) {
	t.Parallel()
	// Arrange two SDK lifecycles served by the same application instance.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	display := NewMockDisplay(controller)
	host := NewMockHost(controller)
	runtime.EXPECT().Open().Times(2).Return(nil)
	runtime.EXPECT().Run(gomock.Any()).Times(2).Return(RuntimeResult{ProgramExited: false, Err: nil})
	runtime.EXPECT().Close().Times(2).Return(nil)
	host.EXPECT().StopDispatch().Times(2)
	display.EXPECT().Publish(gomock.Any()).Times(2)
	service := New(host, display, runtime)

	// Act by completing and closing each connection before admitting the next one.
	for range 2 {
		work, err := service.PrepareInitialize(plugininput.Initialization{})
		require.NoError(t, err)
		require.NoError(t, work.Run(t.Context()))
		work.Release()
		require.NoError(t, service.Run(t.Context()))
		require.NoError(t, service.Close())
	}

	// Assert closed connection state does not retain terminal ownership.
	require.False(t, service.initialized)
	require.False(t, service.running)
}

// TestInitializationTerminalOpenFailureFailsOperation preserves the terminal cause at application initialization.
func TestInitializationTerminalOpenFailureFailsOperation(t *testing.T) {
	t.Parallel()

	// Arrange a terminal runtime that cannot acquire the controlling terminal.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	cause := errors.New("controlling terminal unavailable")
	runtime.EXPECT().Open().Return(cause)
	service := New(NewMockHost(controller), NewMockDisplay(controller), runtime)
	work, err := service.PrepareInitialize(plugininput.Initialization{})
	require.NoError(t, err)

	// Act through accepted initialization work.
	err = work.Run(t.Context())
	work.Release()

	// Assert failed opening needs no terminal cleanup and retains its source error.
	require.ErrorIs(t, err, cause)
	require.EqualError(t, err, "open TUI terminal: controlling terminal unavailable")
	require.NoError(t, service.Close())
}
