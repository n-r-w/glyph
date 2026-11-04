//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestFinalizedTerminateReplacement verifies a complete replacement can remove termination and change the category.
func TestFinalizedTerminateReplacement(t *testing.T) {
	t.Parallel()
	// Arrange a terminating policy-rejection result for the finalized handler.
	entry := categorizedToolHistory(mo.Some(agent.ToolErrorPolicyRejection))
	projection := newReplayProjection()
	request, err := mapFinalizedMessageRequest("finalized", middleware.Transformation[agent.HistoryEntry]{
		Context: middlewareBinding(), Original: entry, Current: entry,
	}, projection)
	require.NoError(t, err)
	current := request.GetFinalizedMessage().GetCurrent()
	original := current.GetToolResult()
	require.True(t, original.GetTerminate())
	// Act by supplying a complete replacement with a different category and no completion signal.
	replacement := extensionpb.SessionTreeToolResult_builder{
		CallId: new(original.GetCallId()), ToolName: new(original.GetToolName()),
		Contents: original.GetContents(), IsError: new(true),
		ErrorCategory: new(extensionpb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE), Terminate: new(false),
	}.Build()
	current.SetToolResult(replacement)
	action, err := mapFinalizedMessageAction(finalizedResponse(extensionpb.FinalizedMessageAction_builder{
		Preserve: nil, Replace: current,
	}.Build()), projection)
	// Assert consumers receive the replacement's flag and category.
	require.NoError(t, err)
	final := action.Replacement.ToolResult.MustGet()
	assert.False(t, final.Terminate)
	assert.Equal(t, mo.Some(agent.ToolErrorFailure), final.ErrorCategory)
}
