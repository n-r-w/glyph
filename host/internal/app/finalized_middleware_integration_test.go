//go:build integration

package app

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// finalizedTraceEnvironment names the trace file written by the finalized-message reference extension.
	finalizedTraceEnvironment = "GLYPH_FINALIZED_TRACE"
	// finalizedSignalsEnvironment names the directory that receives the blocked-invocation marker.
	finalizedSignalsEnvironment = "GLYPH_FINALIZED_SIGNALS"
	// finalizedBlockedSignal is the marker written when the reference starts its blocked invocation.
	finalizedBlockedSignal = "finalized-blocked"
	// finalizedMark identifies the reference handler that marks each finalized message.
	finalizedMark = "finalized-mark"
	// finalizedLater identifies the reference handler that preserves each finalized message.
	finalizedLater = "finalized-later"
	// finalizedEcho identifies the traced call of the reference tool.
	finalizedEcho = "finalized_echo"
	// finalizedUserSuffix is appended to every user text block.
	finalizedUserSuffix = " [final user]"
	// finalizedModelSuffix is appended to every model text block.
	finalizedModelSuffix = " [final model]"
	// finalizedToolSuffix is appended to every tool-result text block.
	finalizedToolSuffix = " [final tool]"
	// finalizedFailureText is the ordinary error of the reference failure scenarios.
	finalizedFailureText = "reference finalized handler failed"
	// finalizedRequestText is the text of the first request.
	finalizedRequestText = "finalize me"
	// finalizedFollowUpText is the text of the request after the first one.
	finalizedFollowUpText = "follow up"
	// finalizedToolText is the visible text of the tool-use response.
	finalizedToolText = "Calling tool."
	// finalizedFinalText is the visible text of finalResponseSSE.
	finalizedFinalText = "Request complete."
	// finalizedEchoOutput is the result text of the reference tool.
	finalizedEchoOutput = "echo output"
	// finalizedToolSSE returns visible text and one call of the reference tool.
	finalizedToolSSE = `data: {"type":"response.completed","response":{"id":"resp-tool","model":"gpt-test",` +
		`"status":"completed","output":[` +
		`{"id":"msg-tool","type":"message","role":"assistant","status":"completed",` +
		`"content":[{"type":"output_text","text":"` + finalizedToolText + `","annotations":[],"logprobs":[]}]},` +
		`{"id":"fc-1","type":"function_call","call_id":"call-1","name":"` + finalizedEcho + `",` +
		`"arguments":"{}","status":"completed"}]}}` + "\n\n" + "data: [DONE]\n\n"
	// finalizedProviderFailure is the complete text of a rejected provider request.
	finalizedProviderFailure = "finalized provider rejection text"
)

// finalizedPair lists the two reference handlers of one finalized message in invocation order.
var finalizedPair = []string{finalizedMark, finalizedLater}

// buildFinalizedFixture builds the public-only finalized-message reference with the selected executable scenario.
func buildFinalizedFixture(t *testing.T, mode string) string {
	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(directory, mode), "./cmd/finalized")
	command.Dir = filepath.Join(repoRoot(t), "testdata", "external-plugins")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}

// enableFinalizedTrace makes the finalized-message reference record each invocation and tool call.
func enableFinalizedTrace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "finalized-trace.tsv")
	t.Setenv(finalizedTraceEnvironment, path)
	return path
}

// finalizedHandlers repeats the handler pair once for each finalized message.
func finalizedHandlers(messages int) []string {
	handlers := make([]string, 0, 2*messages)
	for range messages {
		handlers = append(handlers, finalizedPair...)
	}
	return handlers
}

// finalizedResponse returns the tool-use response at the first position when requested and finalResponseSSE
// otherwise.
func finalizedResponse(toolFirst bool, position int) *http.Response {
	if toolFirst && position == 1 {
		return runtimeFailureHTTPResponse(finalizedToolSSE)
	}
	return runtimeFailureHTTPResponse(finalResponseSSE)
}

// rejectFirstProviderRequest rejects the first provider request and completes every later one.
func rejectFirstProviderRequest(position int) (*http.Response, error) {
	if position == 1 {
		response := runtimeFailureHTTPResponse(`{"error":{"message":"` + finalizedProviderFailure + `"}}`)
		response.StatusCode = http.StatusBadRequest
		response.Header.Set("Content-Type", "application/json")
		return response, nil
	}
	return runtimeFailureHTTPResponse(finalResponseSSE), nil
}

// finalizedObservation is the public result of one Programmatic user request.
type finalizedObservation struct {
	// code is the failure category; empty means completion.
	code string
	// message is the complete failure text.
	message string
	// canceled reports a canceled terminal state.
	canceled bool
	// events contains the text of each completion event, keyed by event type.
	events map[programmaticv1.AgentEventType][]string
	// arguments contains the arguments of each tool call of each message-end event.
	arguments []string
}

// observeFinalizedRequest reads the agent events and the terminal state of one sent user request.
func observeFinalizedRequest(t *testing.T, fixture *programmaticFixture, operationID string) finalizedObservation {
	t.Helper()
	observation := finalizedObservation{
		code: "", message: "", canceled: false, events: make(map[programmaticv1.AgentEventType][]string),
		arguments: nil,
	}
	for {
		_, event := receiveOperationEvent(t, fixture, operationID)
		if agentEvent := event.GetProgress().GetAgentEvent(); agentEvent != nil {
			observation.record(agentEvent)
		}
		switch {
		case event.HasCompleted():
			return observation
		case event.HasCanceled():
			observation.canceled = true
			return observation
		case event.HasFailed():
			observation.code = event.GetFailed().GetFailure().GetCode()
			observation.message = event.GetFailed().GetFailure().GetMessage()
			return observation
		}
		require.False(t, event.HasRejected(), "request was rejected: %v", event)
	}
}

// record keeps the final text of message-end, tool-execution-end, tool-result, and turn-end events and the tool-call
// arguments of message-end events.
func (observation *finalizedObservation) record(event *programmaticv1.AgentEvent) {
	//nolint:exhaustive // Only completion events carry final message values.
	switch event.GetType() {
	case programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END:
		observation.events[event.GetType()] = append(
			observation.events[event.GetType()],
			event.GetModelResponse().GetText(),
		)
		for _, item := range event.GetModelResponse().GetContent() {
			if call := item.GetToolCall(); call != nil {
				observation.arguments = append(observation.arguments, string(call.GetArgumentsJson()))
			}
		}
	case programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT,
		programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END:
		observation.events[event.GetType()] = append(
			observation.events[event.GetType()], toolResultText(event.GetToolResult()),
		)
	case programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END:
		turn := event.GetTurn()
		texts := []string{turn.GetResponse().GetText()}
		for _, result := range turn.GetToolResults() {
			texts = append(texts, toolResultText(result))
		}
		observation.events[event.GetType()] = append(observation.events[event.GetType()], strings.Join(texts, " | "))
	default:
	}
}

// toolResultText joins the text blocks of one tool result.
func toolResultText(result *programmaticv1.ToolResult) string {
	texts := make([]string, 0, len(result.GetContents()))
	for _, content := range result.GetContents() {
		texts = append(texts, content.GetText())
	}
	return strings.Join(texts, "")
}

// finalizedHistory returns each user, model, and tool-result entry of the active session as role and text.
func finalizedHistory(t *testing.T, fixture *programmaticFixture, operationID string) []string {
	t.Helper()
	entries := sendProgrammaticOperation(t, fixture, operationID, func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	history := make([]string, 0, len(entries))
	for _, entry := range entries {
		switch {
		case entry.HasUser():
			texts := make([]string, 0, len(entry.GetUser().GetContent()))
			for _, content := range entry.GetUser().GetContent() {
				texts = append(texts, content.GetText())
			}
			history = append(history, "user: "+strings.Join(texts, ""))
		case entry.HasModel():
			history = append(history, "model: "+entry.GetModel().GetText())
		case entry.HasToolResult():
			history = append(history, "tool: "+toolResultText(entry.GetToolResult()))
		}
	}
	return history
}

// TestPublicFinalizedMessages verifies that finalized user, model, and tool-result replacements are identical in
// persisted entries, completion events, and the next provider requests.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicFinalizedMessages(t *testing.T) {
	// Arrange the reference extension and a provider that requests the reference tool once.
	trace := enableFinalizedTrace(t)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
		return finalizedResponse(true, position), nil
	})
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized"))
	defer fixture.closeOwner(t)

	// Act through a tool-using request, a follow-up request, and a history query.
	require.NoError(t, fixture.stream.Send(userRequest("finalized-request", finalizedRequestText)))
	observation := observeFinalizedRequest(t, fixture, "finalized-request")
	completeProgrammaticRequest(t, fixture, userRequest("finalized-follow-up", finalizedFollowUpText))
	history := finalizedHistory(t, fixture, "finalized-history")

	// Assert one final value per message in history, events, provider requests, and handler invocations.
	finalUser := finalizedRequestText + finalizedUserSuffix
	finalTool := finalizedToolText + finalizedModelSuffix
	finalEcho := finalizedEchoOutput + finalizedToolSuffix
	finalText := finalizedFinalText + finalizedModelSuffix
	require.Empty(t, observation.code, observation.message)
	assert.Equal(t, []string{
		"user: " + finalUser, "model: " + finalTool, "tool: " + finalEcho, "model: " + finalText,
		"user: " + finalizedFollowUpText + finalizedUserSuffix, "model: " + finalText,
	}, history)
	assert.Equal(t, []string{finalTool, finalText},
		observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END])
	assert.Equal(t, []string{finalEcho}, observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT])
	assert.Equal(t, []string{finalEcho},
		observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END])
	assert.Equal(t, []string{finalTool + " | " + finalEcho, finalText},
		observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TURN_END])
	received := bodies.snapshot()
	require.Len(t, received, 3)
	for _, value := range []string{finalUser, finalTool, finalEcho} {
		assert.Contains(t, string(received[1]), value)
	}
	for _, original := range []string{finalizedRequestText + `"`, finalizedToolText + `"`, finalizedEchoOutput + `"`} {
		assert.NotContains(t, string(received[1]), original)
	}
	assert.Contains(t, string(received[2]), finalText)
	assert.NotContains(t, string(received[2]), finalizedFinalText+`"`)
	expected := finalizedHandlers(2)
	expected = append(expected, finalizedEcho)
	expected = append(expected, finalizedHandlers(4)...)
	assert.Equal(t, expected, contextTraceHandlers(readContextTrace(t, trace)))
}

// TestPublicFinalizedMessageFailures verifies that a failed or invalid finalized-message chain stops the request
// without its candidate, later handlers, or a provider retry, and that the next request succeeds.
//
//nolint:paralleltest // The scenarios replace process-global provider HTTP transport and environment.
func TestPublicFinalizedMessageFailures(t *testing.T) {
	finalUser := "user: " + finalizedRequestText + finalizedUserSuffix
	followUp := []string{
		"user: " + finalizedFollowUpText + finalizedUserSuffix, "model: " + finalizedFinalText + finalizedModelSuffix,
	}
	for _, test := range []struct {
		// name identifies the scenario.
		name string
		// mode selects the reference executable.
		mode string
		// toolFirst makes the first provider response request the reference tool.
		toolFirst bool
		// code is the failure category of the first request.
		code string
		// cause is a part of the complete failure text.
		cause string
		// handlers lists the traced invocations of the first request.
		handlers []string
		// history lists the entries of the first request that remain.
		history []string
		// requests is the number of provider requests of the first request.
		requests int
	}{
		{
			name: "tool-result handler failure", mode: "finalized-fail-tool", toolFirst: true,
			code: extensionFailedCode, cause: `middleware handler "finalized-mark": ` + finalizedFailureText,
			handlers: append(finalizedHandlers(2), finalizedEcho, finalizedMark),
			history:  []string{finalUser, "model: " + finalizedToolText + finalizedModelSuffix}, requests: 1,
		},
		{
			name: "changed role", mode: "finalized-role", toolFirst: false, code: extensionFailedCode,
			cause:    "message role user differs from original role model",
			handlers: append(finalizedHandlers(1), finalizedMark), history: []string{finalUser}, requests: 1,
		},
		{
			name: "unknown replay reference", mode: "finalized-bad-reference", toolFirst: false,
			code: extensionFailedCode, cause: `unknown private replay reference "unknown"`,
			handlers: append(finalizedHandlers(1), finalizedMark), history: []string{finalUser}, requests: 1,
		},
		{
			name: "invalid action", mode: "finalized-invalid", toolFirst: false, code: extensionFailedCode,
			cause:    `middleware handler "finalized-mark" returned invalid finalized-message action 0`,
			handlers: append(finalizedHandlers(1), finalizedMark), history: []string{finalUser}, requests: 1,
		},
		{
			name: "user image on text-only model", mode: "finalized-image", toolFirst: false, code: userInputInvalidCode,
			cause:    "process user input: model gpt-active does not accept image input at content 1",
			handlers: finalizedHandlers(1), history: nil, requests: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange the reference scenario and a recording provider.
			trace := enableFinalizedTrace(t)
			paths := testPaths(t, userInputSettings(false))
			writeProgrammaticCredentials(t, paths)
			bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
				return finalizedResponse(test.toolFirst, position), nil
			})
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, test.mode))
			defer fixture.closeOwner(t)

			// Act through a failing request, a follow-up request, and a history query.
			require.NoError(t, fixture.stream.Send(userRequest("failing-request", finalizedRequestText)))
			observation := observeFinalizedRequest(t, fixture, "failing-request")
			failedRequests := len(bodies.snapshot())
			failedHandlers := contextTraceHandlers(readContextTrace(t, trace))
			completeProgrammaticRequest(t, fixture, userRequest("follow-up-request", finalizedFollowUpText))
			history := finalizedHistory(t, fixture, "failure-history")

			// Assert the category, the complete cause, no later work, and retained earlier entries.
			assert.Equal(t, test.code, observation.code)
			assert.Contains(t, observation.message, test.cause)
			assert.Equal(t, test.requests, failedRequests)
			assert.Equal(t, test.handlers, failedHandlers)
			assert.Equal(t, append(test.history, followUp...), history)
			assert.Empty(t, observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT])
		})
	}
}

// TestPublicFinalizedProviderFailure verifies that a failed finalization of a retained failed response determines the
// extension-failure category, keeps both complete causes, and records no response.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicFinalizedProviderFailure(t *testing.T) {
	// Arrange a provider that rejects the first request and a reference that fails the first model response.
	trace := enableFinalizedTrace(t)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := installStandardOverflowTransport(t, rejectFirstProviderRequest)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized-fail-model"))
	defer fixture.closeOwner(t)

	// Act through the failing request, a follow-up request, and a history query.
	require.NoError(t, fixture.stream.Send(userRequest("provider-failure", finalizedRequestText)))
	observation := observeFinalizedRequest(t, fixture, "provider-failure")
	failedRequests := len(bodies.snapshot())
	failedHandlers := contextTraceHandlers(readContextTrace(t, trace))
	completeProgrammaticRequest(t, fixture, userRequest("provider-follow-up", finalizedFollowUpText))
	history := finalizedHistory(t, fixture, "provider-history")

	// Assert the extension category, both causes, one finalization, and no recorded failed response.
	assert.Equal(t, extensionFailedCode, observation.code)
	assert.Contains(t, observation.message, finalizedProviderFailure)
	assert.Contains(t, observation.message, `middleware handler "finalized-mark": `+finalizedFailureText)
	assert.Equal(t, 1, failedRequests)
	assert.Equal(t, append(finalizedHandlers(1), finalizedMark), failedHandlers)
	assert.Equal(t, []string{
		"user: " + finalizedRequestText + finalizedUserSuffix,
		"user: " + finalizedFollowUpText + finalizedUserSuffix, "model: " + finalizedFinalText + finalizedModelSuffix,
	}, history)
}

// TestPublicFinalizedCancellation verifies that canceling a blocked model finalization ends the request without its
// candidate and without another invocation of the canceled chain.
func TestPublicFinalizedCancellation(t *testing.T) {
	// Arrange the reference whose first model finalization blocks until Host cancels it.
	trace := enableFinalizedTrace(t)
	signals := t.TempDir()
	t.Setenv(finalizedSignalsEnvironment, signals)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
		return finalizedResponse(false, position), nil
	})
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized-block"))
	defer fixture.closeOwner(t)

	// Act by canceling the blocked request, then sending a follow-up request and a history query.
	require.NoError(t, fixture.stream.Send(userRequest("blocked-request", finalizedRequestText)))
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(signals, finalizedBlockedSignal))
		return err == nil
	}, 30*time.Second, inputSignalPoll)
	require.NoError(t, fixture.stream.Send(cancelRequest("cancel-blocked", "blocked-request")))
	targetCanceled, cancelCompleted := false, false
	for !targetCanceled || !cancelCompleted {
		id, event := receiveOperationEvent(t, fixture, "blocked-request", "cancel-blocked")
		require.False(t, event.HasFailed() || event.HasRejected(), "%s did not end as expected: %v", id, event)
		targetCanceled = targetCanceled || id == "blocked-request" && event.HasCanceled()
		if id == "cancel-blocked" && event.HasCompleted() {
			cancelCompleted = true
			assert.Equal(t, operationv1.TerminalState_TERMINAL_STATE_CANCELED,
				event.GetCompleted().GetCancel().GetTargetState())
		}
	}
	canceledHandlers := contextTraceHandlers(readContextTrace(t, trace))
	completeProgrammaticRequest(t, fixture, userRequest("after-cancel", finalizedFollowUpText))
	history := finalizedHistory(t, fixture, "cancel-history")

	// Assert one blocked invocation, no later handler, no recorded response, and a successful follow-up.
	assert.Equal(t, append(finalizedHandlers(1), finalizedMark), canceledHandlers)
	assert.Len(t, bodies.snapshot(), 2)
	assert.Equal(t, []string{
		"user: " + finalizedRequestText + finalizedUserSuffix,
		"user: " + finalizedFollowUpText + finalizedUserSuffix, "model: " + finalizedFinalText + finalizedModelSuffix,
	}, history)
}
