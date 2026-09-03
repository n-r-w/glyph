//go:build !integration

package run

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestCloneToolResultClonesImageBytesInsideOption verifies history snapshots do not share mutable image data.
func TestCloneToolResultClonesImageBytesInsideOption(t *testing.T) {
	t.Parallel()

	// Arrange a tool result with mutable image data.
	original := agent.ToolResult{
		Contents: []tool.ResultContent{{
			Kind:  tool.ResultContentImage,
			Text:  mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2, 3}}),
		}}, CallID: "", ToolName: "", IsError: false,
	}
	// Act by cloning the result and mutating the cloned image.
	cloned := original.Clone()
	image, ok := cloned.Contents[0].Image.Get()
	require.True(t, ok)
	image.Data[0] = 9

	// Assert the original image data remains unchanged.
	assert.Equal(t, byte(1), original.Contents[0].Image.OrEmpty().Data[0])
}
