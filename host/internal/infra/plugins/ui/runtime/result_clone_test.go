//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestCloneResultContentsClonesImageBytesInsideOption verifies lifecycle frames do not share mutable image data.
func TestCloneResultContentsClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()
	// Arrange one tool-result image backed by mutable source bytes.

	original := []tool.ResultContent{{
		Kind: tool.ResultContentImage,
		Text: mo.None[string](),
		Image: mo.Some(tool.ResultImage{
			MediaType: "image/png",
			Data:      []byte{1, 2, 3},
		}),
	}}
	// Act by cloning the result and mutating the cloned image bytes.
	cloned := (agent.ToolResult{
		CallID: "", ToolName: "", Contents: original, IsError: false,
	}).Clone().Contents
	image, ok := cloned[0].Image.Get()
	require.True(t, ok)
	image.Data[0] = 9

	// Assert the original image retains its initial byte.
	assert.Equal(t, byte(1), original[0].Image.OrEmpty().Data[0])
}
