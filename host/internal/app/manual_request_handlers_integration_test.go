//go:build integration

// Package app assembles the host application and its public integration fixtures.
package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// manualRequestInputsEnvironment names the environment variable that request handlers use to record their inputs.
const manualRequestInputsEnvironment = "GLYPH_REFERENCE_REQUEST_INPUTS"

// manualSeparateRequestsEnvironment names the environment variable for a separate participant runtime.
const manualSeparateRequestsEnvironment = "GLYPH_REFERENCE_SEPARATE_REQUESTS"

// manualRequestHandlersSuite checks policy-owned request composition through the public application.
type manualRequestHandlersSuite struct {
	// Suite supplies scenario assertions.
	suite.Suite
}

// TestProgrammaticManualRequestHandlers checks ordered actions and terminal handler outcomes.
func TestProgrammaticManualRequestHandlers(t *testing.T) {
	// Arrange serialized fixture configuration and an independent public-entry suite.
	t.Setenv(manualRequestInputsEnvironment, "")
	t.Setenv(manualSeparateRequestsEnvironment, "")
	// Act through public request composition and handler terminal actions.
	// Assert original history, captured inputs, and terminal causes in each scenario.
	suite.Run(t, &manualRequestHandlersSuite{Suite: suite.Suite{}})
}

// TestComposition checks independent request and result changes with original requests isolated from later changes.
func (s *manualRequestHandlersSuite) TestComposition() {
	// Arrange the composition fixture in runScenario.
	// Act through the public compaction operation in runScenario.
	// Assert request composition and original requests isolated from later changes in runScenario.
	s.runScenario(manualRequestCompositionGuidance, manualRequestFinalSummary, "", false)
}

// TestSeparateRuntimeComposition checks captured request participants outside the policy runtime.
func (s *manualRequestHandlersSuite) TestSeparateRuntimeComposition() {
	// Arrange request-only registration in a distinct extension process.
	s.T().Setenv(manualSeparateRequestsEnvironment, manualFixtureEnabled)
	// Act through public composition in runScenario.
	// Assert the captured runtime IDs and the compaction result in runScenario.
	s.runScenario(manualRequestCompositionGuidance, manualRequestFinalSummary, "", false)
}

// TestClearResult checks that request changes survive clearing a preceding supplied result.
func (s *manualRequestHandlersSuite) TestClearResult() {
	// Arrange the result-clearing fixture in runScenario.
	// Act through the public compaction operation in runScenario.
	// Assert preserved request changes and cleared result in runScenario.
	s.runScenario(manualRequestClearGuidance, "", "reference composition has no result", false)
}

// TestCancellation checks that an explicit handler cancellation ends composition before mutation.
func (s *manualRequestHandlersSuite) TestCancellation() {
	// Arrange the canceling handler fixture in runScenario.
	// Act through the public compaction operation in runScenario.
	// Assert cancellation before session mutation in runScenario.
	s.runScenario(manualRequestCancelGuidance, "", "", true)
}

// TestFailure checks that handler failure ends composition and preserves its complete cause.
func (s *manualRequestHandlersSuite) TestFailure() {
	// Arrange the failing handler fixture in runScenario.
	// Act through the public compaction operation in runScenario.
	// Assert the complete handler failure cause in runScenario.
	s.runScenario(manualRequestFailGuidance, "", manualRequestFailureCause, false)
}

// TestHandledChildFailure checks that public compaction retains a handled model-selection error
// and a later handler error.
func (s *manualRequestHandlersSuite) TestHandledChildFailure() {
	// Arrange a participant that handles its nested selection rejection.
	// Act through a later failing handler in runScenario.
	// Assert both the model-selection cause and the handler cause in runScenario.
	s.runScenario(
		manualRequestFailAfterChildGuidance,
		"",
		manualRequestFailureCause,
		false,
	)
}

// TestInvalidAction checks that an invalid action ends composition before mutation.
func (s *manualRequestHandlersSuite) TestInvalidAction() {
	// Arrange the invalid-action fixture in runScenario.
	// Act through the public compaction operation in runScenario.
	// Assert rejection before session mutation in runScenario.
	s.runScenario(manualRequestInvalidGuidance, "", manualRequestInvalidCause, false)
}

// runScenario observes the replacement, original history, and actual next provider request.
func (s *manualRequestHandlersSuite) runScenario(guidance, summary, failure string, canceled bool) {
	const (
		manualRequestExecutable          = "reference-requests"
		manualRequestOriginalOperation   = "request-original"
		manualRequestRetainedOperation   = "request-retained"
		manualRequestCompactionOperation = "request-handler-compaction"
		manualRequestNextOperation       = "request-next"
		manualRequestStoredOperation     = "request-stored"
	)
	t := s.T()
	// Arrange real runtime, SDK, session, and public manual-entry wiring.
	inputs := t.TempDir()
	t.Setenv(manualRequestInputsEnvironment, inputs)
	policyInputPath := filepath.Join(t.TempDir(), manualRequestPolicyInputFile)
	t.Setenv(manualFixtureGlyphReferencePolicyInput, policyInputPath)
	directory := buildManualPolicyFixture(t)
	if os.Getenv(manualSeparateRequestsEnvironment) == manualFixtureEnabled {
		binary, readErr := os.ReadFile(filepath.Join(directory, manualFixtureReferencePolicy))
		require.NoError(t, readErr)
		require.NoError(t, os.WriteFile(filepath.Join(directory, manualRequestExecutable), binary, 0o700))
	}
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest(manualRequestOriginalOperation, manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest(manualRequestRetainedOperation, manualRetainedText))
	// Act through the public operation and then the next provider request.
	terminal := requestProgrammaticCompaction(t, fixture, manualRequestCompactionOperation, guidance)
	if failure != "" {
		require.NotNil(t, terminal.GetFailed())
		require.Contains(t, terminal.GetFailed().GetMessage(), failure)
		if guidance == manualRequestFailAfterChildGuidance {
			require.Contains(t, terminal.GetFailed().GetMessage(), manualSelectionPreparationCause)
		}
	} else {
		require.NotNil(t, terminal.GetCompleted(), "failure: %s", terminal.GetFailed().GetMessage())
		require.Equal(t, canceled, terminal.GetCompleted().GetCompaction().GetCanceled())
		if !canceled {
			require.Equal(
				t,
				summary,
				terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction().GetSummary(),
			)
		}
	}
	completeProgrammaticRequest(t, fixture, userRequest(manualRequestNextOperation, manualNextText))
	// Assert preserved originals, the replacement count, and prepared provider input.
	stored := sendProgrammaticOperation(
		t,
		fixture,
		manualRequestStoredOperation,
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	originalCount, markerCount := countManualStoredEntries(stored)
	require.Equal(t, 1, originalCount)
	actual := bodies.snapshot()
	require.Len(t, actual, 3)
	if summary != "" {
		require.Equal(t, 1, markerCount)
		require.Contains(t, string(actual[2]), summary)
		require.NotContains(t, string(actual[2]), manualOriginalText)
	} else {
		require.Zero(t, markerCount)
		require.Contains(t, string(actual[2]), manualOriginalText)
	}
	require.Contains(t, string(actual[2]), manualRetainedText)
	assertManualRequestInputs(
		t,
		inputs,
		policyInputPath,
		guidance,
		summary != "" || guidance == manualRequestClearGuidance,
	)
	fixture.closeOwner(t)
}

// assertManualRequestInputs checks captured order, immutable originals, and independent current values.
func assertManualRequestInputs(t *testing.T, directory, policyPath, guidance string, finished bool) {
	const (
		manualRequestInitialSummary    = "Initial request handler result."
		manualRequestFirstInstruction  = "first request change"
		manualRequestFirstHandler      = "request-z-first"
		manualRequestSecondHandler     = "request-y-second"
		manualRequestThirdHandler      = "request-x-third"
		manualRequestTerminalHandler   = "request-w-terminal"
		manualRequestLastHandler       = "request-v-last"
		manualRequestArtifactExtension = ".json"
	)
	t.Helper()
	encoded, err := os.ReadFile(policyPath)
	require.NoError(t, err)
	policy := new(extensionpb.CompactionPolicyInvocation)
	require.NoError(t, protojson.Unmarshal(encoded, policy))
	participants := policy.GetRequestParticipants()
	require.Len(t, participants, 5)
	expectedIDs := []string{
		manualRequestFirstHandler,
		manualRequestSecondHandler,
		manualRequestThirdHandler,
		manualRequestTerminalHandler,
		manualRequestLastHandler,
	}
	var original *extensionpb.CompactionRequest
	for order, participant := range participants {
		require.Equal(t, expectedIDs[order], participant.GetHandlerId())
		require.Equal(t, int64(order), participant.GetRegistrationOrder())
		require.NotEmpty(t, participant.GetExtensionId())
		require.NotEmpty(t, participant.GetRuntimeId())
		if os.Getenv(manualSeparateRequestsEnvironment) == manualFixtureEnabled {
			require.NotEqual(t, policy.GetPolicyRuntimeId(), participant.GetRuntimeId())
		} else {
			require.Equal(t, policy.GetPolicyRuntimeId(), participant.GetRuntimeId())
		}
		path := filepath.Join(directory, participant.GetHandlerId()+manualRequestArtifactExtension)
		if order == 4 && !finished {
			require.NoFileExists(t, path)
			continue
		}
		data, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		input := new(extensionpb.CompactionRequestInvocation)
		require.NoError(t, protojson.Unmarshal(data, input))
		if order == 0 {
			original = proto.CloneOf(input.GetOriginal())
			require.Equal(t, guidance, original.GetInstructions())
			require.True(t, proto.Equal(original, input.GetCurrent()))
			require.Nil(t, input.GetCurrentResult())
		} else {
			require.True(t, proto.Equal(original, input.GetOriginal()), "original changed before participant %d", order)
		}
		switch order {
		case 1:
			require.Equal(t, manualRequestFirstInstruction, input.GetCurrent().GetInstructions())
			require.Equal(t, manualRequestInitialSummary, input.GetCurrentResult().GetSummary())
		case 2:
			require.Equal(t, manualRequestSecondInstruction, input.GetCurrent().GetInstructions())
			require.Equal(t, manualRequestInitialSummary, input.GetCurrentResult().GetSummary())
		case 3, 4:
			require.Equal(t, manualRequestSecondInstruction, input.GetCurrent().GetInstructions())
			if order == 4 && guidance == manualRequestClearGuidance {
				require.Nil(t, input.GetCurrentResult())
			} else {
				require.Equal(t, manualRequestFinalSummary, input.GetCurrentResult().GetSummary())
				require.Equal(t, []byte(manualRequestResultDetails), input.GetCurrentResult().GetDetails())
			}
		}
	}
}

// isManualRequestTerminalScenario identifies request-handler terminal fixtures.
func isManualRequestTerminalScenario(guidance string) bool {
	return guidance == manualRequestCancelGuidance || guidance == manualRequestFailGuidance ||
		guidance == manualRequestInvalidGuidance ||
		guidance == manualRequestFailAfterChildGuidance
}
