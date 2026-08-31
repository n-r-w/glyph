//go:build !integration

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTextMessageSetsOnlyText verifies the text input alternative has no image values.
func TestTextMessageSetsOnlyText(t *testing.T) {
	t.Parallel()

	message := TextMessage("hello")

	require.Len(t, message.Content, 1)
	content := message.Content[0]
	assert.Equal(t, InputContentText, content.Kind)
	assert.Equal(t, "hello", content.Text.OrEmpty())
	assert.True(t, content.Text.IsSome())
	assert.True(t, content.MediaType.IsNone())
	assert.True(t, content.Data.IsNone())
}
