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
	"time"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

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

func TestRestoredTerminalFailuresRemainVisible(t *testing.T) {
	t.Parallel()

	for _, outcome := range []string{"aborted", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			errorMessage := "safe terminal failure"
			entry := new(uiv1.SessionEntry)
			entry.SetId("model-entry")
			entry.SetCreatedTime(timestamppb.Now())
			entry.SetModel(uiv1.ModelResponse_builder{
				Text: new(""), Outcome: &outcome, ErrorMessage: &errorMessage,
				Provider: nil, Model: nil, ResponseId: nil, Usage: nil,
				Diagnostics: nil, Content: nil, ResponseModel: nil,
			}.Build())

			lines, err := mapRestoredTranscript([]*uiv1.SessionEntry{entry})

			require.NoError(t, err)
			require.Len(t, lines, 1)
			assert.Equal(t, presentationdomain.LineError, lines[0].Kind)
			assert.Equal(t, mo.Some(errorMessage), lines[0].Text)
		})
	}
}

// TestSessionChangedMapsOrderedRestoredTranscript verifies restored session entries retain transcript order.
func TestSessionChangedMapsOrderedRestoredTranscript(t *testing.T) {
	t.Parallel()

	// Arrange a session-changed request with user, model, tool-call, and tool-result entries.
	createdAt := timestamppb.New(time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC))
	id := "stored"
	workingDirectory := "/project"
	userText := "prior-user"
	modelText := "prior-model"
	callID := "call-1"
	toolName := "read"
	toolResultText := "tool-result"
	//nolint:exhaustruct // OpenRequest_builder sets only the active SessionChanged field.
	request := uiv1.OpenRequest_builder{SessionChanged: uiv1.SessionChanged_builder{
		Info: uiv1.SessionInfo_builder{
			Id: &id, Name: nil, WorkingDirectory: &workingDirectory, StoragePath: nil,
			CreatedTime: createdAt, UpdateTime: createdAt,
		}.Build(),
		Entries: []*uiv1.SessionEntry{
			//nolint:exhaustruct // SessionEntry_builder sets only the active User field.
			uiv1.SessionEntry_builder{
				Id: &id, CreatedTime: createdAt, Model: nil,
				User: uiv1.UserMessage_builder{Content: []*uiv1.UserContent{
					uiv1.UserContent_builder{Text: &userText}.Build(),
				}}.Build(),
			}.Build(),
			//nolint:exhaustruct // SessionEntry_builder sets only the active Model field.
			uiv1.SessionEntry_builder{
				Id: &id, CreatedTime: createdAt, User: nil,
				Model: uiv1.ModelResponse_builder{
					Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
					ResponseId: nil, Usage: nil, Diagnostics: nil, ResponseModel: nil,
					Content: []*uiv1.ModelResponseContent{
						uiv1.ModelResponseContent_builder{
							Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: &modelText, ToolCall: nil,
						}.Build(),
						uiv1.ModelResponseContent_builder{
							Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED), Text: nil,
							ToolCall: uiv1.FinalToolCall_builder{
								CallId: &callID, Name: &toolName, Position: new(int32(1)),
								Arguments: &structpb.Struct{Fields: map[string]*structpb.Value{
									"path": structpb.NewStringValue("input.txt"),
								}},
							}.Build(),
						}.Build(),
					},
				}.Build(),
			}.Build(),
			//nolint:exhaustruct // SessionEntry_builder sets only the active ToolResult field.
			uiv1.SessionEntry_builder{
				Id: &id, CreatedTime: createdAt, User: nil, Model: nil,
				ToolResult: uiv1.ToolResult_builder{
					CallId: &callID, ToolName: &toolName, IsError: new(false),
					Contents: []*uiv1.ToolResultContent{
						uiv1.ToolResultContent_builder{Text: &toolResultText}.Build(),
					},
				}.Build(),
			}.Build(),
		},
	}.Build()}.Build()

	// Act by mapping the restored session request.
	event, err := mapRequest(request)
	// Assert the event preserves transcript order and public content.
	require.NoError(t, err)
	require.Empty(t, event.Startup)
	require.Len(t, event.RestoredTranscript, 4)
	assert.Equal(t, presentationdomain.LineUser, event.RestoredTranscript[0].Kind)
	assert.Equal(t, mo.Some(userText), event.RestoredTranscript[0].Text)
	assert.Equal(t, presentationdomain.LineModel, event.RestoredTranscript[1].Kind)
	assert.Equal(t, mo.Some(modelText), event.RestoredTranscript[1].Text)
	assert.Equal(t, presentationdomain.LineToolStatus, event.RestoredTranscript[2].Kind)
	assert.Equal(t, mo.Some(toolName), event.RestoredTranscript[2].ToolName)
	assert.Equal(t, mo.Some("arguments"), event.RestoredTranscript[2].Status)
	assert.JSONEq(t, `{"path":"input.txt"}`, event.RestoredTranscript[2].Text.MustGet())
	assert.Equal(t, presentationdomain.LineToolDone, event.RestoredTranscript[3].Kind)
	assert.Equal(t, mo.Some(toolName), event.RestoredTranscript[3].ToolName)
	assert.Equal(t, mo.Some(toolResultText), event.RestoredTranscript[3].Text)
}

// TestSessionChangedAcceptsStoredToolResultContentStates verifies valid empty and image states restore safely.
func TestSessionChangedAcceptsStoredToolResultContentStates(t *testing.T) {
	t.Parallel()

	// Arrange the stored tool-result states accepted by persistence.
	tests := []struct {
		name             string
		contents         func() ([]*uiv1.ToolResultContent, []byte)
		expectNil        bool
		expectImageData  mo.Option[[]byte]
		expectedRendered string
	}{
		{
			name: "nil contents",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				return nil, nil
			},
			expectNil: true, expectImageData: mo.None[[]byte](), expectedRendered: "",
		},
		{
			name: "non-nil empty contents",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				return []*uiv1.ToolResultContent{}, nil
			},
			expectNil: false, expectImageData: mo.None[[]byte](), expectedRendered: "",
		},
		{
			name: "present nil image bytes",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				image := new(uiv1.ToolResultImage)
				image.SetMediaType("image/png")
				image.SetData(nil)
				content := new(uiv1.ToolResultContent)
				content.SetImage(image)
				return []*uiv1.ToolResultContent{content}, nil
			},
			expectNil: false, expectImageData: mo.Some([]byte{}),
			expectedRendered: "[image image/png, 0 bytes]",
		},
		{
			name: "present non-nil empty image bytes",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				data := []byte{}
				image := new(uiv1.ToolResultImage)
				image.SetMediaType("image/png")
				image.SetData(data)
				content := new(uiv1.ToolResultContent)
				content.SetImage(image)
				return []*uiv1.ToolResultContent{content}, data
			},
			expectNil: false, expectImageData: mo.Some([]byte{}),
			expectedRendered: "[image image/png, 0 bytes]",
		},
		{
			name: "nonempty image bytes",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				data := []byte{1, 2, 3}
				image := new(uiv1.ToolResultImage)
				image.SetMediaType("image/png")
				image.SetData(data)
				content := new(uiv1.ToolResultContent)
				content.SetImage(image)
				return []*uiv1.ToolResultContent{content}, data
			},
			expectNil: false, expectImageData: mo.Some([]byte{1, 2, 3}),
			expectedRendered: "[image image/png, 3 bytes]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contents, source := test.contents()
			result := uiv1.ToolResult_builder{
				CallId: new("call"), ToolName: new("render"), Contents: contents, IsError: new(false),
			}.Build()
			entry := new(uiv1.SessionEntry)
			entry.SetToolResult(result)
			request := new(uiv1.OpenRequest)
			request.SetSessionChanged(uiv1.SessionChanged_builder{
				Info: testSessionInfo(), Entries: []*uiv1.SessionEntry{entry},
			}.Build())

			// Act by mapping a complete SessionChanged request.
			event, err := mapRequest(request)

			// Assert exact slice, option, byte ownership, and rendered text state.
			require.NoError(t, err)
			require.Len(t, event.RestoredTranscript, 1)
			line := event.RestoredTranscript[0]
			require.True(t, line.Contents.IsPresent())
			mapped := line.Contents.MustGet()
			if test.expectNil {
				assert.Nil(t, mapped)
			} else {
				assert.NotNil(t, mapped)
			}
			assert.Equal(t, mo.Some(test.expectedRendered), line.Text)
			if test.expectImageData.IsPresent() {
				require.Len(t, mapped, 1)
				assert.Equal(t, mo.Some("image/png"), mapped[0].MediaType)
				assert.Equal(t, test.expectImageData, mapped[0].Data)
				if len(source) != 0 {
					source[0] = 99
					assert.Equal(t, test.expectImageData, mapped[0].Data)
				}
			}
		})
	}
}

// TestRestoredTranscriptMapsImagesDiagnosticsAndVisibleModelContent verifies complete public restoration.
func TestRestoredTranscriptMapsImagesDiagnosticsAndVisibleModelContent(t *testing.T) {
	t.Parallel()

	// Arrange user, model, and tool-result entries with every public content type.
	createdAt := timestamppb.Now()
	userText := "before"
	userImage := new(uiv1.UserContent)
	userImage.SetImage(uiv1.UserImage_builder{MediaType: new("image/png"), Data: []byte{1, 2, 3}}.Build())
	userEntry := new(uiv1.SessionEntry)
	userEntry.SetId("user-entry")
	userEntry.SetCreatedTime(createdAt)
	userEntry.SetUser(uiv1.UserMessage_builder{Content: []*uiv1.UserContent{
		uiv1.UserContent_builder{Text: &userText, Image: nil}.Build(), userImage,
	}}.Build())
	reasoning := "visible reasoning"
	refusal := "visible refusal"
	modelEntry := new(uiv1.SessionEntry)
	modelEntry.SetId("model-entry")
	modelEntry.SetCreatedTime(createdAt)
	modelEntry.SetModel(uiv1.ModelResponse_builder{
		Text: nil, Outcome: new("stop"), ErrorMessage: nil, Provider: nil, Model: nil,
		ResponseId: nil, Usage: nil, ResponseModel: nil,
		Diagnostics: []*uiv1.ModelDiagnostic{
			uiv1.ModelDiagnostic_builder{Code: new("notice"), Message: new("safe diagnostic")}.Build(),
		},
		Content: []*uiv1.ModelResponseContent{
			uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING), Text: &reasoning, ToolCall: nil,
			}.Build(),
			uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL), Text: &refusal, ToolCall: nil,
			}.Build(),
		},
	}.Build())
	toolContent := new(uiv1.ToolResultContent)
	toolContent.SetImage(uiv1.ToolResultImage_builder{MediaType: new("image/webp"), Data: []byte{4, 5, 6}}.Build())
	toolEntry := new(uiv1.SessionEntry)
	toolEntry.SetId("tool-entry")
	toolEntry.SetCreatedTime(createdAt)
	toolEntry.SetToolResult(uiv1.ToolResult_builder{
		CallId: new("call"), ToolName: new("render"), Contents: []*uiv1.ToolResultContent{toolContent},
		IsError: new(false),
	}.Build())

	// Act by mapping the ordered restored transcript.
	lines, err := mapRestoredTranscript([]*uiv1.SessionEntry{userEntry, modelEntry, toolEntry})

	// Assert ordered line kinds, text, diagnostics, and image bytes.
	require.NoError(t, err)
	require.Len(t, lines, 5)
	require.True(t, lines[0].Contents.IsPresent())
	require.Equal(t, []byte{1, 2, 3}, lines[0].Contents.MustGet()[1].Data.MustGet())
	require.Equal(t, presentationdomain.LineReasoning, lines[1].Kind)
	require.Equal(t, mo.Some(reasoning), lines[1].Text)
	require.Equal(t, presentationdomain.LineRefusal, lines[2].Kind)
	require.Equal(t, mo.Some(refusal), lines[2].Text)
	require.Equal(t, presentationdomain.LineInformation, lines[3].Kind)
	require.Equal(t, mo.Some("notice: safe diagnostic"), lines[3].Text)
	require.Equal(t, presentationdomain.LineToolDone, lines[4].Kind)
	require.Equal(t, []byte{4, 5, 6}, lines[4].Contents.MustGet()[0].Data.MustGet())
	require.Equal(t, mo.Some("[image image/webp, 3 bytes]"), lines[4].Text)
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
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Information field.
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Information: uiv1.Information_builder{
			Text: new("too early"),
		}.Build(),
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
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
		RestoredTranscript:   nil,
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
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
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

	// Act by opening the stream, sending ordered frames, and closing the client side.
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
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
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
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
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
			RunId:           new("run"),
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
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
	}.Build()))
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

	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active ModelSelectionChanged field.
	changed, err := mapRequest(uiv1.OpenRequest_builder{
		ModelSelectionChanged: uiv1.ModelSelectionChanged_builder{
			Selection: uiv1.ModelSelection_builder{
				ProviderId:      new("openai-codex"),
				ModelId:         new("gpt"),
				ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH),
			}.Build(),
		}.Build(),
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
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
	})
	require.NoError(t, err)
	assert.Equal(t, uiv1.ReasoningChoice_REASONING_CHOICE_MAX, reasoningCommand.GetSelectReasoningChoice().GetChoice())
}

// TestMapRequestRequiresTextFrameScalarPresence verifies selected Host frame scalars cannot be omitted.
func TestMapRequestRequiresTextFrameScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]*uiv1.OpenRequest{
		"authorization URL": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         uiv1.AuthorizationRequest_builder{Url: nil}.Build(),
			Information:           nil,
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"information text": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         nil,
			Information:           uiv1.Information_builder{Text: nil}.Build(),
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"error text": uiv1.OpenRequest_builder{
			Initialization: nil,
			Lifecycle:      nil,
			Authorization:  nil,
			Information:    nil,
			Error: uiv1.Error_builder{
				Text:                nil,
				RetryAuthentication: new(false),
			}.Build(),
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"error retry authentication": uiv1.OpenRequest_builder{
			Initialization: nil,
			Lifecycle:      nil,
			Authorization:  nil,
			Information:    nil,
			Error: uiv1.Error_builder{
				Text:                new("error"),
				RetryAuthentication: nil,
			}.Build(),
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
	}

	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mapRequest(request)
			require.Error(t, err)
		})
	}
}

// TestMapRequestPreservesPresentFalseRetryAuthentication verifies false is not treated as absence.
func TestMapRequestPreservesPresentFalseRetryAuthentication(t *testing.T) {
	t.Parallel()

	event, err := mapRequest(uiv1.OpenRequest_builder{
		Initialization: nil,
		Lifecycle:      nil,
		Authorization:  nil,
		Information:    nil,
		Error: uiv1.Error_builder{
			Text:                new(""),
			RetryAuthentication: new(false),
		}.Build(),
		ModelSelectionChanged: nil,
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), event.Text)
	assert.True(t, event.Availability.IsNone())
}

// TestMapRequestPreservesPresentEmptyText verifies empty text stays active for text frames.
func TestMapRequestPreservesPresentEmptyText(t *testing.T) {
	t.Parallel()

	for name, request := range map[string]*uiv1.OpenRequest{
		"authorization": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         uiv1.AuthorizationRequest_builder{Url: new("")}.Build(),
			Information:           nil,
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
		"information": uiv1.OpenRequest_builder{
			Initialization:        nil,
			Lifecycle:             nil,
			Authorization:         nil,
			Information:           uiv1.Information_builder{Text: new("")}.Build(),
			Error:                 nil,
			ModelSelectionChanged: nil,
			SessionList:           nil,
			SessionChanged:        nil,
			SessionInformation:    nil,
		}.Build(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			event, err := mapRequest(request)
			require.NoError(t, err)
			assert.Equal(t, mo.Some(""), event.Text)
		})
	}
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
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})

	require.Error(t, err)
	assert.Nil(t, response)

	response, err = mapCommand(presentationdomain.Command{
		Kind:            presentationdomain.CommandSubmit,
		Text:            mo.Some(""),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
	})
	require.NoError(t, err)
	assert.True(t, response.GetSubmit().HasText())
	assert.Empty(t, response.GetSubmit().GetText())
}

// TestMapInitializationRequiresScalarPresence verifies initialization keeps its handwritten required fields.
func TestMapInitializationRequiresScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*uiv1.Initialization){
		"selected UI ID": func(initialization *uiv1.Initialization) { initialization.ClearSelectedUiId() },
		"availability":   func(initialization *uiv1.Initialization) { initialization.ClearAvailability() },
		"startup severity": func(initialization *uiv1.Initialization) {
			initialization.GetStartupContent()[0].ClearSeverity()
		},
		"startup text": func(initialization *uiv1.Initialization) {
			initialization.GetStartupContent()[0].ClearText()
		},
		"extension plugin ID": func(initialization *uiv1.Initialization) {
			initialization.GetExtensions()[0].ClearPluginId()
		},
		"extension path": func(initialization *uiv1.Initialization) {
			initialization.GetExtensions()[0].ClearPath()
		},
		"configured provider ID": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].ClearProviderId()
		},
		"configured model ID": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].ClearModelId()
		},
		"reasoning supported": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].GetReasoning().ClearSupported()
		},
		"reasoning default choice": func(initialization *uiv1.Initialization) {
			initialization.GetModels()[0].GetReasoning().ClearDefaultChoice()
		},
		"selection provider ID": func(initialization *uiv1.Initialization) {
			initialization.GetModelSelection().ClearProviderId()
		},
		"selection model ID": func(initialization *uiv1.Initialization) {
			initialization.GetModelSelection().ClearModelId()
		},
		"selection reasoning choice": func(initialization *uiv1.Initialization) {
			initialization.GetModelSelection().ClearReasoningChoice()
		},
	}

	for name, clear := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := proto.Clone(initializationRequest()).(*uiv1.OpenRequest)
			initialization := request.GetInitialization()
			clear(initialization)
			_, err := mapInitialization(initialization)
			require.Error(t, err)
		})
	}
}

// TestMapInitializationPreservesPresentZeroScalars verifies valid zero values stay concrete.
func TestMapInitializationPreservesPresentZeroScalars(t *testing.T) {
	t.Parallel()

	initialization := proto.Clone(initializationRequest().GetInitialization()).(*uiv1.Initialization)
	initialization.SetSelectedUiId("")
	initialization.GetStartupContent()[0].SetText("")
	initialization.GetExtensions()[0].SetPluginId("")
	initialization.GetExtensions()[0].SetPath("")
	initialization.GetModels()[0].SetProviderId("")
	initialization.GetModels()[0].SetModelId("")
	initialization.GetModels()[0].GetReasoning().SetSupported(false)

	event, err := mapInitialization(initialization)
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), event.Startup[0].Text)
	assert.Empty(t, event.Extensions[0].ID)
	assert.Empty(t, event.Extensions[0].Path)
	assert.Empty(t, event.Models[0].ProviderID)
	assert.Empty(t, event.Models[0].ModelID)
	assert.False(t, event.Models[0].Reasoning.Supported)
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
	// Arrange the semantic lifecycle fixture and an initialized presentation state.
	payload, err := os.ReadFile(filepath.Join(repositoryRoot(t), "testdata", "semantic-ui-lifecycle.json"))
	require.NoError(t, err)
	var sequence []semanticFrame
	require.NoError(t, json.Unmarshal(payload, &sequence))

	service := presentationusecase.New()
	initial, err := mapRequest(initializationRequest())
	require.NoError(t, err)
	// Act by mapping and applying every fixture frame.
	state := service.Apply(presentationdomain.State{}, initial)
	for _, frame := range sequence {
		request := lifecycleRequest(frame)
		event, mapErr := mapRequest(request)
		require.NoError(t, mapErr)
		state = service.Apply(state, event)
	}

	assert.Equal(t, mo.Some(true), state.Settled)
	assert.Equal(t, mo.Some(presentationdomain.AvailabilityIdle), state.Availability)
	// Assert the final state contains the expected model and tool transcript entries.
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("Request complete."),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	})
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("bash"),
		Status:   mo.Some("completed"),
		Text:     mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	})
	assert.Contains(t, state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolDone,
		ToolName: mo.Some("bash"),
		Text:     mo.Some("tool-ok\n\n[Exit code: 0]\n"),
		Contents: mo.Some([]presentationdomain.Content{{
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
		ToolName:           nil,
		Text:               nil,
		Outcome:            nil,
		RunId:              new("run"),
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
	if frame.Type == "message_end" {
		var content []*uiv1.ModelResponseContent
		if frame.ModelText != "" {
			content = []*uiv1.ModelResponseContent{uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
				Text: new(frame.ModelText), ToolCall: nil,
			}.Build()}
		}
		lifecycle.SetModelResponse(uiv1.ModelResponse_builder{
			Content:       content,
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
	if frame.Type == "tool_execution_start" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
	}
	if frame.Type == "tool_result" {
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
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
		lifecycle.SetToolCallId("call")
		lifecycle.SetToolName(frame.ToolName)
		lifecycle.SetIsError(frame.ToolStatus != "ok")
	}
	if frame.Type == "tool_result" {
		lifecycle.SetIsError(false)
	}
	if frame.Type == "agent_end" {
		lifecycle.SetOutcome(frame.Outcome)
	}
	if frame.Type == "availability" {
		lifecycle.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
	}
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	return uiv1.OpenRequest_builder{
		Lifecycle:          lifecycle,
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
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
					Text: new("hidden"), ToolCall: nil,
				}.Build(),
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
					Text: new("answer"), ToolCall: nil,
				}.Build(),
				uiv1.ModelResponseContent_builder{
					Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL),
					Text: new("cannot help"), ToolCall: nil,
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
		RunId:              new("run"),
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
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            presentationdomain.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            presentationdomain.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind:            presentationdomain.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		},
		{
			Kind: presentationdomain.CommandCreateSession, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.None[string](),
		},
		{
			Kind: presentationdomain.CommandListSessions, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.None[string](),
		},
		{
			Kind: presentationdomain.CommandResumeSession, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.Some("stored"), SessionName: mo.None[string](),
		},
		{
			Kind: presentationdomain.CommandSetSessionName, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.Some("named"),
		},
		{
			Kind: presentationdomain.CommandGetSessionInfo, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.None[string](),
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
		case presentationdomain.CommandCreateSession:
			assert.NotNil(t, response.GetCreateSession())
		case presentationdomain.CommandListSessions:
			assert.NotNil(t, response.GetListSessions())
		case presentationdomain.CommandResumeSession:
			assert.Equal(t, "stored", response.GetResumeSession().GetSessionId())
		case presentationdomain.CommandSetSessionName:
			assert.Equal(t, "named", response.GetSetSessionName().GetName())
		case presentationdomain.CommandGetSessionInfo:
			assert.NotNil(t, response.GetGetSessionInfo())
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

	// Arrange initialization with a warning and an extension filesystem path.
	initialization := uiv1.Initialization_builder{
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
		SessionInfo: testSessionInfo(),
	}.Build()

	// Act by mapping the initialization payload.
	event, err := mapInitialization(initialization)

	// Assert warning severity and extension path survive mapping.
	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.Line{{
		Kind:     presentationdomain.LineWarning,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Text:     mo.Some("excluded optional UI"),
		Contents: mo.None[[]presentationdomain.Content](),
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

	// Arrange unknown lifecycle and safe-error requests.
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
	unknownLifecycle := uiv1.OpenRequest_builder{
		Lifecycle:          &uiv1.LifecycleEvent{},
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
	}.Build()
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Error field.
	safeError := uiv1.OpenRequest_builder{
		Error: uiv1.Error_builder{
			Text:                new("safe error"),
			RetryAuthentication: new(false),
		}.Build(),
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
	}.Build()

	// Act by mapping both requests.
	_, unknownErr := mapRequest(unknownLifecycle)
	event, err := mapRequest(safeError)

	// Assert the unknown lifecycle fails while the safe error maps without private data.
	require.Error(t, unknownErr)
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{
		RestoredTranscript:   nil,
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
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
	}, event)
}

// TestMapLifecycleRejectsEmptyToolResultContents verifies missing terminal output fails at the UI boundary.
func TestMapLifecycleRejectsEmptyToolResultContents(t *testing.T) {
	t.Parallel()

	_, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT),
		RunId:              new("run"),
		Text:               nil,
		ToolCallId:         new("call"),
		ToolName:           new("tool"),
		ProgressChannel:    nil,
		IsError:            new(false),
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
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      new("call"),
		ToolName:        new("tool"),
		ProgressChannel: nil,
		IsError:         new(false),
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
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      new("call"),
		ToolName:        new("tool"),
		ProgressChannel: nil,
		IsError:         new(false),
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

	// Arrange model lifecycle frames whose terminal response uses another stream position.
	projection := presentationusecase.New()
	state := presentationdomain.State{}
	frames := []*uiv1.LifecycleEvent{
		uiv1.LifecycleEvent_builder{
			Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START),
			RunId:              new("run"),
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
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
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
						Text: new("hidden reasoning"), ToolCall: nil,
					}.Build(),
					uiv1.ModelResponseContent_builder{
						Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
						Text: new("complete answer"), ToolCall: nil,
					}.Build(),
				},
				Outcome:       nil,
				ErrorMessage:  nil,
				ResponseModel: nil,
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
			ModelContent:       nil,
			ToolCallPreview:    nil,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build(),
	}
	// Act by mapping and applying the lifecycle frame sequence.
	for _, lifecycle := range frames {
		//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Lifecycle field.
		event, err := mapRequest(uiv1.OpenRequest_builder{
			Lifecycle:          proto.ValueOrDefault(lifecycle),
			SessionList:        nil,
			SessionChanged:     nil,
			SessionInformation: nil,
		}.Build())
		require.NoError(t, err)
		state = projection.Apply(state, event)
	}

	// Assert the terminal response finalizes the complete ordered transcript without active fragments.
	assert.Equal(t, []presentationdomain.Line{
		{
			Kind:     presentationdomain.LineReasoning,
			Text:     mo.Some("hidden reasoning"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
		{
			Kind:     presentationdomain.LineModel,
			Text:     mo.Some("complete answer"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}

// TestMapLifecycleProjectsModelToolSettlementAndAvailability verifies every approved lifecycle mapping.
func TestMapLifecycleProjectsModelToolSettlementAndAvailability(t *testing.T) {
	t.Parallel()

	// Arrange valid model, tool, settlement, and availability lifecycle cases.
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
					Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
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
			expected: presentationdomain.Event{
				RestoredTranscript:   nil,
				Kind:                 presentationdomain.EventModelDelta,
				Position:             mo.Some(2),
				Text:                 mo.Some("delta"),
				Startup:              nil,
				Extensions:           nil,
				Availability:         mo.None[presentationdomain.Availability](),
				ModelContentKind:     mo.Some(presentationdomain.ModelContentText),
				ModelResponseContent: nil,
				ToolCallID:           mo.None[string](),
				ToolName:             mo.None[string](),
				Status:               mo.None[string](),
				Stream:               mo.None[presentationdomain.OutputStream](),
				Contents:             mo.None[[]presentationdomain.Content](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
			},
		},
		{
			name: "tool start",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START),
				ToolCallId:         new("call-1"),
				ToolName:           new("read"),
				RunId:              new("run"),
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
				RestoredTranscript:   nil,
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
				Contents:             mo.None[[]presentationdomain.Content](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
			},
		},
		{
			name: "tool stderr",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE),
				ToolCallId:         new("call-1"),
				ProgressChannel:    new(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDERR),
				Text:               new("warning"),
				RunId:              new("run"),
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
				RestoredTranscript:   nil,
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
				Contents:             mo.None[[]presentationdomain.Content](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
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
				RunId:           new("run"),
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
				RestoredTranscript: nil,
				Kind:               presentationdomain.EventToolResult,
				ToolCallID:         mo.Some("call-1"),
				ToolName:           mo.Some("read"),
				Failure:            mo.Some(true),
				Contents: mo.Some([]presentationdomain.Content{{
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
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
			},
		},
		{
			name: "failed settlement",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED),
				Outcome:            new("error"),
				ErrorMessage:       new("safe failure"),
				RunId:              new("run"),
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
				RestoredTranscript:   nil,
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
				Contents:             mo.None[[]presentationdomain.Content](),
				ExitCode:             mo.None[int](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
			},
		},
		{
			name: "availability",
			lifecycle: uiv1.LifecycleEvent_builder{
				Type:               new(uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED),
				Availability:       new(uiv1.Availability_AVAILABILITY_RUNNING),
				RunId:              new("run"),
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
				RestoredTranscript:   nil,
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
				Contents:             mo.None[[]presentationdomain.Content](),
				ErrorText:            mo.None[string](),
				ExitCode:             mo.None[int](),
				Failure:              mo.None[bool](),
				ToolCall:             mo.None[presentationdomain.ToolCallState](),
				Models:               nil,
				ModelSelection:       mo.None[presentationdomain.ModelSelection](),
				SessionInfo:          mo.None[presentationdomain.SessionInfo](),
				Sessions:             nil,
			},
		},
	}

	// Act by mapping each lifecycle case independently.
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			event, err := mapLifecycle(testCase.lifecycle)
			// Assert every lifecycle maps to its exact presentation event.
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

	mapped, err := mapToolCallPreview(preview)
	require.NoError(t, err)
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
	}, mapped)
}

// TestMapLifecycleRejectsInactiveAgentStartResponse verifies stale lifecycle payloads fail at ingress.
func TestMapLifecycleRejectsInactiveAgentStartResponse(t *testing.T) {
	t.Parallel()

	lifecycle := messageEndLifecycle(t, nil)
	lifecycle.SetType(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START)
	_, err := mapLifecycle(lifecycle)

	require.Error(t, err)
}

// TestMapLifecycleValidatesActiveAndInactiveFieldsForEveryType verifies the complete lifecycle shape table.
func TestMapLifecycleValidatesActiveAndInactiveFieldsForEveryType(t *testing.T) {
	t.Parallel()

	validLifecycle := func(lifecycleType uiv1.LifecycleType) *uiv1.LifecycleEvent {
		lifecycle := uiv1.LifecycleEvent_builder{
			Type: new(lifecycleType), RunId: new("run"), Text: nil, ToolCallId: nil, ToolName: nil,
			ProgressChannel: nil, IsError: nil, Outcome: nil, ErrorMessage: nil, Availability: nil,
			ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
			ToolResultContents: nil,
		}.Build()
		switch lifecycleType {
		case uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END:
			nestedType := uiv1.ModelContentType_MODEL_CONTENT_TYPE_START
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
				nestedType = uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA
			}
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END {
				nestedType = uiv1.ModelContentType_MODEL_CONTENT_TYPE_END
			}
			text := (*string)(nil)
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
				text = new("")
			}
			lifecycle.SetModelContent(uiv1.ModelContent_builder{
				Type: new(nestedType), Position: new(int32(0)), Text: text,
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END:
			lifecycle.SetModelResponse(uiv1.ModelResponse_builder{
				Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
				ResponseId: nil, Usage: nil, Diagnostics: nil, Content: nil, ResponseModel: nil,
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA:
			lifecycle.SetToolCallPreview(uiv1.ToolCallPreview_builder{
				CallId: new("call"), Name: new("tool"), Position: new(int32(0)), Provisional: new(true), Fields: nil,
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END:
			lifecycle.SetFinalToolCall(uiv1.FinalToolCall_builder{
				CallId: new("call"), Name: new("tool"), Position: new(int32(0)),
				Arguments: &structpb.Struct{Fields: map[string]*structpb.Value{}},
			}.Build())
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START:
			lifecycle.SetToolCallId("")
			lifecycle.SetToolName("")
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE:
			lifecycle.SetText("")
			lifecycle.SetProgressChannel(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT)
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END:
			lifecycle.SetToolCallId("")
			lifecycle.SetToolName("")
			lifecycle.SetIsError(false)
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT:
			lifecycle.SetToolCallId("")
			lifecycle.SetToolName("")
			lifecycle.SetIsError(false)
			lifecycle.SetToolResultContents([]*uiv1.ToolResultContent{uiv1.ToolResultContent_builder{
				Text: new(""), Image: nil,
			}.Build()})
		case uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END:
			lifecycle.SetText("")
		case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END:
			lifecycle.SetOutcome("")
		case uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED:
			lifecycle.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
		case uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START,
			uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
			uiv1.LifecycleType_LIFECYCLE_TYPE_UNSPECIFIED:
		}
		return lifecycle
	}

	lifecycleTypes := []uiv1.LifecycleType{
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
		uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END,
	}
	for _, lifecycleType := range lifecycleTypes {
		t.Run(lifecycleType.String(), func(t *testing.T) {
			t.Parallel()
			valid := roundTripLifecycle(t, validLifecycle(lifecycleType))
			_, err := mapLifecycle(valid)
			require.NoError(t, err)

			malformed := roundTripLifecycle(t, validLifecycle(lifecycleType))
			if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
				malformed.SetModelResponse(uiv1.ModelResponse_builder{
					Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
					ResponseId: nil, Usage: nil, Diagnostics: nil, Content: nil, ResponseModel: nil,
				}.Build())
			} else {
				malformed.SetAvailability(uiv1.Availability_AVAILABILITY_IDLE)
			}
			_, err = mapLifecycle(malformed)
			require.Error(t, err)
		})
	}
}

func TestMapLifecycleRejectsMissingSelectedModelAndPreviewPayloads(t *testing.T) {
	t.Parallel()

	lifecycle := func(
		lifecycleType uiv1.LifecycleType,
		modelContent *uiv1.ModelContent,
		preview *uiv1.ToolCallPreview,
	) *uiv1.LifecycleEvent {
		return uiv1.LifecycleEvent_builder{
			Type:               new(lifecycleType),
			RunId:              new("run"),
			Text:               nil,
			ToolCallId:         nil,
			ToolName:           nil,
			ProgressChannel:    nil,
			IsError:            nil,
			Outcome:            nil,
			ErrorMessage:       nil,
			Availability:       nil,
			ModelContent:       modelContent,
			ModelResponse:      nil,
			ToolCallPreview:    preview,
			FinalToolCall:      nil,
			ToolResultContents: nil,
		}.Build()
	}
	nilFieldPreview := uiv1.ToolCallPreview_builder{
		CallId:      nil,
		Name:        nil,
		Position:    nil,
		Provisional: nil,
		Fields:      []*uiv1.ToolCallPreviewField{nil},
	}.Build()
	unsetFieldPreview := uiv1.ToolCallPreview_builder{
		CallId:      nil,
		Name:        nil,
		Position:    nil,
		Provisional: nil,
		Fields: []*uiv1.ToolCallPreviewField{uiv1.ToolCallPreviewField_builder{
			Name:   new("path"),
			Value:  nil,
			Prefix: nil,
		}.Build()},
	}.Build()

	testCases := []struct {
		name      string
		lifecycle *uiv1.LifecycleEvent
	}{
		{
			name:      "message end response",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END, nil, nil),
		},
		{
			name:      "nil preview field",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START, nil, nilFieldPreview),
		},
		{
			name:      "preview field content",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_DELTA, nil, unsetFieldPreview),
		},
		{
			name: "model position",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START, uiv1.ModelContent_builder{
				Type:     nil,
				Position: nil,
				Text:     nil,
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build(), nil),
		},
		{
			name: "model kind",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END, uiv1.ModelContent_builder{
				Type:     nil,
				Position: new(int32(0)),
				Text:     nil,
				Kind:     nil,
			}.Build(), nil),
		},
		{
			name: "text delta text",
			lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA, uiv1.ModelContent_builder{
				Type:     nil,
				Position: new(int32(0)),
				Text:     nil,
				Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			}.Build(), nil),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(testCase.lifecycle)
			require.Error(t, err)
		})
	}
}

// TestMapLifecycleRejectsMissingRequiredScalarFields verifies each lifecycle variant checks its scalar contract.
func TestMapLifecycleRejectsMissingRequiredScalarFields(t *testing.T) {
	t.Parallel()

	lifecycle := func(lifecycleType uiv1.LifecycleType) *uiv1.LifecycleEvent {
		return uiv1.LifecycleEvent_builder{
			Type: new(lifecycleType), RunId: new("run"), Text: nil, ToolCallId: nil, ToolName: nil,
			ProgressChannel: nil, IsError: nil, Outcome: nil, ErrorMessage: nil, Availability: nil,
			ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
			ToolResultContents: nil,
		}.Build()
	}
	missingRunID := uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_START), RunId: nil, Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil, ModelResponse: nil,
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build()
	missingModelType := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START)
	missingModelType.SetModelContent(uiv1.ModelContent_builder{
		Type: nil, Position: new(int32(0)), Text: nil,
		Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
	}.Build())
	missingPreviewProvisional := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_START)
	missingPreviewProvisional.SetToolCallPreview(uiv1.ToolCallPreview_builder{
		CallId: new("call"), Name: new("tool"), Position: new(int32(0)), Provisional: nil, Fields: nil,
	}.Build())
	missingFinalPosition := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_CALL_END)
	missingFinalPosition.SetFinalToolCall(uiv1.FinalToolCall_builder{
		CallId: new("call"), Name: new("tool"), Position: nil,
		Arguments: &structpb.Struct{Fields: map[string]*structpb.Value{}},
	}.Build())
	missingStartCallID := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START)
	missingStartCallID.SetToolName("tool")
	missingProgressText := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_UPDATE)
	missingProgressText.SetProgressChannel(uiv1.ProgressChannel_PROGRESS_CHANNEL_STDOUT)
	missingEndCallID := lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END)
	missingEndCallID.SetToolName("tool")
	missingEndCallID.SetIsError(false)

	tests := []struct {
		name      string
		lifecycle *uiv1.LifecycleEvent
	}{
		{name: "run ID", lifecycle: missingRunID},
		{name: "model content type", lifecycle: missingModelType},
		{name: "tool call preview provisional", lifecycle: missingPreviewProvisional},
		{name: "final tool call position", lifecycle: missingFinalPosition},
		{name: "tool execution start call ID", lifecycle: missingStartCallID},
		{name: "tool progress text", lifecycle: missingProgressText},
		{name: "tool execution end call ID", lifecycle: missingEndCallID},
		{name: "turn end text", lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_TURN_END)},
		{name: "agent end outcome", lifecycle: lifecycle(uiv1.LifecycleType_LIFECYCLE_TYPE_AGENT_END)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(roundTripLifecycle(t, test.lifecycle))
			require.Error(t, err)
		})
	}
}

// TestMapLifecycleRejectsInvalidModelContentDiscriminators verifies public discriminator consistency.
func TestMapLifecycleRejectsInvalidModelContentDiscriminators(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer         uiv1.LifecycleType
		nested        uiv1.ModelContentType
		kind          uiv1.ModelContentKind
		errorContains string
	}{
		"start rejects text delta": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"start rejects end": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"text delta rejects start": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"text delta rejects end": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"end rejects start": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"end rejects text delta": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"present unspecified nested type": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_UNSPECIFIED,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"present unknown nested type": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType(99),
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			errorContains: "model content type",
		},
		"present unspecified kind": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED,
			errorContains: "model content kind",
		},
		"present unknown kind": {
			outer:         uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested:        uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
			kind:          uiv1.ModelContentKind(99),
			errorContains: "model content kind",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(modelContentLifecycle(testCase.outer, testCase.nested, testCase.kind))
			require.ErrorContains(t, err, testCase.errorContains)
		})
	}
}

// TestMapLifecycleAcceptsMatchingModelContentDiscriminators verifies each valid discriminator pair.
func TestMapLifecycleAcceptsMatchingModelContentDiscriminators(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer  uiv1.LifecycleType
		nested uiv1.ModelContentType
	}{
		"start": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
		},
		"text delta": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA,
		},
		"end": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(modelContentLifecycle(
				testCase.outer,
				testCase.nested,
				uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
			))
			require.NoError(t, err)
		})
	}
}

// TestMapLifecycleAcceptsPresentZeroPositionAndEmptyText verifies present zero values survive mapping.
func TestMapLifecycleAcceptsPresentZeroPositionAndEmptyText(t *testing.T) {
	t.Parallel()

	event, err := mapLifecycle(uiv1.LifecycleEvent_builder{
		Type:            new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      nil,
		ToolName:        nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Position: new(int32(0)),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
			Text:     new(""),
		}.Build(),
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build())
	require.NoError(t, err)
	assert.Equal(t, mo.Some(0), event.Position)
	assert.Equal(t, mo.Some(""), event.Text)
}

// TestMapLifecycleRequiresToolFailurePresence verifies absent false differs from present false on the wire.
func TestMapLifecycleRequiresToolFailurePresence(t *testing.T) {
	t.Parallel()

	for _, lifecycleType := range []uiv1.LifecycleType{
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END,
		uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT,
	} {
		t.Run(lifecycleType.String(), func(t *testing.T) {
			t.Parallel()
			build := func(isError *bool) *uiv1.LifecycleEvent {
				contents := []*uiv1.ToolResultContent(nil)
				if lifecycleType == uiv1.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT {
					contents = []*uiv1.ToolResultContent{uiv1.ToolResultContent_builder{
						Text:  new(""),
						Image: nil,
					}.Build()}
				}
				return roundTripLifecycle(t, uiv1.LifecycleEvent_builder{
					Type: new(lifecycleType), RunId: new("run"), Text: nil,
					ToolCallId: new("call"), ToolName: new("tool"), ProgressChannel: nil,
					IsError: isError, Outcome: nil, ErrorMessage: nil, Availability: nil,
					ModelContent: nil, ModelResponse: nil, ToolCallPreview: nil, FinalToolCall: nil,
					ToolResultContents: contents,
				}.Build())
			}

			_, err := mapLifecycle(build(nil))
			require.Error(t, err)
			event, err := mapLifecycle(build(new(false)))
			require.NoError(t, err)
			assert.Equal(t, mo.Some(false), event.Failure)
		})
	}
}

// TestMapLifecycleValidatesFinalResponseContent verifies malformed items fail and present empty text survives.
func TestMapLifecycleValidatesFinalResponseContent(t *testing.T) {
	t.Parallel()

	valid := uiv1.ModelResponseContent_builder{
		Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT),
		Text: new(""), ToolCall: nil,
	}.Build()
	invalid := []struct {
		name string
		item *uiv1.ModelResponseContent
	}{
		{name: "nil item", item: nil},
		{name: "missing kind", item: uiv1.ModelResponseContent_builder{Kind: nil, Text: new(""), ToolCall: nil}.Build()},
		{name: "unspecified kind", item: uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED), Text: new(""), ToolCall: nil}.Build()},
		{name: "unknown kind", item: uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind(99)), Text: new(""), ToolCall: nil}.Build()},
		{name: "missing text", item: uiv1.ModelResponseContent_builder{Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: nil, ToolCall: nil}.Build()},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := mapLifecycle(messageEndLifecycle(t, []*uiv1.ModelResponseContent{test.item}))
			require.Error(t, err)
		})
	}

	event, err := mapLifecycle(messageEndLifecycle(t, []*uiv1.ModelResponseContent{valid}))
	require.NoError(t, err)
	require.Len(t, event.ModelResponseContent, 1)
	assert.Equal(t, mo.Some(""), event.ModelResponseContent[0].Text)
}

// TestOpenRejectsConflictingModelContentDiscriminatorsAsInvalidArgument verifies stream error ownership.
func TestOpenRejectsConflictingModelContentDiscriminatorsAsInvalidArgument(t *testing.T) {
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
	require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
		Initialization: nil,
		Lifecycle: modelContentLifecycle(
			uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA,
			uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
			uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
		),
		Authorization:         nil,
		Information:           nil,
		Error:                 nil,
		ModelSelectionChanged: nil,
		SessionList:           nil,
		SessionChanged:        nil,
		SessionInformation:    nil,
	}.Build()))
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// TestMapLifecycleRejectsInactiveModelContentText verifies structural variants reject nested text.
func TestMapLifecycleRejectsInactiveModelContentText(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer  uiv1.LifecycleType
		nested uiv1.ModelContentType
	}{
		"content start": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
		},
		"content end": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mapLifecycle(modelContentLifecycleWithText(
				testCase.outer,
				testCase.nested,
				uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
				"",
			))
			require.ErrorContains(t, err, "model content text")
		})
	}
}

// TestOpenRejectsInactiveModelContentTextAsInvalidArgument verifies mapper errors keep gRPC ownership.
func TestOpenRejectsInactiveModelContentTextAsInvalidArgument(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		outer  uiv1.LifecycleType
		nested uiv1.ModelContentType
	}{
		"content start": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_START,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_START,
		},
		"content end": {
			outer:  uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_CONTENT_END,
			nested: uiv1.ModelContentType_MODEL_CONTENT_TYPE_END,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
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
			require.NoError(t, stream.Send(uiv1.OpenRequest_builder{
				Initialization: nil,
				Lifecycle: modelContentLifecycleWithText(
					testCase.outer,
					testCase.nested,
					uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT,
					"malformed",
				),
				Authorization: nil, Information: nil, Error: nil, ModelSelectionChanged: nil,
				SessionList:        nil,
				SessionChanged:     nil,
				SessionInformation: nil,
			}.Build()))
			require.NoError(t, stream.CloseSend())
			_, err = stream.Recv()
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

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
		SessionInformation:    nil,
	}.Build()))
	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func roundTripLifecycle(t *testing.T, lifecycle *uiv1.LifecycleEvent) *uiv1.LifecycleEvent {
	t.Helper()
	data, err := proto.Marshal(lifecycle)
	require.NoError(t, err)
	decoded := new(uiv1.LifecycleEvent)
	require.NoError(t, proto.Unmarshal(data, decoded))
	return decoded
}

// modelContentLifecycle builds a present model-content payload for discriminator boundary tests.
// modelContentLifecycle builds a valid nested model content lifecycle.
func modelContentLifecycle(
	outer uiv1.LifecycleType,
	nested uiv1.ModelContentType,
	kind uiv1.ModelContentKind,
) *uiv1.LifecycleEvent {
	var text *string
	if outer == uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA {
		text = new("")
	}
	return buildModelContentLifecycle(outer, nested, kind, text)
}

// modelContentLifecycleWithText builds a nested lifecycle with an explicit text field.
func modelContentLifecycleWithText(
	outer uiv1.LifecycleType,
	nested uiv1.ModelContentType,
	kind uiv1.ModelContentKind,
	text string,
) *uiv1.LifecycleEvent {
	return buildModelContentLifecycle(outer, nested, kind, new(text))
}

// buildModelContentLifecycle builds the shared generated lifecycle value.
func buildModelContentLifecycle(
	outer uiv1.LifecycleType,
	nested uiv1.ModelContentType,
	kind uiv1.ModelContentKind,
	text *string,
) *uiv1.LifecycleEvent {
	return uiv1.LifecycleEvent_builder{
		Type:            new(outer),
		RunId:           new("run"),
		Text:            nil,
		ToolCallId:      nil,
		ToolName:        nil,
		ProgressChannel: nil,
		IsError:         nil,
		Outcome:         nil,
		ErrorMessage:    nil,
		Availability:    nil,
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(nested),
			Position: new(int32(0)),
			Kind:     new(kind),
			Text:     text,
		}.Build(),
		ModelResponse:      nil,
		ToolCallPreview:    nil,
		FinalToolCall:      nil,
		ToolResultContents: nil,
	}.Build()
}

func messageEndLifecycle(t *testing.T, content []*uiv1.ModelResponseContent) *uiv1.LifecycleEvent {
	t.Helper()
	return roundTripLifecycle(t, uiv1.LifecycleEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END), RunId: new("run"), Text: nil,
		ToolCallId: nil, ToolName: nil, ProgressChannel: nil, IsError: nil, Outcome: nil,
		ErrorMessage: nil, Availability: nil, ModelContent: nil,
		ModelResponse: uiv1.ModelResponse_builder{
			Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
			ResponseId: nil, Usage: nil, Diagnostics: nil, Content: content, ResponseModel: nil,
		}.Build(),
		ToolCallPreview: nil, FinalToolCall: nil, ToolResultContents: nil,
	}.Build())
}

// TestMapSafeAuthenticationErrorEnablesManualRetry verifies retry state comes only from safe Host errors.
func TestMapSafeAuthenticationErrorEnablesManualRetry(t *testing.T) {
	t.Parallel()

	// Arrange a safe authentication error that permits manual retry.
	//nolint:exhaustruct // uiv1.OpenRequest_builder sets only the active Error field.
	request := uiv1.OpenRequest_builder{
		Error: uiv1.Error_builder{
			Text:                new("Authentication failed."),
			RetryAuthentication: new(true),
		}.Build(),
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
	}.Build()

	// Act by mapping the authentication error request.
	event, err := mapRequest(request)

	// Assert the event exposes retry availability and only the safe message.
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.Event{
		RestoredTranscript:   nil,
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
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
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
				Path:     new(""),
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
			SelectedUiId: new("glyph-tui"),
			SessionInfo:  testSessionInfo(),
		}.Build(),
		SessionList:        nil,
		SessionChanged:     nil,
		SessionInformation: nil,
	}.Build()
}

func testSessionInfo() *uiv1.SessionInfo {
	createdAt := timestamppb.New(time.Unix(1, 0).UTC())
	return uiv1.SessionInfo_builder{
		Id:               new("session"),
		WorkingDirectory: new("/project"),
		CreatedTime:      createdAt,
		UpdateTime:       createdAt, Name: nil, StoragePath: nil,
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
