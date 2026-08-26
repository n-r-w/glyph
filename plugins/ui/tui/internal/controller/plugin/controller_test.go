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

	"github.com/samber/mo"
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
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Information field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Information: uiv1.Information_builder{
			Text: new("too early"),
		}.Build(),
	}.Build()))
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
				Kind: presentationdomain.EventInitialization,
				Startup: []presentationdomain.Line{{
					Kind:               presentationdomain.LineInformation,
					Text:               mo.Some("ready"),
					ToolName:           mo.None[string](),
					Status:             mo.None[string](),
					ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
				}},
				Availability: mo.Some(presentationdomain.AvailabilityIdle),
				Extensions: []presentationdomain.Extension{{
					ID:    "tools",
					Tools: []string{"read"},
					Path:  "",
				}},
				Models: []presentationdomain.ConfiguredModel{{
					ProviderID: "openai-codex",
					ModelID:    "gpt",
					Reasoning:  testReasoning(presentationdomain.ReasoningChoiceHigh),
				}},
				ModelSelection: mo.Some(presentationdomain.ModelSelection{
					ProviderID:      "openai-codex",
					ModelID:         "gpt",
					ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
				}),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
				ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
			}, initial)
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error {
		close(started)
		<-runDone
		return nil
	})
	program.EXPECT().Send(presentationdomain.Event{
		Kind:                 presentationdomain.EventInformation,
		Text:                 mo.Some("information"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	})
	program.EXPECT().Send(gomock.Any()).Do(func(event presentationdomain.Event) {
		contentKind, ok := event.ModelContentKind.Get()
		require.True(t, ok)
		position, ok := event.Position.Get()
		require.True(t, ok)
		text, ok := event.Text.Get()
		require.True(t, ok)
		switch contentKind {
		case presentationdomain.ModelContentReasoning:
			assert.Equal(t, 1, position)
			assert.Equal(t, "hidden reasoning", text)
		case presentationdomain.ModelContentText:
			assert.Equal(t, 2, position)
			assert.Equal(t, "delta", text)
		case presentationdomain.ModelContentUnspecified, presentationdomain.ModelContentRefusal:
			require.Fail(t, "unexpected model content kind")
		default:
			require.Fail(t, "unexpected model content kind")
		}
	}).Times(2)
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	<-started
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Information field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Information: uiv1.Information_builder{
			Text: new("information"),
		}.Build(),
	}.Build()))
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Lifecycle: uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING),
				Position: new(int32(1)),
				Text:     new("hidden reasoning"),
			}.Build(),
			RunId:              nil,
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelResponse:      nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
	}.Build()))
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Lifecycle: uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				Position: new(int32(2)),
				Text:     new("delta"),
			}.Build(),
			RunId:           nil,
			Text:            nil,
			ToolCallId:      nil,
			ToolName:        nil,
			ProgressChannel: nil,
			IsError:         nil,
			Outcome:         nil,
			ErrorMessage:    nil,
			Availability:    nil,

			ModelResponse:      nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
	}.Build()))
	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// TestModelSelectionFramesAndCommandsPreserveContract verifies selection transport mappings.
func TestModelSelectionFramesAndCommandsPreserveContract(t *testing.T) {
	t.Parallel()

	initial, err := mapInitialization(uiv1.Initialization_builder{
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
		Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
			ProviderId: new("openrouter"),
			ModelId:    new("sonnet"),
			Reasoning: testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_OFF,
				uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build()},
		ModelSelection: uiv1.ModelSelection_builder{
			ProviderId:      new("openrouter"),
			ModelId:         new("sonnet"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
		SelectedUiId:   nil,
		StartupContent: nil,
		Extensions:     nil,
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.ConfiguredModel{{
		ProviderID: "openrouter",
		ModelID:    "sonnet",
		Reasoning:  testReasoning(presentationdomain.ReasoningChoiceOff, presentationdomain.ReasoningChoiceHigh),
	}}, initial.Models)
	assert.Equal(t, mo.Some(presentationdomain.ModelSelection{
		ProviderID:      "openrouter",
		ModelID:         "sonnet",
		ReasoningChoice: presentationdomain.ReasoningChoiceHigh,
	}), initial.ModelSelection)

	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active ModelSelectionChanged field.
	changed, err := mapRequest(uiv1.OpenRequest_builder{
		ModelSelectionChanged: uiv1.ModelSelectionChanged_builder{
			Selection: uiv1.ModelSelection_builder{
				ProviderId:      new("openai-codex"),
				ModelId:         new("gpt"),
				ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH),
			}.Build(),
		}.Build(),
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventModelSelectionChanged, changed.Kind)
	assert.Equal(t, mo.Some(presentationdomain.ModelSelection{
		ProviderID:      "openai-codex",
		ModelID:         "gpt",
		ReasoningChoice: presentationdomain.ReasoningChoiceXHigh,
	}), changed.ModelSelection)

	modelCommand, err := mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSelectModel,
		ProviderID:      mo.Some("openai-codex"),
		ModelID:         mo.Some("gpt"),
		Text:            mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
	})
	require.NoError(t, err)
	assert.Equal(t, "openai-codex", modelCommand.GetSelectModel().GetProviderId())
	assert.Equal(t, "gpt", modelCommand.GetSelectModel().GetModelId())

	reasoningCommand, err := mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSelectReasoningChoice,
		ReasoningChoice: mo.Some(presentationdomain.ReasoningChoiceMax),
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
	})
	require.NoError(t, err)
	assert.Equal(t, uiv1.ReasoningChoice_REASONING_CHOICE_MAX, reasoningCommand.GetSelectReasoningChoice().GetChoice())
}

// TestMapCommandRejectsMissingSelectedPayload verifies malformed presentation commands do not emit zero payloads.
func TestMapCommandRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	response, err := mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
	})

	require.Error(t, err)
	assert.Nil(t, response)

	response, err = mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.Some(""),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
	})
	require.NoError(t, err)
	assert.True(t, response.GetSubmit().HasText())
	assert.Empty(t, response.GetSubmit().GetText())
}

// TestReasoningMappingsCoverEveryValue verifies public and presentation enums stay exact.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()

	values := []struct {
		public       uiv1.ReasoningChoice
		presentation presentationdomain.ReasoningChoice
	}{
		{uiv1.ReasoningChoice_REASONING_CHOICE_OFF, presentationdomain.ReasoningChoiceOff},
		{uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL, presentationdomain.ReasoningChoiceMinimal},
		{uiv1.ReasoningChoice_REASONING_CHOICE_LOW, presentationdomain.ReasoningChoiceLow},
		{uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM, presentationdomain.ReasoningChoiceMedium},
		{uiv1.ReasoningChoice_REASONING_CHOICE_HIGH, presentationdomain.ReasoningChoiceHigh},
		{uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH, presentationdomain.ReasoningChoiceXHigh},
		{uiv1.ReasoningChoice_REASONING_CHOICE_MAX, presentationdomain.ReasoningChoiceMax},
	}
	for _, value := range values {
		mapped, err := mapReasoningChoice(value.public)
		require.NoError(t, err)
		assert.Equal(t, value.presentation, mapped)
		assert.Equal(t, value.public, mapReasoningChoiceToProto(value.presentation))
	}
	_, err := mapReasoningChoice(uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED)
	require.Error(t, err)
	_, err = mapReasoningChoice(uiv1.ReasoningChoice(99))
	require.Error(t, err)
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

	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:               presentationdomain.LineModel,
		Text:               mo.Some("Request complete."),
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	})
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:               presentationdomain.LineToolDone,
		ToolName:           mo.Some("bash"),
		Status:             mo.Some("completed"),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	})
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("bash"),
		Text:     mo.Some("tool-ok\n\n[Exit code: 0]\n"),
		ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
			Text:      mo.Some("tool-ok\n\n[Exit code: 0]\n"),
			MediaType: mo.None[string](),
			Data:      mo.None[[]byte](),
		}}),
		Status: mo.None[string](),
	})
	assert.Empty(t, state.ActiveTools)
}

// semanticFrame describes the stable lifecycle fields shared by both fixtures.
type semanticFrame struct {
	Type               string `json:"type"`
	ToolName           string `json:"tool_name"`
	ToolStatus         string `json:"tool_status"`
	Text               string `json:"text"`
	ToolResultContents []struct {
		Text string `json:"text"`
	} `json:"tool_result_contents"`
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
	lifecycle := uiv1.LifecycleEvent_builder{
		Type:               new(typeValue),
		ToolName:           new(frame.ToolName),
		Text:               new(frame.Text),
		Outcome:            new(frame.Outcome),
		RunId:              nil,
		ToolCallId:         nil,
		ProgressChannel:    nil,
		IsError:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build()
	if frame.Type == "message_end" && frame.ModelText != "" {
		lifecycle.SetModelResponse(uiv1.ModelResponse_builder{
			Content: []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				Text: new(frame.ModelText),
			}.Build()},
			Text:          nil,
			Outcome:       nil,
			ErrorMessage:  nil,
			Provider:      nil,
			Model:         nil,
			ResponseId:    nil,
			Usage:         nil,
			Diagnostics:   nil,
			ResponseModel: nil,
		}.Build())
	}
	if frame.Type == "tool_result" {
		contents := make([]*uiv1.ToolResultContent, 0, len(frame.ToolResultContents))
		for _, content := range frame.ToolResultContents {
			//nolint:exhaustruct // uiv1.ToolResultContent_builder sets only the active Text field.
			contents = append(contents, uiv1.ToolResultContent_builder{
				Text: new(content.Text),
			}.Build())
		}
		lifecycle.SetToolResultContents(contents)
	}
	if frame.Type == "tool_execution_end" {
		lifecycle.SetIsError(frame.ToolStatus != "ok")
	}
	if frame.Type == "availability" {
		lifecycle.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
	}
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	return uiv1.OpenRequest_builder{
		Lifecycle: lifecycle,
	}.Build()
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
			Position: new(int32(3)),
			Text:     new("cannot help"),
		}.Build(),
		RunId:              nil,
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventModelDelta, event.Kind)
	assert.Equal(t, mo.Some(presentationdomain.ModelContentRefusal), event.ModelContentKind)
	assert.Equal(t, mo.Some("cannot help"), event.Text)
}

// TestMapLifecyclePreservesFinalizedVisibleBlocks verifies mixed visible content reaches presentation state.
func TestMapLifecyclePreservesFinalizedVisibleBlocks(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
		ModelResponse: uiv1.ModelResponse_builder{
			Content: []*uiv1.ModelResponseContent{
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING),
					Text: new("hidden"),
				}.Build(),
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
					Text: new("answer"),
				}.Build(),
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL),
					Text: new("cannot help"),
				}.Build(),
			},
			Text:          nil,
			Outcome:       nil,
			ErrorMessage:  nil,
			Provider:      nil,
			Model:         nil,
			ResponseId:    nil,
			Usage:         nil,
			Diagnostics:   nil,
			ResponseModel: nil,
		}.Build(),
		RunId:              nil,
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.ModelResponseContent{
		{
			Kind: presentationdomain.ModelContentReasoning,
			Text: mo.Some("hidden"),
		},
		{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("answer"),
		},
		{
			Kind: presentationdomain.ModelContentRefusal,
			Text: mo.Some("cannot help"),
		},
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
		{
			Kind:            presentationdomain.CommandSubmit,
			Text:            mo.Some("hello"),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		},
		{
			Kind:            presentationdomain.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		},
		{
			Kind:            presentationdomain.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		},
		{
			Kind:            presentationdomain.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		},
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
		case presentationdomain.CommandUnspecified,
			presentationdomain.CommandSelectModel,
			presentationdomain.CommandSelectReasoningChoice:
			t.Fatal("unexpected command")
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
			PluginId: new("glyph-tools"),
			Tools:    []string{"read"},
			Path:     new("/plugins/glyph-tools"),
		}.Build()},
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
		Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
			ProviderId: new("openai-codex"),
			ModelId:    new("gpt"),
			Reasoning:  testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build()},
		ModelSelection: uiv1.ModelSelection_builder{
			ProviderId:      new("openai-codex"),
			ModelId:         new("gpt"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
	}.Build())

	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.Line{{
		Kind:               presentationdomain.LineWarning,
		ToolName:           mo.None[string](),
		Status:             mo.None[string](),
		Text:               mo.Some("excluded optional UI"),
		ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
	}}, event.Startup)
	assert.Equal(t, []presentationdomain.Extension{{
		ID:    "glyph-tools",
		Path:  "/plugins/glyph-tools",
		Tools: []string{"read"},
	}}, event.Extensions)
}

// TestMapRequestRejectsUnknownLifecycleAndMapsSafeError verifies malformed frames and safe errors.
func TestMapRequestRejectsUnknownLifecycleAndMapsSafeError(t *testing.T) {
	t.Parallel()

	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	_, err := mapRequest(uiv1.OpenRequest_builder{
		Lifecycle: &uiv1.LifecycleEvent{},
	}.Build())
	require.Error(t, err)

	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Error field.
	event, err := mapRequest(uiv1.OpenRequest_builder{
		Error: uiv1.Error_builder{
			Text:                new("safe error"),
			RetryAuthentication: nil,
		}.Build(),
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{
		Kind:                 presentationdomain.EventError,
		Text:                 mo.Some("safe error"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	}, event)
}

// TestMapLifecycleRejectsEmptyToolResultContents verifies missing terminal output fails at the UI boundary.
func TestMapLifecycleRejectsEmptyToolResultContents(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		RunId:              nil,
		Text:               nil,
		ToolCallId:         nil,
		ToolName:           nil,
		ProgressChannel:    nil,
		IsError:            nil,
		Outcome:            nil,
		ErrorMessage:       nil,
		Availability:       nil,
		ModelContent:       nil,
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	require.ErrorContains(t, err, "tool result contents are empty")
}

// TestMapLifecycleRejectsMissingToolResultContent verifies malformed blocks fail at the UI boundary.
func TestMapLifecycleRejectsMissingToolResultContent(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolResultContents: []*uiv1.ToolResultContent{
			uiv1.ToolResultContent_builder{}.Build(),
		},
		RunId:           nil,
		Text:            nil,
		ToolCallId:      nil,
		ToolName:        nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	require.ErrorContains(t, err, "tool result content 0 is missing")
}

// TestMapLifecycleRejectsEmptyToolResultImage prevents empty image payloads from reaching presentation.
func TestMapLifecycleRejectsEmptyToolResultImage(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		ToolResultContents: []*uiv1.ToolResultContent{
			//nolint:exhaustruct // uiv1.ToolResultContent_builder sets only the active Image field.
			uiv1.ToolResultContent_builder{
				Image: uiv1.ToolResultImage_builder{
					MediaType: new("image/png"),
					Data:      nil,
				}.Build(),
			}.Build(),
		},
		RunId:           nil,
		Text:            nil,
		ToolCallId:      nil,
		ToolName:        nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent:    nil,
		ModelResponse:   nil,
		ToolCallPreview: nil,
		FinalToolCall:   nil,
	}.Build())
	require.ErrorContains(t, err, "tool result image 0 is invalid")
}

// TestHostMessageEndFinalizesTextStreamAtDifferentPosition verifies complete terminal model projection.
func TestHostMessageEndFinalizesTextStreamAtDifferentPosition(t *testing.T) {
	t.Parallel()

	projection := presentationusecase.New()
	state := presentationdomain.State{}
	frames := []*uiv1.LifecycleEvent{
		uiv1.LifecycleEvent_builder{
			Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START),
			RunId:              nil,
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelContent:       nil,
			ModelResponse:      nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
				Position: new(int32(1)),
				Text:     new("complete answer"),
				Kind:     nil,
			}.Build(),
			RunId:              nil,
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelResponse:      nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
		uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END),
			ModelResponse: uiv1.ModelResponse_builder{
				Text:       new("complete answer"),
				Provider:   new("openai-codex"),
				Model:      new("gpt-test"),
				ResponseId: new("resp-1"),
				Usage: uiv1.ModelUsage_builder{
					InputTokens:       new(int64(3)),
					OutputTokens:      new(int64(2)),
					TotalTokens:       new(int64(5)),
					CachedInputTokens: nil,
					CacheWriteTokens:  nil,
					ReasoningTokens:   nil,
				}.Build(),
				Diagnostics: []*uiv1.ModelDiagnostic{uiv1.ModelDiagnostic_builder{
					Code:    new("recovered_output"),
					Message: new("hidden diagnostic"),
				}.Build()},
				Content: []*uiv1.ModelResponseContent{
					uiv1.ModelResponseContent_builder{
						Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING),
						Text: new("hidden reasoning"),
					}.Build(),
					uiv1.ModelResponseContent_builder{
						Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
						Text: new("complete answer"),
					}.Build(),
				},
				Outcome:       nil,
				ErrorMessage:  nil,
				ResponseModel: nil,
			}.Build(),
			RunId:              nil,
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelContent:       nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
	}
	for _, lifecycle := range frames {
		//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
		event, err := mapRequest(uiv1.OpenRequest_builder{
			Lifecycle: proto.ValueOrDefault(lifecycle),
		}.Build())
		require.NoError(t, err)
		state = projection.Apply(state, event)
	}

	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:               presentationdomain.LineReasoning,
			Text:               mo.Some("hidden reasoning"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
		{
			Kind:               presentationdomain.LineModel,
			Text:               mo.Some("complete answer"),
			ToolName:           mo.None[string](),
			Status:             mo.None[string](),
			ToolResultContents: mo.None[[]presentationdomain.ToolResultContent](),
		},
	}, state.Transcript)
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
				Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
				ModelContent: uiv1.ModelContent_builder{
					Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
					Position: new(int32(2)),
					Text:     new("delta"),
					Kind:     nil,
				}.Build(),
				RunId:              nil,
				Text:               nil,
				ToolCallId:         nil,
				ToolName:           nil,
				ProgressChannel:    nil,
				IsError:            nil,
				Outcome:            nil,
				ErrorMessage:       nil,
				Availability:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				Kind:                 presentationdomain.EventModelDelta,
				Position:             mo.Some(2),
				Text:                 mo.Some("delta"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			},
		},
		{
			name: "tool start",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START),
				ToolCallId:         new("call-1"),
				ToolName:           new("read"),
				RunId:              nil,
				Text:               nil,
				ProgressChannel:    nil,
				IsError:            nil,
				Outcome:            nil,
				ErrorMessage:       nil,
				Availability:       nil,
				ModelContent:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				Kind:                 presentationdomain.EventToolStarted,
				ToolCallID:           mo.Some("call-1"),
				ToolName:             mo.Some("read"),
				Status:               mo.Some("started"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
				ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			},
		},
		{
			name: "tool stderr",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
				ToolCallId:         new("call-1"),
				ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR),
				Text:               new("warning"),
				RunId:              nil,
				ToolName:           nil,
				IsError:            nil,
				Outcome:            nil,
				ErrorMessage:       nil,
				Availability:       nil,
				ModelContent:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				Kind:                 presentationdomain.EventToolOutput,
				ToolCallID:           mo.Some("call-1"),
				Stream:               mo.Some(presentationdomain.OutputStderr),
				Text:                 mo.Some("warning"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			},
		},
		{
			name: "failed tool result",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:       new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
				ToolCallId: new("call-1"),
				ToolName:   new("read"),
				IsError:    new(true),
				ToolResultContents: []*uiv1.ToolResultContent{
					//nolint:exhaustruct // uiv1.ToolResultContent_builder sets only the active Text field.
					uiv1.ToolResultContent_builder{
						Text: new("denied"),
					}.Build(),
				},
				RunId:           nil,
				Text:            nil,
				ProgressChannel: nil,
				Outcome:         nil,
				ErrorMessage:    nil,
				Availability:    nil,
				ModelContent:    nil,
				ModelResponse:   nil,
				ToolCallPreview: nil,
				FinalToolCall:   nil,
			}.Build(),
			expected: presentationdomain.Event{
				Kind:       presentationdomain.EventToolResult,
				ToolCallID: mo.Some("call-1"),
				ToolName:   mo.Some("read"),
				Failure:    mo.Some(true),
				ToolResultContents: mo.Some([]presentationdomain.ToolResultContent{{
					Text:      mo.Some("denied"),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				}}),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			},
		},
		{
			name: "failed settlement",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED),
				Outcome:            new("error"),
				ErrorMessage:       new("safe failure"),
				RunId:              nil,
				Text:               nil,
				ToolCallId:         nil,
				ToolName:           nil,
				ProgressChannel:    nil,
				IsError:            nil,
				Availability:       nil,
				ModelContent:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				Kind:                 presentationdomain.EventAgentSettled,
				Text:                 mo.Some("safe failure"),
				ErrorText:            mo.Some("safe failure"),
				Status:               mo.Some("error"),
				Failure:              mo.Some(true),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
				ExitCode:             mo.None[int](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			},
		},
		{
			name: "availability",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED),
				Availability:       new(uiv1.Availability_AVAILABILITY_RUNNING),
				RunId:              nil,
				Text:               nil,
				ToolCallId:         nil,
				ToolName:           nil,
				ProgressChannel:    nil,
				IsError:            nil,
				Outcome:            nil,
				ErrorMessage:       nil,
				ModelContent:       nil,
				ModelResponse:      nil,
				ToolCallPreview:    nil,
				FinalToolCall:      nil,
				ToolResultContents: nil,
			}.Build(),
			expected: presentationdomain.Event{
				Kind:                 presentationdomain.EventAvailability,
				Availability:         mo.Some(presentationdomain.AvailabilityRunning),
				Startup:              nil,
				Extensions:           nil,
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
				ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
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
	nullValue, err := structpb.NewValue(nil)
	require.NoError(t, err)
	preview := uiv1.ToolCallPreview_builder{
		CallId:      new("call-17"),
		Name:        new("sample"),
		Position:    new(int32(23)),
		Provisional: new(true),
		Fields: []*uiv1.ToolCallPreviewField{
			uiv1.ToolCallPreviewField_builder{
				Name:   new("complete"),
				Value:  proto.ValueOrDefault(completeValue),
				Prefix: nil,
			}.Build(),
			uiv1.ToolCallPreviewField_builder{
				Name:   new("null"),
				Value:  proto.ValueOrDefault(nullValue),
				Prefix: nil,
			}.Build(),
			uiv1.ToolCallPreviewField_builder{
				Name:   new("prefix"),
				Prefix: new(`{"partial":`),
				Value:  nil,
			}.Build(),
		},
	}.Build()

	assert.Equal(t, presentationdomain.ToolCallState{
		CallID:      "call-17",
		Name:        "sample",
		Position:    23,
		Provisional: true,
		Fields: []presentationdomain.ToolCallField{
			{
				Name: "complete",
				Value: mo.Some[any](map[string]any{
					"nested": []any{"value", float64(2), true},
				}),
				Prefix: mo.None[string](),
			},
			{
				Name:   "null",
				Value:  mo.Some[any](nil),
				Prefix: mo.None[string](),
			},
			{
				Name:   "prefix",
				Value:  mo.None[any](),
				Prefix: mo.Some(`{"partial":`),
			},
		},
		Arguments: nil,
	}, mapToolCallPreview(preview))
}

// TestMapSafeAuthenticationErrorEnablesManualRetry verifies retry state comes only from safe Host errors.
func TestMapSafeAuthenticationErrorEnablesManualRetry(t *testing.T) {
	t.Parallel()

	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Error field.
	event, err := mapRequest(uiv1.OpenRequest_builder{
		Error: uiv1.Error_builder{
			Text:                new("Authentication failed."),
			RetryAuthentication: new(true),
		}.Build(),
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{
		Kind:                 presentationdomain.EventError,
		Text:                 mo.Some("Authentication failed."),
		Availability:         mo.Some(presentationdomain.AvailabilityAuthenticationFailed),
		Startup:              nil,
		Extensions:           nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		ToolResultContents:   mo.None[[]presentationdomain.ToolResultContent](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
	}, event)
}

// initializationRequest builds the first valid Host frame used by stream tests.
func initializationRequest() *uiv1.OpenRequest {
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Initialization field.
	return uiv1.OpenRequest_builder{
		Initialization: uiv1.Initialization_builder{
			StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
				Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_INFORMATION),
				Text:     new("ready"),
			}.Build()},
			Extensions: []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{
				PluginId: new("tools"),
				Tools:    []string{"read"},
				Path:     nil,
			}.Build()},
			Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
			Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
				ProviderId: new("openai-codex"),
				ModelId:    new("gpt"),
				Reasoning:  testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
			}.Build()},
			ModelSelection: uiv1.ModelSelection_builder{
				ProviderId:      new("openai-codex"),
				ModelId:         new("gpt"),
				ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
			}.Build(),
			SelectedUiId: nil,
		}.Build(),
	}.Build()
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}

func testUIReasoning(choices ...uiv1.ReasoningChoice) *uiv1.ReasoningCapabilities {
	return uiv1.ReasoningCapabilities_builder{
		Supported:     new(true),
		Choices:       choices,
		DefaultChoice: new(choices[len(choices)-1]),
	}.Build()
}
