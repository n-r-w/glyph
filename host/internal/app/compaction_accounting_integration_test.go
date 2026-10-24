//go:build integration

package app

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// accountingZero is the scenario with reported zero usage.
	accountingZero = "zero"
	// accountingMissingUsage is the scenario with absent usage.
	accountingMissingUsage = "missing-usage"
	// accountingMissingPricing is the scenario with unconfigured pricing for the contributing model.
	accountingMissingPricing = "missing-pricing"
	// accountingSessionEnvironment names the environment variable for the session inspected by the UI fixture.
	accountingSessionEnvironment = "GLYPH_ACCOUNTING_SESSION"
)

// TestCompactionAccountingPersistsRequestsAcrossClientsAndRestart exercises public accounting and saved projections.
func TestCompactionAccountingPersistsRequestsAcrossClientsAndRestart(t *testing.T) {
	const (
		// accountingGuidance is the guidance value for real contributing requests in the reference extension.
		accountingGuidance = "accounting:"
		// accountingPriced is the scenario with complete usage and pricing.
		accountingPriced = "priced"
	)

	// Arrange one independently built public reference extension for the scenarios.
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	directory := buildManualPolicyFixture(t)
	for _, mode := range []string{accountingPriced, accountingZero, accountingMissingUsage, accountingMissingPricing} {
		t.Run(mode, func(t *testing.T) {
			// Arrange provider reports and per-request pricing for two contributing calls.
			t.Setenv(appUITraceEnvironment, filepath.Join(t.TempDir(), "accounting-ui.json"))
			inputPath := filepath.Join(t.TempDir(), "accounting-extension.json")
			t.Setenv(manualFixtureGlyphReferencePolicyInput, inputPath)
			paths := testPaths(t, newCompactionAccountingSettings(mode))
			writeProgrammaticCredentials(t, paths)
			transport := newCompactionAccountingTransport(t, mode)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("accounting-original", manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest("accounting-retained", manualRetainedText))
			info := sendProgrammaticOperation(t, fixture, "accounting-info", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionInfo(new(programmaticpb.GetSessionInfo))
			}).GetSessionInfo().GetInfo()

			// Act by persisting two compactions, each with its own pair of real model requests.
			var committed []*programmaticpb.Compaction
			for _, operationID := range []string{"accounting-first", "accounting-second"} {
				terminal := requestProgrammaticCompaction(t, fixture, operationID, accountingGuidance+mode)
				require.NotNil(
					t,
					terminal.GetCompleted(),
					"failure: %s",
					terminal.GetFailed().GetFailure().GetMessage(),
				)
				marker := terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction()
				require.NotNil(t, marker)
				committed = append(committed, proto.CloneOf(marker))
			}
			entries := readAccountingEntries(t, fixture)
			statistics := readAccountingStatistics(t, fixture)

			// Assert request order, optional presence, independently selected tiers, and one count per saved entry.
			require.Len(t, committed, 2)
			for _, marker := range committed {
				assertCompactionAccounting(t, marker, mode)
			}
			assertCompactionAccountingStatistics(t, statistics, mode)
			assertAccountingExtensionPrevious(t, inputPath, committed[0])
			fixture.closeOwner(t)

			// Act by restarting with summary-model pricing removed and restoring through the public session operation.
			require.NoError(
				t,
				os.WriteFile(
					paths.SettingsFile,
					[]byte(newCompactionAccountingSettings(accountingMissingPricing)),
					0o600,
				),
			)
			restored := startProgrammaticFixtureWithExtension(t, paths, directory)
			sendProgrammaticOperation(t, restored, "accounting-resume", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(
					request,
				).SetResumeSession(programmaticpb.ResumeSession_builder{SessionId: new(info.GetId())}.Build())
			})
			restoredEntries := readAccountingEntries(t, restored)
			restoredStatistics := readAccountingStatistics(t, restored)

			// Assert every detailed entry and saved request cost survives without repricing.
			require.Len(t, restoredEntries, len(entries))
			for index := range entries {
				require.True(t, proto.Equal(entries[index], restoredEntries[index]))
			}
			require.True(t, proto.Equal(statistics, restoredStatistics))
			restored.closeOwner(t)
			assertAccountingUIRestore(t, paths, directory, info.GetId(), committed, mode)
		})
	}
}

// newCompactionAccountingSettings supplies tiered summary-model pricing.
func newCompactionAccountingSettings(mode string) string {
	const (
		// accountingBasePricing contains pricing rates.
		accountingBasePricing = "pricing: {input: 1000000, output: 2000000, cacheRead: 3000000, cacheWrite: 4000000, " +
			"tiers: [{inputTokensAbove: 10, input: 10000000, output: 20000000, cacheRead: 30000000, cacheWrite: 40000000}]}"
	)

	const zeroPricing = "pricing: {input: 0, output: 0, cacheRead: 0, cacheWrite: 0}"
	const modelSettingsPrefix = "      - id: " + configuredRequestModelID

	offset := strings.Index(configuredRequestSettings, modelSettingsPrefix)
	suffix := configuredRequestSettings[offset:]
	pricing := accountingBasePricing
	if mode == accountingMissingPricing {
		pricing = ""
	}
	return configuredRequestSettings[:offset] + strings.Replace(suffix, zeroPricing, pricing, 1)
}

// newCompactionAccountingTransport creates a transport with provider responses for ordinary and contributing calls.
func newCompactionAccountingTransport(t *testing.T, mode string) *MockHTTPRoundTripper {
	const modelMatch = `"model":"` + configuredRequestModelID + `"`

	const (
		// accountingReportedUsage contains provider usage data.
		accountingReportedUsage = `,"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11,` +
			`"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},` +
			`"output_tokens_details":{"reasoning_tokens":3}}`
		// accountingZeroUsage is a present all-zero provider report.
		accountingZeroUsage = `,"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,` +
			`"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},` +
			`"output_tokens_details":{"reasoning_tokens":0}}`
	)

	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	contributing := 0
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			response := configuredModelResponseSSE
			if bytes.Contains(body, []byte(modelMatch)) {
				contributing++
				if contributing%2 == 0 {
					switch mode {
					case accountingZero:
						response = strings.Replace(response, accountingReportedUsage, accountingZeroUsage, 1)
					case accountingMissingUsage:
						response = strings.Replace(response, accountingReportedUsage, "", 1)
					}
				}
			}
			return &http.Response{
				Status: "", StatusCode: http.StatusOK, Proto: "", ProtoMajor: 0, ProtoMinor: 0,
				Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), ContentLength: 0,
				TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
			}, nil
		})
	return transport
}

// readAccountingEntries reads detailed public session entries.
func readAccountingEntries(t *testing.T, fixture *programmaticFixture) []*programmaticpb.SessionEntry {
	t.Helper()
	return sendProgrammaticOperation(t, fixture, "accounting-entries", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
}

// readAccountingStatistics reads public aggregate accounting.
func readAccountingStatistics(t *testing.T, fixture *programmaticFixture) *programmaticpb.SessionStatistics {
	t.Helper()
	return sendProgrammaticOperation(t, fixture, "accounting-statistics", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionStats(new(programmaticpb.GetSessionStats))
	}).GetSessionStats().GetStatistics()
}

// assertCompactionAccounting checks the contributing requests exposed by Programmatic Control.
func assertCompactionAccounting(t *testing.T, marker *programmaticpb.Compaction, mode string) {
	const providerID = "openai-codex"

	t.Helper()
	requests := marker.GetSource().GetModelRequests().GetRequests()
	require.Len(t, requests, 2)
	for index, request := range requests {
		require.Equal(t, providerID, request.GetProviderId())
		require.Equal(t, configuredRequestModelID, request.GetModelId())
		choice := programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH
		if index == 1 {
			choice = programmaticpb.ReasoningChoice_REASONING_CHOICE_OFF
		}
		require.Equal(t, choice, request.GetReasoningChoice())
		usagePresent := mode != accountingMissingUsage || index == 0
		require.Equal(t, usagePresent, request.HasUsage())
		costPresent := usagePresent && mode != accountingMissingPricing
		require.Equal(t, costPresent, request.HasEstimatedCost())
		if !usagePresent {
			continue
		}
		if mode == accountingZero && index == 1 {
			require.Zero(t, request.GetUsage().GetTotalTokens())
			require.Zero(t, request.GetEstimatedCost().GetTotal())
		} else {
			require.Equal(t, int64(11), request.GetUsage().GetTotalTokens())
			require.Equal(t, int64(4), request.GetUsage().GetInputTokens())
			if costPresent {
				require.InDelta(t, float64(4), request.GetEstimatedCost().GetInput(), 1e-9)
				require.InDelta(t, float64(8), request.GetEstimatedCost().GetOutput(), 1e-9)
				require.InDelta(t, float64(6), request.GetEstimatedCost().GetCacheRead(), 1e-9)
				require.InDelta(t, float64(4), request.GetEstimatedCost().GetCacheWrite(), 1e-9)
				require.InDelta(t, float64(22), request.GetEstimatedCost().GetTotal(), 1e-9)
			}
		}
	}
}

// assertCompactionAccountingStatistics checks all persisted entries' own contributions.
func assertCompactionAccountingStatistics(t *testing.T, statistics *programmaticpb.SessionStatistics, mode string) {
	const conversationModelID = "gpt-active"

	t.Helper()
	require.Equal(t, int64(4), statistics.GetTotalMessages())
	require.Equal(t, mode != accountingMissingUsage, statistics.HasTokens())
	if mode != accountingMissingUsage {
		expected := int64(66)
		if mode == accountingZero {
			expected = 44
		}
		require.Equal(t, expected, statistics.GetTokens().GetTotalTokens())
	}
	costPresent := mode != accountingMissingUsage && mode != accountingMissingPricing
	require.Equal(t, costPresent, statistics.HasEstimatedCost())
	breakdown := statistics.GetCostBreakdown()
	require.Len(t, breakdown, 2)
	require.Equal(t, conversationModelID, breakdown[0].GetModelId())
	require.True(t, breakdown[0].HasEstimatedCost())
	require.Zero(t, breakdown[0].GetEstimatedCost().GetTotal())
	require.Equal(t, configuredRequestModelID, breakdown[1].GetModelId())
	require.Equal(t, costPresent, breakdown[1].HasEstimatedCost())
	if costPresent {
		expected := float64(88)
		if mode == accountingZero {
			expected = 44
		}
		require.InDelta(t, expected, statistics.GetEstimatedCost().GetTotal(), 1e-9)
		require.InDelta(t, expected, breakdown[1].GetEstimatedCost().GetTotal(), 1e-9)
	}
}

// assertAccountingUIRestore checks public UI SDK restoration and tree projections of saved request records.
func assertAccountingUIRestore(
	t *testing.T,
	paths persistence.Paths,
	directory, sessionID string,
	markers []*programmaticpb.Compaction,
	mode string,
) {
	t.Helper()
	uiDirectory := t.TempDir()
	reportPath := os.Getenv(appUITraceEnvironment)

	t.Setenv(accountingSessionEnvironment, sessionID)
	writeUIExecutable(t, uiDirectory, manualFixtureManualUi)
	require.NoError(t, runWithPaths(t.Context(), paths, cli.Command{
		Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: directory,
		UIDirectory: uiDirectory, UIID: manualFixtureManualUi, SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{}))
	encoded, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report accountingUIReport
	require.NoError(t, json.Unmarshal(encoded, &report))
	restored := new(uipb.HostCompleted)
	tree := new(uipb.HostCompleted)
	require.NoError(t, protojson.Unmarshal([]byte(report.Restored), restored))
	require.NoError(t, protojson.Unmarshal([]byte(report.Tree), tree))
	var restoredMarkers, treeMarkers []*uipb.Compaction
	for _, entry := range restored.GetSessionChanged().GetEntries() {
		if entry.GetCompaction() != nil {
			restoredMarkers = append(restoredMarkers, entry.GetCompaction())
		}
	}
	for _, entry := range tree.GetSessionTree().GetTree().GetEntries() {
		if entry.GetCompaction() != nil {
			treeMarkers = append(treeMarkers, entry.GetCompaction())
		}
	}
	require.Len(t, restoredMarkers, len(markers))
	require.Len(t, treeMarkers, len(markers))
	for index, marker := range restoredMarkers {
		require.True(t, proto.Equal(marker, treeMarkers[index]))
		publicJSON, err := protojson.Marshal(marker)
		require.NoError(t, err)
		projected := new(programmaticpb.Compaction)
		require.NoError(t, protojson.Unmarshal(publicJSON, projected))
		require.True(t, proto.Equal(markers[index], projected))
		assertCompactionAccounting(t, projected, mode)
	}
}

// assertAccountingExtensionPrevious checks saved request records supplied to the next policy invocation.
func assertAccountingExtensionPrevious(t *testing.T, inputPath string, previous *programmaticpb.Compaction) {
	const firstReasoningChoice = "high"
	const secondReasoningChoice = "off"

	t.Helper()
	encoded, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	invocation := new(extensionpb.CompactionPolicyInvocation)
	require.NoError(t, protojson.Unmarshal(encoded, invocation))
	requests := invocation.GetPrevious().GetSource().GetModelRequests().GetRequests()
	expected := previous.GetSource().GetModelRequests().GetRequests()
	require.Len(t, requests, len(expected))
	for index, request := range requests {
		require.Equal(t, expected[index].GetProviderId(), request.GetSelection().GetProviderId())
		require.Equal(t, expected[index].GetModelId(), request.GetSelection().GetModelId())
		choice := firstReasoningChoice
		if index == 1 {
			choice = secondReasoningChoice
		}
		require.Equal(t, choice, request.GetSelection().GetReasoningChoice())
		require.Equal(t, expected[index].HasUsage(), request.HasUsage())
		require.Equal(t, expected[index].GetUsage().GetTotalTokens(), request.GetUsage().GetTotalTokens())
		require.Equal(t, expected[index].HasEstimatedCost(), request.HasEstimatedCost())
		require.InDelta(t, expected[index].GetEstimatedCost().GetTotal(), request.GetEstimatedCost().GetTotal(), 1e-9)
	}
}
