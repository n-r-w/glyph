//go:build !integration

package plugin

import (
	"bytes"
	"io"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestControllerMapsOperationProgressAndCompletion verifies retained rendering over the operation stream.
func TestControllerMapsOperationProgressAndCompletion(t *testing.T) {
	t.Parallel()

	// Arrange an initialized controller with one running presentation program.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	programs := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	emitter := make(chan Emit, 1)
	runDone := make(chan struct{})
	programStarted := make(chan struct{})
	sent := make(chan struct{}, 2)
	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(new(bytes.Buffer))
	programs.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ presentationdomain.Event, _ io.Reader, _ io.Writer, emit Emit) Program {
			emitter <- emit
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error {
		close(programStarted)
		<-runDone
		return nil
	})
	program.EXPECT().Send(gomock.Any()).Times(2).Do(func(presentationdomain.Event) { sent <- struct{}{} })
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)
	client := uisdk.TestClient(t, New(terminal, programs))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendInitialization(t, stream)
	emit := <-emitter
	<-programStarted

	// Act by starting one submit operation and delivering progress and completion.
	emitDone := make(chan error, 1)
	go func() { emitDone <- emit(commandFixture(presentationdomain.CommandSubmit, mo.Some("hello"))) }()
	request, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, "hello", request.GetRequest().GetSubmit().GetText())
	operationID := request.GetOperationId()
	sendHostEvent(t, stream, operationID, acceptedHostEvent())
	sendHostEvent(t, stream, operationID, runningHostEvent())
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: new("run"), Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil, ModelResponse: nil,
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build())
	progressEvent := new(uiv1.HostEvent)
	progressEvent.SetProgress(progress)
	sendHostEvent(t, stream, operationID, progressEvent)
	completed := new(uiv1.HostCompleted)
	completed.SetSubmit(new(uiv1.SubmitCompleted))
	completedEvent := new(uiv1.HostEvent)
	completedEvent.SetCompleted(completed)
	sendHostEvent(t, stream, operationID, completedEvent)

	// Assert the command joins after both retained presentation events are delivered.
	require.NoError(t, <-emitDone)
	<-sent
	<-sent
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// TestSnapshotRequestDoesNotReplaceForegroundCancelTarget verifies concurrent foreground ownership.
func TestSnapshotRequestDoesNotReplaceForegroundCancelTarget(t *testing.T) {
	t.Parallel()
	// Arrange Submit as foreground work with navigation and snapshot commands running concurrently.

	// Act by canceling through the controller while all operations are active.
	// Assert cancellation targets Submit rather than either concurrent command.
	assertForegroundCancelTarget(t,
		commandFixture(presentationdomain.CommandSubmit, mo.Some("hello")),
		[]presentationdomain.Command{
			navigateCommandFixture(),
			commandFixture(presentationdomain.CommandGetSessionInfo, mo.None[string]()),
		},
	)
}

// TestNavigateSessionTreeRemainsForegroundCancelTarget verifies blocked navigation cancellation ownership.
func TestNavigateSessionTreeRemainsForegroundCancelTarget(t *testing.T) {
	t.Parallel()
	// Arrange navigation as foreground work with a session-information snapshot running concurrently.

	// Act by canceling through the controller while both operations are active.
	// Assert cancellation targets the blocked navigation operation.
	assertForegroundCancelTarget(t,
		navigateCommandFixture(),
		[]presentationdomain.Command{commandFixture(
			presentationdomain.CommandGetSessionInfo, mo.None[string](),
		)},
	)
}

// assertForegroundCancelTarget runs one real SDK foreground-cancellation scenario.
func assertForegroundCancelTarget(
	t *testing.T,
	foreground presentationdomain.Command,
	concurrent []presentationdomain.Command,
) {
	t.Helper()

	// Arrange one running controller and capture its presentation emitter.
	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	programs := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	emitter := make(chan Emit, 1)
	runDone := make(chan struct{})
	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(new(bytes.Buffer))
	programs.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ presentationdomain.Event, _ io.Reader, _ io.Writer, emit Emit) Program {
			emitter <- emit
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
	program.EXPECT().Send(gomock.Any()).AnyTimes()
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)
	client := uisdk.TestClient(t, New(terminal, programs))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendInitialization(t, stream)
	emit := <-emitter

	// Act by starting the foreground request, concurrent work, and Stop.
	require.NoError(t, emit(foreground))
	foregroundRequest, err := stream.Recv()
	require.NoError(t, err)
	for _, command := range concurrent {
		require.NoError(t, emit(command))
		_, err = stream.Recv()
		require.NoError(t, err)
	}
	require.NoError(t, emit(commandFixture(presentationdomain.CommandStop, mo.None[string]())))
	cancellation, err := stream.Recv()
	require.NoError(t, err)

	// Assert Stop still targets the first active foreground operation.
	assert.Equal(t, foregroundRequest.GetOperationId(), cancellation.GetRequest().GetCancel().GetTargetOperationId())
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// navigateCommandFixture creates one valid navigation foreground command.
func navigateCommandFixture() presentationdomain.Command {
	command := commandFixture(presentationdomain.CommandNavigateSessionTree, mo.None[string]())
	command.TreeCommand = mo.Some(presentationdomain.TreeCommand{
		TargetEntryID: mo.Some("target"), SummaryMode: presentationdomain.SummaryModeSummarize,
		CustomFocus: mo.None[string](), Label: mo.None[string](),
	})
	return command
}

// sendInitialization completes the SDK-owned initialization lifecycle.
func sendInitialization(t *testing.T, stream uiv1.UIService_OpenClient) {
	t.Helper()
	request := new(uiv1.HostRequest)
	request.SetInitialize(validInitialization())
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new("initialize"), Request: request, Event: nil, ConnectionEvent: nil, Close: nil,
	}.Build()))
	for range 3 {
		_, err := stream.Recv()
		require.NoError(t, err)
	}
}

// sendHostEvent delivers one correlated Host lifecycle event.
func sendHostEvent(t *testing.T, stream uiv1.UIService_OpenClient, operationID string, event *uiv1.HostEvent) {
	t.Helper()
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(operationID), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
	}.Build()))
}

// acceptedHostEvent creates one accepted lifecycle event.
func acceptedHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return event
}

// runningHostEvent creates one running lifecycle event.
func runningHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return event
}
