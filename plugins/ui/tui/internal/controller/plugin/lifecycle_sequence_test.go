//go:build !integration

package plugin

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapLifecyclePreservesRefusalKind verifies refusal deltas stay distinct from ordinary model text.
func TestMapLifecyclePreservesRefusalKind(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for DecodeLifecycle to verify refusal deltas stay distinct from ordinary model text.

	// Act by invoking DecodeLifecycle to exercise refusal deltas stay distinct from ordinary model text.
	event, err := DecodeLifecycle(uiv1.AgentEvent_builder{
		Type: new(uiv1.LifecycleType_LIFECYCLE_TYPE_MODEL_TEXT_DELTA),
		ModelContent: uiv1.ModelContent_builder{
			Type:     new(uiv1.ModelContentType_MODEL_CONTENT_TYPE_TEXT_DELTA),
			Kind:     new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL),
			Position: new(int64(3)),
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

	// Assert refusal deltas stay distinct from ordinary model text.
	require.NoError(t, err)
	assert.Equal(t, AgentModelDelta, event.Kind)
	assert.Equal(t, mo.Some(ModelContentRefusal), event.ModelContentKind)
	assert.Equal(t, mo.Some("cannot help"), event.Text)
}

// TestMapLifecyclePreservesFinalizedVisibleBlocks verifies mixed visible content reaches presentation state.
func TestMapLifecyclePreservesFinalizedVisibleBlocks(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for DecodeLifecycle to verify mixed visible content reaches presentation state.

	// Act by invoking DecodeLifecycle to exercise mixed visible content reaches presentation state.
	event, err := DecodeLifecycle(uiv1.AgentEvent_builder{
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

	// Assert mixed visible content reaches presentation state.
	require.NoError(t, err)
	assert.Equal(t, []ModelResponseContent{
		{
			Kind: ModelContentReasoning,
			Text: mo.Some("hidden"),
		},
		{
			Kind: ModelContentText,
			Text: mo.Some("answer"),
		},
		{
			Kind: ModelContentRefusal,
			Text: mo.Some("cannot help"),
		},
	}, event.ModelResponseContent)
}
