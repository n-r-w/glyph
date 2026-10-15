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

// TestRepeatedOverflowPreservesCompletedTool checks repeated admitted recovery through public conversation submission.
func TestRepeatedOverflowPreservesCompletedTool(t *testing.T) {
	// Arrange a tool response followed by two provider overflows in one user request.
	const overflowBody = `{"error":{"message":"provider repeated overflow source",` +
		`"type":"invalid_request_error","code":"context_length_exceeded"}}`
	toolPath := filepath.Join(t.TempDir(), "completed-tool.json")
	policyPath := filepath.Join(t.TempDir(), "captured-overflow-policy.json")
	observationPath := filepath.Join(t.TempDir(), "successful-observation.json")
	t.Setenv(conversationToolEnvironment, toolPath)
	t.Setenv(conversationProviderOverflowEnvironment, manualFixtureEnabled)
	t.Setenv(manualFixtureGlyphReferencePolicyInput, policyPath)
	t.Setenv(conversationObservationEnvironment, observationPath)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	bodies := &requestBodies{}
	toolReports := &requestBodies{}
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		Times(4).
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			position := bodies.append(encoded)
			body := finalResponseSSE
			if position == 1 {
				body = conversationToolResponseSSE
			}
			if position == 2 {
				toolBytes, readErr := os.ReadFile(toolPath)
				if readErr != nil {
					return nil, readErr
				}
				toolReports.append(toolBytes)
			}
			if position == 2 || position == 3 {
				body = overflowBody
			}
			response := runtimeFailureHTTPResponse(body)
			if position == 2 || position == 3 {
				response.StatusCode = http.StatusBadRequest
			}
			return response, nil
		})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)

	// Act through one public user request with a completed tool and two admitted replacement attempts.
	events, completed, limit := completeProgrammaticRetryRequest(
		t,
		fixture,
		userRequest("repeated-overflow", manualOriginalText),
	)

	// Assert one logical result, generic retry accounting and no replay of the completed tool.
	require.EqualValues(t, 2, completed)
	require.EqualValues(t, 4, limit)
	require.Equal(t, 2, countValue(events, int32(programmaticpb.AgentEventType_AGENT_EVENT_TYPE_RETRY_PROGRESS)))
	require.Equal(t, 2, countValue(events, int32(programmaticpb.AgentEventType_AGENT_EVENT_TYPE_MESSAGE_END)))
	attempts := bodies.snapshot()
	require.Len(t, attempts, 4)
	assertSingleCompletedToolInRequest(t, attempts[1])
	assertSingleCompletedToolInRequest(t, attempts[2])
	for _, attempt := range attempts[2:] {
		require.Contains(t, string(attempt), "conversation overflow replacement summary")
	}
	assertSingleCompletedToolInRequest(t, attempts[3])
	finalToolBytes, err := os.ReadFile(toolPath)
	require.NoError(t, err)
	toolSnapshots := toolReports.snapshot()
	require.Len(t, toolSnapshots, 1)
	require.Equal(t, 1, strings.Count(string(toolSnapshots[0]), "\n"))
	require.Equal(t, toolSnapshots[0], finalToolBytes)
	require.Equal(t, 1, strings.Count(string(finalToolBytes), "\n"))
	policyBytes, err := os.ReadFile(policyPath)
	require.NoError(t, err)
	policy := new(extensionpb.CompactionPolicyInvocation)
	require.NoError(t, protojson.Unmarshal(policyBytes, policy))
	require.Equal(t, extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW, policy.GetPurpose())
	require.Contains(t, policy.GetOverflowError(), "provider repeated overflow source")
	require.NotNil(t, policy.GetPrevious())
	require.NotContains(t, string(policyBytes), "private-observation-replay")
	observationBytes, err := os.ReadFile(observationPath)
	require.NoError(t, err)
	require.Contains(t, string(observationBytes), "conversation overflow replacement summary")
	require.Equal(t, 2, strings.Count(string(observationBytes), "conversation overflow observer message"))
	require.NotContains(t, string(observationBytes), "private-observation-replay")
	stored := sendProgrammaticOperation(t, fixture, "repeated-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	markers, tools, models := 0, 0, 0
	originalFound := false
	firstReceipt := ""
	for _, entry := range stored {
		if entry.GetCompaction() != nil {
			markers++
			if firstReceipt == "" {
				firstReceipt = entry.GetId()
				require.Equal(t, entry.GetCompaction().GetSummary(), policy.GetPrevious().GetSummary())
			}
		}
		if entry.GetToolResult() != nil {
			tools++
		}
		if entry.GetModel() != nil {
			models++
		}
		for _, content := range entry.GetUser().GetContent() {
			originalFound = originalFound || content.GetText() == manualOriginalText
		}
	}
	require.Equal(t, 2, markers)
	require.Equal(t, 1, tools)
	require.Equal(t, 2, models)
	require.True(t, originalFound)
	priorReceiptCaptured := false
	privateSizeObserved := false
	for _, entry := range policy.GetEntries() {
		priorReceiptCaptured = priorReceiptCaptured || entry.GetEntry().GetId() == firstReceipt
		privateSizeObserved = privateSizeObserved || entry.GetPrivatePayloadBytes() > 0
	}
	require.True(t, priorReceiptCaptured)
	require.True(t, privateSizeObserved)
}
