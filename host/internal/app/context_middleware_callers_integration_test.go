//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// contextTraceEnvironment names the trace file written by the context reference extension.
	contextTraceEnvironment = "GLYPH_CONTEXT_TRACE"
	// contextFailureText is the complete ordinary error returned by context reference failure scenarios.
	contextFailureText = "reference context handler failed: unexpected EOF"
	// summaryContextFailureBehavior makes the UI fixture record a failed navigation and then navigate again.
	summaryContextFailureBehavior = "summary-context-failure"
	// contextPrivateReplay is provider-owned replay data that must never reach an extension.
	contextPrivateReplay = "private-context-replay"
	// contextReplayResponseSSE returns visible reasoning with private replay and final text.
	contextReplayResponseSSE = `data: {"type":"response.completed","response":{"id":"resp-replay",` +
		`"model":"gpt-test","status":"completed","output":[` +
		`{"id":"reason-replay","type":"reasoning","encrypted_content":"` + contextPrivateReplay + `",` +
		`"summary":[{"type":"summary_text","text":"visible replay reasoning"}]},` +
		`{"id":"message-replay","type":"message","role":"assistant","status":"completed",` +
		`"content":[{"type":"output_text","text":"Request complete.","annotations":[],"logprobs":[]}]}]}}` +
		"\n\n" + "data: [DONE]\n\n"
)

// contextTraceEntry is one invocation recorded by the context reference extension.
type contextTraceEntry struct {
	// handler identifies the invoked handler.
	handler string
	// payload contains the protobuf JSON payload received by the handler.
	payload string
}

// enableContextTrace makes the context reference extension record each invocation.
func enableContextTrace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "context-trace.tsv")
	t.Setenv(contextTraceEnvironment, path)
	return path
}

// readContextTrace returns the recorded invocations in order.
func readContextTrace(t *testing.T, path string) []contextTraceEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var entries []contextTraceEntry
	for line := range strings.Lines(string(data)) {
		handler, payload, found := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		require.True(t, found, line)
		entries = append(entries, contextTraceEntry{handler: handler, payload: payload})
	}
	return entries
}

// contextTraceHandlers returns the handler identifiers of recorded invocations in order.
func contextTraceHandlers(entries []contextTraceEntry) []string {
	handlers := make([]string, len(entries))
	for index, entry := range entries {
		handlers[index] = entry.handler
	}
	return handlers
}

// assertComposedRequest checks a request whose base instructions are owned by another caller.
func assertComposedRequest(t *testing.T, body []byte) {
	t.Helper()
	var request struct {
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	require.True(t, strings.HasSuffix(request.Instructions, "\n\n"+contextProjectInstructions(t)), request.Instructions)
	require.NotContains(t, string(body), contextInstructionsMarkerA)
	require.NotContains(t, string(body), contextMarkerA)
	require.Equal(t, 1, strings.Count(string(body), contextMarkerB))
}

// navigateWithSummary runs one Programmatic navigation with branch summary and returns its terminal response.
func navigateWithSummary(t *testing.T, fixture *programmaticFixture, operationID string) *programmaticv1.OpenResponse {
	t.Helper()
	request := testProgrammaticRequest(operationID, func(payload *programmaticv1.ControllerRequest) {
		payload.SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
			TargetEntryId: new("user"),
			SummaryMode:   new(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE),
			CustomFocus:   nil,
		}.Build())
	})
	require.NoError(t, fixture.stream.Send(request))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != operationID || !response.HasEvent() {
			continue
		}
		if response.GetEvent().GetFailed() != nil || response.GetEvent().GetCompleted() != nil {
			return response
		}
	}
}

// TestPublicContextReplayReferences restores or discards private replay chosen by a public history handler.
//
//nolint:paralleltest // These scenarios replace the process-global provider HTTP transport.
func TestPublicContextReplayReferences(t *testing.T) {
	for _, scenario := range []struct {
		mode     string
		retained bool
	}{
		{mode: "context", retained: true},
		{mode: "context-drop-replay", retained: false},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			// Arrange a provider whose first response contains private reasoning replay.
			trace := enableContextTrace(t)
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
				if position == 1 {
					return runtimeFailureHTTPResponse(contextReplayResponseSSE), nil
				}
				return runtimeFailureHTTPResponse(finalResponseSSE), nil
			})
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildContextFixture(t, scenario.mode))
			defer fixture.closeOwner(t)

			// Act through two public requests; the second one carries the first model response in its history.
			completeProgrammaticRequest(t, fixture, userRequest("replay-first", "first input"))
			completeProgrammaticRequest(t, fixture, userRequest("replay-second", "second input"))

			// Assert extensions saw only references and the provider received replay only for a retained reference.
			referenceSeen := false
			for _, entry := range readContextTrace(t, trace) {
				require.NotContains(t, entry.payload, contextPrivateReplay)
				referenceSeen = referenceSeen || strings.Contains(entry.payload, "replayReference")
			}
			require.True(t, referenceSeen)
			received := bodies.snapshot()
			require.Len(t, received, 2)
			require.Contains(t, string(received[0]), "first input")
			require.Equal(t, scenario.retained, strings.Contains(string(received[1]), contextPrivateReplay))
		})
	}
}

// TestPublicContextInvalidReplayReference fails before dispatch and stops the history chain.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicContextInvalidReplayReference(t *testing.T) {
	// Arrange an extension whose first history handler returns an unissued replay reference.
	trace := enableContextTrace(t)
	paths := testPaths(t, codexSettings("retry:\n  enabled: true\n  maxRetries: 2\n  delays: [0s]\n"))
	writeProgrammaticCredentials(t, paths)
	bodies := newRecordingProviderFixture(t, 0, nil)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildContextFixture(t, "context-bad-reference"))
	defer fixture.closeOwner(t)

	// Act through a failing public operation and a later ordinary operation.
	failure, _ := requestProgrammaticFailure(t, fixture, userRequest("bad-reference", "user input"))
	failedCalls := len(bodies.snapshot())
	completeProgrammaticRequest(t, fixture, userRequest("bad-reference-later", "later input"))

	// Assert category, complete cause, no dispatch, no later handler, and a successful later request.
	require.Equal(t, "EXTENSION_FAILED", failure.Code)
	require.Contains(t, failure.Message, `unknown private replay reference "unknown"`)
	require.Zero(t, failedCalls)
	require.Equal(t, []string{
		"instructions-a", "instructions-b", "context-a",
		"instructions-a", "instructions-b", "context-a", "context-b",
	}, contextTraceHandlers(readContextTrace(t, trace)))
	received := bodies.snapshot()
	require.Len(t, received, 1)
	assertContextRequest(t, received[0], codingagent.Instructions(), contextMarkerB)
}

// TestPublicContextConfiguredFailure fails one configured invocation without dispatching it or restarting its chain.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicContextConfiguredFailure(t *testing.T) {
	// Arrange a contextual tool that starts one configured request and an extension failing that invocation.
	trace := enableContextTrace(t)
	directory := buildPublicExtensionFixture(t)
	addContextFixture(t, directory, "context-fail-second")
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := configuredRequestProviderTransport(t, "configured-request", false)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)

	// Act through the outer request whose tool performs the configured request, then a later request.
	failure, _ := requestProgrammaticFailure(t, fixture, userRequest("configured-failure", "request configured model"))
	failedCalls := bodies.snapshot()
	completeProgrammaticRequest(t, fixture, userRequest("configured-later", "later input"))

	// Assert the public category and cause, no configured dispatch, and no later handler of the failed chain.
	require.Equal(t, "EXTENSION_FAILED", failure.Code)
	require.Contains(t, failure.Message, contextFailureText)
	require.Len(t, failedCalls, 1)
	require.NotContains(t, string(failedCalls[0]), `"model":"`+configuredRequestModelID+`"`)
	require.Equal(t, []string{
		"instructions-a", "instructions-b", "context-a", "context-b",
		"instructions-a",
		"instructions-a", "instructions-b", "context-a", "context-b",
	}, contextTraceHandlers(readContextTrace(t, trace)))
	received := bodies.snapshot()
	require.Len(t, received, 2)
	assertContextRequest(t, received[1], codingagent.Instructions(), contextMarkerB)
}

// TestPublicContextBranchSummaryAcrossClients composes branch-summary requests in UI and Programmatic clients.
//
//nolint:paralleltest // Each client scenario replaces process-global provider transport.
func TestPublicContextBranchSummaryAcrossClients(t *testing.T) {
	for _, client := range []string{"ui", "programmatic"} {
		t.Run(client, func(t *testing.T) {
			// Arrange one stored abandoned branch, the context reference, and a summary provider that retries once.
			paths := testPaths(t, retryModeSettings)
			writeProgrammaticCredentials(t, paths)
			seedSummaryControlSession(t, paths)
			transport, bodies := summaryRetryProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			directory := buildContextFixture(t, "context")

			// Act through the selected assembled public navigation operation.
			switch client {
			case "ui":
				trace := filepath.Join(t.TempDir(), "summary-context.json")
				runSummaryControlUI(t, paths, directory, trace, "summary-retry")
				terminalData, err := os.ReadFile(trace)
				require.NoError(t, err)
				terminal := new(uiv1.SessionTreeNavigationResult)
				require.NoError(t, protojson.Unmarshal(terminalData, terminal))
				require.Equal(t, uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
					terminal.GetStatus())
			case "programmatic":
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetResumeSession(
						programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build(),
					)
				})
				terminal := navigateWithSummary(t, fixture, "summary-context")
				require.NotNil(t, terminal.GetEvent().GetCompleted(), "failure: %v", terminal.GetEvent().GetFailed())
				fixture.closeOwner(t)
			}

			// Assert both summary attempts received the single composition of the summary request.
			received := bodies.snapshot()
			require.Len(t, received, 2)
			for _, body := range received {
				assertComposedRequest(t, body)
			}
		})
	}
}

// TestPublicContextBranchSummaryFailureAcrossClients fails one summary before dispatch and allows another.
//
//nolint:paralleltest // Each client scenario replaces process-global provider transport.
func TestPublicContextBranchSummaryFailureAcrossClients(t *testing.T) {
	for _, client := range []string{"ui", "programmatic"} {
		t.Run(client, func(t *testing.T) {
			// Arrange one stored abandoned branch and an extension failing its first instruction invocation.
			trace := enableContextTrace(t)
			paths := testPaths(t, retryModeSettings)
			writeProgrammaticCredentials(t, paths)
			seedSummaryControlSession(t, paths)
			transport, bodies := summaryRetryProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			directory := buildContextFixture(t, "context-fail")
			var code, message string

			// Act through a failing navigation and a later navigation in the same client.
			switch client {
			case "ui":
				uiTrace := filepath.Join(t.TempDir(), "summary-context-failure")
				runSummaryControlUI(t, paths, directory, uiTrace, summaryContextFailureBehavior)
				codeData, err := os.ReadFile(uiTrace + ".failure-code")
				require.NoError(t, err)
				messageData, err := os.ReadFile(uiTrace + ".failure-message")
				require.NoError(t, err)
				code, message = string(codeData), string(messageData)
				terminalData, err := os.ReadFile(uiTrace)
				require.NoError(t, err)
				terminal := new(uiv1.SessionTreeNavigationResult)
				require.NoError(t, protojson.Unmarshal(terminalData, terminal))
				require.Equal(t, uiv1.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_COMMITTED,
					terminal.GetStatus())
			case "programmatic":
				fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
				sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticv1.OpenRequest) {
					programmaticRequest(request).SetResumeSession(
						programmaticv1.ResumeSession_builder{SessionId: new("source")}.Build(),
					)
				})
				failed := navigateWithSummary(t, fixture, "summary-context-failure").GetEvent().GetFailed()
				require.NotNil(t, failed)
				code, message = failed.GetFailure().GetCode(), failed.GetFailure().GetMessage()
				later := navigateWithSummary(t, fixture, "summary-context-later")
				require.NotNil(t, later.GetEvent().GetCompleted(), "failure: %v", later.GetEvent().GetFailed())
				fixture.closeOwner(t)
			}

			// Assert the category and cause, no dispatch for the failure, and no later handler of its chain.
			require.Equal(t, "EXTENSION_FAILED", code)
			require.Contains(t, message, contextFailureText)
			require.Equal(t, []string{
				"instructions-a",
				"instructions-a", "instructions-b", "context-a", "context-b",
			}, contextTraceHandlers(readContextTrace(t, trace)))
			received := bodies.snapshot()
			require.Len(t, received, 2)
			for _, body := range received {
				assertComposedRequest(t, body)
			}
		})
	}
}
