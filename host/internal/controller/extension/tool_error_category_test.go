//go:build !integration

package extension

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestToolErrorCategoryCompactionRoundTrip verifies that the category of a tool result survives the projection to the
// compaction history and the decoding of the shared session-tree message.
func TestToolErrorCategoryCompactionRoundTrip(t *testing.T) {
	t.Parallel()
	for name, category := range map[string]mo.Option[agent.ToolErrorCategory]{
		"none":              mo.None[agent.ToolErrorCategory](),
		"unavailable tool":  mo.Some(agent.ToolErrorUnavailableTool),
		"invalid arguments": mo.Some(agent.ToolErrorInvalidArguments),
		"tool failure":      mo.Some(agent.ToolErrorFailure),
		"policy rejection":  mo.Some(agent.ToolErrorPolicyRejection),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange an error result with the scenario category.
			result := agent.ToolResult{
				CallID: "call", ToolName: "read", Contents: tool.TextContents("failed"), IsError: true,
				ErrorCategory: category,
			}
			history := []agent.HistoryEntry{{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
				ToolResult: mo.Some(result),
			}}

			// Act through the projection and the decoder of the shared message.
			records, err := MapCompactionHistory(history)
			require.NoError(t, err)
			require.Len(t, records, 1)
			decoded, decodeErr := DecodeSessionTreeToolResult(records[0].GetToolResult())

			// Assert the wire presence and the decoded result.
			require.NoError(t, decodeErr)
			assert.Equal(t, category.IsPresent(), records[0].GetToolResult().HasErrorCategory())
			assert.Equal(t, result, decoded)
		})
	}
}

// TestDecodeSessionTreeToolResultRejectsInvalidCategory verifies that an unspecified or unknown category and a category
// on a successful result are rejected with the complete cause.
func TestDecodeSessionTreeToolResultRejectsInvalidCategory(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		isError   bool
		category  extensionpb.ToolErrorCategory
		errorText string
	}{
		"unspecified": {
			isError: true, category: extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED,
			errorText: "tool error category 0 is outside the closed set",
		},
		"unknown": {
			isError: true, category: extensionpb.ToolErrorCategory(99),
			errorText: "tool error category 99 is outside the closed set",
		},
		"successful result": {
			isError: false, category: extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE,
			errorText: "tool error category requires an error result",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a shared message with the invalid category.
			message := extensionpb.SessionTreeToolResult_builder{
				CallId: new("call"), ToolName: new("read"), Contents: nil, IsError: new(test.isError),
				ErrorCategory: new(test.category),
			}.Build()

			// Act through the decoder.
			_, err := DecodeSessionTreeToolResult(message)

			// Assert the complete rejection text.
			require.EqualError(t, err, test.errorText)
		})
	}
}

// TestEncodeToolErrorCategoryRejectsUnknownValue verifies that a category outside the closed set is not published and
// that an absent category stays absent.
func TestEncodeToolErrorCategoryRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	// Arrange a category outside the closed set and an absent category.
	unknown := mo.Some(agent.ToolErrorCategory(99))
	absent := mo.None[agent.ToolErrorCategory]()

	// Act through the encoder and through the compaction projection that uses it.
	_, unknownErr := EncodeToolErrorCategory(unknown)
	wire, absentErr := EncodeToolErrorCategory(absent)
	_, projectionErr := mapCompactionToolResult(agent.ToolResult{
		CallID: "call", ToolName: "read", Contents: tool.TextContents("failed"), IsError: true,
		ErrorCategory: unknown,
	})

	// Assert the complete rejection text and the absent value.
	require.EqualError(t, unknownErr, "tool error category 99 is outside the closed set")
	require.EqualError(t, projectionErr, "tool error category 99 is outside the closed set")
	require.NoError(t, absentErr)
	assert.Nil(t, wire)
}
