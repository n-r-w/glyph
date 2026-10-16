//go:build integration

package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// buildStandardOverflowFixture adds public outcome observation to the standard policy and generator.
func buildStandardOverflowFixture(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GLYPH_REFERENCE_POLICY_DISABLED", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_OBSERVER", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_REQUESTS", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_GENERATOR", "1")
	t.Setenv("GLYPH_REFERENCE_SEPARATE_RESULTS", "1")
	outcomePath := filepath.Join(t.TempDir(), "overflow-outcome.json")
	t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
	directory := buildStandardCompaction(t)
	reference := buildManualPolicyFixture(t)
	binary, err := os.ReadFile(filepath.Join(reference, manualFixtureReferencePolicy))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "reference-observer"), binary, 0o700))
	return directory, outcomePath
}

// startStandardOverflowFixture opens Programmatic Control for overflow tests.
func startStandardOverflowFixture(t *testing.T, directory, retries string) *programmaticFixture {
	t.Helper()
	settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 8000")
	settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
	settings += "retry:\n  enabled: true\n  maxRetries: " + retries + "\n"
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	return startProgrammaticFixtureWithExtension(t, paths, directory)
}

// installStandardOverflowTransport lets overflow tests record requests and supply provider responses.
func installStandardOverflowTransport(
	t *testing.T, respond func(int) (*http.Response, error),
) *requestBodies {
	t.Helper()
	bodies := &requestBodies{}
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			return respond(bodies.append(encoded))
		},
	)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	return bodies
}

// newStandardOverflowRecoveryResponse supplies provider responses for overflow recovery tests.
func newStandardOverflowRecoveryResponse(position int) (*http.Response, error) {
	if position == 2 {
		return newStandardOverflowResponse("provider context limit source"), nil
	}
	return runtimeFailureHTTPResponse(finalResponseSSE), nil
}

// newStandardOverflowResponse supplies a classified provider context rejection.
func newStandardOverflowResponse(message string) *http.Response {
	const (
		standardOverflowResponsePrefix = `{"error":{"message":"`
		standardOverflowResponseSuffix = `","type":"invalid_request_error","code":"context_length_exceeded"}}`
	)
	response := runtimeFailureHTTPResponse(standardOverflowResponsePrefix + message + standardOverflowResponseSuffix)
	response.StatusCode = http.StatusBadRequest
	return response
}

// assertStandardOverflowHistory checks stored originals, tools, and replacement cardinality.
func assertStandardOverflowHistory(
	t *testing.T, fixture *programmaticFixture, originalExpected, committed bool, expectedTools int,
) *programmaticpb.SessionEntry {
	t.Helper()
	stored := sendProgrammaticOperation(t, fixture, "overflow-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	markers, tools := 0, 0
	originalFound, recentFound := false, false
	var marker *programmaticpb.SessionEntry
	for _, entry := range stored {
		if entry.GetCompaction() != nil {
			markers++
			marker = entry
		}
		if entry.GetToolResult() != nil {
			tools++
		}
		for _, block := range entry.GetUser().GetContent() {
			originalFound = originalFound || block.GetText() == manualOriginalText
			recentFound = recentFound || block.GetText() == manualRetainedText
		}
	}
	require.Equal(t, originalExpected, originalFound)
	require.True(t, recentFound)
	if committed {
		require.Equal(t, 1, markers)
	} else {
		require.Zero(t, markers)
	}
	require.Equal(t, expectedTools, tools)
	return marker
}

// readStandardOverflowOutcome reads the public observer's captured receipt and state.
func readStandardOverflowOutcome(t *testing.T, path string) *extensionpb.CompactionOutcome {
	t.Helper()
	encoded, err := os.ReadFile(path)
	require.NoError(t, err)
	outcome := new(extensionpb.CompactionOutcome)
	require.NoError(t, protojson.Unmarshal(encoded, outcome))
	return outcome
}

// assertStandardOverflowIntent checks overflow metadata and the context-token estimate.
func assertStandardOverflowIntent(t *testing.T, request *extensionpb.CompactionRequest) {
	t.Helper()
	require.Equal(t, extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW, request.GetTrigger())
	require.True(t, request.GetRetryIntent())
	require.LessOrEqual(t, request.GetContextTokens(), request.GetContextWindow()-request.GetResponseBudget())
}

// assertStandardOverflowProjection checks the replacement summary and exclusion of the summarized original.
func assertStandardOverflowProjection(t *testing.T, input []byte, marker *programmaticpb.SessionEntry) {
	t.Helper()
	require.Contains(t, string(input), marker.GetCompaction().GetSummary())
	require.NotContains(t, string(input), manualOriginalText)
}
