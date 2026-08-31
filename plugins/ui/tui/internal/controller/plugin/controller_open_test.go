//go:build !integration

package plugin

import (
	"bytes"
	"io"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// modelTextDeltaOpenRequest creates one model text-delta frame.
func modelTextDeltaOpenRequest(
	kind uiv1.ModelContentKind,
	position int32,
	text string,
) *uiv1.OpenRequest {
	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	return uiv1.OpenRequest_builder{
		Lifecycle: uiv1.LifecycleEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
				Kind:     new(kind),
				Position: new(position),
				Text:     new(text),
			}.Build(),
			RunId:              new("run"),
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
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
		SessionTree:           nil,
		SessionTreeNavigation: nil,
		SessionTreeFailed:     nil,
		SessionForked:         nil,
		SessionCloned:         nil,
		EntryLabelSet:         nil,
	}.Build()
}

func TestOpenRejectsNonInitializationBeforeOpeningTerminal(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	client := uisdk.TestClient(t, New(
		NewMockTerminal(mockController),
		NewMockProgramFactory(mockController),
	))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Information field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Information: uiv1.Information_builder{
			Text: new("too early"),
		}.Build(),
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
		SessionTree:           nil,
		SessionTreeNavigation: nil,
		SessionTreeFailed:     nil,
		SessionForked:         nil,
		SessionCloned:         nil,
		EntryLabelSet:         nil,
	}.Build()))
	require.NoError(t, stream.CloseSend())

	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestOpenStartsAfterInitializationDeliversFramesAndClosesNormally verifies ordered startup and cleanup.
func TestOpenStartsAfterInitializationDeliversFramesAndClosesNormally(t *testing.T) {
	t.Parallel()

	// Arrange terminal, program, and stream expectations for initialization and lifecycle frames.
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
				RestoredTranscript: nil,
				Kind:               presentationdomain.EventInitialization,
				Startup: []presentationdomain.Line{{
					Kind:     presentationdomain.LineInformation,
					Text:     mo.Some("ready"),
					ToolName: mo.None[string](),
					Status:   mo.None[string](),
					Contents: mo.None[[]presentationdomain.Content](),
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
				SessionInfo: mo.Some(presentationdomain.SessionInfo{
					ID:               "session",
					Name:             "",
					NamePresent:      false,
					WorkingDirectory: "/project",
					StoragePath:      "",
					StoragePresent:   false,
					CreatedAt:        time.Unix(1, 0).UTC(),
					UpdatedAt:        time.Unix(1, 0).UTC(),
				}),
				Sessions:             nil,
				Position:             mo.None[int](),
				ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Text:                 mo.None[string](),
				Contents:             mo.None[[]presentationdomain.Content](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
				TreeEvent:            mo.None[presentationdomain.TreeEvent](),
			}, initial)
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error {
		close(started)
		<-runDone
		return nil
	})
	program.EXPECT().Send(testTextEvent(presentationdomain.EventInformation, "information"))
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

	// Act by opening the stream, sending ordered frames, and closing the client side.
	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	<-started
	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active Information field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Information: uiv1.Information_builder{
			Text: new("information"),
		}.Build(),
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
		SessionTree:           nil,
		SessionTreeNavigation: nil,
		SessionTreeFailed:     nil,
		SessionForked:         nil,
		SessionCloned:         nil,
		EntryLabelSet:         nil,
	}.Build()))
	require.NoError(t, stream.Send(modelTextDeltaOpenRequest(
		uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING, 1, "hidden reasoning",
	)))
	require.NoError(t, stream.Send(modelTextDeltaOpenRequest(
		uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT, 2, "delta",
	)))
	require.NoError(t, stream.CloseSend())

	// Assert the server closes normally with EOF after delivering every frame.
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
		SelectedUiId:   new("glyph-tui"),
		StartupContent: nil,
		Extensions:     nil,
		SessionInfo:    testSessionInfo(),
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

	//nolint:exhaustruct_v5 // uiv1.OpenRequest_builder sets only the active ModelSelectionChanged field.
	changed, err := mapRequest(uiv1.OpenRequest_builder{
		ModelSelectionChanged: uiv1.ModelSelectionChanged_builder{
			Selection: uiv1.ModelSelection_builder{
				ProviderId:      new("openai-codex"),
				ModelId:         new("gpt"),
				ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH),
			}.Build(),
		}.Build(),
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
		SessionTree:           nil,
		SessionTreeNavigation: nil,
		SessionTreeFailed:     nil,
		SessionForked:         nil,
		SessionCloned:         nil,
		EntryLabelSet:         nil,
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
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[presentationdomain.TreeCommand](),
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
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TreeCommand:     mo.None[presentationdomain.TreeCommand](),
	})
	require.NoError(t, err)
	assert.Equal(t, uiv1.ReasoningChoice_REASONING_CHOICE_MAX, reasoningCommand.GetSelectReasoningChoice().GetChoice())
}
