//nolint:exhaustruct // Tests set only fields used by the active contract or presentation event kind.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestGetCapabilitiesIsPure verifies discovery does not open the controlling terminal.
func TestGetCapabilitiesIsPure(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	client := uisdk.TestClient(t, New(
		NewMockTerminal(mockController),
		NewMockProgramFactory(mockController),
	))

	capabilities, err := client.GetCapabilities(t.Context(), &uiv1.GetCapabilitiesRequest{})
	require.NoError(t, err)
	assert.True(t, capabilities.GetControlsTerminal())
}

// TestOpenRejectsNonInitializationBeforeOpeningTerminal verifies initialization-first enforcement.
func TestOpenRejectsNonInitializationBeforeOpeningTerminal(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	client := uisdk.TestClient(t, New(
		NewMockTerminal(mockController),
		NewMockProgramFactory(mockController),
	))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{Information: uiv1.Information_builder{Text: new("too early")}.Build()}.Build()))
	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestOpenStartsAfterInitializationDeliversFramesAndClosesNormally verifies ordered startup and cleanup.
func TestOpenStartsAfterInitializationDeliversFramesAndClosesNormally(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	input := bytes.NewBufferString("")
	output := &bytes.Buffer{}
	runDone := make(chan struct{})
	started := make(chan struct{})

	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(input)
	session.EXPECT().Output().Return(output)
	factory.EXPECT().New(gomock.Any(), input, output, gomock.Any()).DoAndReturn(
		func(initial presentationdomain.Event, _ io.Reader, _ io.Writer, _ Emit) Program {
			assert.Equal(t, presentationdomain.Event{
				Kind:         presentationdomain.EventInitialization,
				Startup:      []presentationdomain.Line{{Kind: presentationdomain.LineInformation, Text: "ready"}},
				Availability: presentationdomain.AvailabilityIdle,
				Extensions:   []presentationdomain.Extension{{ID: "tools", Tools: []string{"read"}}},
			}, initial)
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error {
		close(started)
		<-runDone
		return nil
	})
	program.EXPECT().Send(presentationdomain.Event{Kind: presentationdomain.EventInformation, Text: "information"})
	program.EXPECT().Send(gomock.Any()).Do(func(event presentationdomain.Event) {
		assert.NotEqual(t, "hidden reasoning", event.Text)
		assert.Equal(t, presentationdomain.Event{
			Kind: presentationdomain.EventModelDelta, Position: 2,
			ModelContentKind: presentationdomain.ModelContentText, Text: "delta",
		}, event)
	}).AnyTimes()
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	<-started
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{Information: uiv1.Information_builder{Text: new("information")}.Build()}.Build()))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{Lifecycle: uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING),
			Position: new(int32(1)), Text: new("hidden reasoning"),
		}.Build(),
	}.Build()}.Build()))
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{Lifecycle: uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			Position: new(int32(2)), Text: new("delta"),
		}.Build(),
	}.Build()}.Build()))
	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// TestSemanticLifecycleSequenceUsesContractMapping verifies shared lifecycle data through the standard consumer mapping.
func TestSemanticLifecycleSequenceUsesContractMapping(t *testing.T) {
	t.Parallel()
	payload, err := os.ReadFile(filepath.Join(repositoryRoot(t), "testdata", "semantic-ui-lifecycle.json"))
	require.NoError(t, err)
	var sequence []semanticFrame
	require.NoError(t, json.Unmarshal(payload, &sequence))

	service := presentationusecase.New()
	initial, err := mapRequest(initializationRequest())
	require.NoError(t, err)
	state := service.Apply(presentationdomain.State{}, initial)
	for _, frame := range sequence {
		request := lifecycleRequest(frame)
		event, mapErr := mapRequest(request)
		require.NoError(t, mapErr)
		state = service.Apply(state, event)
	}

	assert.True(t, state.Settled)
	assert.Equal(t, presentationdomain.AvailabilityIdle, state.Availability)
	assert.Contains(t, state.Transcript, presentationdomain.Line{Kind: presentationdomain.LineModel, Text: "Request complete."})
	assert.Contains(t, state.Transcript, presentationdomain.Line{Kind: presentationdomain.LineToolDone, ToolName: "bash", Status: "completed"})
	assert.Contains(t, state.Transcript, presentationdomain.Line{Kind: presentationdomain.LineToolDone, ToolName: "bash", Text: "{\"stdout\":\"tool-ok\",\"stderr\":\"\",\"exitCode\":0}"})
	assert.Empty(t, state.ActiveTools)
}

// semanticFrame describes the stable lifecycle fields shared by both fixtures.
type semanticFrame struct {
	Type         string `json:"type"`
	ToolName     string `json:"tool_name"`
	ToolStatus   string `json:"tool_status"`
	Text         string `json:"text"`
	ModelText    string `json:"model_text"`
	Outcome      string `json:"outcome"`
	Availability string `json:"availability"`
}

// lifecycleRequest builds a public protobuf frame for the real controller mapper.
func lifecycleRequest(frame semanticFrame) *uiv1.OpenRequest {
	typeValue := uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED
	switch frame.Type {
	case "agent_start":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START
	case "message_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END
	case "tool_execution_start":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START
	case "tool_execution_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END
	case "tool_result":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT
	case "agent_settled":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED
	case "agent_end":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END
	case "availability":
		typeValue = uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED
	}
	lifecycle := uiv1.LifecycleEvent_builder{Type: new(typeValue), ToolName: new(frame.ToolName), Text: new(frame.Text), Outcome: new(frame.Outcome)}.Build()
	if frame.Type == "message_end" && frame.ModelText != "" {
		lifecycle.SetModelResponse(uiv1.ModelResponse_builder{Content: []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: new(frame.ModelText)}.Build()}}.Build())
	}
	if frame.Type == "tool_execution_end" {
		lifecycle.SetIsError(frame.ToolStatus != "ok")
	}
	if frame.Type == "availability" {
		lifecycle.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
	}
	return uiv1.OpenRequest_builder{Lifecycle: lifecycle}.Build()
}

// repositoryRoot resolves shared testdata from the source file location.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", ".."))
}

// TestMapLifecyclePreservesRefusalKind verifies refusal deltas stay distinct from ordinary model text.
func TestMapLifecyclePreservesRefusalKind(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL),
			Position: new(int32(3)), Text: new("cannot help"),
		}.Build(),
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventModelDelta, event.Kind)
	assert.Equal(t, presentationdomain.ModelContentRefusal, event.ModelContentKind)
	assert.Equal(t, "cannot help", event.Text)
}

// TestMapLifecyclePreservesFinalizedRefusalBlocks verifies mixed visible content reaches presentation state.
func TestMapLifecyclePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
		ModelResponse: uiv1.ModelResponse_builder{Content: []*uiv1.ModelResponseContent{
			uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING), Text: new("hidden")}.Build(),
			uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: new("answer")}.Build(),
			uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL), Text: new("cannot help")}.Build(),
		}}.Build(),
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.ModelResponseContent{
		{Kind: presentationdomain.ModelContentText, Text: "answer"},
		{Kind: presentationdomain.ModelContentRefusal, Text: "cannot help"},
	}, event.ModelResponseContent)
}

// TestOpenMapsCommandsThroughOneStreamSender verifies UI commands use one serialized sender.
func TestOpenMapsCommandsThroughOneStreamSender(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	runDone := make(chan struct{})
	emitter := make(chan Emit, 1)

	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(&bytes.Buffer{})
	factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ presentationdomain.Event, _ io.Reader, _ io.Writer, emit Emit) Program {
			emitter <- emit
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	emit := <-emitter

	commands := []presentationdomain.Command{
		{Kind: presentationdomain.CommandSubmit, Text: "hello"},
		{Kind: presentationdomain.CommandStop},
		{Kind: presentationdomain.CommandRetryAuthentication},
		{Kind: presentationdomain.CommandQuit},
	}
	for _, command := range commands {
		emitResult := make(chan error, 1)
		go func() { emitResult <- emit(command) }()
		response, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		require.NoError(t, <-emitResult)
		switch command.Kind {
		case presentationdomain.CommandSubmit:
			assert.Equal(t, "hello", response.GetSubmit().GetText())
		case presentationdomain.CommandStop:
			assert.NotNil(t, response.GetStop())
		case presentationdomain.CommandRetryAuthentication:
			assert.NotNil(t, response.GetRetryAuthentication())
		case presentationdomain.CommandQuit:
			assert.NotNil(t, response.GetQuit())
		case presentationdomain.CommandUnspecified:
			t.Fatal("unexpected unspecified command")
		}
	}

	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// TestOpenReturnsProgramErrorAndClosesTerminal verifies program failures restore terminal ownership.
func TestOpenReturnsProgramErrorAndClosesTerminal(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)

	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(&bytes.Buffer{})
	factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(program)
	program.EXPECT().Run().Return(errors.New("program failed"))
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestMapInitializationPreservesWarningAndExtensionPath verifies startup diagnostics reach presentation.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()

	event, err := mapInitialization(uiv1.Initialization_builder{
		SelectedUiId: new("glyph-tui"),
		StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
			Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING),
			Text:     new("excluded optional UI"),
		}.Build()},
		Extensions: []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{
			PluginId: new("glyph-tools"), Tools: []string{"read"}, Path: new("/plugins/glyph-tools"),
		}.Build()},
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.Line{{
		Kind: presentationdomain.LineWarning, ToolName: "", Status: "", Text: "excluded optional UI",
	}}, event.Startup)
	assert.Equal(t, []presentationdomain.Extension{{
		ID: "glyph-tools", Path: "/plugins/glyph-tools", Tools: []string{"read"},
	}}, event.Extensions)
}

// TestMapRequestRejectsUnknownLifecycleAndMapsSafeError verifies malformed frames and safe errors.
func TestMapRequestRejectsUnknownLifecycleAndMapsSafeError(t *testing.T) {
	t.Parallel()

	_, err := mapRequest(uiv1.OpenRequest_builder{Lifecycle: &uiv1.LifecycleEvent{}}.Build())
	require.Error(t, err)

	event, err := mapRequest(uiv1.OpenRequest_builder{Error: uiv1.Error_builder{Text: new("safe error")}.Build()}.Build())
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{Kind: presentationdomain.EventError, Text: "safe error"}, event)
}

// TestHostMessageEndFinalizesTextStreamAtDifferentPosition verifies complete terminal model projection.
func TestHostMessageEndFinalizesTextStreamAtDifferentPosition(t *testing.T) {
	t.Parallel()

	projection := presentationusecase.New()
	state := presentationdomain.State{}
	frames := []*uiv1.LifecycleEvent{
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START),
		}.Build(),
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type: new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA), Position: new(int32(1)), Text: new("complete answer"),
			}.Build(),
		}.Build(),
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
			ModelResponse: uiv1.ModelResponse_builder{
				Text: new("complete answer"), Provider: new("openai-codex"), Model: new("gpt-test"), ResponseId: new("resp-1"),
				Usage:       uiv1.ModelUsage_builder{InputTokens: new(int64(3)), OutputTokens: new(int64(2)), TotalTokens: new(int64(5))}.Build(),
				Diagnostics: []*uiv1.ModelDiagnostic{uiv1.ModelDiagnostic_builder{Code: new("recovered_output"), Message: new("hidden diagnostic")}.Build()},
				Content: []*uiv1.ModelResponseContent{
					uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING), Text: new("hidden reasoning")}.Build(),
					uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: new("complete answer")}.Build(),
				},
			}.Build(),
		}.Build(),
	}
	for _, lifecycle := range frames {
		event, err := mapRequest(uiv1.OpenRequest_builder{Lifecycle: proto.ValueOrDefault(lifecycle)}.Build())
		require.NoError(t, err)
		state = projection.Apply(state, event)
	}

	assert.Equal(t, []presentationdomain.Line{{
		Kind: presentationdomain.LineModel,
		Text: "complete answer",
	}}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestMapLifecycleProjectsModelToolSettlementAndAvailability verifies every approved lifecycle mapping.
func TestMapLifecycleProjectsModelToolSettlementAndAvailability(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		lifecycle *uiv1.LifecycleEvent
		expected  presentationdomain.Event
	}{
		{
			name: "model delta",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:         new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
				ModelContent: uiv1.ModelContent_builder{Type: new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA), Position: new(int32(2)), Text: new("delta")}.Build(),
			}.Build(),
			expected: presentationdomain.Event{Kind: presentationdomain.EventModelDelta, Position: 2, Text: "delta"},
		},
		{
			name: "tool start",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START),
				ToolCallId: new("call-1"), ToolName: new("read"),
			}.Build(),
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolStarted,
				ToolCallID: "call-1", ToolName: "read", Status: "started",
			},
		},
		{
			name: "tool stderr",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
				ToolCallId: new("call-1"), ProgressChannel: new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR), Text: new("warning"),
			}.Build(),
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolOutput,
				ToolCallID: "call-1", Stream: presentationdomain.OutputStderr, Text: "warning",
			},
		},
		{
			name: "failed tool result",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
				ToolCallId: new("call-1"), ToolName: new("read"), Text: new("denied"), IsError: new(true),
			}.Build(),
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolResult,
				ToolCallID: "call-1", ToolName: "read", Text: "denied", Failure: true,
			},
		},
		{
			name: "failed settlement",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:    new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED),
				Outcome: new("error"), ErrorMessage: new("safe failure"),
			}.Build(),
			expected: presentationdomain.Event{
				Kind: presentationdomain.EventAgentSettled,
				Text: "safe failure", ErrorText: "safe failure", Status: "error", Failure: true,
			},
		},
		{
			name: "availability",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:         new(uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED),
				Availability: new(uiv1.Availability_AVAILABILITY_RUNNING),
			}.Build(),
			expected: presentationdomain.Event{
				Kind:         presentationdomain.EventAvailability,
				Availability: presentationdomain.AvailabilityRunning,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			event, err := mapLifecycle(testCase.lifecycle)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, event)
		})
	}
}

// TestMapToolCallPreviewPreservesCompleteSnapshot verifies direct protobuf projection without truncation.
func TestMapToolCallPreviewPreservesCompleteSnapshot(t *testing.T) {
	t.Parallel()

	completeValue, err := structpb.NewValue(map[string]any{
		"nested": []any{"value", float64(2), true},
	})
	require.NoError(t, err)
	preview := uiv1.ToolCallPreview_builder{
		CallId: new("call-17"), Name: new("sample"), Position: new(int32(23)), Provisional: new(true),
		Fields: []*uiv1.ToolCallPreviewField{
			uiv1.ToolCallPreviewField_builder{Name: new("complete"), Value: proto.ValueOrDefault(completeValue)}.Build(),
			uiv1.ToolCallPreviewField_builder{Name: new("prefix"), Prefix: new(`{"partial":`)}.Build(),
		},
	}.Build()

	assert.Equal(t, presentationdomain.ToolCallState{
		CallID: "call-17", Name: "sample", Position: 23, Provisional: true,
		Fields: []presentationdomain.ToolCallField{
			{Name: "complete", Value: map[string]any{
				"nested": []any{"value", float64(2), true},
			}, Prefix: "", Complete: true},
			{Name: "prefix", Value: nil, Prefix: `{"partial":`, Complete: false},
		},
		Arguments: nil,
	}, mapToolCallPreview(preview))
}

// TestMapSafeAuthenticationErrorEnablesManualRetry verifies retry state comes only from safe Host errors.
func TestMapSafeAuthenticationErrorEnablesManualRetry(t *testing.T) {
	t.Parallel()

	event, err := mapRequest(uiv1.OpenRequest_builder{Error: uiv1.Error_builder{Text: new("Authentication failed."), RetryAuthentication: new(true)}.Build()}.Build())
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{
		Kind:         presentationdomain.EventError,
		Text:         "Authentication failed.",
		Availability: presentationdomain.AvailabilityAuthenticationFailed,
	}, event)
}

// initializationRequest builds the first valid Host frame used by stream tests.
func initializationRequest() *uiv1.OpenRequest {
	return uiv1.OpenRequest_builder{Initialization: uiv1.Initialization_builder{
		StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
			Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION),
			Text:     new("ready"),
		}.Build()},
		Extensions:   []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{PluginId: new("tools"), Tools: []string{"read"}}.Build()},
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
	}.Build()}.Build()
}
