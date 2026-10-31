//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// finalizedReplacedArguments are the tool-call arguments of the reference arguments scenario.
	finalizedReplacedArguments = `{"value":"final"}`
	// finalizedFailingModelEnvironment selects the one-based model response that the reference fails.
	finalizedFailingModelEnvironment = "GLYPH_FINALIZED_FAILING_MODEL"
)

// persistedToolArguments returns the arguments of each tool call of the persisted model responses.
func persistedToolArguments(t *testing.T, fixture *programmaticFixture, operationID string) []string {
	t.Helper()
	entries := sendProgrammaticOperation(t, fixture, operationID, func(request *programmaticv1.OpenRequest) {
		programmaticRequest(request).SetGetMessages(new(programmaticv1.GetMessages))
	}).GetMessages().GetEntries()
	var arguments []string
	for _, entry := range entries {
		for _, item := range entry.GetModel().GetContent() {
			if call := item.GetToolCall(); call != nil {
				arguments = append(arguments, string(call.GetArgumentsJson()))
			}
		}
	}
	return arguments
}

// TestPublicFinalizedToolArguments verifies that a model replacement with changed tool-call arguments controls the
// executed call, its completion events, the persisted response, and the next provider request.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicFinalizedToolArguments(t *testing.T) {
	// Arrange the reference that replaces tool-call arguments and a provider that requests the reference tool.
	trace := enableFinalizedTrace(t)
	paths := testPaths(t, userInputSettings(false))
	writeProgrammaticCredentials(t, paths)
	bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
		return finalizedResponse(true, position), nil
	})
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized-arguments"))
	defer fixture.closeOwner(t)

	// Act through the tool-using request and a history query.
	require.NoError(t, fixture.stream.Send(userRequest("arguments-request", finalizedRequestText)))
	observation := observeFinalizedRequest(t, fixture, "arguments-request")
	persisted := persistedToolArguments(t, fixture, "arguments-history")

	// Assert the executed arguments, the final values in events and history, and the next provider request.
	require.Empty(t, observation.code, observation.message)
	var executed []string
	for _, entry := range readContextTrace(t, trace) {
		if entry.handler == finalizedEcho {
			executed = append(executed, entry.payload)
		}
	}
	assert.Equal(t, []string{finalizedReplacedArguments}, executed)
	assert.Equal(t, []string{finalizedReplacedArguments}, observation.arguments)
	assert.Equal(t, []string{finalizedReplacedArguments}, persisted)
	finalEcho := finalizedEchoOutput + finalizedToolSuffix
	assert.Equal(t, []string{finalEcho},
		observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_EXECUTION_END])
	assert.Equal(t, []string{finalEcho}, observation.events[programmaticv1.AgentEventType_AGENT_EVENT_TYPE_TOOL_RESULT])
	received := bodies.snapshot()
	require.Len(t, received, 2)
	assert.Contains(t, string(received[1]), `\"value\":\"final\"`)
}

// TestPublicFinalizedReplay verifies that model finalization keeps private replay without exposing it to the
// extension and that the next provider request receives it.
//
//nolint:paralleltest // The scenario replaces process-global provider HTTP transport and environment.
func TestPublicFinalizedReplay(t *testing.T) {
	// Arrange a provider whose first response contains private reasoning replay.
	trace := enableFinalizedTrace(t)
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
		if position == 1 {
			return runtimeFailureHTTPResponse(contextReplayResponseSSE), nil
		}
		return runtimeFailureHTTPResponse(finalResponseSSE), nil
	})
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildFinalizedFixture(t, "finalized"))
	defer fixture.closeOwner(t)

	// Act through two requests; the second one carries the first finalized model response in its history.
	completeProgrammaticRequest(t, fixture, userRequest("replay-first", "first input"))
	completeProgrammaticRequest(t, fixture, userRequest("replay-second", "second input"))
	history := finalizedHistory(t, fixture, "replay-history")

	// Assert that the extension saw only a reference and the provider received the retained private replay.
	referenceSeen := false
	for _, entry := range readContextTrace(t, trace) {
		require.NotContains(t, entry.payload, contextPrivateReplay)
		referenceSeen = referenceSeen || strings.Contains(entry.payload, "replayReference")
	}
	assert.True(t, referenceSeen)
	received := bodies.snapshot()
	require.Len(t, received, 2)
	assert.Contains(t, string(received[1]), contextPrivateReplay)
	assert.Contains(t, history, "model: "+finalizedFinalText+finalizedModelSuffix)
}

// TestPublicFinalizedConfiguredFailure verifies that a failed configured-model finalization reaches its public caller
// as an extension failure with the complete text.
func TestPublicFinalizedConfiguredFailure(t *testing.T) {
	// Arrange a tool that awaits one configured request and a reference that fails the configured response.
	directory := buildPublicExtensionFixture(t)
	t.Setenv(finalizedFailingModelEnvironment, "2")
	binary, err := os.ReadFile(filepath.Join(buildFinalizedFixture(t, "finalized"), "finalized"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "finalized"), binary, 0o700))
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := configuredRequestProviderTransport(t, "configured-request", false)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)

	// Act through the outer request whose tool performs the configured request.
	failure, _ := requestProgrammaticFailure(t, fixture, userRequest("configured-failure", "request configured model"))

	// Assert the category, the complete cause, and one configured dispatch.
	assert.Equal(t, extensionFailedCode, failure.Code)
	assert.Contains(t, failure.Message, "finalize configured model response: ")
	assert.Contains(t, failure.Message, `middleware handler "finalized-mark": `+finalizedFailureText)
	assert.Len(t, bodies.snapshot(), 2)
}

// TestPublicFinalizedOverflowFailure verifies that a failed finalization of a response retained after a failed
// overflow recovery determines the extension-failure category and keeps the three complete causes.
func TestPublicFinalizedOverflowFailure(t *testing.T) {
	// Arrange an overflow owner whose final rejection fails recovery and a reference that fails the retained response.
	trace := enableFinalizedTrace(t)
	t.Setenv(finalizedFailingModelEnvironment, "3")
	t.Setenv(conversationProviderOverflowEnvironment, manualFixtureEnabled)
	t.Setenv(automaticFixtureModeEnvironment, automaticFixtureRejectFinalMode)
	t.Setenv(automaticFixtureTargetEnvironment, "unsubmitted automatic target")
	t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
	t.Setenv(automaticFixtureOutgoingEnvironment, filepath.Join(t.TempDir(), "overflow-outgoing.json"))
	t.Setenv(automaticFixtureOutcomeEnvironment, filepath.Join(t.TempDir(), "overflow-outcome.json"))
	directory := buildManualPolicyFixture(t)
	binary, err := os.ReadFile(filepath.Join(buildFinalizedFixture(t, "finalized-preserve"), "finalized-preserve"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "finalized-preserve"), binary, 0o700))
	settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 4000")
	settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 500")
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("overflow-retained", manualRetainedText))

	// Act through provider overflow, failed recovery, and failed finalization of the retained response.
	failure, _ := requestProgrammaticFailure(t, fixture, userRequest("overflow-failure", manualNextText))

	// Assert the extension category, the three complete causes, no recovery request, and one finalization.
	assert.Equal(t, extensionFailedCode, failure.Code)
	assert.Contains(t, failure.Message, `middleware handler "finalized-mark": `+finalizedFailureText)
	assert.Contains(t, failure.Message, "provider context limit source")
	assert.Contains(t, failure.Message, automaticFixtureFinalCause)
	assert.Len(t, bodies.snapshot(), 3)
	handlers := contextTraceHandlers(readContextTrace(t, trace))
	require.NotEmpty(t, handlers)
	assert.Equal(t, finalizedMark, handlers[len(handlers)-1])
}
