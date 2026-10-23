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

// Public composition fixtures register independently named reference participants.
const (
	// standardGeneratorExecutable names the independently registered reference generator runtime.
	standardGeneratorExecutable = "reference-generator"
	// standardResultExecutable names the independently registered reference result runtime.
	standardResultExecutable = "reference-results"
	// referenceObserverExecutable names the runtime that records compaction outcomes.
	referenceObserverExecutable = "reference-observer"
	// policyDisabledEnvironment selects reference participants without reference policy ownership.
	policyDisabledEnvironment = "GLYPH_REFERENCE_POLICY_DISABLED"
	// separateObserverEnvironment confines outcome observers to their dedicated runtime.
	separateObserverEnvironment = "GLYPH_REFERENCE_SEPARATE_OBSERVER"
	// separateRequestsEnvironment confines request handlers to their dedicated runtime.
	separateRequestsEnvironment = "GLYPH_REFERENCE_SEPARATE_REQUESTS"
	// separateGeneratorEnvironment confines reference generators to their dedicated runtime.
	separateGeneratorEnvironment = "GLYPH_REFERENCE_SEPARATE_GENERATOR"
	// separateResultsEnvironment confines result handlers to their dedicated runtime.
	separateResultsEnvironment = "GLYPH_REFERENCE_SEPARATE_RESULTS"
	// resultModeEnvironment selects the reference result participant's behavior.
	resultModeEnvironment = "GLYPH_REFERENCE_RESULT_MODE"
	// resultCancelMode selects an explicit result-participant Cancel action.
	resultCancelMode = "cancel"
	// generatorDisabled disables the bundled generator for supplied-result and cardinality scenarios.
	generatorDisabled = "false"
	// observationSuffix identifies the observer-call artifact beside its outcome artifact.
	observationSuffix = ".calls"
	// successObservation records invocation of the compaction success observer.
	successObservation = "success\n"
	// failureObservation records invocation of the compaction failure observer.
	failureObservation = "failure\n"
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
			name: "custom-generator", guidance: manualRequestClearGuidance,
			generator: false, extraGenerator: true, results: false, canceled: false, cause: "",
		},
		{
			name: "result-cancel", guidance: manualRequestCompositionGuidance,
			generator: false, extraGenerator: false, results: true, canceled: true, cause: "",
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
			// Arrange the bundled policy and independently registered public participants.
			t.Setenv(policyDisabledEnvironment, manualFixtureEnabled)
			t.Setenv(separateObserverEnvironment, manualFixtureEnabled)
			t.Setenv(separateRequestsEnvironment, manualFixtureEnabled)
			t.Setenv(separateGeneratorEnvironment, manualFixtureEnabled)
			t.Setenv(separateResultsEnvironment, manualFixtureEnabled)
			outcomePath := configureManualOutcomeFixture(t, test.guidance)
			if test.name == "result-cancel" {
				t.Setenv(resultModeEnvironment, resultCancelMode)
			}
			t.Setenv(standardRetainedEnvironment, "1")
			if !test.generator {
				t.Setenv(standardGeneratorEnvironment, generatorDisabled)
			}
			directory := buildStandardCompaction(t)
			fixtureDirectory := buildManualPolicyFixture(t)
			binary, err := os.ReadFile(filepath.Join(fixtureDirectory, manualFixtureReferencePolicy))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(directory, referenceObserverExecutable), binary, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(directory, standardRequestExecutable), binary, 0o700))
			if test.results {
				t.Setenv(manualResultsEnvironment, manualFixtureEnabled)
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
				if test.name == "custom-generator" {
					require.Equal(t, manualGeneratorSummary,
						terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction().GetSummary())
				}
				if test.results && !test.canceled {
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
			if test.guidance == manualRequestClearGuidance && test.cause == "" && test.generator {
				require.Len(t, bodies.snapshot(), 3)
			} else {
				require.Len(t, bodies.snapshot(), 2)
			}
			calls, err := os.ReadFile(outcomePath + observationSuffix)
			require.NoError(t, err)
			expected := failureObservation
			if committed {
				expected = successObservation
			}
			require.Equal(t, expected, string(calls))
			fixture.closeOwner(t)
		})
	}
}
