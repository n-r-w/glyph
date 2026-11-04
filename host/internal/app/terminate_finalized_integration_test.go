//go:build integration

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicFinalizedTerminate verifies replacement control values through real finalized handlers.
func TestPublicFinalizedTerminate(t *testing.T) {
	t.Parallel()
	// Arrange an echo tool and finalized handlers in a real extension process.
	directory := t.TempDir()
	buildExternalCommand(t, directory, "finalized", "finalized-terminate-category")
	provider := startQueueProvider(t, 0, toolCallSSE(t, "call-1", finalizedEcho, `{}`), finalResponseSSE)
	fixture := startQueueFixture(t, provider, directory)
	defer fixture.closeOwner(t)
	// Act through the public request.
	require.NoError(t, fixture.stream.Send(userRequest(toolBatchRunID, "finish")))
	observation := observeFinalizedRequest(t, fixture, toolBatchRunID)
	// Assert the replacement's flag and category reach events and summaries and control completion.
	require.Empty(t, observation.code, observation.message)
	require.Equal(t, programmaticv1.RunOutcome_RUN_OUTCOME_COMPLETED, observation.outcome)
	for _, kind := range []programmaticv1.AgentEventType{
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END,
	} {
		results := observation.toolResults[kind]
		require.Len(t, results, 1)
		assert.True(t, results[0].GetTerminate())
		assert.True(t, results[0].GetIsError())
		assert.Equal(
			t,
			programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE,
			results[0].GetErrorCategory(),
		)
		assert.Equal(t, categoryReplacementText, toolResultText(results[0]))
	}
	assert.Len(t, provider.snapshot(), 1)
}
