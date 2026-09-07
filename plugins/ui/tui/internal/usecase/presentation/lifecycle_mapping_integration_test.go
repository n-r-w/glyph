//go:build integration

package presentation

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
)

// TestHostMessageEndFinalizesTextStreamAtDifferentPosition verifies complete terminal model projection.
func TestHostMessageEndFinalizesTextStreamAtDifferentPosition(t *testing.T) {
	t.Parallel()

	// Arrange model lifecycle frames whose terminal response uses another stream position.
	service := newProjectionService(t)
	frames := []*uiv1.AgentEvent{
		uiv1.AgentEvent_builder{
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
		uiv1.AgentEvent_builder{
			Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
			ModelContent: uiv1.ModelContent_builder{
				Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
				Position: new(int64(1)),
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
		uiv1.AgentEvent_builder{
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
		event, err := plugininput.DecodeLifecycle(lifecycle)
		require.NoError(t, err)
		require.NoError(t, service.applyInput(mo.Some(plugininput.AgentPayload(event))))
	}

	state := service.model.state

	// Assert the terminal response finalizes the complete ordered transcript without active fragments.
	assert.Equal(t, []Line{
		{
			Kind:     LineReasoning,
			Text:     mo.Some("hidden reasoning"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
		{
			Kind:     LineModel,
			Text:     mo.Some("complete answer"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]Content](),
		},
	}, state.Transcript)
	assert.Empty(t, state.ActiveModel)
}
