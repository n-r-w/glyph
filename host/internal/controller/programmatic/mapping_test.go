//nolint:exhaustruct // Tests set only fields used by each union variant.
package programmatic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapResponsePreservesEveryResult verifies every response and history oneof.
func TestMapResponsePreservesEveryResult(t *testing.T) {
	t.Parallel()

	responseModel := ""
	tests := map[string]Response{
		"accepted": {CorrelationID: "accepted", Kind: ResponseUserRequestAccepted},
		"aborted":  {CorrelationID: "aborted", Kind: ResponseAbortCompleted},
		"state": {
			CorrelationID: "state", Kind: ResponseRunState,
			State: RunStateResult{State: RunStateRunning, ActiveCorrelationID: "active"},
		},
		"messages": {
			CorrelationID: "messages", Kind: ResponseMessages,
			Messages: []HistoryEntry{
				{Kind: HistoryEntryUser, UserText: "user"},
				{Kind: HistoryEntryModel, Model: maximalModelResponse(&responseModel)},
				{Kind: HistoryEntryToolResult, ToolResult: maximalToolResult()},
			},
		},
		"rejected": {
			CorrelationID: "rejected", Kind: ResponseRejected,
			Rejection: Rejection{Command: CommandUnspecified, Code: RejectionInvalidArgument, Message: "invalid"},
		},
		"models": {
			CorrelationID: "models", Kind: ResponseModels,
			Models: ModelsResult{
				Models: []model.Descriptor{{
					Provider: "provider", Model: "model",
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: true,
						Choices: []model.ReasoningChoice{
							model.ReasoningChoiceOff, model.ReasoningChoiceMinimal, model.ReasoningChoiceLow,
							model.ReasoningChoiceMedium, model.ReasoningChoiceHigh,
							model.ReasoningChoiceXHigh, model.ReasoningChoiceMax,
						},
						Default: model.ReasoningChoiceHigh,
					},
				}, {
					Provider: "ollama", Model: "ornith",
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceOn},
						Default: model.ReasoningChoiceOn,
					},
				}},
				ActiveSelection: model.Selection{
					Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh,
				},
			},
		},
		"model selection": {
			CorrelationID: "selection", Kind: ResponseModelSelection,
			Selection: model.Selection{
				Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceMax,
			},
		},
	}

	for name, response := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := mapResponse(response)
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
				assert.Equal(t, "user", entries[0].GetUser().GetText())
				assertModelResponse(t, entries[1].GetModel(), true)
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
			case ResponseUnspecified:
				require.Fail(t, "unexpected response kind")
			}
		})
	}

	mapped, err := mapResponse(Response{
		CorrelationID: "absent", Kind: ResponseMessages,
		Messages: []HistoryEntry{{Kind: HistoryEntryModel, Model: ModelResponse{Outcome: ModelOutcomeStop}}},
	})
	require.NoError(t, err)
	assert.False(t, mapped.GetCommandResponse().GetMessages().GetEntries()[0].GetModel().HasResponseModel())
}

// TestMapEventPreservesEveryEvent verifies every event enum and payload oneof.
func TestMapEventPreservesEveryEvent(t *testing.T) {
	t.Parallel()

	responseModel := ""
	tests := []struct {
		typeValue AgentEventType
		payload   string
		event     AgentEvent
	}{
		{typeValue: AgentEventAgentStart},
		{typeValue: AgentEventTurnStart},
		{typeValue: AgentEventMessageStart},
		{typeValue: AgentEventModelContentStart, payload: "model_content", event: AgentEvent{ModelContent: ModelContent{Kind: ModelContentReasoning, Position: 2}}},
		{typeValue: AgentEventModelTextDelta, payload: "model_content", event: AgentEvent{ModelContent: ModelContent{Kind: ModelContentText, Position: 1, Text: "delta"}}},
		{typeValue: AgentEventModelContentEnd, payload: "model_content", event: AgentEvent{ModelContent: ModelContent{Kind: ModelContentRefusal, Position: 3}}},
		{typeValue: AgentEventToolCallStart, payload: "tool_call_preview", event: AgentEvent{ToolCallPreview: maximalToolCallPreview()}},
		{typeValue: AgentEventToolCallDelta, payload: "tool_call_preview", event: AgentEvent{ToolCallPreview: maximalToolCallPreview()}},
		{typeValue: AgentEventToolCallEnd, payload: "final_tool_call", event: AgentEvent{FinalToolCall: maximalFinalToolCall()}},
		{typeValue: AgentEventMessageEnd, payload: "model_response", event: AgentEvent{ModelResponse: maximalModelResponse(&responseModel)}},
		{typeValue: AgentEventToolExecutionStart, payload: "tool_execution", event: AgentEvent{ToolExecution: ToolExecution{CallID: "call", ToolName: "tool"}}},
		{typeValue: AgentEventToolExecutionUpdate, payload: "tool_progress", event: AgentEvent{ToolProgress: ToolProgress{Channel: ProgressChannelStderr, Content: "progress"}}},
		{typeValue: AgentEventToolExecutionEnd, payload: "tool_result", event: AgentEvent{ToolResult: maximalToolResult()}},
		{typeValue: AgentEventToolResult, payload: "tool_result", event: AgentEvent{ToolResult: maximalToolResult()}},
		{typeValue: AgentEventTurnEnd, payload: "turn", event: AgentEvent{Turn: TurnSummary{Response: maximalModelResponse(&responseModel), ToolResults: []ToolResult{maximalToolResult()}}}},
		{typeValue: AgentEventAgentEnd, payload: "agent", event: AgentEvent{Agent: AgentSummary{Outcome: RunOutcomeFailed, ErrorMessage: "failed"}}},
		{typeValue: AgentEventAgentSettled},
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

	tests := map[string]func() error{
		"response": func() error { _, err := mapResponse(Response{Kind: ResponseUnspecified}); return err },
		"history": func() error {
			_, err := mapResponse(Response{Kind: ResponseMessages, Messages: []HistoryEntry{{Kind: HistoryEntryUnspecified}}})
			return err
		},
		"event": func() error { _, err := mapEvent(AgentEvent{Type: AgentEventUnspecified}); return err },
		"content": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventModelContentStart, ModelContent: ModelContent{Kind: ModelContentUnspecified}})
			return err
		},
		"preview kind": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventToolCallStart, ToolCallPreview: ToolCallPreview{Fields: []ToolCallPreviewField{{Kind: ToolCallPreviewFieldUnspecified}}}})
			return err
		},
		"preview JSON": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventToolCallStart, ToolCallPreview: ToolCallPreview{Fields: []ToolCallPreviewField{{Kind: ToolCallPreviewFieldComplete, Value: make(chan int)}}}})
			return err
		},
		"progress": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventToolExecutionUpdate, ToolProgress: ToolProgress{Channel: ProgressChannelUnspecified}})
			return err
		},
		"tool result": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventToolResult, ToolResult: ToolResult{Contents: []ToolResultContent{{Kind: ToolResultContentUnspecified}}}})
			return err
		},
		"model outcome": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventMessageEnd, ModelResponse: ModelResponse{Outcome: ModelOutcomeUnspecified}})
			return err
		},
		"model content": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventMessageEnd, ModelResponse: ModelResponse{Outcome: ModelOutcomeStop, Content: []ModelResponseContent{{Kind: ModelResponseContentUnspecified}}}})
			return err
		},
		"run outcome": func() error {
			_, err := mapEvent(AgentEvent{Type: AgentEventAgentEnd, Agent: AgentSummary{Outcome: RunOutcomeUnspecified}})
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
		CallID: "call", Name: "tool", Position: 4, Provisional: true,
		Fields: []ToolCallPreviewField{
			{Name: "null", Kind: ToolCallPreviewFieldComplete, Value: nil},
			{Name: "prefix", Kind: ToolCallPreviewFieldPrefix, Prefix: ""},
		},
	}
}

func maximalFinalToolCall() FinalToolCall {
	return FinalToolCall{
		CallID: "call", Name: "tool", Position: 4,
		Arguments: map[string]any{"null": nil, "array": []any{"value", float64(2)}},
	}
}

func maximalToolResult() ToolResult {
	return ToolResult{
		CallID: "call", ToolName: "tool", IsError: true,
		Contents: []ToolResultContent{
			{Kind: ToolResultContentText, Text: ""},
			{Kind: ToolResultContentImage, Image: ToolResultImage{MediaType: "image/png", Data: []byte{0, 1, 255}}},
		},
	}
}

func maximalModelResponse(responseModel *string) ModelResponse {
	return ModelResponse{
		Text: "text", Outcome: ModelOutcomeToolUse, ErrorMessage: "error", Provider: "provider", Model: "model",
		ResponseModel: responseModel, ResponseID: "response",
		Usage:       ModelUsage{InputTokens: 1, OutputTokens: 2, CachedInputTokens: 3, CacheWriteTokens: 4, ReasoningTokens: 5, TotalTokens: 6},
		Diagnostics: []ModelDiagnostic{{Code: "code", Message: "message"}},
		Content: []ModelResponseContent{
			{Kind: ModelResponseContentText, Text: "text"},
			{Kind: ModelResponseContentRefusal, Text: "refusal"},
			{Kind: ModelResponseContentReasoning, Text: "reasoning"},
			{Kind: ModelResponseContentToolCall, ToolCall: maximalFinalToolCall()},
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
	assert.Equal(t, "error", response.GetErrorMessage())
	assert.Equal(t, "provider", response.GetProvider())
	assert.Equal(t, "model", response.GetModel())
	assert.Equal(t, hasResponseModel, response.HasResponseModel())
	assert.Empty(t, response.GetResponseModel())
	assert.Equal(t, "response", response.GetResponseId())
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
