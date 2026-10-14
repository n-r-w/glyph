//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestAutomaticPreparationAcrossApplicationModes checks preparation through headless and UI entry points.
func TestAutomaticPreparationAcrossApplicationModes(t *testing.T) {
	for _, assembly := range []struct {
		name string
		mode cli.Mode
	}{{name: "headless", mode: cli.ModeHeadless}, {name: "ui", mode: cli.ModeUI}} {
		for _, decision := range []string{automaticFixturePreserveMode, automaticFixtureReplaceMode} {
			t.Run(assembly.name+"/"+decision, func(t *testing.T) {
				// Arrange report paths before starting the connected application and reference owner.
				const inputEnvironment = "GLYPH_REFERENCE_AUTOMATIC_INPUT"
				scratch := t.TempDir()
				inputPath := filepath.Join(scratch, "input.json")
				outgoingPath := filepath.Join(scratch, "outgoing.json")
				outcomePath := filepath.Join(scratch, "outcome.json")
				t.Setenv(inputEnvironment, inputPath)
				t.Setenv(automaticFixtureModeEnvironment, decision)
				t.Setenv(automaticFixtureTargetEnvironment, "")
				t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
				t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
				directory := buildManualPolicyFixture(t)
				paths := testPaths(t, configuredRequestSettings)
				writeProgrammaticCredentials(t, paths)
				transport, bodies := newManualCompactionProviderTransport(t)
				previous := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = previous })
				// Act through the public assembly's ordinary conversation entry point.
				runPublicExtensionMode(t, assembly.mode, paths, directory, "automatic-mode", automaticFixtureTarget)
				// Assert one preparation and one provider call with policy-selected history.
				encoded, err := os.ReadFile(inputPath)
				require.NoError(t, err)
				input := new(extensionpb.CompactionPolicyInvocation)
				require.NoError(t, protojson.Unmarshal(encoded, input))
				require.Equal(
					t,
					extensionpb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC,
					input.GetPurpose(),
				)
				require.NotEmpty(t, input.GetPolicyRuntimeId())
				actual := bodies.snapshot()
				require.Len(t, actual, 1)
				if decision == automaticFixturePreserveMode {
					for _, record := range input.GetHistory() {
						for _, content := range record.GetUser().GetContent() {
							if content.GetText() != "" {
								require.Contains(t, string(actual[0]), content.GetText())
							}
						}
					}
					_, err = os.Stat(outgoingPath)
					require.True(t, os.IsNotExist(err))
					_, err = os.Stat(outcomePath)
					require.True(t, os.IsNotExist(err))
				} else {
					encoded, err = os.ReadFile(outgoingPath)
					require.NoError(t, err)
					outgoing := new(extensionpb.CompactionOutgoingInput)
					require.NoError(t, protojson.Unmarshal(encoded, outgoing))
					require.Equal(t, input.GetModel().GetProviderId(), outgoing.GetModel().GetProviderId())
					require.Equal(t, input.GetModel().GetModelId(), outgoing.GetModel().GetModelId())
					require.Equal(t, input.GetReasoningChoice(), outgoing.GetReasoningChoice())
					require.Equal(t, input.GetInstructions(), outgoing.GetInstructions())
					require.Contains(t, string(actual[0]), automaticFixtureSummary)
					require.Contains(t, string(encoded), automaticFixtureSummary)
				}
			})
		}
	}
}
