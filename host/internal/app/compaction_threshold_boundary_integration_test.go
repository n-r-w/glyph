//go:build integration

// Package app assembles the host application and its public integration fixtures.
package app

import (
	"net/http"
	"os"
	"path/filepath"
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
	// Arrange a handler that splits a tool call from its result.
	const splitBoundaryEnvironment = "GLYPH_THRESHOLD_SPLIT_BOUNDARY"
	t.Setenv(splitBoundaryEnvironment, manualFixtureEnabled)
	// Act through threshold-triggered conversation preparation.
	suite.Run(t, &thresholdBoundarySuite{Suite: suite.Suite{}})
	// Assert the original split-boundary cause and no replacement in TestScenario.
}

// TestScenario checks structural failure from a real public threshold-compaction request.
func (s *thresholdBoundarySuite) TestScenario() {
	t := s.T()
	const (
		historyOperation   = "threshold-history"
		selectionOperation = "threshold-model"
		nextOperation      = "threshold-next"
		storedOperation    = "threshold-stored"
		providerID         = "openai-codex"
		// modelID identifies the smaller context-capacity model.
		modelID     = "gpt-test"
		failureCode = "COMPACTION_FAILED"
	)
	// Arrange real tool-call history and a policy owner that selects a split boundary.
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
	policyDirectory := buildManualPolicyFixture(t)
	policyExecutable, err := os.ReadFile(filepath.Join(policyDirectory, manualFixtureReferencePolicy))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, manualFixtureReferencePolicy), policyExecutable, 0o755))
	t.Setenv(automaticFixtureModeEnvironment, automaticFixtureSplitBoundaryMode)
	t.Setenv(automaticFixtureTargetEnvironment, "next request")
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

	// Act by submitting conversation work so the captured policy owner proposes a replacement before provider execution.
	require.NoError(t, fixture.stream.Send(userRequest(nextOperation, "next request")))
	terminal := receiveAutomaticTerminal(t, fixture, nextOperation).GetEvent()
	require.NotNil(t, terminal.GetFailed(), automaticUnexpectedTerminalFormat, terminal)
	require.Equal(t, failureCode, terminal.GetFailed().GetFailure().GetCode())
	message := terminal.GetFailed().GetFailure().GetMessage()

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
