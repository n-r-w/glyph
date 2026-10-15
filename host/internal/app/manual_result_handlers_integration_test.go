//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// manualResultsEnvironment is the environment variable name for enabling public result participants.
	manualResultsEnvironment = "GLYPH_REFERENCE_RESULTS"
	// manualResultInputsEnvironment is the environment variable name
	// for the directory of recorded result invocations.
	manualResultInputsEnvironment = "GLYPH_REFERENCE_RESULT_INPUTS"
	// manualResultSummary is the expected composed replacement.
	manualResultSummary = "Reference composed result summary."
)

// manualResultHandlersSuite exercises result composition through Programmatic Control.
type manualResultHandlersSuite struct {
	// Suite provides scenario assertions.
	suite.Suite
}

// TestProgrammaticManualResultHandlers checks public result composition and durable history.
func TestProgrammaticManualResultHandlers(t *testing.T) {
	// Arrange isolated runtime configuration.
	t.Setenv(manualResultsEnvironment, manualFixtureEnabled)
	// Act through the public-entry scenarios.
	// Assert composition and durable behavior in each scenario.
	suite.Run(t, &manualResultHandlersSuite{Suite: suite.Suite{}})
}

// TestOrderedComposition checks immutable originals, accepted current inputs and one durable marker.
func (s *manualResultHandlersSuite) TestOrderedComposition() {
	// Arrange ordinary result participants.
	// Act through public manual composition.
	// Assert ordered inputs and durable projection in the scenario helper.
	s.runOrderedComposition("", false, manualResultSummary)
}

// TestSeparateRuntime checks selected result participants outside the policy runtime.
func (s *manualResultHandlersSuite) TestSeparateRuntime() {
	// Arrange a participant-only runtime.
	// Act through public manual composition.
	// Assert source preservation across runtime boundaries in the scenario helper.
	s.runOrderedComposition("", true, manualResultSummary)
}

// TestConfiguredModel checks ordinary model access and complete normalized result usage.
func (s *manualResultHandlersSuite) TestConfiguredModel() {
	// Arrange a result participant with an ordinary configured-model request.
	// Act through public manual composition.
	// Assert conversation isolation and normalized accounting in the scenario helper.
	s.runOrderedComposition("model", false, manualResultSummary)
}

// TestHistoricalModel checks a historical source independent of the conversation model.
func (s *manualResultHandlersSuite) TestHistoricalModel() {
	// Arrange a result with a historical model selection and absent usage.
	// Act through public manual composition.
	// Assert historical source retention in the scenario helper.
	s.runOrderedComposition("historical", false, manualResultSummary)
}

// TestOriginalDerivedResult checks later replacement from the immutable original result.
func (s *manualResultHandlersSuite) TestOriginalDerivedResult() {
	// Arrange a later participant that derives its result from the original.
	// Act through public manual composition.
	// Assert current input and final replacement in the scenario helper.
	s.runOrderedComposition("original", false, "Reference original-derived result summary.")
}

// TestRequestSuppliedResult checks result composition after accepted request updates.
func (s *manualResultHandlersSuite) TestRequestSuppliedResult() {
	// Arrange a complete result supplied by a request handler.
	// Act through the public manual operation.
	// Assert generator bypass and accepted result inputs in the scenario helper.
	s.runOrderedComposition("request", false, manualResultSummary)
}

// TestGeneratedResult checks result composition after accepted generation.
func (s *manualResultHandlersSuite) TestGeneratedResult() {
	// Arrange a cleared request result and one registered generator.
	// Act through the public manual operation.
	// Assert accepted generator input to result handling in the scenario helper.
	s.runOrderedComposition("generator", false, manualResultSummary)
}

// runOrderedComposition checks ordered accepted inputs and one durable replacement.
func (s *manualResultHandlersSuite) runOrderedComposition(mode string, separate bool, summary string) {
	t := s.T()
	t.Setenv("GLYPH_REFERENCE_RESULT_MODE", mode)
	t.Setenv("GLYPH_REFERENCE_SEPARATE_RESULTS", "")
	guidance := ""
	if mode == "request" {
		guidance = manualRequestCompositionGuidance
	}
	if mode == "generator" {
		guidance = manualRequestClearGuidance
		t.Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
		t.Setenv(manualGeneratorModeEnvironment, "")
	}
	// Arrange real plugin, SDK, session and provider wiring.
	inputs := t.TempDir()
	t.Setenv(manualResultInputsEnvironment, inputs)
	directory := buildManualPolicyFixture(t)
	if separate {
		t.Setenv("GLYPH_REFERENCE_SEPARATE_RESULTS", manualFixtureEnabled)
		binary, err := os.ReadFile(filepath.Join(directory, manualFixtureReferencePolicy))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(directory, "reference-results"), binary, 0o700))
	}
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("result-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("result-retained", manualRetainedText))
	// Act through manual compaction and the next conversation request.
	terminal := requestProgrammaticCompaction(t, fixture, "result-compaction", guidance)
	// Assert the public result before inspecting participant inputs.
	require.NotNil(t, terminal.GetCompleted(), "failure: %s", terminal.GetFailed().GetMessage())
	marker := terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction()
	require.Equal(t, summary, marker.GetSummary())
	first := readManualResultInput(t, inputs, "result-z-first")
	second := readManualResultInput(t, inputs, "result-a-second")
	require.True(t, proto.Equal(first.GetOriginalRequest(), second.GetOriginalRequest()))
	require.True(t, proto.Equal(first.GetCurrentRequest(), second.GetCurrentRequest()))
	require.True(t, proto.Equal(first.GetOriginalResult(), second.GetOriginalResult()))
	require.True(t, proto.Equal(first.GetOriginalResult(), first.GetCurrentResult()))
	require.Equal(t, manualResultSummary, second.GetCurrentResult().GetSummary())
	require.NotEqual(t, manualResultSummary, second.GetOriginalResult().GetSummary())
	if mode == "original" {
		require.Equal(t, first.GetOriginalResult().HasDetails(), marker.HasDetails())
	} else {
		require.True(t, marker.HasDetails())
		require.Empty(t, marker.GetDetails())
	}
	if separate {
		require.Equal(t, "reference-results", marker.GetSource().GetExtensionId())
	}
	if mode == "historical" {
		modelSource := marker.GetSource().GetModelRequests().GetRequests()[0]
		require.Equal(t, "result-historical-provider", modelSource.GetProviderId())
		require.Equal(t, "result-historical-model", modelSource.GetModelId())
		require.Equal(t, programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH, modelSource.GetReasoningChoice())
		require.Nil(t, modelSource.GetUsage())
	}
	completeProgrammaticRequest(t, fixture, userRequest("result-next", manualNextText))
	stored := sendProgrammaticOperation(t, fixture, "result-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	originals, markers := countManualStoredEntries(stored)
	require.Equal(t, 1, originals)
	require.Equal(t, 1, markers)
	actual := bodies.snapshot()
	if mode == "model" {
		require.Len(t, actual, 4)
		assertConfiguredProviderRequest(t, actual[2], "", true)
		modelSource := marker.GetSource().GetModelRequests().GetRequests()[0]
		require.Equal(t, configuredRequestModelID, modelSource.GetModelId())
		require.Equal(t, programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH, modelSource.GetReasoningChoice())
		usage := modelSource.GetUsage()
		require.NotNil(t, usage)
		require.Equal(t, int64(4), usage.GetInputTokens())
		require.Equal(t, int64(4), usage.GetOutputTokens())
		require.Equal(t, int64(2), usage.GetCacheReadTokens())
		require.Equal(t, int64(1), usage.GetCacheWriteTokens())
		require.Equal(t, int64(3), usage.GetReasoningTokens())
		require.Equal(t, int64(11), usage.GetTotalTokens())
		actual = append(actual[:2], actual[3])
		assertOuterRequestUnaffected(t, actual[2])
	}
	require.Len(t, actual, 3)
	require.Contains(t, string(actual[2]), summary)
	require.Contains(t, string(actual[2]), manualRetainedText)
	require.NotContains(t, string(actual[2]), manualOriginalText)
	fixture.closeOwner(t)
}

// readManualResultInput decodes a participant's recorded public input.
func readManualResultInput(t *testing.T, directory, handler string) *extensionpb.CompactionResultInvocation {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join(directory, handler+".json"))
	require.NoError(t, err)
	input := new(extensionpb.CompactionResultInvocation)
	require.NoError(t, protojson.Unmarshal(encoded, input))
	return input
}
