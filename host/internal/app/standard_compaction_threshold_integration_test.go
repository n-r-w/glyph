//go:build integration

package app

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardAutomaticExactThreshold checks automatic compaction at the input-budget boundary.
func TestStandardAutomaticExactThreshold(t *testing.T) {
	const contextWindowSettingsFormat = "contextWindow: %d"

	// Arrange the provider-visible instructions and response to measure the captured input.
	t.Setenv(standardRetainedEnvironment, "1")
	directory := buildStandardCompaction(t)
	original := manualOriginalText + strings.Repeat("x", 400)
	bodies := installStandardConversationTransport(t, finalResponseSSE)
	probePaths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, probePaths)
	probe := startProgrammaticFixtureWithExtension(t, probePaths, directory)
	completeProgrammaticRequest(t, probe, userRequest("threshold-probe", original))
	var outgoing struct {
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(bodies.snapshot()[0], &outgoing))
	require.NotEmpty(t, outgoing.Instructions)
	entries := sendProgrammaticOperation(t, probe, "threshold-entries", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	// Text sizing is the public content-size policy: four bytes per token and four tokens per message.
	budget := int64((len(outgoing.Instructions)+3)/4 + 4 + (len(manualRetainedText)+3)/4)
	for _, entry := range entries {
		if entry.GetUser() != nil {
			budget += 4
			for _, block := range entry.GetUser().GetContent() {
				budget += int64((len(block.GetText()) + 3) / 4)
			}
		}
		if entry.GetModel() != nil {
			budget += 4
			for _, block := range entry.GetModel().GetContent() {
				budget += int64((len(block.GetText().GetText()) + 3) / 4)
			}
		}
	}
	probe.closeOwner(t)
	for _, above := range []bool{false, true} {
		func() {
			// Arrange a descriptor whose captured reserve leaves exactly the measured input budget.
			window := budget + 1000
			if above {
				window--
			}
			settings := strings.ReplaceAll(
				configuredRequestSettings,
				"contextWindow: 131072",
				fmt.Sprintf(contextWindowSettingsFormat, window),
			)
			settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			start := len(bodies.snapshot())
			completeProgrammaticRequest(t, fixture, userRequest("threshold-original", original))
			// Act at the exact content threshold or one token above it.
			completeProgrammaticRequest(t, fixture, userRequest("threshold-recent", manualRetainedText))
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"threshold-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			_, markers := countManualStoredEntries(stored)
			// Assert that before conversation dispatch, the compaction extension preserves context at the input budget
			// and replaces older context above it.
			if above {
				require.Equal(t, 1, markers)
				require.Len(t, bodies.snapshot(), start+3)
			} else {
				require.Zero(t, markers)
				require.Len(t, bodies.snapshot(), start+2)
			}
		}()
	}
}
