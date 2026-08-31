//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapLifecycleCarriesTypedTerminalData verifies the generated terminal contract mapping.
func TestMapLifecycleCarriesTypedTerminalData(t *testing.T) {
	t.Parallel()

	event := domainui.Lifecycle{
		Type:               domainui.LifecycleMessageEnd,
		RunID:              mo.Some("run"),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse: mo.Some(domainui.ModelResponse{
			Text:          "visible",
			Outcome:       mo.Some("stop"),
			ErrorMessage:  mo.Some(""),
			Provider:      mo.Some("openai-codex"),
			Model:         mo.Some("gpt-test"),
			ResponseModel: mo.Some("gpt-actual"),
			ResponseID:    mo.Some("resp-1"),
			Content: []domainui.ModelResponseContent{
				{
					Kind: domainui.ModelContentKindReasoning,
					Text: "hidden", ToolCall: mo.None[domainui.FinalToolCall](),
				},
				{
					Kind: domainui.ModelContentKindText,
					Text: "visible", ToolCall: mo.None[domainui.FinalToolCall](),
				},
				{
					Kind: domainui.ModelContentKindRefusal,
					Text: "cannot help", ToolCall: mo.None[domainui.FinalToolCall](),
				},
			},
			Usage: mo.Some(domainui.ModelUsage{
				InputTokens:       10,
				OutputTokens:      7,
				CachedInputTokens: 4,
				CacheWriteTokens:  1,
				ReasoningTokens:   3,
				TotalTokens:       17,
			}),
			Diagnostics: []domainui.ModelDiagnostic{{
				Code:    "recovered_output",
				Message: "safe",
			}},
		}),
		ToolCallPreview: mo.None[domainui.ToolCallPreview](),
		FinalToolCall:   mo.None[domainui.FinalToolCall](),
		ToolCallID:      mo.None[string](),
		ToolName:        mo.None[string](),
		ProgressChannel: mo.None[domainui.ProgressChannel](),
		IsError:         mo.None[bool](),
		Outcome:         mo.None[string](),
		ErrorMessage:    mo.None[string](),
		Availability:    mo.None[domainui.Availability](),
	}

	mappedLifecycle, err := mapLifecycle(event)
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
	assert.Equal(t, uipb.ModelContentKind_MODEL_CONTENT_KIND_REASONING, mapped.GetContent()[0].GetKind())
	assert.Equal(t, uipb.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL, mapped.GetContent()[2].GetKind())
	require.Len(t, mapped.GetDiagnostics(), 1)
}

// TestMapLifecycleCarriesToolResultBlocks verifies ordered text and exact image bytes.
func TestMapLifecycleCarriesToolResultBlocks(t *testing.T) {
	t.Parallel()

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
	event := domainui.Lifecycle{
		Type:               domainui.LifecycleToolResult,
		RunID:              mo.Some("run"),
		Text:               mo.None[string](),
		ToolResultContents: mo.Some(contents),
		ModelContent:       mo.None[domainui.ModelContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.Some("call"),
		ToolName:           mo.Some("read"),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.Some(false),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.None[domainui.Availability](),
	}
	mappedLifecycle, err := mapLifecycle(event)
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

	for _, kind := range []domainui.FrameKind{
		domainui.FrameInitialization,
		domainui.FrameLifecycle,
		domainui.FrameAuthorization,
		domainui.FrameInformation,
		domainui.FrameError,
		domainui.FrameModelSelectionChanged,
	} {
		_, err := mapFrame(domainui.Frame{
			SessionEntries:      nil,
			Kind:                kind,
			Initialization:      mo.None[domainui.Initialization](),
			Lifecycle:           mo.None[domainui.Lifecycle](),
			AuthorizationURL:    mo.None[string](),
			Text:                mo.None[string](),
			RetryAuthentication: mo.None[bool](),
			ModelSelection:      mo.None[domainui.ModelSelection](),
			SessionInfo:         mo.None[session.Info](),
			Sessions:            nil,
			SessionStatistics:   mo.None[session.Statistics](),
			SessionTree:         mo.None[domainui.SessionTree](),
			TreeNavigation:      mo.None[domainui.TreeNavigationResult](),
			TreeFailure:         mo.None[domainui.TreeFailure](),
		})
		require.Error(t, err)
	}
	_, err := mapCommand(&uipb.OpenResponse{})
	require.Error(t, err)
}

// TestMapLifecycleRejectsMissingSelectedPayload verifies required lifecycle alternatives.
func TestMapLifecycleRejectsMissingSelectedPayload(t *testing.T) {
	t.Parallel()

	for _, lifecycleType := range []domainui.LifecycleType{
		domainui.LifecycleModelContentStart,
		domainui.LifecycleModelTextDelta,
		domainui.LifecycleModelContentEnd,
		domainui.LifecycleMessageEnd,
		domainui.LifecycleToolCallStart,
		domainui.LifecycleToolCallDelta,
		domainui.LifecycleToolCallEnd,
		domainui.LifecycleToolExecutionStart,
		domainui.LifecycleToolExecutionUpdate,
		domainui.LifecycleToolExecutionEnd,
		domainui.LifecycleToolResult,
		domainui.LifecycleTurnEnd,
		domainui.LifecycleAgentEnd,
		domainui.LifecycleAvailabilityChanged,
	} {
		event := domainui.Lifecycle{
			Type:               lifecycleType,
			RunID:              mo.Some("run"),
			Text:               mo.None[string](),
			ToolResultContents: mo.None[[]tool.ResultContent](),
			ModelContent:       mo.None[domainui.ModelContent](),
			ModelResponse:      mo.None[domainui.ModelResponse](),
			ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
			FinalToolCall:      mo.None[domainui.FinalToolCall](),
			ToolCallID:         mo.None[string](),
			ToolName:           mo.None[string](),
			ProgressChannel:    mo.None[domainui.ProgressChannel](),
			IsError:            mo.None[bool](),
			Outcome:            mo.None[string](),
			ErrorMessage:       mo.None[string](),
			Availability:       mo.None[domainui.Availability](),
		}
		_, err := mapLifecycle(event)
		require.Error(t, err)
	}
	_, err := mapLifecycle(domainui.Lifecycle{
		Type:  domainui.LifecycleModelTextDelta,
		RunID: mo.Some("run"),
		ModelContent: mo.Some(domainui.ModelContent{
			Type: domainui.ModelContentTextDelta, Kind: domainui.ModelContentKindText,
			Position: 0, Text: mo.None[string](),
		}),
		Text:               mo.None[string](),
		ToolResultContents: mo.None[[]tool.ResultContent](),
		ModelResponse:      mo.None[domainui.ModelResponse](),
		ToolCallPreview:    mo.None[domainui.ToolCallPreview](),
		FinalToolCall:      mo.None[domainui.FinalToolCall](),
		ToolCallID:         mo.None[string](),
		ToolName:           mo.None[string](),
		ProgressChannel:    mo.None[domainui.ProgressChannel](),
		IsError:            mo.None[bool](),
		Outcome:            mo.None[string](),
		ErrorMessage:       mo.None[string](),
		Availability:       mo.None[domainui.Availability](),
	})
	require.Error(t, err)
}

// TestMapToolCallPreviewPreservesPresentZeroValues verifies oneof presence at the Protobuf boundary.
func TestMapToolCallPreviewPreservesPresentZeroValues(t *testing.T) {
	t.Parallel()

	mapped, err := mapToolCallPreview(domainui.ToolCallPreview{
		CallID:      "call",
		Name:        "tool",
		Position:    0,
		Provisional: false,
		Fields: []domainui.ToolCallPreviewField{
			{Name: "value", Value: mo.Some[any](nil), Prefix: mo.None[string](), Complete: true},
			{Name: "prefix", Value: mo.None[any](), Prefix: mo.Some(""), Complete: false},
		},
	})

	require.NoError(t, err)
	require.Len(t, mapped.GetFields(), 2)
	assert.True(t, mapped.GetFields()[0].HasValue())
	assert.Equal(t, structpb.NullValue_NULL_VALUE, mapped.GetFields()[0].GetValue().GetNullValue())
	assert.True(t, mapped.GetFields()[1].HasPrefix())
	assert.Empty(t, mapped.GetFields()[1].GetPrefix())
}
