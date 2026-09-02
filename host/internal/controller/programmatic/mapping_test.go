//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/types/known/structpb"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

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
		assert.GreaterOrEqual(t, content.GetPosition(), int64(1))
	case "tool_call_preview":
		preview := event.GetToolCallPreview()
		assert.Equal(t, "call", preview.GetCallId())
		assert.Equal(t, "tool", preview.GetName())
		assert.Equal(t, int64(4), preview.GetPosition())
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
	assert.Equal(t, int64(4), call.GetPosition())
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
