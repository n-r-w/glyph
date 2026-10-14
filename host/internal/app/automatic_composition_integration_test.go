//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestAutomaticCompositionPreservesConversationBinding checks composition and nested-model isolation.
func TestAutomaticCompositionPreservesConversationBinding(t *testing.T) {
	for _, guidance := range []string{manualRequestCompositionGuidance, manualRequestClearGuidance} {
		t.Run(guidance, func(t *testing.T) {
			// Arrange participant reports and a configured generator before starting the public application.
			const inputEnvironment = "GLYPH_REFERENCE_AUTOMATIC_INPUT"
			scratch := t.TempDir()
			inputs := t.TempDir()
			inputPath := filepath.Join(scratch, "input.json")
			outgoingPath := filepath.Join(scratch, "outgoing.json")
			outcomePath := filepath.Join(scratch, "outcome.json")
			observationPath := filepath.Join(scratch, "observation.json")
			t.Setenv(inputEnvironment, inputPath)
			t.Setenv(automaticFixtureModeEnvironment, guidance)
			t.Setenv(automaticFixtureTargetEnvironment, automaticFixtureTarget)
			t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
			t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
			t.Setenv(manualRequestInputsEnvironment, inputs)
			t.Setenv(conversationObservationEnvironment, observationPath)
			t.Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
			if guidance == manualRequestClearGuidance {
				t.Setenv(manualGeneratorModeEnvironment, manualGeneratorImagesMode)
			}
			directory := buildManualPolicyFixture(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			completeProgrammaticRequest(t, fixture, userRequest("composition-seed", manualOriginalText))
			// Act through ordinary preparation, composition, final validation and successful model dispatch.
			completeProgrammaticRequest(t, fixture, userRequest("composition-next", automaticFixtureTarget))
			// Assert ordered immutable composition and a frozen conversation binding despite diagnostic model changes.
			assertManualRequestInputs(t, inputs, inputPath, guidance, true)
			encoded, err := os.ReadFile(inputPath)
			require.NoError(t, err)
			captured := new(extensionpb.CompactionPolicyInvocation)
			require.NoError(t, protojson.Unmarshal(encoded, captured))
			encoded, err = os.ReadFile(outgoingPath)
			require.NoError(t, err)
			outgoing := new(extensionpb.CompactionOutgoingInput)
			require.NoError(t, protojson.Unmarshal(encoded, outgoing))
			require.True(t, proto.Equal(captured.GetModel(), outgoing.GetModel()))
			require.Equal(t, captured.GetInstructions(), outgoing.GetInstructions())
			require.Equal(t, captured.GetReasoningChoice(), outgoing.GetReasoningChoice())
			encoded, err = os.ReadFile(outcomePath)
			require.NoError(t, err)
			outcome := new(extensionpb.CompactionOutcome)
			require.NoError(t, protojson.Unmarshal(encoded, outcome))
			require.Equal(t, configuredRequestModelID, outcome.GetState().GetCurrentRequest().GetModel().GetModelId())
			actual := bodies.snapshot()
			expected := 2
			if guidance == manualRequestClearGuidance {
				expected++
				assertConfiguredProviderRequest(t, actual[1], "", true)
			}
			require.Len(t, actual, expected)
			var body struct {
				Model        string `json:"model"`
				Instructions string `json:"instructions"`
				Reasoning    struct {
					Effort string `json:"effort"`
				} `json:"reasoning"`
			}
			require.NoError(t, json.Unmarshal(actual[len(actual)-1], &body))
			require.Equal(t, captured.GetModel().GetModelId(), body.Model)
			require.Equal(t, captured.GetInstructions(), body.Instructions)
			require.Equal(t, automaticWireEffortNone, body.Reasoning.Effort)
			require.Contains(t, string(actual[len(actual)-1]), automaticFixtureTarget)
			require.NotContains(t, string(actual[len(actual)-1]), manualOriginalText)
			calls, err := filepath.Glob(observationPath + conversationObservationCallGlob)
			require.NoError(t, err)
			require.Len(t, calls, 2)
		})
	}
}
