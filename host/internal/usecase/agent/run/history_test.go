package run

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestCloneMessageClonesImageBytesInsideOption verifies cloned input images do not share mutable data.
func TestCloneMessageClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	original := model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](),
		MediaType: mo.Some("image/png"), Data: mo.Some([]byte{1, 2, 3}),
	}}}

	cloned := cloneMessage(original)
	clonedData, ok := cloned.Content[0].Data.Get()
	require.True(t, ok)
	clonedData[0] = 9
	originalData, ok := original.Content[0].Data.Get()
	require.True(t, ok)

	assert.Equal(t, byte(1), originalData[0])
	assert.True(t, cloned.Content[0].Text.IsNone())
	assert.Equal(t, "image/png", cloned.Content[0].MediaType.OrEmpty())
}

// TestCloneModelResponseClonesMutableOptionValues verifies output snapshots isolate provider bytes and tool arguments.
func TestCloneModelResponseClonesMutableOptionValues(t *testing.T) {
	t.Parallel()

	original := model.Response{
		Content: []model.Content{
			{
				Kind:  model.ContentReasoning,
				Text:  mo.Some("reason"),
				Final: false,
				ProviderContext: mo.Some(model.ProviderContext{
					Source:  model.ProviderContextSource{},
					Payload: []byte{1, 2, 3},
				}),
				ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           false,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall: mo.Some(model.ToolCall{
					ID:        "",
					Name:      "",
					Arguments: map[string]any{"items": []any{"first"}},
				}),
			},
		},
		Outcome:       mo.None[model.Outcome](),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}

	cloned := cloneModelResponse(original)
	clonedContext := cloned.Content[0].ProviderContext.OrEmpty()
	clonedContext.Payload[0] = 9
	clonedCall := cloned.Content[1].ToolCall.OrEmpty()
	clonedCall.Arguments["items"].([]any)[0] = "changed"

	assert.Equal(t, byte(1), original.Content[0].ProviderContext.OrEmpty().Payload[0])
	assert.Equal(t, "first", original.Content[1].ToolCall.OrEmpty().Arguments["items"].([]any)[0])
	assert.True(t, cloned.Content[0].ToolCall.IsNone())
	assert.True(t, cloned.Content[1].ProviderContext.IsNone())
}
