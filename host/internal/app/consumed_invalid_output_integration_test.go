//go:build integration

package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// consumedInvalidOutputEnvironment makes the real policy consume participant rejection.
	consumedInvalidOutputEnvironment = "GLYPH_REFERENCE_CONSUME_PARTICIPANT_FAILURE"
	// consumedChildGuidanceEnvironment selects the tool's nested compaction request.
	consumedChildGuidanceEnvironment = "GLYPH_REFERENCE_CONSUMED_GUIDANCE"
	// consumedRequestSourceEnvironment selects request-result producer attribution.
	consumedRequestSourceEnvironment = "GLYPH_REFERENCE_REQUEST_SOURCE"
	// consumedRequestForgedProducerMode selects another extension in request output.
	consumedRequestForgedProducerMode = "other-extension"
	// consumedResultForgedProducerMode selects another extension in result output.
	consumedResultForgedProducerMode = "producer"
	// consumedGeneratorProducer selects the invalid generator output scenario.
	consumedGeneratorProducer = "generator"
	// consumedRequestProducer selects the invalid request output scenario.
	consumedRequestProducer = "request"
	// consumedResultProducer selects the invalid result output scenario.
	consumedResultProducer = "result"
)

// configureConsumedInvalidProducer selects a real participant and its complete rejection cause.
func configureConsumedInvalidProducer(t *testing.T, participant string) (string, string) {
	t.Helper()
	t.Setenv(manualGeneratorsEnvironment, "")
	t.Setenv(manualGeneratorModeEnvironment, "")
	t.Setenv(consumedRequestSourceEnvironment, "")
	t.Setenv(manualResultsEnvironment, "")
	t.Setenv(resultModeEnvironment, "")
	switch participant {
	case consumedGeneratorProducer:
		t.Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
		t.Setenv(manualGeneratorModeEnvironment, manualGeneratorForgedProducerMode)
		return manualRequestClearGuidance,
			`generator producer "forged-generator-extension" does not match the selected extension "reference-policy"`
	case consumedRequestProducer:
		t.Setenv(consumedRequestSourceEnvironment, consumedRequestForgedProducerMode)
		return manualRequestCompositionGuidance,
			`result producer "request-forged-producer" does not match the selected extension "reference-policy"`
	case consumedResultProducer:
		t.Setenv(manualResultsEnvironment, manualFixtureEnabled)
		t.Setenv(resultModeEnvironment, consumedResultForgedProducerMode)
		return manualRequestCompositionGuidance,
			`result producer "result-forged-producer" does not match the selected extension "reference-policy"`
	default:
		t.Fatalf("unknown participant %q", participant)
		return "", ""
	}
}

// TestConsumedInvalidProducerFailsPublic verifies failed terminals despite a policy's canceled decision.
func TestConsumedInvalidProducerFailsPublic(t *testing.T) {
	for _, participant := range []string{consumedGeneratorProducer, consumedRequestProducer, consumedResultProducer} {
		t.Run(participant, func(t *testing.T) {
			// Arrange the real policy and invalid participant output through Programmatic Control.
			t.Setenv(consumedInvalidOutputEnvironment, manualFixtureEnabled)
			guidance, completeCause := configureConsumedInvalidProducer(t, participant)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildManualPolicyFixture(t))
			completeProgrammaticRequest(t, fixture, userRequest("invalid-original", manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest("invalid-retained", manualRetainedText))

			// Act while the policy consumes the rejection and returns CANCELED without an error.
			terminal := requestProgrammaticCompaction(t, fixture, "invalid-producer", guidance)

			// Assert failure retains the entire producer rejection and prevents replacement.
			require.NotNil(t, terminal.GetFailed(), "terminal: %s", terminal)
			require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), completeCause)
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"invalid-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			originals, markers := countManualStoredEntries(stored)
			require.Equal(t, 1, originals)
			require.Zero(t, markers)
			require.Len(t, bodies.snapshot(), 2)
			// Assert released admission permits healthy independent work on the existing connection.
			completeProgrammaticRequest(t, fixture, userRequest("invalid-independent", manualNextText))
			require.Len(t, bodies.snapshot(), 3)
			fixture.closeOwner(t)
		})
	}
}

// TestConsumedInvalidProducerStopsCore verifies retained history, stopped future work, and healthy reuse.
func TestConsumedInvalidProducerStopsCore(t *testing.T) {
	for _, participant := range []string{consumedGeneratorProducer, consumedRequestProducer, consumedResultProducer} {
		t.Run(participant, func(t *testing.T) {
			// Arrange the ordinary tool's nested request with a consuming policy and invalid output.
			t.Setenv(consumedInvalidOutputEnvironment, manualFixtureEnabled)
			guidance, completeCause := configureConsumedInvalidProducer(t, participant)
			t.Setenv(consumedChildGuidanceEnvironment, guidance)
			// Act through Core using the shared real-process scenario.
			// Assert failure, history, later admission rejection, and healthy reuse in the scenario.
			assertConsumedCompactionStopsCore(t, completeCause)
		})
	}
}
