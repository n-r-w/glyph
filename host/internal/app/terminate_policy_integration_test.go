//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicFinalizedPolicyRejection verifies copy-and-edit retains the policy category for model and client.
func TestPublicFinalizedPolicyRejection(t *testing.T) {
	t.Parallel()
	// Arrange a rejecting policy followed by real finalized-message handlers.
	directory := buildToolPolicyFixture(t, toolPolicyRejectFirst, toolPolicyToolOwner)
	buildExternalCommand(t, directory, "finalized", "finalized")
	provider := startQueueProvider(
		t,
		0,
		toolCallSSE(t, "call-1", toolPolicyToolName, toolPolicyForbiddenArguments),
		finalResponseSSE,
	)
	fixture := startQueueFixture(t, provider, directory)
	defer fixture.closeOwner(t)
	// Act through the public request.
	require.NoError(t, fixture.stream.Send(userRequest(toolBatchRunID, "reject")))
	observation := observeFinalizedRequest(t, fixture, toolBatchRunID)
	// Assert the rewritten result retains the category and requires a model continuation.
	require.Empty(t, observation.code, observation.message)
	results := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT]
	assertToolResultCategory(t, results, programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION)
	assert.False(t, results[0].GetTerminate())
	assert.Contains(t, toolResultText(results[0]), finalizedToolSuffix)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[1], toolPolicyCategoryName)
	assert.Contains(t, bodies[1], finalizedToolSuffix)
}
