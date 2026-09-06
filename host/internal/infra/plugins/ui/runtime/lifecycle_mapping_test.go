//go:build !integration

package runtime

import (
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapLifecycleCarriesTypedTerminalData verifies the generated terminal contract mapping.
func TestMapLifecycleCarriesTypedTerminalData(t *testing.T) {
	t.Parallel()
	// Arrange a MessageEnd lifecycle with visible, refusal, reasoning, usage, and diagnostic data.

	event := controllerui.Lifecycle{
		Type:               controllerui.LifecycleMessageEnd,
		RunID:              mo.Some("run"),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[controllerui.ModelContent](),
		ModelResponse: mo.Some(controllerui.ModelResponse{
			Text:          "visible",
			Outcome:       mo.Some("stop"),
			ErrorMessage:  mo.Some(""),
			Provider:      mo.Some("openai-codex"),
			Model:         mo.Some("gpt-test"),
			ResponseModel: mo.Some("gpt-actual"),
			ResponseID:    mo.Some("resp-1"),
			Content: []controllerui.ModelResponseContent{
				{
					Kind: controllerui.ModelContentKindReasoning,
					Text: "hidden", ToolCall: mo.None[controllerui.FinalToolCall](),
				},
				{
					Kind: controllerui.ModelContentKindText,
					Text: "visible", ToolCall: mo.None[controllerui.FinalToolCall](),
				},
				{
					Kind: controllerui.ModelContentKindRefusal,
					Text: "cannot help", ToolCall: mo.None[controllerui.FinalToolCall](),
				},
			},
			Usage: mo.Some(controllerui.ModelUsage{
				InputTokens:       10,
				OutputTokens:      7,
				CachedInputTokens: 4,
				CacheWriteTokens:  1,
				ReasoningTokens:   3,
				TotalTokens:       17,
			}),
			Diagnostics: []controllerui.ModelDiagnostic{{
				Code:    "recovered_output",
				Message: "safe",
			}},
		}),
		ToolCallPreview: mo.None[controllerui.ToolCallPreview](),
		FinalToolCall:   mo.None[controllerui.FinalToolCall](),
		ToolCallID:      mo.None[string](),
		ToolName:        mo.None[string](),
		ProgressChannel: mo.None[controllerui.ProgressChannel](),
		IsError:         mo.None[bool](),
		Outcome:         mo.None[string](),
		ErrorMessage:    mo.None[string](),
	}

	// Act by mapping the complete MessageEnd lifecycle to the wire contract.
	mappedLifecycle, err := mapLifecycle(event)

	// Assert all public terminal fields survive with their exact kinds and values.
	require.NoError(t, err)
	mapped := mappedLifecycle.GetModelResponse()

	require.NotNil(t, mapped)
	assert.Equal(t, "openai-codex", mapped.GetProvider())
	assert.Equal(t, "gpt-test", mapped.GetModel())
	require.NotNil(t, proto.ValueOrNil(mapped.HasResponseModel(), mapped.GetResponseModel))
	assert.Equal(t, "gpt-actual", mapped.GetResponseModel())
	assert.Equal(t, "resp-1", mapped.GetResponseId())
	assert.Equal(t, int64(17), mapped.GetUsage().GetTotalTokens())
	require.Len(t, mapped.GetContent(), 3)
	assert.Equal(t, uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING, mapped.GetContent()[0].GetKind())
	assert.Equal(t, uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL, mapped.GetContent()[2].GetKind())
	require.Len(t, mapped.GetDiagnostics(), 1)
}

// TestMapLifecycleCarriesToolResultBlocks verifies ordered text and exact image bytes.
func TestMapLifecycleCarriesToolResultBlocks(t *testing.T) {
	t.Parallel()
	// Arrange contents and event for mapLifecycle to verify ordered text and exact image bytes.

	contents := []tool.ResultContent{
		{
			Kind:  tool.ResultContentText,
			Text:  mo.Some("first"),
			Image: mo.None[tool.ResultImage](),
		},
		{
			Kind: tool.ResultContentImage,
			Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{
				MediaType: "image/png",
				Data:      []byte{1, 2, 3},
			}),
		},
	}
	event := controllerui.Lifecycle{
		Type:               controllerui.LifecycleToolResult,
		RunID:              mo.Some("run"),
		Text:               mo.None[string](),
		ToolResultContents: mo.Some(contents),
		ModelContent:       mo.None[controllerui.ModelContent](),
		ModelResponse:      mo.None[controllerui.ModelResponse](),
		ToolCallPreview:    mo.None[controllerui.ToolCallPreview](),
		FinalToolCall:      mo.None[controllerui.FinalToolCall](),
		ToolCallID:         mo.Some("call"),
		ToolName:           mo.Some("read"),
		ProgressChannel:    mo.None[controllerui.ProgressChannel](),
		IsError:            mo.Some(false),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
	}
	// Act by invoking mapLifecycle to exercise ordered text and exact image bytes.
	mappedLifecycle, err := mapLifecycle(event)
	// Assert ordered text and exact image bytes.
	require.NoError(t, err)
	mapped := mappedLifecycle.GetToolResultContents()
	image, present := contents[1].Image.Get()
	require.True(t, present)
	image.Data[0] = 9

	require.Len(t, mapped, 2)
	assert.Equal(t, "first", mapped[0].GetText())
	assert.Equal(t, "image/png", mapped[1].GetImage().GetMediaType())
	assert.Equal(t, []byte{1, 2, 3}, mapped[1].GetImage().GetData())
}

// TestMappingRejectsMissingPayloads verifies malformed stream items fail explicitly.
func TestMappingRejectsMissingPayloads(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for mapFrame to verify malformed stream items fail explicitly.

	for _, kind := range []controllerui.FrameKind{
		controllerui.FrameLifecycle,
		controllerui.FrameAuthorization,
		controllerui.FrameModelSelectionChanged,
	} {
		// Act by invoking mapFrame to exercise malformed stream items fail explicitly.
		_, err := mapFrame(controllerui.Frame{
			NextInput:      mo.None[string](),
			SessionEntries: nil,
			Kind:           kind,

			Lifecycle:        mo.None[controllerui.Lifecycle](),
			AuthorizationURL: mo.None[string](),

			ModelSelection:    mo.None[model.Selection](),
			SessionInfo:       mo.None[session.Info](),
			Sessions:          nil,
			SessionStatistics: mo.None[session.Statistics](),
			SessionTree:       mo.None[controllerui.SessionTree](),
			TreeNavigation:    mo.None[controllerui.TreeNavigationResult](),
		})
		// Assert malformed stream items fail explicitly.
		require.Error(t, err)
	}
}

// TestMapLifecycleRejectsMissingSelectedPayload verifies required lifecycle alternatives.
func TestMapLifecycleRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()
	// Arrange event for mapLifecycle to verify required lifecycle alternatives.

	for _, lifecycleType := range []controllerui.LifecycleType{
		controllerui.LifecycleModelContentStart,
		controllerui.LifecycleModelTextDelta,
		controllerui.LifecycleModelContentEnd,
		controllerui.LifecycleMessageEnd,
		controllerui.LifecycleToolCallStart,
		controllerui.LifecycleToolCallDelta,
		controllerui.LifecycleToolCallEnd,
		controllerui.LifecycleToolExecutionStart,
		controllerui.LifecycleToolExecutionUpdate,
		controllerui.LifecycleToolExecutionEnd,
		controllerui.LifecycleToolResult,
		controllerui.LifecycleTurnEnd,
		controllerui.LifecycleAgentEnd,
	} {
		event := controllerui.Lifecycle{
			Type:               lifecycleType,
			RunID:              mo.Some("run"),
			Text:               mo.None[string](),
			ToolResultContents: mo.None[[]tool.ResultContent](),
			ModelContent:       mo.None[controllerui.ModelContent](),
			ModelResponse:      mo.None[controllerui.ModelResponse](),
			ToolCallPreview:    mo.None[controllerui.ToolCallPreview](),
			FinalToolCall:      mo.None[controllerui.FinalToolCall](),
			ToolCallID:         mo.None[string](),
			ToolName:           mo.None[string](),
			ProgressChannel:    mo.None[controllerui.ProgressChannel](),
			IsError:            mo.None[bool](),
			Outcome:            mo.None[string](),
			ErrorMessage:       mo.None[string](),
		}
		// Act by invoking mapLifecycle to exercise required lifecycle alternatives.
		_, err := mapLifecycle(event)
		// Assert required lifecycle alternatives.
		require.Error(t, err)
	}
	_, err := mapLifecycle(controllerui.Lifecycle{
		Type:  controllerui.LifecycleModelTextDelta,
		RunID: mo.Some("run"),
		ModelContent: mo.Some(controllerui.ModelContent{
			Type: controllerui.ModelContentTextDelta, Kind: controllerui.ModelContentKindText,
			Position: 0, Text: mo.None[string](),
		}),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelResponse:      mo.None[controllerui.ModelResponse](),
		ToolCallPreview:    mo.None[controllerui.ToolCallPreview](),
		FinalToolCall:      mo.None[controllerui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[controllerui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
	})
	require.Error(t, err)
}

// TestMapToolCallPreviewPreservesPresentZeroValues verifies oneof presence at the Protobuf boundary.
func TestMapToolCallPreviewPreservesPresentZeroValues(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for mapToolCallPreview to verify oneof presence at the Protobuf boundary.

	// Act by invoking mapToolCallPreview to exercise oneof presence at the Protobuf boundary.
	mapped, err := mapToolCallPreview(controllerui.ToolCallPreview{
		CallID:      "call",
		Name:        "tool",
		Position:    0,
		Provisional: false,
		Fields: []controllerui.ToolCallPreviewField{
			{Name: "value", Value: mo.Some[any](nil), Prefix: mo.None[string](), Complete: true},
			{Name: "prefix", Value: mo.None[any](), Prefix: mo.Some(""), Complete: false},
		},
	})

	// Assert oneof presence at the Protobuf boundary.
	require.NoError(t, err)
	require.Len(t, mapped.GetFields(), 2)
	assert.True(t, mapped.GetFields()[0].HasValue())
	assert.Equal(t, structpb.NullValue_NULL_VALUE, mapped.GetFields()[0].GetValue().GetNullValue())
	assert.True(t, mapped.GetFields()[1].HasPrefix())
	assert.Empty(t, mapped.GetFields()[1].GetPrefix())
}
