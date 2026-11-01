//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestToolResultErrorCategoryRoundTrip verifies that the Host error category of a tool result passes through the
// policy representation and back to the participant request, and that a result without a category stays without one.
func TestToolResultErrorCategoryRoundTrip(t *testing.T) {
	t.Parallel()
	for name, category := range map[string]*pb.ToolErrorCategory{
		"none":              nil,
		"unavailable tool":  new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL),
		"invalid arguments": new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS),
		"tool failure":      new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a prepared error result with the scenario category.
			wire := new(pb.CompactionHistoryRecord)
			wire.SetSourceEntryId("source")
			wire.SetToolResult(pb.SessionTreeToolResult_builder{
				CallId: new("call"), ToolName: new("read"), Contents: nil, IsError: new(true),
				ErrorCategory: category,
			}.Build())

			// Act through policy decoding and participant-request encoding.
			decoded := DecodeHistory([]*pb.CompactionHistoryRecord{wire})
			encoded := encodeHistory(decoded)

			// Assert the category on both sides of the policy.
			require.Len(t, encoded, 1)
			result := encoded[0].GetToolResult()
			assert.Equal(t, category != nil, result.HasErrorCategory())
			if category != nil {
				assert.Equal(t, *category, result.GetErrorCategory())
			}
			assert.True(t, result.GetIsError())
		})
	}
}

// TestToolResultUnrecognizedErrorCategory verifies that an unspecified or unknown category is not turned into a valid
// one and returns to Host as the unspecified value, which Host rejects.
func TestToolResultUnrecognizedErrorCategory(t *testing.T) {
	t.Parallel()
	for name, category := range map[string]pb.ToolErrorCategory{
		"unspecified": pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED,
		"unknown":     pb.ToolErrorCategory(99),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a prepared error result with a category outside the closed set.
			wire := new(pb.CompactionHistoryRecord)
			wire.SetSourceEntryId("source")
			wire.SetToolResult(pb.SessionTreeToolResult_builder{
				CallId: new("call"), ToolName: new("read"), Contents: nil, IsError: new(true),
				ErrorCategory: new(category),
			}.Build())

			// Act through policy decoding and participant-request encoding.
			decoded := DecodeHistory([]*pb.CompactionHistoryRecord{wire})
			encoded := encodeHistory(decoded)

			// Assert the unrecognized marker and the unspecified value on the wire.
			assert.Equal(t, mo.Some(compaction.ToolErrorInvalid), decoded[0].ErrorCategory)
			require.Len(t, encoded, 1)
			require.True(t, encoded[0].GetToolResult().HasErrorCategory())
			assert.Equal(
				t, pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED, encoded[0].GetToolResult().GetErrorCategory(),
			)
		})
	}
}
