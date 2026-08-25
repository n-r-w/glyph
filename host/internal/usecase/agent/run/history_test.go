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
