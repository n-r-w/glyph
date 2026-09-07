//go:build integration

package host

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestMalformedInitializationIsRejectedBeforeAcceptance preserves bounded input validation and later admission.
func TestMalformedInitializationIsRejectedBeforeAcceptance(t *testing.T) {
	t.Parallel()
	// Arrange the real input/application/SDK path with a terminal-open failure after valid input.
	controller := gomock.NewController(t)
	runtime := presentation.NewMockRuntime(controller)
	runtime.EXPECT().Open().Return(errors.New("stop after valid initialization"))
	adapter := sdkApplication(controller, runtime)
	client := uisdk.TestClient(t, adapter)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	invalid := new(uiv1.HostRequest)
	invalid.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("invalid"), Request: invalid, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	// Act by receiving rejection and then submitting complete initialization.
	rejected, err := stream.Recv()
	require.NoError(t, err)
	valid := new(uiv1.HostRequest)
	valid.SetInitialize(validInitialization())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("valid"), Request: valid, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	accepted, err := stream.Recv()
	require.NoError(t, err)
	running, err := stream.Recv()
	require.NoError(t, err)
	failed, err := stream.Recv()
	require.NoError(t, err)
	// Assert rejection does not consume admission and initialization effects retain the complete cause.
	require.Equal(t, "INVALID_ARGUMENT", rejected.GetEvent().GetRejected().GetCode())
	require.Equal(
		t,
		"map TUI initialization: selected UI ID is required",
		rejected.GetEvent().GetRejected().GetMessage(),
	)
	require.NotNil(t, accepted.GetEvent().GetAccepted())
	require.NotNil(t, running.GetEvent().GetRunning())
	require.Equal(t, "open TUI terminal: stop after valid initialization", failed.GetEvent().GetFailed().GetMessage())
	require.NoError(t, stream.CloseSend())
}

// TestApplicationPreservesProgramAndTerminalCloseCauses retains both errors across the SDK lifecycle.
func TestApplicationPreservesProgramAndTerminalCloseCauses(t *testing.T) {
	t.Parallel()
	// Arrange a local program failure and a later terminal cleanup failure.
	controller := gomock.NewController(t)
	runtime := presentation.NewMockRuntime(controller)
	runtime.EXPECT().Open().Return(nil)
	runtime.EXPECT().Run(gomock.Any()).Return(presentation.RuntimeResult{
		ProgramExited: true, Err: errors.New("start presentation program failed"),
	})
	runtime.EXPECT().Close().Return(errors.New("restore controlling terminal failed"))
	client := uisdk.TestClient(t, sdkApplication(controller, runtime))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendInitialization(t, stream)
	// Act by acknowledging the application's requested connection close.
	closed, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, closed.GetClose())
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	// Assert complete program and cleanup diagnostics cross the SDK boundary.
	require.ErrorContains(t, err, "start presentation program failed")
	require.ErrorContains(t, err, "close TUI terminal: restore controlling terminal failed")
}

// TestApplicationOwnsTerminalAcrossSDKLifecycle preserves initialization and connection-owned cleanup.
func TestApplicationOwnsTerminalAcrossSDKLifecycle(t *testing.T) {
	t.Parallel()
	// Arrange initialized resources held until the SDK connection ends.
	controller := gomock.NewController(t)
	runtime := presentation.NewMockRuntime(controller)
	runtime.EXPECT().Open().Return(nil)
	runtime.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) presentation.RuntimeResult {
		<-ctx.Done()
		return presentation.RuntimeResult{ProgramExited: false, Err: nil}
	})
	runtime.EXPECT().Close().Return(nil)
	client := uisdk.TestClient(t, sdkApplication(controller, runtime))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	// Act by completing initialization and then closing the owning connection.
	sendInitialization(t, stream)
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	// Assert ordinary stream closure joins application cleanup.
	require.ErrorIs(t, err, io.EOF)
}

// TestSequentialSDKConnectionsOwnIndependentRuntimeLifetimes preserves repeated use of the SDK service instance.
func TestSequentialSDKConnectionsOwnIndependentRuntimeLifetimes(t *testing.T) {
	t.Parallel()
	// Arrange one SDK service instance with two sequential runtime lifetimes.
	controller := gomock.NewController(t)
	runtime := presentation.NewMockRuntime(controller)
	runtime.EXPECT().Open().Times(2).Return(nil)
	runtime.EXPECT().Run(gomock.Any()).Times(2).DoAndReturn(func(ctx context.Context) presentation.RuntimeResult {
		<-ctx.Done()
		return presentation.RuntimeResult{ProgramExited: false, Err: nil}
	})
	runtime.EXPECT().Close().Times(2).Return(nil)
	client := uisdk.TestClient(t, sdkApplication(controller, runtime))

	// Act by opening, initializing, and closing each SDK stream before the next one starts.
	for range 2 {
		stream, err := client.Open(t.Context())
		require.NoError(t, err)
		sendInitialization(t, stream)
		require.NoError(t, stream.CloseSend())
		_, err = stream.Recv()
		// Assert each connection joins its independent initialized runtime and closes normally.
		require.ErrorIs(t, err, io.EOF)
	}
}

// TestDispatchUsesActiveRunCancellation preserves the initialized SDK context before terminal cleanup ends.
func TestDispatchUsesActiveRunCancellation(t *testing.T) {
	t.Parallel()
	// Arrange the real adapter with a runtime that inspects canceled dispatch before returning.
	controller := gomock.NewController(t)
	runtime := presentation.NewMockRuntime(controller)
	runtime.EXPECT().Open().Return(nil)
	adapter := sdkApplication(controller, runtime)
	dispatchResult := make(chan error, 1)
	cleanupDone := make(chan struct{})
	runtime.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) presentation.RuntimeResult {
		<-ctx.Done()
		dispatchResult <- adapter.Send("late", commandFixture(presentation.CommandSubmit, mo.Some("late")), "")
		return presentation.RuntimeResult{ProgramExited: false, Err: context.Cause(ctx)}
	})
	runtime.EXPECT().Close().DoAndReturn(func() error { close(cleanupDone); return nil })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := uisdk.TestClient(t, adapter)
	stream, err := client.Open(ctx)
	require.NoError(t, err)
	sendInitialization(t, stream)
	// Act by canceling the SDK stream and waiting for application cleanup.
	cancel()
	<-cleanupDone
	// Assert cancellation reaches the adapter before resources finish closing.
	require.ErrorIs(t, <-dispatchResult, context.Canceled)
	require.Error(t, adapter.Send("after", commandFixture(presentation.CommandSubmit, mo.Some("after")), ""))
}

// sdkApplication binds the actual input/application/SDK owners with an isolated runtime.
func sdkApplication(controller *gomock.Controller, runtime presentation.Runtime) *Service {
	adapter := New()
	display := presentation.NewMockDisplay(controller)
	display.EXPECT().Publish(gomock.Any()).AnyTimes()
	application := presentation.New(adapter, display, runtime)
	adapter.BindInput(plugininput.New(application))
	return adapter
}

// sendInitialization checks the SDK's accepted, running, and completed startup order.
func sendInitialization(t *testing.T, stream uiv1.UIService_OpenClient) {
	t.Helper()
	request := new(uiv1.HostRequest)
	request.SetInitialize(validInitialization())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	accepted, err := stream.Recv()
	require.NoError(t, err)
	running, err := stream.Recv()
	require.NoError(t, err)
	completed, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, accepted.GetEvent().GetAccepted())
	require.NotNil(t, running.GetEvent().GetRunning())
	require.NotNil(t, completed.GetEvent().GetCompleted().GetInitialized())
}
