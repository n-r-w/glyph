//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	"github.com/n-r-w/glyph/host/internal/controller/cli"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// contextInstructionsMarkerA is appended to current instructions
	// and dropped by the next handler, which restarts from original.
	contextInstructionsMarkerA = "glyph-instructions-A"
	// contextMarkerA is appended to current history
	// and dropped by the next handler, which restarts from original.
	contextMarkerA = "glyph-context-A"
	// contextMarkerB is the request-local message that the reference derives from the original.
	contextMarkerB = "glyph-context-B"
)

// buildContextFixture builds the public-only context reference with the selected executable scenario.
func buildContextFixture(t *testing.T, mode string) string {
	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(directory, mode), "./cmd/context")
	command.Dir = filepath.Join(repoRoot(t), "testdata", "external-plugins")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}

// addContextFixture copies the selected context reference scenario into an extension directory.
func addContextFixture(t *testing.T, directory, mode string) {
	t.Helper()
	binary, err := os.ReadFile(filepath.Join(buildContextFixture(t, mode), mode))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, mode), binary, 0o700))
}

// contextProjectInstructions reads the example document that the reference appends to instructions.
func contextProjectInstructions(t *testing.T) string {
	t.Helper()
	document, err := os.ReadFile(
		filepath.Join(repoRoot(t), "testdata", "external-plugins", "cmd", "context", "project-instructions.md"),
	)
	require.NoError(t, err)
	return strings.TrimSpace(string(document))
}

// assertContextRequest checks the effective instructions and the request-local history marker of one request.
func assertContextRequest(t *testing.T, body []byte, baseInstructions, marker string) {
	t.Helper()
	var request struct {
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	require.Equal(t, baseInstructions+"\n\n"+contextProjectInstructions(t), request.Instructions)
	require.NotContains(t, string(body), contextInstructionsMarkerA)
	require.NotContains(t, string(body), contextMarkerA)
	require.Equal(t, 1, strings.Count(string(body), contextMarkerB))
	require.Contains(t, string(body), marker)
}

// assertNoContextMarkers checks that persisted session entries contain no request-local values.
func assertNoContextMarkers(t *testing.T, fixture *programmaticFixture) {
	t.Helper()
	entries := sendProgrammaticOperation(t, fixture, "context-entries", func(r *programmaticv1.OpenRequest) {
		programmaticRequest(r).SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
	}).GetSessionEntries()
	encoded, err := protojson.Marshal(entries)
	require.NoError(t, err)
	require.NotEmpty(t, entries.GetEntries())
	for _, marker := range []string{contextInstructionsMarkerA, contextMarkerA, contextMarkerB} {
		require.NotContains(t, string(encoded), marker)
	}
}

// TestPublicContextMiddleware composes instructions and history through ordinary public extension operations.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicContextMiddleware(t *testing.T) {
	// Arrange the real public context extension, Host assembly and local HTTP provider.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	bodies := newRecordingProviderFixture(t, 0, nil)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildContextFixture(t, "context"))
	defer fixture.closeOwner(t)

	// Act through two public accepted user operations.
	completeProgrammaticRequest(t, fixture, userRequest("context-first", "first input"))
	completeProgrammaticRequest(t, fixture, userRequest("context-second", "second input"))

	// Assert each request received only its own final replacement and durable history stayed marker-free.
	received := bodies.snapshot()
	require.Len(t, received, 2)
	assertContextRequest(t, received[0], codingagent.Instructions(), contextMarkerB)
	assertContextRequest(t, received[1], codingagent.Instructions(), contextMarkerB)
	require.Contains(t, string(received[1]), "first input")
	assertNoContextMarkers(t, fixture)
}

// TestPublicContextMiddlewareAcrossModes applies the same composition in one-shot and UI assemblies.
//
//nolint:paralleltest // These scenarios replace the process-global provider HTTP transport.
func TestPublicContextMiddlewareAcrossModes(t *testing.T) {
	directory := buildContextFixture(t, "context")
	for name, mode := range map[string]cli.Mode{"headless": cli.ModeHeadless, "ui": cli.ModeUI} {
		t.Run(name, func(t *testing.T) {
			// Arrange isolated application paths and a recording provider.
			paths := testPaths(t, codexSettings(""))
			writeProgrammaticCredentials(t, paths)
			bodies := newRecordingProviderFixture(t, 0, nil)

			// Act through the public caller for this application mode.
			runPublicExtensionMode(t, mode, paths, directory, "context-mode", "mode input")

			// Assert the provider received the effective instructions and history.
			received := bodies.snapshot()
			require.Len(t, received, 1)
			assertContextRequest(t, received[0], codingagent.Instructions(), contextMarkerB)
		})
	}
}

// TestPublicContextMiddlewareFailure fails before dispatch, never repeats the chain, and allows a later request.
//
//nolint:paralleltest // These scenarios replace the process-global provider HTTP transport.
func TestPublicContextMiddlewareFailure(t *testing.T) {
	for _, scenario := range []struct {
		mode  string
		cause string
	}{
		{mode: "context-fail", cause: "reference context handler failed: unexpected EOF"},
		{mode: "context-invalid", cause: "invalid model-context action"},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			// Arrange retry permission and an extension that fails only its first invocation.
			paths := testPaths(t, codexSettings("retry:\n  enabled: true\n  maxRetries: 2\n  delays: [0s]\n"))
			writeProgrammaticCredentials(t, paths)
			bodies := newRecordingProviderFixture(t, 0, nil)
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildContextFixture(t, scenario.mode))
			defer fixture.closeOwner(t)

			// Act through a failing public operation and a later ordinary operation.
			failure, _ := requestProgrammaticFailure(t, fixture, userRequest("context-failure", "user input"))
			failedCalls := len(bodies.snapshot())
			completeProgrammaticRequest(t, fixture, userRequest("context-later", "later input"))

			// Assert category, complete cause, zero dispatch for the failure and a successful later request.
			require.Equal(t, "EXTENSION_FAILED", failure.Code)
			require.Contains(t, failure.Message, scenario.cause)
			require.Zero(t, failedCalls)
			received := bodies.snapshot()
			require.Len(t, received, 1)
			assertContextRequest(t, received[0], codingagent.Instructions(), contextMarkerB)
		})
	}
}

// TestPublicContextMiddlewareRetry sends one composed revision to every transient provider retry.
//
//nolint:paralleltest // This fixture replaces the process-global provider HTTP transport.
func TestPublicContextMiddlewareRetry(t *testing.T) {
	// Arrange a counting extension and a provider returning 429 once before success.
	paths := testPaths(t, codexSettings("retry:\n  enabled: true\n  maxRetries: 1\n  delays: [0s]\n"))
	writeProgrammaticCredentials(t, paths)
	bodies := newRecordingProviderFixture(t, 1, nil)
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildContextFixture(t, "context-counted"))
	defer fixture.closeOwner(t)

	// Act through the ordinary public request and Host-owned retry policy.
	completeProgrammaticRequest(t, fixture, userRequest("context-retry", "user input"))

	// Assert both raw attempts carried the first and only composition.
	received := bodies.snapshot()
	require.Len(t, received, 2)
	for _, body := range received {
		assertContextRequest(t, body, codingagent.Instructions(), contextMarkerB+"-1")
	}
}

// TestPublicContextMiddlewareAutomaticCompaction composes after initial preparation and keeps policy observation.
func TestPublicContextMiddlewareAutomaticCompaction(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		usage   string
		compact bool
	}{
		{name: "compacts", usage: "", compact: true},
		{name: "reported-usage-preserves", usage: "low", compact: false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange the bundled compaction extension, the context reference and a 7,000-token input budget.
			t.Setenv(standardRetainedEnvironment, "1")
			directory := buildStandardCompaction(t)
			addContextFixture(t, directory, "context")
			settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 8000")
			settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
			settings = strings.Replace(settings, "defaultModel: gpt-active", "defaultModel: gpt-test", 1)
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			response := finalResponseSSE
			if scenario.usage != "" {
				response = conversationResponseSSE
			}
			bodies := installStandardConversationTransport(t, response)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			original := manualOriginalText + strings.Repeat("a", 16000)
			completeProgrammaticRequest(t, fixture, userRequest("automatic-original", original))

			// Act through the public ordinary-conversation entry point.
			recent := manualRetainedText + strings.Repeat("b", 16000)
			completeProgrammaticRequest(t, fixture, userRequest("automatic-recent", recent))

			// Assert the last request carries both the prepared history and the middleware result.
			actual := bodies.snapshot()
			last := actual[len(actual)-1]
			require.Contains(t, string(last), recent)
			require.Contains(t, string(last), contextMarkerB)
			require.NotContains(t, string(last), contextMarkerA)
			if scenario.compact {
				require.Len(t, actual, 3)
				require.NotContains(t, string(last), original)
			} else {
				require.Len(t, actual, 2)
				require.Contains(t, string(last), original)
			}
			assertNoContextMarkers(t, fixture)
		})
	}
}

// TestPublicContextMiddlewareOverflow composes a new revision after committed overflow recovery.
func TestPublicContextMiddlewareOverflow(t *testing.T) {
	// Arrange standard overflow recovery and the counting context reference.
	t.Setenv(standardRetainedEnvironment, "1")
	directory, _ := buildStandardOverflowFixture(t)
	addContextFixture(t, directory, "context-counted")
	bodies := installStandardOverflowTransport(t, newStandardOverflowRecoveryResponse)
	fixture := startStandardOverflowFixture(t, directory, "3\n  delays: [1ms, 1ms, 1ms]")
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))

	// Act through ordinary submission and the provider's context-overflow classification.
	require.NoError(t, fixture.stream.Send(userRequest("overflow-recent", manualRetainedText)))
	terminal := receiveAutomaticTerminal(t, fixture, "overflow-recent").GetEvent()

	// Assert the retried request is a new composition of the recovered history.
	require.NotNil(t, terminal.GetCompleted(), "failure: %v", terminal.GetFailed())
	marker := assertStandardOverflowHistory(t, fixture, true, true, 0)
	actual := bodies.snapshot()
	require.Len(t, actual, 4)
	require.Contains(t, string(actual[1]), contextMarkerB+"-2")
	assertStandardOverflowProjection(t, actual[3], marker)
	require.Contains(t, string(actual[3]), contextMarkerB+"-")
	require.NotContains(t, string(actual[3]), contextMarkerB+"-2")
	require.NotContains(t, string(actual[3]), contextMarkerA)
	assertNoContextMarkers(t, fixture)
}

// TestPublicContextConfiguredAcrossModes shares composition between outer and configured requests.
//
//nolint:paralleltest // These scenarios replace the process-global provider HTTP transport.
func TestPublicContextConfiguredAcrossModes(t *testing.T) {
	// Arrange independently built public extension processes sharing only public contracts and SDK.
	directory := buildPublicExtensionFixture(t)
	addContextFixture(t, directory, "context")
	for name, mode := range map[string]cli.Mode{
		"headless": cli.ModeHeadless, "ui": cli.ModeUI, "programmatic": cli.ModeRPC,
	} {
		t.Run(name, func(t *testing.T) {
			// Arrange a contextual tool that performs one explicit configured-model request.
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := configuredRequestProviderTransport(t, "configured-request", false)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })

			// Act through the public caller for this application mode.
			runPublicExtensionMode(t, mode, paths, directory, "configured-request", "request configured model")

			// Assert the outer and configured requests were composed by the same handlers from their own originals.
			configured, outer := 0, 0
			for _, body := range bodies.snapshot() {
				if strings.Contains(string(body), `"model":"`+configuredRequestModelID+`"`) {
					configured++
					assertContextRequest(t, body, "extension instructions", contextMarkerB)
					continue
				}
				outer++
				assertContextRequest(t, body, codingagent.Instructions(), contextMarkerB)
			}
			require.Equal(t, 1, configured)
			require.Equal(t, 2, outer)
		})
	}
}
