//go:build integration

package plugin

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestMalformedInitializationIsRejectedBeforeAcceptance verifies bounded initialization preparation.
func TestMalformedInitializationIsRejectedBeforeAcceptance(t *testing.T) {
	t.Parallel()

	// Arrange a real SDK stream and an initialization without required payload fields.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	terminal.EXPECT().Open().AnyTimes().Return(nil, errors.New("stop after valid initialization"))
	client := uisdk.TestClient(t, New(terminal, NewMockProgramFactory(mockController)))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	invalid := new(uiv1.HostRequest)
	invalid.SetInitialize(new(uiv1.Initialization))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("invalid"), Request: invalid, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))

	// Act by receiving rejection and then sending a valid initialization on the open stream.
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

	// Assert rejection and accepted failure retain exact categories and complete source text.
	assert.Equal(t, "invalid", rejected.GetOperationId())
	assert.Equal(t, "INVALID_ARGUMENT", rejected.GetEvent().GetRejected().GetCode())
	assert.Equal(t, "map TUI initialization: selected UI ID is required",
		rejected.GetEvent().GetRejected().GetMessage())
	assert.Equal(t, "valid", accepted.GetOperationId())
	assert.NotNil(t, accepted.GetEvent().GetAccepted())
	assert.Equal(t, "valid", running.GetOperationId())
	assert.NotNil(t, running.GetEvent().GetRunning())
	assert.Equal(t, "valid", failed.GetOperationId())
	assert.Equal(t, "INTERNAL", failed.GetEvent().GetFailed().GetCode())
	assert.Equal(t, "open TUI terminal: stop after valid initialization",
		failed.GetEvent().GetFailed().GetMessage())
	require.NoError(t, stream.CloseSend())
}

// TestControllerRunPreservesProgramAndTerminalCloseCauses verifies consumed-resource cleanup errors.
func TestControllerRunPreservesProgramAndTerminalCloseCauses(t *testing.T) {
	t.Parallel()

	// Arrange a prepared terminal whose program and close both fail.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	programs := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	runCause := errors.New("start presentation program failed")
	closeCause := errors.New("restore controlling terminal failed")
	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(new(bytes.Buffer))
	programs.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(program)
	program.EXPECT().Run().Return(runCause)
	session.EXPECT().Close().Return(closeCause)
	client := uisdk.TestClient(t, New(terminal, programs))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	initialize := new(uiv1.HostRequest)
	initialize.SetInitialize(validInitialization())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: initialize, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err = stream.Recv()
		require.NoError(t, err)
	}

	// Act by acknowledging controller-requested close and receiving the server failure.
	closeResponse, err := stream.Recv()
	require.NoError(t, err)
	assert.NotNil(t, closeResponse.GetClose())
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()

	// Assert both source causes cross the SDK boundary with complete text.
	require.Error(t, err)
	require.ErrorContains(t, err, runCause.Error())
	require.ErrorContains(t, err, closeCause.Error())
}

// TestControllerOwnsTerminalAcrossSDKLifecycle verifies TUI process resource ownership and startup order.
func TestControllerOwnsTerminalAcrossSDKLifecycle(t *testing.T) {
	t.Parallel()

	// Arrange terminal and program resources owned by one initialized controller.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	programs := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	input := bytes.NewBuffer(nil)
	output := new(bytes.Buffer)
	runDone := make(chan struct{})
	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(input)
	session.EXPECT().Output().Return(output)
	programs.EXPECT().New(gomock.Any(), input, output, gomock.Any()).DoAndReturn(
		func(initial presentationdomain.Event, _ io.Reader, _ io.Writer, _ Emit) Program {
			assert.Equal(t, presentationdomain.EventInitialization, initial.Kind)
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)
	client := uisdk.TestClient(t, New(terminal, programs))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	initialize := new(uiv1.HostRequest)
	initialize.SetInitialize(validInitialization())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: initialize, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))

	// Act by receiving startup and then closing the owning connection.
	accepted, err := stream.Recv()
	require.NoError(t, err)
	running, err := stream.Recv()
	require.NoError(t, err)
	completed, err := stream.Recv()
	require.NoError(t, err)
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()

	// Assert lifecycle order and joined terminal cleanup.
	assert.NotNil(t, accepted.GetEvent().GetAccepted())
	assert.NotNil(t, running.GetEvent().GetRunning())
	assert.NotNil(t, completed.GetEvent().GetCompleted().GetInitialized())
	assert.ErrorIs(t, err, io.EOF)
}
