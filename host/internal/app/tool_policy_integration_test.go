//go:build integration

package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// toolPolicyTraceEnvironment names the trace file written by the tool-policy reference extension.
	toolPolicyTraceEnvironment = "GLYPH_TOOLPOLICY_TRACE"
	// toolPolicyRunID identifies the user request of the tool-policy scenarios.
	toolPolicyRunID = "tool-policy-run"
	// toolPolicyToolName is the tool that the reference tool executable owns.
	toolPolicyToolName = "policy_echo"
	// toolPolicyToolOwner is the executable that owns the tool and registers no handler.
	toolPolicyToolOwner = "toolpolicy-tool"
	// toolPolicyReplaceFirst replaces the input of every call.
	toolPolicyReplaceFirst = "toolpolicy-1-replace"
	// toolPolicyRejectFirst rejects the calls with the value forbidden.
	toolPolicyRejectFirst = "toolpolicy-1-reject"
	// toolPolicyAllowFirst allows every call as the first handler.
	toolPolicyAllowFirst = "toolpolicy-1-allow"
	// toolPolicyAllowSecond allows every call as the second handler.
	toolPolicyAllowSecond = "toolpolicy-2-allow"
	// toolPolicyOriginalArguments are the arguments that the model requests in the replacement scenario.
	toolPolicyOriginalArguments = `{"value":"original"}`
	// toolPolicyReplacedArguments are the arguments that the replacing handler sets.
	toolPolicyReplacedArguments = `{"value":"replaced"}`
	// toolPolicyForbiddenArguments are the arguments that the rejecting handler rejects.
	toolPolicyForbiddenArguments = `{"value":"forbidden"}`
	// toolPolicyPermittedArguments are the arguments that the rejecting handler allows.
	toolPolicyPermittedArguments = `{"value":"permitted"}`
	// toolPolicyInvalidArguments violate the input schema of the tool.
	toolPolicyInvalidArguments = `{"value":1}`
	// toolPolicyRejectReason is the complete reason of the rejecting handler.
	toolPolicyRejectReason = "tool policy forbids the value forbidden; use another value"
	// toolPolicyFailureText is the complete error of the failing handler.
	toolPolicyFailureText = "reference tool policy failed"
	// toolPolicyToolOutput starts the result text of the tool, followed by the value of the call.
	toolPolicyToolOutput = "policy tool ran with "
	// toolPolicyCategoryName is the text that tells the model about the policy-rejection category.
	toolPolicyCategoryName = "policy_rejection"
	// toolPolicySteerText is the steer message that the client sends during the rejected call.
	toolPolicySteerText = "unique policy steer instruction"
)

// buildToolPolicyFixture builds the public-only tool-policy reference once for each executable name into one
// extension directory; the directory order of the names is the registration order of their handlers.
func buildToolPolicyFixture(t *testing.T, names ...string) string {
	t.Helper()
	directory := t.TempDir()
	for _, name := range names {
		buildExternalCommand(t, directory, "toolpolicy", name)
	}
	return directory
}

// toolPolicyTraceLine builds the expected trace line of a handler invocation with its original and current input.
func toolPolicyTraceLine(executable, original, current string) string {
	return executable + "\t" + original + "\t" + current
}

// toolPolicyToolLine builds the expected trace line of an execution of the tool with its final input.
func toolPolicyToolLine(arguments string) string {
	return "tool\t" + arguments
}

// TestPublicToolPolicyReplacesInput verifies that the second handler receives the original call and the call that the
// first handler replaced, and that the tool runs with the replaced input and gives the model its result.
//
//nolint:paralleltest // The scenario sets the trace environment of the extension processes.
func TestPublicToolPolicyReplacesInput(t *testing.T) {
	// Arrange two policy extensions and the tool extension, and a provider that calls the tool once.
	trace := enableTraceFile(t, toolPolicyTraceEnvironment)
	provider := startQueueProvider(
		t, 0, toolCallSSE(t, "call-1", toolPolicyToolName, toolPolicyOriginalArguments), finalResponseSSE,
	)
	fixture := startQueueFixture(
		t, provider, buildToolPolicyFixture(t, toolPolicyReplaceFirst, toolPolicyAllowSecond, toolPolicyToolOwner),
	)
	defer fixture.closeOwner(t)

	// Act through one user request.
	require.NoError(t, fixture.stream.Send(userRequest(toolPolicyRunID, "run the tool")))
	observation := observeFinalizedRequest(t, fixture, toolPolicyRunID)

	// Assert the handler order with the original and current input, the replaced input of the tool, and its result.
	require.Empty(t, observation.code, observation.message)
	assert.Equal(t, []string{
		toolPolicyTraceLine(toolPolicyReplaceFirst, toolPolicyOriginalArguments, toolPolicyOriginalArguments),
		toolPolicyTraceLine(toolPolicyAllowSecond, toolPolicyOriginalArguments, toolPolicyReplacedArguments),
		toolPolicyToolLine(toolPolicyReplacedArguments),
	}, readTraceLines(t, trace))
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[1], toolPolicyToolOutput+"replaced")
	results := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT]
	require.Len(t, results, 1)
	assert.False(t, results[0].GetIsError())
}

// TestPublicToolPolicyRejectsCall verifies that a rejection gives the client and the model an error result with the
// policy category and the complete reason without running the tool or a later handler, that a steer message sent
// meanwhile reaches the same model request, and that the extension handles a later call.
//
//nolint:paralleltest // The scenario sets the trace environment of the extension processes.
func TestPublicToolPolicyRejectsCall(t *testing.T) {
	// Arrange a rejecting first handler, a second handler, and a provider that holds its first request.
	trace := enableTraceFile(t, toolPolicyTraceEnvironment)
	provider := startQueueProvider(
		t, 1,
		toolCallSSE(t, "call-1", toolPolicyToolName, toolPolicyForbiddenArguments),
		toolCallSSE(t, "call-2", toolPolicyToolName, toolPolicyPermittedArguments),
		finalResponseSSE,
	)
	fixture := startQueueFixture(
		t, provider, buildToolPolicyFixture(t, toolPolicyRejectFirst, toolPolicyAllowSecond, toolPolicyToolOwner),
	)
	defer fixture.closeOwner(t)
	require.NoError(t, fixture.stream.Send(userRequest(toolPolicyRunID, "run the tool")))
	<-provider.held[0]

	// Act by sending a steer message while the model requests the forbidden call.
	queued := sendSteer(t, fixture, "policy-steer", toolPolicySteerText)
	close(provider.release[0])
	observation := observeFinalizedRequest(t, fixture, toolPolicyRunID)
	history := historyToolResults(t, fixture, "policy-history")

	// Assert the rejection reaches the client with its category and reason, the second handler and the tool did not
	// run for it, and the extension handled the later call.
	requireQueued(t, queued)
	require.Empty(t, observation.code, observation.message)
	assert.Equal(t, []string{
		toolPolicyTraceLine(toolPolicyRejectFirst, toolPolicyForbiddenArguments, toolPolicyForbiddenArguments),
		toolPolicyTraceLine(toolPolicyRejectFirst, toolPolicyPermittedArguments, toolPolicyPermittedArguments),
		toolPolicyTraceLine(toolPolicyAllowSecond, toolPolicyPermittedArguments, toolPolicyPermittedArguments),
		toolPolicyToolLine(toolPolicyPermittedArguments),
	}, readTraceLines(t, trace))
	results := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT]
	require.Len(t, results, 2)
	assert.True(t, results[0].GetIsError())
	assert.Equal(
		t, programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION, results[0].GetErrorCategory(),
	)
	assert.Contains(t, toolResultText(results[0]), toolPolicyRejectReason)
	assert.False(t, results[1].GetIsError())
	require.Len(t, history, 2)
	assert.Equal(
		t, programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION, history[0].GetErrorCategory(),
	)
	bodies := provider.snapshot()
	require.Len(t, bodies, 3)
	reasonIndex := strings.Index(bodies[1], toolPolicyRejectReason)
	steerIndex := strings.Index(bodies[1], toolPolicySteerText)
	require.NotEqual(t, -1, reasonIndex)
	require.NotEqual(t, -1, steerIndex)
	assert.Less(t, reasonIndex, steerIndex)
	assert.Contains(t, bodies[1], toolPolicyCategoryName)
	assert.NotContains(t, bodies[1], toolPolicyToolOutput)
	assert.Contains(t, bodies[2], toolPolicyToolOutput+"permitted")
}

// TestPublicToolPolicyFailureFailsRun verifies that a handler error, a replacement that violates the tool schema, and
// an invalid action stop the chain, leave the tool unrun, and fail the agent run with the complete cause and without
// another model request.
//
//nolint:paralleltest // The scenarios set the trace environment of the extension processes.
func TestPublicToolPolicyFailureFailsRun(t *testing.T) {
	tests := []struct {
		// name describes the failure.
		name string
		// failing is the executable that fails as the first handler.
		failing string
		// cause is text that the failure message must contain, when a requirement defines it.
		cause string
	}{
		{name: "handler error", failing: "toolpolicy-1-fail", cause: toolPolicyFailureText},
		{name: "schema-invalid replacement", failing: "toolpolicy-1-invalid", cause: ""},
		{name: "invalid action", failing: "toolpolicy-1-empty", cause: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange the failing first handler, an allowing second handler, and a provider that calls the tool.
			trace := enableTraceFile(t, toolPolicyTraceEnvironment)
			provider := startQueueProvider(
				t, 0, toolCallSSE(t, "call-1", toolPolicyToolName, toolPolicyOriginalArguments), finalResponseSSE,
			)
			fixture := startQueueFixture(
				t, provider, buildToolPolicyFixture(t, test.failing, toolPolicyAllowSecond, toolPolicyToolOwner),
			)
			defer fixture.closeOwner(t)

			// Act through one user request.
			require.NoError(t, fixture.stream.Send(userRequest(toolPolicyRunID, "run the tool")))
			observation := observeFinalizedRequest(t, fixture, toolPolicyRunID)

			// Assert the extension failure with the complete cause, only the first handler invoked, no tool run, and
			// no repeated model request.
			assert.Equal(t, extensionFailedCode, observation.code)
			assert.Contains(t, observation.message, test.cause)
			assert.NotEmpty(t, observation.message)
			assert.Equal(t, []string{
				toolPolicyTraceLine(test.failing, toolPolicyOriginalArguments, toolPolicyOriginalArguments),
			}, readTraceLines(t, trace))
			assert.Len(t, provider.snapshot(), 1)
		})
	}
}

// TestPublicToolPolicyInvalidOriginalArguments verifies that Host validates the original arguments after the chain when
// no handler replaced them, and that the model receives the invalid-arguments result while the tool does not run.
//
//nolint:paralleltest // The scenario sets the trace environment of the extension processes.
func TestPublicToolPolicyInvalidOriginalArguments(t *testing.T) {
	// Arrange an allowing handler and a provider that calls the tool with schema-invalid arguments.
	trace := enableTraceFile(t, toolPolicyTraceEnvironment)
	provider := startQueueProvider(
		t, 0, toolCallSSE(t, "call-1", toolPolicyToolName, toolPolicyInvalidArguments), finalResponseSSE,
	)
	fixture := startQueueFixture(t, provider, buildToolPolicyFixture(t, toolPolicyAllowFirst, toolPolicyToolOwner))
	defer fixture.closeOwner(t)

	// Act through one user request.
	require.NoError(t, fixture.stream.Send(userRequest(toolPolicyRunID, "run the tool")))
	observation := observeFinalizedRequest(t, fixture, toolPolicyRunID)

	// Assert the handler saw the call, the tool did not run, and the client and the model got the category.
	require.Empty(t, observation.code, observation.message)
	assert.Equal(t, []string{
		toolPolicyTraceLine(toolPolicyAllowFirst, toolPolicyInvalidArguments, toolPolicyInvalidArguments),
	}, readTraceLines(t, trace))
	results := observation.toolResults[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT]
	require.Len(t, results, 1)
	assert.Equal(
		t, programmaticv1.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS, results[0].GetErrorCategory(),
	)
	bodies := provider.snapshot()
	require.Len(t, bodies, 2)
	assert.Contains(t, bodies[1], categoryInvalidName)
}
