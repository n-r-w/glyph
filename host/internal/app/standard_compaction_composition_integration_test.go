//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardManualComposition coordinates real independent request and result runtimes.
func TestStandardManualComposition(t *testing.T) {
	for _, test := range []struct {
		name           string
		guidance       string
		generator      bool
		extraGenerator bool
		results        bool
		canceled       bool
		cause          string
	}{
		{
			name: "supplied-without-generator", guidance: manualRequestCompositionGuidance,
			generator: false, extraGenerator: false, results: true, canceled: false, cause: "",
		},
		{
			name: "supplied-with-several-generators", guidance: manualRequestCompositionGuidance,
			generator: true, extraGenerator: true, results: true, canceled: false, cause: "",
		},
		{
			name: "clear-to-standard-generation", guidance: manualRequestClearGuidance,
			generator: true, extraGenerator: false, results: false, canceled: false, cause: "",
		},
		{
			name: "clear-with-several-generators", guidance: manualRequestClearGuidance,
			generator: true, extraGenerator: true, results: false, canceled: false, cause: "found 2",
		},
		{
			name: "request-cancel", guidance: manualRequestCancelGuidance,
			generator: true, extraGenerator: false, results: false, canceled: true, cause: "",
		},
		{
			name: "request-failure", guidance: manualRequestFailGuidance,
			generator: true, extraGenerator: false, results: false, canceled: false, cause: manualRequestFailureCause,
		},
		{
			name: "request-invalid", guidance: manualRequestInvalidGuidance,
			generator: true, extraGenerator: false, results: false, canceled: false,
			cause: "cancellation must be exclusive",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			const standardGeneratorExecutable = "reference-generator"

			const standardResultExecutable = "reference-results"

			// Arrange the bundled policy and independently registered public participants.
			t.Setenv(standardRetainedEnvironment, "1")
			if !test.generator {
				t.Setenv(standardGeneratorEnvironment, "false")
			}
			directory := buildStandardCompaction(t)
			fixtureDirectory := buildManualPolicyFixture(t)
			binary, err := os.ReadFile(filepath.Join(fixtureDirectory, manualFixtureReferencePolicy))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(directory, standardRequestExecutable), binary, 0o700))
			if test.results {
				t.Setenv("GLYPH_REFERENCE_RESULTS", "1")
				require.NoError(t, os.WriteFile(filepath.Join(directory, standardResultExecutable), binary, 0o700))
			}
			if test.extraGenerator {
				t.Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
				require.NoError(t, os.WriteFile(filepath.Join(directory, standardGeneratorExecutable), binary, 0o700))
			}
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("standard-composition-original", manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest("standard-composition-recent", manualRetainedText))
			// Act through public manual composition.
			terminal := requestProgrammaticCompaction(t, fixture, "standard-composition", test.guidance)
			// Assert complete terminal causes, bypass/cardinality behavior, and one durable marker.
			committed := test.cause == "" && !test.canceled
			if test.cause != "" {
				require.NotNil(t, terminal.GetFailed())
				require.Contains(t, terminal.GetFailed().GetMessage(), test.cause)
			} else {
				require.NotNil(t, terminal.GetCompleted(), "failure: %s", terminal.GetFailed().GetMessage())
				require.Equal(t, test.canceled, terminal.GetCompleted().GetCompaction().GetCanceled())
				if test.results {
					require.Equal(
						t,
						manualResultSummary,
						terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction().GetSummary(),
					)
				}
			}
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"standard-composition-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			originals, markers := countManualStoredEntries(stored)
			require.Equal(t, 1, originals)
			if committed {
				require.Equal(t, 1, markers)
			} else {
				require.Zero(t, markers)
			}
			if test.guidance == manualRequestClearGuidance && test.cause == "" {
				require.Len(t, bodies.snapshot(), 3)
			} else {
				require.Len(t, bodies.snapshot(), 2)
			}
			fixture.closeOwner(t)
		})
	}
}
