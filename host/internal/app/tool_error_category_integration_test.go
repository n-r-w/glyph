//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// categoryMissingTool names a tool that no extension registered.
	categoryMissingTool = "missing_tool"
	// categoryInvalidArguments are arguments that violate the input schema of the reference tool.
	categoryInvalidArguments = `{"value":1}`
	// categoryInvalidName is the text that tells the model about the invalid-arguments category.
	categoryInvalidName = "invalid_arguments"
	// categoryUnavailableName is the text that tells the model about the unavailable-tool category.
	categoryUnavailableName = "unavailable_tool"
	// categoryFailureName is the text that tells the model about the tool-failure category.
	categoryFailureName = "tool_failure"
	// categoryReplacementText is the text of the complete replacement of the category scenarios.
	categoryReplacementText = "replaced result"
)

// installCategoryTransport records provider requests, answers the first one with a call of the tool, and completes
// each later request.
func installCategoryTransport(t *testing.T, toolName, arguments string) *requestBodies {
	t.Helper()
	encodedArguments, err := json.Marshal(arguments)
	require.NoError(t, err)
	callSSE := `data: {"type":"response.completed","response":{"id":"resp-tool","model":"gpt-test",` +
		`"status":"completed","output":[` +
		`{"id":"fc-1","type":"function_call","call_id":"call-1","name":"` + toolName + `",` +
		`"arguments":` + string(encodedArguments) + `,"status":"completed"}]}}` + "\n\n" + "data: [DONE]\n\n"
	return installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
		if position == 1 {
			return runtimeFailureHTTPResponse(callSSE), nil
		}
		return runtimeFailureHTTPResponse(finalResponseSSE), nil
	})
}

// historyToolResults returns the tool results of the active session as the client reads them with get_messages.
func historyToolResults(t *testing.T, fixture *programmaticFixture, operationID string) []*programmaticv1.ToolResult {
	t.Helper()
	entries := sendProgrammaticOperation(t, fixture, operationID, func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	results := make([]*programmaticv1.ToolResult, 0, 1)
	for _, entry := range entries {
		if entry.HasToolResult() {
			results = append(results, entry.GetToolResult())
		}
	}
	return results
}

// assertToolResultCategory checks that every tool result carries the expected category and error flag.
func assertToolResultCategory(
	t *testing.T,
	results []*programmaticv1.ToolResult,
	expected programmaticv1.ToolErrorCategory,
) {
	t.Helper()
	require.Len(t, results, 1)
	assert.True(t, results[0].GetIsError())
	require.True(t, results[0].HasErrorCategory())
	assert.Equal(t, expected, results[0].GetErrorCategory())
}

// TestPublicToolErrorCategoryInvalidArguments verifies that a model call with schema-invalid arguments gives the
// client and the model the invalid-arguments category, also after Host restarts, while the stored text stays unchanged.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicToolErrorCategoryInvalidArguments(t *testing.T) {
	// Arrange the preserving reference extension and a provider that calls its tool with invalid arguments.
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := installCategoryTransport(t, finalizedEcho, categoryInvalidArguments)
	extensionDirectory := buildFinalizedFixture(t, "finalized-preserve")
	fixture := startProgrammaticFixtureWithExtension(t, paths, extensionDirectory)

	// Act through the request, the history query, and the session information.
	require.NoError(t, fixture.stream.Send(userRequest("category-request", finalizedRequestText)))
	observation := observeFinalizedRequest(t, fixture, "category-request")
	history := historyToolResults(t, fixture, "category-history")
	sessionID := sendProgrammaticOperation(t, fixture, "category-info", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	}).GetSessionInfo().GetInfo().GetId()
	fixture.closeOwner(t)

	// Act again after a Host restart that resumes the persisted session.
	restarted := startProgrammaticFixtureWithExtension(t, paths, extensionDirectory)
	defer restarted.closeOwner(t)
	sendProgrammaticOperation(t, restarted, "category-resume", func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetResumeSession(
			programmaticv1.ResumeSession_builder{SessionId: new(sessionID)}.Build(),
		)
	})
	restored := historyToolResults(t, restarted, "category-restored")

	// Assert the category in every client view, the model-visible category, and the unchanged stored text.
	require.Empty(t, observation.code, observation.message)
	for _, eventType := range []programmaticv1.AgentEventType{
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END,
	} {
		assertToolResultCategory(
			t,
			observation.toolResults[eventType],
			programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS,
		)
	}
	assertToolResultCategory(t, history, programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS)
	assertToolResultCategory(t, restored, programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS)
	assert.NotContains(t, toolResultText(history[0]), categoryInvalidName)
	assert.Equal(t, toolResultText(history[0]), toolResultText(restored[0]))
	received := bodies.snapshot()
	require.Len(t, received, 2)
	assert.NotContains(t, string(received[0]), categoryInvalidName)
	assert.Contains(t, string(received[1]), categoryInvalidName)
}

// TestPublicToolErrorCategoryKeptByHandler verifies that a tool-result handler that copies the current error result and
// changes only its text keeps the category for the client and the model.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicToolErrorCategoryKeptByHandler(t *testing.T) {
	// Arrange the marking reference extension and a provider that calls an unavailable tool.
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := installCategoryTransport(t, categoryMissingTool, "{}")
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized"))
	defer fixture.closeOwner(t)

	// Act through the request and the history query.
	require.NoError(t, fixture.stream.Send(userRequest("kept-request", finalizedRequestText)))
	observation := observeFinalizedRequest(t, fixture, "kept-request")
	history := historyToolResults(t, fixture, "kept-history")

	// Assert the new text with the unchanged category for the client and the model.
	require.Empty(t, observation.code, observation.message)
	unavailable := programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL
	for _, eventType := range []programmaticv1.AgentEventType{
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT,
	} {
		assertToolResultCategory(t, observation.toolResults[eventType], unavailable)
		assert.Contains(t, toolResultText(observation.toolResults[eventType][0]), finalizedToolSuffix)
	}
	assertToolResultCategory(t, history, unavailable)
	assert.Contains(t, toolResultText(history[0]), finalizedToolSuffix)
	received := bodies.snapshot()
	require.Len(t, received, 2)
	assert.Contains(t, string(received[1]), categoryUnavailableName)
	assert.Contains(t, string(received[1]), finalizedToolSuffix)
}

// TestPublicToolErrorCategoryReplacement verifies that a complete replacement sets the category of the replacement and
// that a category outside the closed set fails the run with the complete cause.
//
//nolint:paralleltest // The scenarios replace process-global provider HTTP transport and environment.
func TestPublicToolErrorCategoryReplacement(t *testing.T) {
	t.Run("replacement category", func(t *testing.T) {
		// Arrange a reference that replaces the unavailable-tool result with a tool-failure result.
		paths := testPaths(t, userInputSettings(false))
		writeProgrammaticCredentials(t, paths)
		bodies := installCategoryTransport(t, categoryMissingTool, "{}")
		fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized-category"))
		defer fixture.closeOwner(t)

		// Act through the request and the history query.
		require.NoError(t, fixture.stream.Send(userRequest("replace-request", finalizedRequestText)))
		observation := observeFinalizedRequest(t, fixture, "replace-request")
		history := historyToolResults(t, fixture, "replace-history")

		// Assert the category of the replacement for the client and the model.
		require.Empty(t, observation.code, observation.message)
		failure := programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE
		assertToolResultCategory(
			t, observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT], failure,
		)
		assertToolResultCategory(t, history, failure)
		assert.Equal(t, categoryReplacementText, toolResultText(history[0]))
		received := bodies.snapshot()
		require.Len(t, received, 2)
		assert.Contains(t, string(received[1]), categoryFailureName)
		assert.NotContains(t, string(received[1]), categoryUnavailableName)
	})
	t.Run("category outside the closed set", func(t *testing.T) {
		// Arrange a reference that replaces the result with an unknown category.
		paths := testPaths(t, userInputSettings(false))
		writeProgrammaticCredentials(t, paths)
		installCategoryTransport(t, categoryMissingTool, "{}")
		fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized-bad-category"))
		defer fixture.closeOwner(t)

		// Act through the request.
		require.NoError(t, fixture.stream.Send(userRequest("bad-request", finalizedRequestText)))
		observation := observeFinalizedRequest(t, fixture, "bad-request")

		// Assert the extension failure code, the cause that names the category value for the client, and no recorded
		// result.
		assert.Equal(t, extensionFailedCode, observation.code)
		assert.NotEmpty(t, observation.message)
		assert.Contains(t, observation.message, "99")
		assert.Empty(t, observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT])
	})
}
