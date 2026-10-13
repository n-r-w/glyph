//go:build integration

package app

import (
	"os"
	"path/filepath"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// manualGeneratorRetryEnvironment selects one retryable configured-model attempt.
const manualGeneratorRetryEnvironment = "GLYPH_REFERENCE_GENERATOR_RETRY"

// manualGeneratorProgressEnvironment names the fixture's public retry-progress artifact.
const manualGeneratorProgressEnvironment = "GLYPH_REFERENCE_GENERATOR_PROGRESS"

// TestGeneratorRetryProgress checks ordinary retries and public progress inside generation.
func (s *manualRequestHandlersSuite) TestGeneratorRetryProgress() {
	t := s.T()
	// Arrange one failed configured-model attempt and a public progress artifact.
	t.Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	t.Setenv(manualGeneratorModeEnvironment, manualGeneratorImagesMode)
	t.Setenv(manualGeneratorRetryEnvironment, manualFixtureEnabled)
	path := filepath.Join(t.TempDir(), "generator-progress.json")
	t.Setenv(manualGeneratorProgressEnvironment, path)
	// Act through public compaction and the ordinary configured-model retry path.
	s.runScenario(manualRequestClearGuidance, manualGeneratorSummary, "", false)
	// Assert the SDK progress has the ordinary allowance and full failed-provider cause.
	encoded, err := os.ReadFile(path)
	require.NoError(t, err)
	progress := new(extensionpb.ConfiguredModelRetryProgress)
	require.NoError(t, protojson.Unmarshal(encoded, progress))
	require.Equal(t, int64(1), progress.GetCompletedAttempts())
	require.Equal(t, int64(4), progress.GetAttemptLimit())
	require.Equal(t, int64(1000), progress.GetDelayMilliseconds())
	require.Contains(t, progress.GetError(), "generator retry provider source")
}

// TestGeneratorConfiguredImages checks nested image input and conversation isolation.
func (s *manualRequestHandlersSuite) TestGeneratorConfiguredImages() {
	// Arrange a generator that requests an image-capable configured model.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorImagesMode)
	observationPath := filepath.Join(s.T().TempDir(), "generator-conversation-observation.json")
	s.T().Setenv(conversationObservationEnvironment, observationPath)
	// Act through public manual compaction in runScenario.
	// Assert ordered image input, explicit selection, result metadata and isolated conversation history.
	s.runScenario(manualRequestClearGuidance, manualGeneratorSummary, "", false)
	calls, err := filepath.Glob(observationPath + conversationObservationCallGlob)
	s.Require().NoError(err)
	s.Require().Len(calls, 3)
}
