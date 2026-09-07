//go:build integration

package host

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestSDKMapsOperationProgressAndCompletion preserves correlation through real input, application, and SDK owners.
func TestSDKMapsOperationProgressAndCompletion(t *testing.T) {
	t.Parallel()
	// Arrange a serialized runtime fixture around the real application and SDK adapter.
	controller := gomock.NewController(t)
	runtime := presentation.NewMockRuntime(controller)
	display := presentation.NewMockDisplay(controller)
	adapter := New()
	application := presentation.New(adapter, display, runtime)
	input := plugininput.New(application)
	adapter.BindInput(input)
	var snapshot presentation.Snapshot
	display.EXPECT().Publish(gomock.Any()).AnyTimes().Do(func(value presentation.Snapshot) { snapshot = value })
	runtime.EXPECT().Open().Return(nil)
	runtime.EXPECT().Close().Return(nil)
	projected := make(chan presentation.Snapshot, 1)
	failure := make(chan error, 1)
	runtime.EXPECT().Run(gomock.Any()).DoAndReturn(func(ctx context.Context) presentation.RuntimeResult {
		// All state transitions stay on this one runtime execution point.
		application.Key(tuiinput.Key{Code: 0, Text: "hello", Mod: 0})
		work := application.Key(tuiinput.Key{Code: tuiinput.KeyEnter, Text: "", Mod: 0})
		application.Complete(work.Execute())
		for range 2 {
			notification, err := adapter.ReceiveNotification(ctx)
			if err != nil {
				failure <- err
				return presentation.RuntimeResult{ProgramExited: false, Err: err}
			}
			if err := input.Notify(notification); err != nil {
				failure <- err
				return presentation.RuntimeResult{ProgramExited: false, Err: err}
			}
		}
		projected <- snapshot
		<-ctx.Done()
		return presentation.RuntimeResult{ProgramExited: false, Err: nil}
	})
	client := uisdk.TestClient(t, adapter)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	sendInitialization(t, stream)

	// Act by accepting the encoded Submit and delivering ordered progress and completion.
	request, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "hello", request.GetRequest().GetSubmit().GetText())
	id := request.GetOperationId()
	accepted := new(uiv1.HostEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	sendHostEvent(t, stream, id, accepted)
	running := new(uiv1.HostEvent)
	running.SetRunning(new(operationv1.Running))
	sendHostEvent(t, stream, id, running)
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: new("run"), Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil, ModelResponse: nil,
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build())
	progressEvent := new(uiv1.HostEvent)
	progressEvent.SetProgress(progress)
	sendHostEvent(t, stream, id, progressEvent)
	completed := new(uiv1.HostCompleted)
	completed.SetSubmit(new(uiv1.SubmitCompleted))
	completedEvent := new(uiv1.HostEvent)
	completedEvent.SetCompleted(completed)
	sendHostEvent(t, stream, id, completedEvent)

	// Assert both correlated inputs reach one coherent projection before connection cleanup.
	select {
	case final := <-projected:
		require.Len(t, final.Body.Transcript, 1)
		require.Equal(t, "hello", final.Body.Transcript[0].Text.OrEmpty())
		require.Empty(t, final.Input)
	case err := <-failure:
		require.NoError(t, err)
	}
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF)
}

// sendHostEvent delivers one correlated Host lifecycle event on the real SDK stream.
func sendHostEvent(t *testing.T, stream uiv1.UIService_OpenClient, id string, event *uiv1.HostEvent) {
	t.Helper()
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		OperationId: new(id), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
	}.Build()))
}
