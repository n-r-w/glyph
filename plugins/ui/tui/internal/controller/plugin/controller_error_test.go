package plugin

import (
	"bytes"

	"testing"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestOpenRejectsMalformedLifecycleAsInvalidArgument verifies public stream mapping keeps its protocol error code.
func TestOpenRejectsMalformedLifecycleAsInvalidArgument(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	runDone := make(chan struct{})
	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(&bytes.Buffer{})
	factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(program)
	program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
	program.EXPECT().Send(gomock.Any()).AnyTimes()
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	malformed := uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: new("run"), Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil,
		IsError: nil, Outcome: nil, ErrorMessage: nil, Availability: nil,
		ModelContent: nil,
		ModelResponse: uiv1.ModelResponse_builder{
			Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
			ResponseId: nil, Usage: nil, Diagnostics: nil, Content: nil, ResponseModel: nil,
		}.Build(),
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build()
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Initialization:        nil,
		Lifecycle:             malformed,
		Authorization:         nil,
		Information:           nil,
		Error:                 nil,
		ModelSelectionChanged: nil,
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil, SessionTree: nil, SessionTreeNavigation: nil,
		SessionTreeFailed: nil, SessionForked: nil, SessionCloned: nil, EntryLabelSet: nil,
	}.Build()))
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
