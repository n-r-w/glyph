//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// categorizedToolHistory returns a history entry with an error result of the given category.
func categorizedToolHistory(category mo.Option[agent.ToolErrorCategory]) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "read", Contents: tool.TextContents("failed"), IsError: true,
			ErrorCategory: category,
		}),
	}
}

// TestToolErrorCategorySurvivesPublicRoundTrip verifies that the category of a tool result reaches a handler through
// the shared session-tree message, stays with a copy that changes only the text, and comes back with the replay
// restoration of context history.
func TestToolErrorCategorySurvivesPublicRoundTrip(t *testing.T) {
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

			// Arrange one finalized tool result with the scenario category.
			entry := categorizedToolHistory(category)
			input := middleware.Transformation[agent.HistoryEntry]{
				Context: middlewareBinding(), Original: entry, Current: entry,
			}
			projection := newReplayProjection()

			// Act by sending the result to a handler that copies it and changes only its text, and by projecting and
			// restoring the history.
			request, err := mapFinalizedMessageRequest("finalized", input, projection)
			require.NoError(t, err)
			copied := request.GetFinalizedMessage().GetCurrent().GetToolResult()
			copied.GetContents()[0].SetText("changed")
			replaced, replaceErr := mapFinalizedMessageAction(finalizedResponse(
				extensionpb.FinalizedMessageAction_builder{
					Preserve: nil, Replace: request.GetFinalizedMessage().GetCurrent(),
				}.Build(),
			), projection)
			projected, projectErr := projection.project([]agent.HistoryEntry{entry})
			restored, restoreErr := projection.restore(projected)

			// Assert the category on the wire, in the decoded replacement, and in the restored history.
			require.NoError(t, replaceErr)
			require.NoError(t, projectErr)
			require.NoError(t, restoreErr)
			assert.Equal(t, category.IsPresent(), copied.HasErrorCategory())
			assert.Equal(t, category, replaced.Replacement.ToolResult.OrEmpty().ErrorCategory)
			assert.Equal(t, "changed", replaced.Replacement.ToolResult.OrEmpty().Contents[0].Text.OrEmpty())
			assert.Equal(t, []agent.HistoryEntry{entry}, restored)
		})
	}
}

// TestToolErrorCategoryRejectsValueOutsideClosedSet verifies that a replacement with an unspecified or unknown
// category is rejected with the complete cause.
func TestToolErrorCategoryRejectsValueOutsideClosedSet(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		category extensionpb.ToolErrorCategory
		message  string
	}{
		"unspecified": {
			category: extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED,
			message:  "tool error category 0 is outside the closed set",
		},
		"unknown": {category: extensionpb.ToolErrorCategory(99), message: "tool error category 99 is outside the closed set"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a replacement whose category is outside the closed set.
			entry := categorizedToolHistory(mo.Some(agent.ToolErrorFailure))
			projection := newReplayProjection()
			projected, err := projection.project([]agent.HistoryEntry{entry})
			require.NoError(t, err)
			projected.GetEntries()[0].GetToolResult().SetErrorCategory(test.category)

			// Act by restoring the replacement.
			_, restoreErr := projection.restore(projected)

			// Assert the rejection text.
			require.ErrorContains(t, restoreErr, test.message)
		})
	}
}
