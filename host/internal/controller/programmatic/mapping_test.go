package programmatic

import (
	"bytes"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

func TestMapResponsePreservesSessionPresence(t *testing.T) {
	t.Parallel()

	info := session.Info{
		ID: "stored", Name: mo.None[string](), WorkingDirectory: "/project",
		StoragePath: mo.None[string](), CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
	mapped, err := mapResponse(Response{
		SessionEntries: nil,
		CorrelationID:  "information", Kind: ResponseSessionInfo,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.Some(info), Sessions: nil,
		Rejection: mo.None[Rejection](),
	})
	require.NoError(t, err)
	wireInfo := mapped.GetCommandResponse().GetSessionInfo().GetInfo()
	assert.Equal(t, "stored", wireInfo.GetId())
	assert.False(t, wireInfo.HasName())
	assert.False(t, wireInfo.HasStoragePath())

	mapped, err = mapResponse(Response{
		SessionEntries: nil,
		CorrelationID:  "list", Kind: ResponseSessions,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](),
		Sessions:  []session.Summary{{Info: info, FirstUserText: mo.Some("first"), TotalMessages: 2}},
		Rejection: mo.None[Rejection](),
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
			CorrelationID:  "accepted",
			Kind:           ResponseUserRequestAccepted,
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
		},
		"aborted": {
			CorrelationID:  "aborted",
			Kind:           ResponseAbortCompleted,
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
		},
		"state": {
			CorrelationID: "state",
			Kind:          ResponseRunState,
			State: mo.Some(RunStateResult{
				State:               RunStateRunning,
				ActiveCorrelationID: mo.Some("active"),
			}),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
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
			State:          mo.None[RunStateResult](),
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
		},
		"rejected": {
			CorrelationID: "rejected",
			Kind:          ResponseRejected,
			Rejection: mo.Some(Rejection{
				Command: CommandUnspecified,
				Code:    RejectionInvalidArgument,
				Message: "invalid",
			}),
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Selection:      mo.None[model.Selection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
		},
		"models": {
			CorrelationID: "models",
			Kind:          ResponseModels,
			Models: mo.Some(ModelsResult{
				Models: []model.Descriptor{{
					Provider: "provider",
					Model:    "model",
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: true,
						Choices: []model.ReasoningChoice{
							model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
							model.ReasoningChoiceMedium, model.ReasoningChoiceHigh,
							model.ReasoningChoiceXHigh, model.ReasoningChoiceMax,
						},
						Default: model.ReasoningChoiceHigh,
					},
					ToolCapabilities: model.ToolCapabilities{},
				}, {
					Provider: "ollama",
					Model:    "ornith",
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: true,
						Choices:   []model.ReasoningChoice{model.ReasoningChoiceOn},
						Default:   model.ReasoningChoiceOn,
					},
					ToolCapabilities: model.ToolCapabilities{},
				}},
				ActiveSelection: mo.Some(model.Selection{
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: model.ReasoningChoiceHigh,
				}),
			}),
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Selection:      mo.None[model.Selection](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
		},
		"model selection": {
			CorrelationID: "selection",
			Kind:          ResponseModelSelection,
			Selection: mo.Some(model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceMax,
			}),
			State:          mo.None[RunStateResult](),
			Messages:       nil,
			Models:         mo.None[ModelsResult](),
			Rejection:      mo.None[Rejection](),
			SessionInfo:    mo.None[session.Info](),
			SessionEntries: nil,
			Sessions:       nil,
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
			case ResponseUnspecified, ResponseSessionInfo, ResponseSessions, ResponseSessionEntries:
				require.Fail(t, "unexpected response kind")
			}
		})
	}

	mapped, err := mapResponse(Response{
		SessionEntries: nil,
		CorrelationID:  "absent",
		Kind:           ResponseMessages,
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
		Sessions:    nil,
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
		SessionEntries: nil,
		CorrelationID:  "idle",
		Kind:           ResponseRunState,
		State: mo.Some(RunStateResult{
			State:               RunStateIdle,
			ActiveCorrelationID: mo.None[string](),
		}),
		Messages:    nil,
		Models:      mo.None[ModelsResult](),
		Selection:   mo.None[model.Selection](),
		Rejection:   mo.None[Rejection](),
		SessionInfo: mo.None[session.Info](),
		Sessions:    nil,
	})
	require.NoError(t, err)
	assert.False(t, mapped.GetCommandResponse().GetRunState().HasActiveCorrelationId())
}

// TestMapEventPreservesEveryEvent verifies every event enum and payload oneof.
func TestMapEventPreservesEveryEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typeValue AgentEventType
		payload   string
		event     AgentEvent
	}{
		{
			typeValue: AgentEventAgentStart,
			payload:   "",
			event:     AgentEvent{},
		},
		{
			typeValue: AgentEventTurnStart,
			payload:   "",
			event:     AgentEvent{},
		},
		{
			typeValue: AgentEventMessageStart,
			payload:   "",
			event:     AgentEvent{},
		},
		{
			typeValue: AgentEventModelContentStart,
			payload:   "model_content",
			event: AgentEvent{
				ModelContent: mo.Some(ModelContent{
					Kind:     ModelContentReasoning,
					Position: 2,
					Text:     mo.None[string](),
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventModelTextDelta,
			payload:   "model_content",
			event: AgentEvent{
				ModelContent: mo.Some(ModelContent{
					Kind:     ModelContentText,
					Position: 1,
					Text:     mo.Some("delta"),
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventModelContentEnd,
			payload:   "model_content",
			event: AgentEvent{
				ModelContent: mo.Some(ModelContent{
					Kind:     ModelContentRefusal,
					Position: 3,
					Text:     mo.None[string](),
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolCallStart,
			payload:   "tool_call_preview",
			event: AgentEvent{
				ToolCallPreview: mo.Some(maximalToolCallPreview()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolCallDelta,
			payload:   "tool_call_preview",
			event: AgentEvent{
				ToolCallPreview: mo.Some(maximalToolCallPreview()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolCallEnd,
			payload:   "final_tool_call",
			event: AgentEvent{
				FinalToolCall:   mo.Some(maximalFinalToolCall()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventMessageEnd,
			payload:   "model_response",
			event: AgentEvent{
				ModelResponse:   mo.Some(maximalModelResponse(mo.Some(""))),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolExecutionStart,
			payload:   "tool_execution",
			event: AgentEvent{
				ToolExecution: mo.Some(ToolExecution{
					CallID:   "call",
					ToolName: "tool",
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolExecutionUpdate,
			payload:   "tool_progress",
			event: AgentEvent{
				ToolProgress: mo.Some(ToolProgress{
					Channel: ProgressChannelStderr,
					Content: "progress",
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolExecutionEnd,
			payload:   "tool_result",
			event: AgentEvent{
				ToolResult:      mo.Some(maximalToolResult()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventToolResult,
			payload:   "tool_result",
			event: AgentEvent{
				ToolResult:      mo.Some(maximalToolResult()),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventTurnEnd,
			payload:   "turn",
			event: AgentEvent{
				Turn: mo.Some(TurnSummary{
					Response:    maximalModelResponse(mo.Some("")),
					ToolResults: []ToolResult{maximalToolResult()},
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Agent:           mo.None[AgentSummary](),
			},
		},
		{
			typeValue: AgentEventAgentEnd,
			payload:   "agent",
			event: AgentEvent{
				Agent: mo.Some(AgentSummary{
					Outcome:      RunOutcomeFailed,
					ErrorMessage: mo.Some("failed"),
				}),
				CorrelationID:   "",
				Type:            0,
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
			},
		},
		{
			typeValue: AgentEventAgentSettled,
			payload:   "",
			event:     AgentEvent{},
		},
	}

	for _, test := range tests {
		t.Run(programmaticv1.AgentEventType(test.typeValue).String(), func(t *testing.T) {
			t.Parallel()
			test.event.Type = test.typeValue
			test.event.CorrelationID = "correlation"
			test.event.RunID = "run"
			got, err := mapEvent(test.event)
			require.NoError(t, err)
			assert.Equal(t, "correlation", got.GetCorrelationId())
			event := got.GetAgentEvent()
			assert.Equal(t, programmaticv1.AgentEventType(test.typeValue), event.GetType())
			assert.Equal(t, "run", event.GetRunId())
			if test.payload == "" {
				assert.False(t, event.HasPayload())
			} else {
				assert.Equal(t, test.payload, event.WhichPayload().String())
			}
			assertEventPayload(t, test.payload, event)
		})
	}
}

// TestMappingRejectsInvalidValues verifies closed unions and malformed JSON values.
func TestMappingRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	// Arrange test dependencies and scenario inputs.
	tests := map[string]func() error{
		"response": func() error {
			_, err := mapResponse(Response{
				SessionEntries: nil,
				Kind:           ResponseUnspecified,
				CorrelationID:  "",
				State:          mo.None[RunStateResult](),
				Messages:       nil,
				Models:         mo.None[ModelsResult](),
				Selection:      mo.None[model.Selection](),
				Rejection:      mo.None[Rejection](),
				SessionInfo:    mo.None[session.Info](),
				Sessions:       nil,
			})
			return err
		},
		"history": func() error {
			_, err := mapResponse(Response{
				SessionEntries: nil,
				Kind:           ResponseMessages,
				Messages: []HistoryEntry{{
					Kind: HistoryEntryUnspecified, User: mo.None[model.Message](),
					Model:      mo.None[ModelResponse](),
					ToolResult: mo.None[ToolResult](),
				}},
				CorrelationID: "",
				State:         mo.None[RunStateResult](),
				Models:        mo.None[ModelsResult](),
				Selection:     mo.None[model.Selection](),
				Rejection:     mo.None[Rejection](),
				SessionInfo:   mo.None[session.Info](),
				Sessions:      nil,
			})
			return err
		},
		"event": func() error {
			_, err := mapEvent(AgentEvent{
				Type:            AgentEventUnspecified,
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"content": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventModelContentStart,
				ModelContent: mo.Some(ModelContent{
					Kind:     ModelContentUnspecified,
					Position: 0,
					Text:     mo.None[string](),
				}),
				CorrelationID:   "",
				RunID:           "",
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"preview kind": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolCallStart,
				ToolCallPreview: mo.Some(ToolCallPreview{
					Fields: []ToolCallPreviewField{{
						Kind:   ToolCallPreviewFieldUnspecified,
						Name:   "",
						Value:  mo.None[any](),
						Prefix: mo.None[string](),
					}},
					CallID:      "",
					Name:        "",
					Position:    0,
					Provisional: false,
				}),
				CorrelationID: "",
				RunID:         "",
				ModelContent:  mo.None[ModelContent](),
				FinalToolCall: mo.None[FinalToolCall](),
				ToolExecution: mo.None[ToolExecution](),
				ToolProgress:  mo.None[ToolProgress](),
				ToolResult:    mo.None[ToolResult](),
				ModelResponse: mo.None[ModelResponse](),
				Turn:          mo.None[TurnSummary](),
				Agent:         mo.None[AgentSummary](),
			})
			return err
		},
		"preview JSON": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolCallStart,
				ToolCallPreview: mo.Some(ToolCallPreview{
					Fields: []ToolCallPreviewField{{
						Kind:   ToolCallPreviewFieldComplete,
						Value:  mo.Some[any](make(chan int)),
						Name:   "",
						Prefix: mo.None[string](),
					}},
					CallID:      "",
					Name:        "",
					Position:    0,
					Provisional: false,
				}),
				CorrelationID: "",
				RunID:         "",
				ModelContent:  mo.None[ModelContent](),
				FinalToolCall: mo.None[FinalToolCall](),
				ToolExecution: mo.None[ToolExecution](),
				ToolProgress:  mo.None[ToolProgress](),
				ToolResult:    mo.None[ToolResult](),
				ModelResponse: mo.None[ModelResponse](),
				Turn:          mo.None[TurnSummary](),
				Agent:         mo.None[AgentSummary](),
			})
			return err
		},
		"progress": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolExecutionUpdate,
				ToolProgress: mo.Some(ToolProgress{
					Channel: ProgressChannelUnspecified,
					Content: "",
				}),
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"tool result": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventToolResult,
				ToolResult: mo.Some(ToolResult{
					Contents: []ToolResultContent{{
						Kind:  ToolResultContentUnspecified,
						Text:  mo.None[string](),
						Image: mo.None[ToolResultImage](),
					}},
					CallID:   "",
					ToolName: "",
					IsError:  false,
				}),
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"model outcome": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventMessageEnd,
				ModelResponse: mo.Some(ModelResponse{
					Outcome:       mo.Some(ModelOutcomeUnspecified),
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
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"model content": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventMessageEnd,
				ModelResponse: mo.Some(ModelResponse{
					Outcome: mo.Some(ModelOutcomeStop),
					Content: []ModelResponseContent{{
						Kind:     ModelResponseContentUnspecified,
						Text:     mo.None[string](),
						ToolCall: mo.None[FinalToolCall](),
					}},
					Text:          "",
					ErrorMessage:  mo.None[string](),
					Provider:      mo.None[string](),
					Model:         mo.None[string](),
					ResponseModel: mo.None[string](),
					ResponseID:    mo.None[string](),
					Usage:         mo.None[ModelUsage](),
					Diagnostics:   nil,
				}),
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				Turn:            mo.None[TurnSummary](),
				Agent:           mo.None[AgentSummary](),
			})
			return err
		},
		"run outcome": func() error {
			_, err := mapEvent(AgentEvent{
				Type: AgentEventAgentEnd,
				Agent: mo.Some(AgentSummary{
					Outcome:      RunOutcomeUnspecified,
					ErrorMessage: mo.None[string](),
				}),
				CorrelationID:   "",
				RunID:           "",
				ModelContent:    mo.None[ModelContent](),
				ToolCallPreview: mo.None[ToolCallPreview](),
				FinalToolCall:   mo.None[FinalToolCall](),
				ToolExecution:   mo.None[ToolExecution](),
				ToolProgress:    mo.None[ToolProgress](),
				ToolResult:      mo.None[ToolResult](),
				ModelResponse:   mo.None[ModelResponse](),
				Turn:            mo.None[TurnSummary](),
			})
			return err
		},
	}
	// Act by executing the scenario.
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Assert the scenario produces the required observable result.
			assert.Error(t, test())
		})
	}
}

// TestMapEventRejectsMissingSelectedPayload verifies malformed selected events fail before serialization.
func TestMapEventRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	_, err := mapEvent(AgentEvent{
		CorrelationID:   "correlation",
		Type:            AgentEventToolExecutionStart,
		RunID:           "run",
		ModelContent:    mo.None[ModelContent](),
		ToolCallPreview: mo.None[ToolCallPreview](),
		FinalToolCall:   mo.None[FinalToolCall](),
		ToolExecution:   mo.None[ToolExecution](),
		ToolProgress:    mo.None[ToolProgress](),
		ToolResult:      mo.None[ToolResult](),
		ModelResponse:   mo.None[ModelResponse](),
		Turn:            mo.None[TurnSummary](),
		Agent:           mo.None[AgentSummary](),
	})

	assert.Error(t, err)
}

// TestMappingRejectsMissingNestedAlternatives verifies nested discriminators require their selected payloads.
func TestMappingRejectsMissingNestedAlternatives(t *testing.T) {
	t.Parallel()

	tests := map[string]func() error{
		"model text": func() error {
			_, err := mapModelContent(ModelContent{
				Kind: ModelContentText, Position: 0, Text: mo.None[string](),
			}, true)
			return err
		},
		"preview value": func() error {
			_, err := mapToolCallPreview(ToolCallPreview{
				CallID: "call", Name: "tool", Position: 0, Provisional: true,
				Fields: []ToolCallPreviewField{{
					Name: "field", Kind: ToolCallPreviewFieldComplete,
					Value: mo.None[any](), Prefix: mo.None[string](),
				}},
			})
			return err
		},
		"tool result text": func() error {
			_, err := mapToolResult(ToolResult{
				CallID: "call", ToolName: "tool", IsError: false,
				Contents: []ToolResultContent{{
					Kind: ToolResultContentText, Text: mo.None[string](), Image: mo.None[ToolResultImage](),
				}},
			})
			return err
		},
		"model response tool call": func() error {
			_, err := mapModelResponse(ModelResponse{
				Text: "", Outcome: mo.Some(ModelOutcomeToolUse), ErrorMessage: mo.None[string](),
				Provider: mo.None[string](), Model: mo.None[string](), ResponseModel: mo.None[string](),
				ResponseID: mo.None[string](), Usage: mo.None[ModelUsage](), Diagnostics: nil,
				Content: []ModelResponseContent{{
					Kind: ModelResponseContentToolCall, Text: mo.None[string](),
					ToolCall: mo.None[FinalToolCall](),
				}},
			})
			return err
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, test())
		})
	}
}

// TestMapModelContentPreservesPresentZeroValues verifies zero position and empty text remain present values.
func TestMapModelContentPreservesPresentZeroValues(t *testing.T) {
	t.Parallel()

	mapped, err := mapModelContent(ModelContent{
		Kind: ModelContentText, Position: 0, Text: mo.Some(""),
	}, true)
	require.NoError(t, err)
	assert.Equal(t, int32(0), mapped.GetPosition())
	assert.Empty(t, mapped.GetText())
}

// TestApprovedEnumValuesMapExactly verifies all approved nonzero enum values.
func TestApprovedEnumValuesMapExactly(t *testing.T) {
	t.Parallel()

	for index, value := range []CommandKind{
		CommandUnspecified, CommandUserRequest, CommandAbort, CommandGetRunState, CommandGetMessages,
		CommandGetModels, CommandSelectModel, CommandSelectReasoningChoice,
	} {
		mapped, err := mapCommandType(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.CommandType(index), mapped)
	}
	for index, value := range []RejectionCode{
		RejectionInvalidArgument, RejectionBusy, RejectionNoActiveRun, RejectionCorrelationInUse,
		RejectionInternal, RejectionNotFound, RejectionReasoningUnsupported, RejectionCredentialUnavailable,
	} {
		mapped, err := mapRejectionCode(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.RejectionCode(index+1), mapped)
	}
	for index, value := range []RunState{RunStateIdle, RunStateRunning} {
		mapped, err := mapRunState(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.RunState(index+1), mapped)
	}
	for index, value := range []ModelContentKind{ModelContentText, ModelContentReasoning, ModelContentRefusal} {
		mapped, err := mapModelContentKind(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.ModelContentKind(index+1), mapped)
	}
	for index, value := range []ProgressChannel{ProgressChannelStatus, ProgressChannelStdout, ProgressChannelStderr} {
		mapped, err := mapProgressChannel(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.ProgressChannel(index+1), mapped)
	}
	for index, value := range []ModelOutcome{ModelOutcomeStop, ModelOutcomeToolUse, ModelOutcomeLength, ModelOutcomeAborted, ModelOutcomeFailed} {
		mapped, err := mapModelOutcome(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.ModelOutcome(index+1), mapped)
	}
	for index, value := range []RunOutcome{RunOutcomeCompleted, RunOutcomeAborted, RunOutcomeFailed} {
		mapped, err := mapRunOutcome(value)
		require.NoError(t, err)
		assert.Equal(t, programmaticv1.RunOutcome(index+1), mapped)
	}
}

func maximalToolCallPreview() ToolCallPreview {
	return ToolCallPreview{
		CallID:      "call",
		Name:        "tool",
		Position:    4,
		Provisional: true,
		Fields: []ToolCallPreviewField{
			{
				Name:   "null",
				Kind:   ToolCallPreviewFieldComplete,
				Value:  mo.Some[any](nil),
				Prefix: mo.None[string](),
			},
			{
				Name:   "prefix",
				Kind:   ToolCallPreviewFieldPrefix,
				Prefix: mo.Some(""),
				Value:  mo.None[any](),
			},
		},
	}
}

func maximalFinalToolCall() FinalToolCall {
	return FinalToolCall{
		CallID:    "call",
		Name:      "tool",
		Position:  4,
		Arguments: map[string]any{"null": nil, "array": []any{"value", float64(2)}},
	}
}

func maximalToolResult() ToolResult {
	return ToolResult{
		CallID:   "call",
		ToolName: "tool",
		IsError:  true,
		Contents: []ToolResultContent{
			{
				Kind:  ToolResultContentText,
				Text:  mo.Some(""),
				Image: mo.None[ToolResultImage](),
			},
			{
				Kind: ToolResultContentImage,
				Image: mo.Some(ToolResultImage{
					MediaType: "image/png",
					Data:      []byte{0, 1, 255},
				}),
				Text: mo.None[string](),
			},
		},
	}
}

func maximalModelResponse(responseModel mo.Option[string]) ModelResponse {
	return ModelResponse{
		Text:          "text",
		Outcome:       mo.Some(ModelOutcomeToolUse),
		ErrorMessage:  mo.Some("error"),
		Provider:      mo.Some("provider"),
		Model:         mo.Some("model"),
		ResponseModel: responseModel,
		ResponseID:    mo.Some("response"),
		Usage: mo.Some(ModelUsage{
			InputTokens:       1,
			OutputTokens:      2,
			CachedInputTokens: 3,
			CacheWriteTokens:  4,
			ReasoningTokens:   5,
			TotalTokens:       6,
		}),
		Diagnostics: []ModelDiagnostic{{
			Code:    "code",
			Message: "message",
		}},
		Content: []ModelResponseContent{
			{
				Kind:     ModelResponseContentText,
				Text:     mo.Some("text"),
				ToolCall: mo.None[FinalToolCall](),
			},
			{
				Kind:     ModelResponseContentRefusal,
				Text:     mo.Some("refusal"),
				ToolCall: mo.None[FinalToolCall](),
			},
			{
				Kind:     ModelResponseContentReasoning,
				Text:     mo.Some("reasoning"),
				ToolCall: mo.None[FinalToolCall](),
			},
			{
				Kind:     ModelResponseContentToolCall,
				ToolCall: mo.Some(maximalFinalToolCall()),
				Text:     mo.None[string](),
			},
		},
	}
}

func assertEventPayload(t *testing.T, kind string, event *programmaticv1.AgentEvent) {
	t.Helper()
	switch kind {
	case "":
		assert.False(t, event.HasPayload())
	case "model_content":
		content := event.GetModelContent()
		assert.NotEqual(t, programmaticv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED, content.GetKind())
		assert.GreaterOrEqual(t, content.GetPosition(), int32(1))
	case "tool_call_preview":
		preview := event.GetToolCallPreview()
		assert.Equal(t, "call", preview.GetCallId())
		assert.Equal(t, "tool", preview.GetName())
		assert.Equal(t, int32(4), preview.GetPosition())
		assert.True(t, preview.GetProvisional())
		fields := preview.GetFields()
		require.Len(t, fields, 2)
		assert.Equal(t, structpb.NullValue_NULL_VALUE, fields[0].GetValue().GetNullValue())
		assert.Equal(t, programmaticv1.ToolCallPreviewField_Prefix_case, fields[1].WhichContent())
		assert.Empty(t, fields[1].GetPrefix())
	case "final_tool_call":
		assertFinalToolCall(t, event.GetFinalToolCall())
	case "tool_execution":
		assert.Equal(t, "call", event.GetToolExecution().GetCallId())
		assert.Equal(t, "tool", event.GetToolExecution().GetToolName())
	case "tool_progress":
		assert.Equal(t, programmaticv1.ProgressChannel_PROGRESS_CHANNEL_STDERR, event.GetToolProgress().GetChannel())
		assert.Equal(t, "progress", event.GetToolProgress().GetContent())
	case "tool_result":
		assertToolResult(t, event.GetToolResult())
	case "model_response":
		assertModelResponse(t, event.GetModelResponse(), true)
	case "turn":
		assertModelResponse(t, event.GetTurn().GetResponse(), true)
		require.Len(t, event.GetTurn().GetToolResults(), 1)
		assertToolResult(t, event.GetTurn().GetToolResults()[0])
	case "agent":
		assert.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_FAILED, event.GetAgent().GetOutcome())
		assert.Equal(t, "failed", event.GetAgent().GetErrorMessage())
	default:
		require.Fail(t, "unknown payload", kind)
	}
}

func assertFinalToolCall(t *testing.T, call *programmaticv1.FinalToolCall) {
	t.Helper()
	assert.Equal(t, "call", call.GetCallId())
	assert.Equal(t, "tool", call.GetName())
	assert.Equal(t, int32(4), call.GetPosition())
	arguments := call.GetArguments().AsMap()
	assert.Contains(t, arguments, "null")
	assert.Nil(t, arguments["null"])
	assert.Equal(t, []any{"value", float64(2)}, arguments["array"])
}

func assertToolResult(t *testing.T, result *programmaticv1.ToolResult) {
	t.Helper()
	assert.Equal(t, "call", result.GetCallId())
	assert.Equal(t, "tool", result.GetToolName())
	assert.True(t, result.GetIsError())
	require.Len(t, result.GetContents(), 2)
	assert.Equal(t, programmaticv1.ToolResultContent_Text_case, result.GetContents()[0].WhichContent())
	assert.Empty(t, result.GetContents()[0].GetText())
	assert.Equal(t, programmaticv1.ToolResultContent_Image_case, result.GetContents()[1].WhichContent())
	assert.Equal(t, "image/png", result.GetContents()[1].GetImage().GetMediaType())
	assert.Equal(t, []byte{0, 1, 255}, result.GetContents()[1].GetImage().GetData())
}

func assertModelResponse(t *testing.T, response *programmaticv1.ModelResponse, hasResponseModel bool) {
	t.Helper()
	assert.Equal(t, "text", response.GetText())
	assert.Equal(t, programmaticv1.ModelOutcome_MODEL_OUTCOME_TOOL_USE, response.GetOutcome())
	assert.True(t, response.HasErrorMessage())
	assert.Equal(t, "error", response.GetErrorMessage())
	assert.True(t, response.HasProvider())
	assert.Equal(t, "provider", response.GetProvider())
	assert.True(t, response.HasModel())
	assert.Equal(t, "model", response.GetModel())
	assert.Equal(t, hasResponseModel, response.HasResponseModel())
	assert.Empty(t, response.GetResponseModel())
	assert.True(t, response.HasResponseId())
	assert.Equal(t, "response", response.GetResponseId())
	assert.True(t, response.HasUsage())
	assert.Equal(t, int64(1), response.GetUsage().GetInputTokens())
	assert.Equal(t, int64(2), response.GetUsage().GetOutputTokens())
	assert.Equal(t, int64(3), response.GetUsage().GetCachedInputTokens())
	assert.Equal(t, int64(4), response.GetUsage().GetCacheWriteTokens())
	assert.Equal(t, int64(5), response.GetUsage().GetReasoningTokens())
	assert.Equal(t, int64(6), response.GetUsage().GetTotalTokens())
	require.Len(t, response.GetDiagnostics(), 1)
	assert.Equal(t, "code", response.GetDiagnostics()[0].GetCode())
	assert.Equal(t, "message", response.GetDiagnostics()[0].GetMessage())
	require.Len(t, response.GetContent(), 4)
	assert.Equal(t, programmaticv1.ModelResponseItem_Text_case, response.GetContent()[0].WhichContent())
	assert.Equal(t, "text", response.GetContent()[0].GetText().GetText())
	assert.Equal(t, programmaticv1.ModelResponseItem_Refusal_case, response.GetContent()[1].WhichContent())
	assert.Equal(t, "refusal", response.GetContent()[1].GetRefusal().GetText())
	assert.Equal(t, programmaticv1.ModelResponseItem_Reasoning_case, response.GetContent()[2].WhichContent())
	assert.Equal(t, "reasoning", response.GetContent()[2].GetReasoning().GetText())
	assert.Equal(t, programmaticv1.ModelResponseItem_ToolCall_case, response.GetContent()[3].WhichContent())
	assertFinalToolCall(t, response.GetContent()[3].GetToolCall())
}
