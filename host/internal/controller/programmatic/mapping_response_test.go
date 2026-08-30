package programmatic

import (
	"bytes"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapResponsePreservesSessionPresence verifies absent optional session fields stay absent on the wire.
func TestMapResponsePreservesSessionPresence(t *testing.T) {
	t.Parallel()

	// Arrange session information without a name or storage path.
	info := session.Info{
		ID: "stored", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.None[string](), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}

	// Act by mapping information and list responses for the same session.
	mapped, err := mapResponse(Response{
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		CorrelationID:     "information", Kind: ResponseSessionInfo,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.Some(info), Sessions: nil,
		Rejection: mo.None[Rejection](), SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
	})
	// Assert optional presence and summary values survive protobuf mapping.
	require.NoError(t, err)
	wireInfo := mapped.GetCommandResponse().GetSessionInfo().GetInfo()
	assert.Equal(t, "stored", wireInfo.GetId())
	assert.False(t, wireInfo.HasName())
	assert.False(t, wireInfo.HasStoragePath())

	mapped, err = mapResponse(Response{
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		CorrelationID:     "list", Kind: ResponseSessions,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](),
		Sessions:  []session.Summary{{Info: info, FirstUserText: mo.Some("first"), TotalMessages: 2}},
		Rejection: mo.None[Rejection](), SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
	})
	require.NoError(t, err)
	rows := mapped.GetCommandResponse().GetSessions().GetSessions()
	require.Len(t, rows, 1)
	assert.Equal(t, "first", rows[0].GetFirstUserText())
	assert.Equal(t, int64(2), rows[0].GetTotalMessages())
}

// TestProgrammaticImageDataPresence verifies image data presence and ownership after wire serialization.
func TestProgrammaticImageDataPresence(t *testing.T) {
	t.Parallel()

	// Arrange image inputs for every observable data-presence state.
	tests := []struct {
		name        string
		data        mo.Option[[]byte]
		expectError bool
		expectData  []byte
	}{
		{name: "absent data", data: mo.None[[]byte](), expectError: true, expectData: nil},
		{name: "present nil data", data: mo.Some[[]byte](nil), expectError: false, expectData: []byte{}},
		{name: "present non-nil empty data", data: mo.Some([]byte{}), expectError: false, expectData: []byte{}},
		{name: "nonempty data", data: mo.Some([]byte{1, 2, 3}), expectError: false, expectData: []byte{1, 2, 3}},
	}

	for _, test := range tests {
		t.Run("user "+test.name, func(t *testing.T) {
			t.Parallel()
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			message := model.Message{Content: []model.InputContent{{
				Kind: model.InputContentImage, Text: mo.None[string](),
				MediaType: mo.Some("image/png"), Data: inputData,
			}}}

			// Act by mapping the user image to Programmatic protobuf.
			mapped, err := mapUserMessage(message)

			// Assert validation, oneof selection, data presence, bytes, and ownership.
			if test.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if source, present := inputData.Get(); present && len(source) != 0 {
				source[0] = 99
			}
			payload, err := proto.Marshal(mapped)
			require.NoError(t, err)
			roundTripped := new(programmaticv1.UserMessage)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetContent(), 1)
			content := roundTripped.GetContent()[0]
			assert.Equal(t, programmaticv1.UserContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})

		t.Run("tool result "+test.name, func(t *testing.T) {
			t.Parallel()
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			image := mo.None[ToolResultImage]()
			if data, present := inputData.Get(); present {
				image = mo.Some(ToolResultImage{MediaType: "image/png", Data: data})
			}
			// Act by mapping the tool-result image to Programmatic protobuf.
			mapped, err := mapToolResult(ToolResult{
				CallID: "call", ToolName: "render", IsError: false,
				Contents: []ToolResultContent{{
					Kind: ToolResultContentImage, Text: mo.None[string](), Image: image,
				}},
			})

			// Assert validation, oneof selection, data presence, bytes, and ownership.
			if test.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if source, present := inputData.Get(); present && len(source) != 0 {
				source[0] = 99
			}
			payload, err := proto.Marshal(mapped)
			require.NoError(t, err)
			roundTripped := new(programmaticv1.ToolResult)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetContents(), 1)
			content := roundTripped.GetContents()[0]
			assert.Equal(t, programmaticv1.ToolResultContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})
	}
}

// TestMapResponsePreservesEveryResult verifies every response oneof preserves complete public payloads.
func TestMapResponsePreservesEveryResult(t *testing.T) {
	t.Parallel()

	// Arrange one response for each command result and full history content.
	tests := map[string]Response{
		"accepted": {
			CorrelationID:     "accepted",
			Kind:              ResponseUserRequestAccepted,
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
		"aborted": {
			CorrelationID:     "aborted",
			Kind:              ResponseAbortCompleted,
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
		"state": {
			CorrelationID: "state",
			Kind:          ResponseRunState,
			State: mo.Some(RunStateResult{
				State:               RunStateRunning,
				ActiveCorrelationID: mo.Some("active"),
			}),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
		"messages": {
			CorrelationID: "messages",
			Kind:          ResponseMessages,
			Messages: []HistoryEntry{
				{
					Kind: HistoryEntryUser, User: mo.Some(model.Message{Content: []model.InputContent{
						{Kind: model.InputContentText, Text: mo.Some("user"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
						{Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some([]byte{1, 2})},
					}}),
					Model: mo.None[ModelResponse](), ToolResult: mo.None[ToolResult](),
				},
				{
					Kind: HistoryEntryModel, User: mo.None[model.Message](),
					Model: mo.Some(maximalModelResponse(mo.Some(""))), ToolResult: mo.None[ToolResult](),
				},
				{
					Kind: HistoryEntryToolResult, User: mo.None[model.Message](),
					Model: mo.None[ModelResponse](), ToolResult: mo.Some(maximalToolResult()),
				},
			},
			State:             mo.None[RunStateResult](),
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
		"rejected": {
			CorrelationID: "rejected",
			Kind:          ResponseRejected,
			Rejection: mo.Some(Rejection{
				Command: CommandUnspecified,
				Code:    RejectionInvalidArgument,
				Message: "invalid",
			}),
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Selection:         mo.None[model.Selection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
		"models": {
			CorrelationID: "models",
			Kind:          ResponseModels,
			Models: mo.Some(ModelsResult{
				Models: []model.Descriptor{{
					Provider:      "provider",
					Model:         "model",
					Input:         []model.InputModality{model.InputModalityText},
					ContextWindow: 131072,
					MaxTokens:     16384,
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: true,
						Choices: []model.ReasoningChoice{
							model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
							model.ReasoningChoiceMedium, model.ReasoningChoiceHigh,
							model.ReasoningChoiceXHigh, model.ReasoningChoiceMax,
						},
						Default: model.ReasoningChoiceHigh,
					},
					ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
				}, {
					Provider:      "ollama",
					Model:         "ornith",
					Input:         []model.InputModality{model.InputModalityText, model.InputModalityImage},
					ContextWindow: 262144,
					MaxTokens:     32768,
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: true,
						Choices:   []model.ReasoningChoice{model.ReasoningChoiceOn},
						Default:   model.ReasoningChoiceOn,
					},
					ToolCapabilities: model.ToolCapabilities{}, Pricing: mo.None[model.Pricing](),
				}},
				ActiveSelection: mo.Some(model.Selection{
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: model.ReasoningChoiceHigh,
				}),
			}),
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Selection:         mo.None[model.Selection](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
		"model selection": {
			CorrelationID: "selection",
			Kind:          ResponseModelSelection,
			Selection: mo.Some(model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceMax,
			}),
			State:             mo.None[RunStateResult](),
			Messages:          nil,
			Models:            mo.None[ModelsResult](),
			Rejection:         mo.None[Rejection](),
			SessionInfo:       mo.None[session.Info](),
			SessionEntries:    nil,
			SessionStatistics: mo.None[session.Statistics](),
			Sessions:          nil,
			SessionTree:       mo.None[SessionTree](),
			TreeNavigation:    mo.None[TreeNavigationResult](),
		},
	}

	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Act by mapping the internal response to protobuf.
			got, err := mapResponse(response)

			// Assert correlation, selected oneof, nested content, and field presence.
			require.NoError(t, err)
			assert.Equal(t, response.CorrelationID, got.GetCorrelationId())
			wire := got.GetCommandResponse()
			require.NotNil(t, wire)
			switch response.Kind {
			case ResponseUserRequestAccepted:
				assert.True(t, wire.HasUserRequestAccepted())
			case ResponseAbortCompleted:
				assert.True(t, wire.HasAbortCompleted())
			case ResponseRunState:
				assert.Equal(t, programmaticv1.RunState_RUN_STATE_RUNNING, wire.GetRunState().GetState())
				assert.Equal(t, "active", wire.GetRunState().GetActiveCorrelationId())
			case ResponseMessages:
				entries := wire.GetMessages().GetEntries()
				require.Len(t, entries, 3)
				require.Len(t, entries[0].GetUser().GetContent(), 2)
				assert.Equal(t, "user", entries[0].GetUser().GetContent()[0].GetText())
				assert.Equal(t, "image/png", entries[0].GetUser().GetContent()[1].GetImage().GetMediaType())
				assert.Equal(t, []byte{1, 2}, entries[0].GetUser().GetContent()[1].GetImage().GetData())
				modelEntry := entries[1].GetModel()
				assertModelResponse(t, modelEntry, true)
				assertToolResult(t, entries[2].GetToolResult())
			case ResponseRejected:
				rejected := wire.GetRejected()
				assert.True(t, rejected.HasCommand())
				assert.Equal(t, programmaticv1.CommandType_COMMAND_TYPE_UNSPECIFIED, rejected.GetCommand())
				assert.Equal(t, programmaticv1.RejectionCode_REJECTION_CODE_INVALID_ARGUMENT, rejected.GetCode())
				assert.Equal(t, "invalid", rejected.GetMessage())
			case ResponseModels:
				models := wire.GetModels()
				require.Len(t, models.GetModels(), 2)
				assert.Equal(t, "provider", models.GetModels()[0].GetProviderId())
				assert.Equal(t, "model", models.GetModels()[0].GetModelId())
				assert.Equal(t, []programmaticv1.InputModality{
					programmaticv1.InputModality_INPUT_MODALITY_TEXT,
				}, models.GetModels()[0].GetInputModalities())
				assert.Equal(t, int64(131072), models.GetModels()[0].GetContextWindow())
				assert.Equal(t, int64(16384), models.GetModels()[0].GetMaxTokens())
				assert.Equal(t, []programmaticv1.ReasoningChoice{
					programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
					programmaticv1.ReasoningChoice_REASONING_CHOICE_MINIMAL,
					programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW,
					programmaticv1.ReasoningChoice_REASONING_CHOICE_MEDIUM,
					programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH,
					programmaticv1.ReasoningChoice_REASONING_CHOICE_XHIGH,
					programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX,
				}, models.GetModels()[0].GetReasoning().GetChoices())
				assert.True(t, models.GetModels()[0].GetReasoning().GetSupported())
				assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, models.GetModels()[0].GetReasoning().GetDefaultChoice())
				assert.Equal(t, []programmaticv1.InputModality{
					programmaticv1.InputModality_INPUT_MODALITY_TEXT,
					programmaticv1.InputModality_INPUT_MODALITY_IMAGE,
				}, models.GetModels()[1].GetInputModalities())
				assert.Equal(t, int64(262144), models.GetModels()[1].GetContextWindow())
				assert.Equal(t, int64(32768), models.GetModels()[1].GetMaxTokens())
				assert.Equal(t, []programmaticv1.ReasoningChoice{
					programmaticv1.ReasoningChoice_REASONING_CHOICE_ON,
				}, models.GetModels()[1].GetReasoning().GetChoices())
				assert.True(t, models.GetModels()[1].GetReasoning().GetSupported())
				assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_ON, models.GetModels()[1].GetReasoning().GetDefaultChoice())
				assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, models.GetActiveSelection().GetReasoningChoice())
			case ResponseModelSelection:
				selection := wire.GetModelSelection().GetSelection()
				assert.Equal(t, "provider", selection.GetProviderId())
				assert.Equal(t, "model", selection.GetModelId())
				assert.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX, selection.GetReasoningChoice())
			case ResponseUnspecified, ResponseSessionInfo, ResponseSessions, ResponseSessionEntries, ResponseSessionStats,
				ResponseSessionTree, ResponseSessionTreeNavigation:
				require.Fail(t, "unexpected response kind")
			}
		})
	}

	mapped, err := mapResponse(Response{
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		CorrelationID:     "absent",
		Kind:              ResponseMessages,
		Messages: []HistoryEntry{{
			Kind: HistoryEntryModel,
			Model: mo.Some(ModelResponse{
				Outcome:       mo.Some(ModelOutcomeStop),
				Text:          "",
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[string](),
				Model:         mo.None[string](),
				ResponseModel: mo.None[string](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[ModelUsage](),
				Diagnostics:   nil,
				Content:       nil,
			}),
			User: mo.None[model.Message](), ToolResult: mo.None[ToolResult](),
		}},
		State:       mo.None[RunStateResult](),
		Models:      mo.None[ModelsResult](),
		Selection:   mo.None[model.Selection](),
		Rejection:   mo.None[Rejection](),
		SessionInfo: mo.None[session.Info](),
		Sessions:    nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
	})
	require.NoError(t, err)
	modelResponse := mapped.GetCommandResponse().GetMessages().GetEntries()[0].GetModel()
	assert.False(t, modelResponse.HasErrorMessage())
	assert.False(t, modelResponse.HasProvider())
	assert.False(t, modelResponse.HasModel())
	assert.False(t, modelResponse.HasResponseModel())
	assert.False(t, modelResponse.HasResponseId())
	assert.False(t, modelResponse.HasUsage())

	mapped, err = mapResponse(Response{
		SessionEntries:    nil,
		SessionStatistics: mo.None[session.Statistics](),
		CorrelationID:     "idle",
		Kind:              ResponseRunState,
		State: mo.Some(RunStateResult{
			State:               RunStateIdle,
			ActiveCorrelationID: mo.None[string](),
		}),
		Messages:    nil,
		Models:      mo.None[ModelsResult](),
		Selection:   mo.None[model.Selection](),
		Rejection:   mo.None[Rejection](),
		SessionInfo: mo.None[session.Info](),
		Sessions:    nil, SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
	})
	require.NoError(t, err)
	assert.False(t, mapped.GetCommandResponse().GetRunState().HasActiveCorrelationId())
}
