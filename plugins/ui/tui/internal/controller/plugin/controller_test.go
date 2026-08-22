//nolint:exhaustruct // Tests set only fields used by the active contract or presentation event kind.
package plugin

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	require.NoError(t, stream.Send(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Information{
		Information: &uiv1.Information{Text: "too early"},
	}}))
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
	require.NoError(t, stream.Send(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Information{
		Information: &uiv1.Information{Text: "information"},
	}}))
	require.NoError(t, stream.Send(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Lifecycle{
		Lifecycle: &uiv1.LifecycleEvent{
			Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			ModelContent: &uiv1.ModelContent{
				Type:     uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
				Kind:     uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING,
				Position: 1, Text: "hidden reasoning",
			},
		},
	}}))
	require.NoError(t, stream.Send(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Lifecycle{
		Lifecycle: &uiv1.LifecycleEvent{
			Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			ModelContent: &uiv1.ModelContent{
				Type:     uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
				Kind:     uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
				Position: 2, Text: "delta",
			},
		},
	}}))
	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// TestMapLifecyclePreservesRefusalKind verifies refusal deltas stay distinct from ordinary model text.
func TestMapLifecyclePreservesRefusalKind(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(&uiv1.LifecycleEvent{
		Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		ModelContent: &uiv1.ModelContent{
			Type:     uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
			Kind:     uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL,
			Position: 3, Text: "cannot help",
		},
	})

	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventModelDelta, event.Kind)
	assert.Equal(t, presentationdomain.ModelContentRefusal, event.ModelContentKind)
	assert.Equal(t, "cannot help", event.Text)
}

// TestMapLifecyclePreservesFinalizedRefusalBlocks verifies mixed visible content reaches presentation state.
func TestMapLifecyclePreservesFinalizedRefusalBlocks(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(&uiv1.LifecycleEvent{
		Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END,
		ModelResponse: &uiv1.ModelResponse{Content: []*uiv1.ModelResponseContent{
			{Kind: uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING, Text: "hidden"},
			{Kind: uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT, Text: "answer"},
			{Kind: uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL, Text: "cannot help"},
		}},
	})

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

	event, err := mapInitialization(&uiv1.Initialization{
		SelectedUiId: "glyph-tui",
		StartupContent: []*uiv1.StartupContent{{
			Severity: uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING,
			Text:     "excluded optional UI",
		}},
		Extensions: []*uiv1.ExtensionAvailability{{
			PluginId: "glyph-tools", Tools: []string{"read"}, Path: "/plugins/glyph-tools",
		}},
		Availability: uiv1.Availability_AVAILABILITY_IDLE,
	})

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

	_, err := mapRequest(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Lifecycle{
		Lifecycle: &uiv1.LifecycleEvent{},
	}})
	require.Error(t, err)

	event, err := mapRequest(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Error{
		Error: &uiv1.Error{Text: "safe error"},
	}})
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{Kind: presentationdomain.EventError, Text: "safe error"}, event)
}

// TestHostMessageEndFinalizesTextStreamAtDifferentPosition verifies complete terminal model projection.
func TestHostMessageEndFinalizesTextStreamAtDifferentPosition(t *testing.T) {
	t.Parallel()

	projection := presentationusecase.New()
	state := presentationdomain.State{}
	frames := []*uiv1.LifecycleEvent{
		{
			Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START,
		},
		{
			Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			ModelContent: &uiv1.ModelContent{
				Type: uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA, Position: 1, Text: "complete answer",
			},
		},
		{
			Type: uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END,
			ModelResponse: &uiv1.ModelResponse{
				Text: "complete answer", Provider: "openai-codex", Model: "gpt-test", ResponseId: "resp-1",
				Usage:       &uiv1.ModelUsage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5},
				Diagnostics: []*uiv1.ModelDiagnostic{{Code: "recovered_output", Message: "hidden diagnostic"}},
				Content: []*uiv1.ModelResponseContent{
					{Kind: uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING, Text: "hidden reasoning"},
					{Kind: uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT, Text: "complete answer"},
				},
			},
		},
	}
	for _, lifecycle := range frames {
		event, err := mapRequest(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Lifecycle{
			Lifecycle: lifecycle,
		}})
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
			lifecycle: &uiv1.LifecycleEvent{
				Type:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
				ModelContent: &uiv1.ModelContent{Type: uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA, Position: 2, Text: "delta"},
			},
			expected: presentationdomain.Event{Kind: presentationdomain.EventModelDelta, Position: 2, Text: "delta"},
		},
		{
			name: "tool start",
			lifecycle: &uiv1.LifecycleEvent{
				Type:       uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
				ToolCallId: "call-1", ToolName: "read",
			},
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolStarted,
				ToolCallID: "call-1", ToolName: "read", Status: "started",
			},
		},
		{
			name: "tool stderr",
			lifecycle: &uiv1.LifecycleEvent{
				Type:       uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
				ToolCallId: "call-1", ProgressChannel: uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR, Text: "warning",
			},
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolOutput,
				ToolCallID: "call-1", Stream: presentationdomain.OutputStderr, Text: "warning",
			},
		},
		{
			name: "failed tool result",
			lifecycle: &uiv1.LifecycleEvent{
				Type:       uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT,
				ToolCallId: "call-1", ToolName: "read", Text: "denied", IsError: true,
			},
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolResult,
				ToolCallID: "call-1", ToolName: "read", Text: "denied", Failure: true,
			},
		},
		{
			name: "failed settlement",
			lifecycle: &uiv1.LifecycleEvent{
				Type:    uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
				Outcome: "error", ErrorMessage: "safe failure",
			},
			expected: presentationdomain.Event{
				Kind: presentationdomain.EventAgentSettled,
				Text: "safe failure", ErrorText: "safe failure", Status: "error", Failure: true,
			},
		},
		{
			name: "availability",
			lifecycle: &uiv1.LifecycleEvent{
				Type:         uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED,
				Availability: uiv1.Availability_AVAILABILITY_RUNNING,
			},
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
	preview := &uiv1.ToolCallPreview{
		CallId: "call-17", Name: "sample", Position: 23, Provisional: true,
		Fields: []*uiv1.ToolCallPreviewField{
			{Name: "complete", Content: &uiv1.ToolCallPreviewField_Value{Value: completeValue}},
			{Name: "prefix", Content: &uiv1.ToolCallPreviewField_Prefix{Prefix: `{"partial":`}},
		},
	}

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

	event, err := mapRequest(&uiv1.OpenRequest{Content: &uiv1.OpenRequest_Error{
		Error: &uiv1.Error{Text: "Authentication failed.", RetryAuthentication: true},
	}})
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{
		Kind:         presentationdomain.EventError,
		Text:         "Authentication failed.",
		Availability: presentationdomain.AvailabilityAuthenticationFailed,
	}, event)
}

// initializationRequest builds the first valid Host frame used by stream tests.
func initializationRequest() *uiv1.OpenRequest {
	return &uiv1.OpenRequest{Content: &uiv1.OpenRequest_Initialization{
		Initialization: &uiv1.Initialization{
			StartupContent: []*uiv1.StartupContent{{
				Severity: uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION,
				Text:     "ready",
			}},
			Extensions:   []*uiv1.ExtensionAvailability{{PluginId: "tools", Tools: []string{"read"}}},
			Availability: uiv1.Availability_AVAILABILITY_IDLE,
		},
	}}
}
