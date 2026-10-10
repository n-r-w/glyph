//go:build integration

// Package app assembles the host application and its public integration fixtures.
package app

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// thresholdBoundarySuite owns the public threshold-compaction scenario.
type thresholdBoundarySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestThresholdCompactionPreservesSplitBoundaryCause checks the retained automatic projection error contract.
func TestThresholdCompactionPreservesSplitBoundaryCause(t *testing.T) {
	// Arrange the public fixture setting.
	// splitBoundaryEnvironment names the tool-result boundary fixture setting.
	const splitBoundaryEnvironment = "GLYPH_THRESHOLD_SPLIT_BOUNDARY"
	t.Setenv(splitBoundaryEnvironment, manualFixtureEnabled)
	// Act by running the public scenario.
	suite.Run(t, &thresholdBoundarySuite{Suite: suite.Suite{}})
	// Assert the public outcome in the suite scenario.
}

// TestScenario checks structural failure from a real public threshold-compaction request.
func (s *thresholdBoundarySuite) TestScenario() {
	t := s.T()
	const (
		// historyOperation identifies the original public conversation turn.
		historyOperation = "threshold-history"
		// selectionOperation identifies the smaller-model selection.
		selectionOperation = "threshold-model"
		// nextOperation identifies the public threshold-triggering request.
		nextOperation = "threshold-next"
		// storedOperation identifies original-entry retrieval after rejection.
		storedOperation = "threshold-stored"
		// compactOwner identifies the registered automatic result producer.
		compactOwner = "01-compact"
		// providerID identifies the configured conversation provider.
		providerID = "openai-codex"
		// modelID identifies the smaller context-capacity model.
		modelID = "gpt-test"
		// failureCode identifies retained extension compaction failure.
		failureCode = "EXTENSION_FAILED"
	)
	// Arrange real tool-call history and a registered automatic handler that splits its boundary.
	// modelMarker locates the smaller model configuration.
	const modelMarker = "- id: gpt-test"
	at := strings.Index(configuredRequestSettings, modelMarker)
	require.Positive(t, at)
	settings := configuredRequestSettings[:at] + strings.NewReplacer(
		"contextWindow: 131072", "contextWindow: 4000",
		"maxTokens: 16384", "maxTokens: 500",
	).
		Replace(configuredRequestSettings[at:])
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	directory := buildToolsExecutable(t)
	writeHandlerFixtureScript(t, directory, compactOwner, handlerFixtureCompactionSupplyMode, "")
	count := &atomic.Int32{}
	previous := http.DefaultTransport
	http.DefaultTransport = newDeterministicCodexTransport(t, count, nil)
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(
		t,
		fixture,
		userRequest(historyOperation, strings.Repeat("oversized history ", 3000)),
	)
	require.EqualValues(t, 2, count.Load())
	sendProgrammaticOperation(t, fixture, selectionOperation, func(request *programmaticpb.OpenRequest) {
		programmaticRequest(
			request,
		).SetSelectModel(programmaticpb.SelectModel_builder{
			ProviderId: new(providerID),
			ModelId:    new(modelID),
		}.Build())
	})

	// Act by requesting conversation work that triggers threshold preparation before provider execution.
	require.NoError(t, fixture.stream.Send(userRequest(nextOperation, "next request")))
	var message string
	for message == "" {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == nextOperation && response.GetEvent().GetFailed() != nil {
			require.Equal(t, failureCode, response.GetEvent().GetFailed().GetCode())
			message = response.GetEvent().GetFailed().GetMessage()
		}
	}

	// Assert that Host retains the specific split boundary cause and adds no replacement.
	require.Contains(t, message, "compaction boundary cannot separate a tool call from its result")
	require.EqualValues(t, 2, count.Load())
	stored := sendProgrammaticOperation(t, fixture, storedOperation, func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	for _, entry := range stored {
		require.Nil(t, entry.GetCompaction())
	}
}
