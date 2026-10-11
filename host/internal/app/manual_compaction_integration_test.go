//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"bytes"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// manualReferenceSummary identifies the supplied reference result.
	manualReferenceSummary = "Reference policy replacement summary."
	// manualOriginalText identifies context replaced by the reference policy.
	manualOriginalText = "original manual context"
	// manualRetainedText identifies the retained original suffix.
	manualRetainedText = "retained manual context"
	// manualNextText identifies the next actual conversation request.
	manualNextText = "next manual context"
)

// TestScenario checks public replacement and subsequent provider input.
func (s *programmaticManualCompactionUsesRegisteredPolicySuite) TestScenario() {
	const manualFixtureStoredOriginals = "stored-originals"

	const manualFixturePolicyruntimeid = "policyRuntimeId"

	const manualFixtureManualReplacement = "manual-replacement"

	t := s.T()
	// Arrange a real reference extension and an application with recorded provider requests.
	directory := buildManualPolicyFixture(t)
	inputPath := os.Getenv(manualFixtureGlyphReferencePolicyInput)

	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))

	// Act through manual compaction and the next actual model request.
	terminal := requestProgrammaticCompaction(
		t,
		fixture,
		manualFixtureManualReplacement,
		manualFixturePreserveTheRecentUserTurn,
	)
	require.NotNil(t, terminal.GetCompleted(), "terminal failure: %s", terminal.GetFailed().GetMessage())
	compacted := terminal.GetCompleted().GetCompaction()
	require.NotNil(t, compacted.GetCommitted())
	require.Equal(t, manualReferenceSummary, compacted.GetCommitted().GetCompaction().GetSummary())
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureNext, manualNextText))

	// Assert that provider input uses the replacement and retained suffix rather than the original prefix.
	actualBodies := bodies.snapshot()
	require.Len(t, actualBodies, 3)
	encoded := actualBodies[2]
	require.Contains(t, string(encoded), manualReferenceSummary)
	require.Contains(t, string(encoded), manualRetainedText)
	require.Contains(t, string(encoded), manualNextText)
	require.NotContains(t, string(encoded), manualOriginalText)
	captured, err := os.ReadFile(inputPath)
	require.NoError(t, err)
	require.Contains(t, string(captured), manualOriginalText)
	require.Contains(t, string(captured), manualRetainedText)
	require.Contains(t, string(captured), manualFixturePolicyruntimeid)
	stored := sendProgrammaticOperation(
		t,
		fixture,
		manualFixtureStoredOriginals,
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		},
	).GetSessionEntries().
		GetEntries()
	require.Len(t, stored, 7)
	originalCount, markerCount := countManualStoredEntries(stored)
	require.Equal(t, 1, originalCount)
	require.Equal(t, 1, markerCount)
	fixture.closeOwner(t)
}

// TestScenario checks explicit owner absence through the public client.
func (s *programmaticManualCompactionRejectsAbsentPolicySuite) TestScenario() {
	const manualFixtureManualAbsence = "manual-absence"

	t := s.T()
	// Arrange an initialized application with no registered extension policy.
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	fixture := startProgrammaticFixture(t, paths)

	// Act by requesting manual compaction through Programmatic Control.
	terminal := requestProgrammaticCompaction(t, fixture, manualFixtureManualAbsence, "manual guidance")

	// Assert that owner absence is distinct from history or generator errors.
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetMessage(), "no compaction policy owner is registered")
	fixture.closeOwner(t)
}

// requestProgrammaticCompaction awaits any terminal result for one public manual call.
func requestProgrammaticCompaction(
	t *testing.T,
	fixture *programmaticFixture,
	operationID, instructions string,
) *programmaticpb.HostEvent {
	t.Helper()
	command := new(programmaticpb.ControllerRequest)
	command.SetCompact(programmaticpb.Compact_builder{Instructions: new(instructions)}.Build())
	request := programmaticpb.OpenRequest_builder{OperationId: new(operationID), Request: command}.Build()
	require.NoError(t, fixture.stream.Send(request))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() != operationID {
			continue
		}
		event := response.GetEvent()
		if event.HasCompleted() || event.HasFailed() || event.HasCanceled() || event.HasRejected() {
			return event
		}
	}
}

// buildManualPolicyFixture builds an extension that uses only the public SDK.
func buildManualPolicyFixture(t *testing.T) string {
	const manualFixtureTestdata = "testdata"

	const manualFixtureGo = "go"

	const manualFixtureExternalPlugins = "external-plugins"

	const manualFixtureBuild = "build"

	const manualFixtureCmdCompactionPolicy = "./cmd/compaction-policy"

	const manualFixtureO = "-o"

	t.Helper()
	directory := t.TempDir()
	command := exec.CommandContext(
		t.Context(),
		manualFixtureGo,
		manualFixtureBuild,
		manualFixtureO,
		filepath.Join(directory, manualFixtureReferencePolicy),
		manualFixtureCmdCompactionPolicy,
	)
	command.Dir = filepath.Join(repoRoot(t), manualFixtureTestdata, manualFixtureExternalPlugins)
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	return directory
}

// newManualCompactionProviderTransport records actual model requests and supplies terminal responses.
func newManualCompactionProviderTransport(t *testing.T) (*MockHTTPRoundTripper, *requestBodies) {
	t.Helper()
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	bodies := &requestBodies{}
	transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
		func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			bodies.append(encoded)
			return &http.Response{
				Status:     "",
				StatusCode: http.StatusOK,
				Proto:      "",
				ProtoMajor: 0,
				ProtoMinor: 0,
				Header: make(
					http.Header,
				),
				Body:             io.NopCloser(bytes.NewBufferString(finalResponseSSE)),
				ContentLength:    0,
				TransferEncoding: nil,
				Close:            false,
				Uncompressed:     false,
				Trailer:          nil,
				Request:          nil,
				TLS:              nil,
			}, nil
		},
	)
	return transport, bodies
}

// TestScenario checks public UI replacement and subsequent provider input.
func (s *uIManualCompactionUsesRegisteredPolicySuite) TestScenario() {
	t := s.T()
	// Arrange the public UI process and the independently built policy.
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	uiDirectory := t.TempDir()
	reportPath := filepath.Join(t.TempDir(), manualFixtureManualUiJson)

	t.Setenv(appUITraceEnvironment, reportPath)
	t.Setenv(manualGuidanceEnvironment, manualFixturePreserveTheRecentUserTurn)
	writeUIExecutable(t, uiDirectory, manualFixtureManualUi)

	// Act through the real UI SDK connection.
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: directory},
		ExtensionDirectory: directory, UIDirectory: uiDirectory, UIID: manualFixtureManualUi, SocketPath: "",
	}, &bytes.Buffer{}, &bytes.Buffer{})

	// Assert a durable receipt and replacement projection in the next actual request.
	require.NoError(t, err)
	encoded, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report manualPublicReport
	require.NoError(t, json.Unmarshal(encoded, &report))
	require.True(t, report.Committed, report.Error)
	require.Empty(t, report.Error)
	require.Equal(t, manualReferenceSummary, report.Summary)
	actualBodies := bodies.snapshot()
	require.Len(t, actualBodies, 3)
	require.Contains(t, string(actualBodies[2]), manualReferenceSummary)
	require.Contains(t, string(actualBodies[2]), manualRetainedText)
	require.NotContains(t, string(actualBodies[2]), manualOriginalText)
}

// TestScenario checks cancellation, stale capture, and durable failure receipts.
func (s *programmaticManualCompactionTerminalStatesSuite) TestScenario() {
	const manualFixtureTerminalState = "terminal-state"

	const manualFixtureStoredTerminalState = "stored-terminal-state"

	t := s.T()
	// Arrange a serialized application fixture for each policy terminal state.
	for _, scenario := range append(newManualTerminalScenarios(), newManualUnavailableObserverScenario()) {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange the policy and caller fixtures for this terminal result.
			outcomePath := configureManualOutcomeFixture(t, scenario.guidance)
			directory := buildManualOutcomePolicyFixture(t, scenario.guidance)

			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))

			// Act through the public manual operation and read the stored originals.
			terminal := requestProgrammaticCompaction(t, fixture, manualFixtureTerminalState, scenario.guidance)
			result := terminal.GetCompleted().GetCompaction()
			if scenario.committed || scenario.canceled {
				require.NotNil(t, result, terminal.GetFailed().GetMessage())
				require.Equal(t, scenario.committed, result.GetCommitted() != nil)
				require.Equal(t, scenario.canceled, result.GetCanceled())
				if scenario.cause != "" {
					require.Contains(t, result.GetError(), scenario.cause)
				}
			} else {
				require.NotNil(t, terminal.GetFailed())
				require.Contains(t, terminal.GetFailed().GetMessage(), scenario.cause)
			}
			assertManualOutcome(t, outcomePath, scenario.guidance, scenario.committed, result.GetError())
			stored := sendProgrammaticOperation(
				t,
				fixture,
				manualFixtureStoredTerminalState,
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()

			// Assert one replacement at most and preserved original user data.
			originalCount, markerCount := countManualStoredEntries(stored)
			require.Equal(t, 1, originalCount)
			if scenario.committed {
				require.Equal(t, 1, markerCount)
				completeProgrammaticRequest(t, fixture, userRequest(manualFixtureNext, manualNextText))
				actual := bodies.snapshot()
				require.Contains(t, string(actual[len(actual)-1]), manualReferenceSummary)
				require.NotContains(t, string(actual[len(actual)-1]), manualOriginalText)
			} else {
				require.Zero(t, markerCount)
			}
			fixture.closeOwner(t)
		})
	}
}

// TestScenario checks public UI failure and cancellation contracts.
func (s *uIManualCompactionTerminalStatesSuite) TestScenario() {
	t := s.T()
	// Arrange the public UI protocol fixture for the policy terminal states.

	scenarios := append([]manualTerminalScenario{newManualAbsentOwnerScenario()}, newManualTerminalScenarios()...)
	for _, scenario := range append(scenarios, newManualUnavailableObserverScenario()) {
		t.Run(scenario.guidance, func(t *testing.T) {
			// Arrange the policy and caller fixtures for this terminal result.
			outcomePath := configureManualOutcomeFixture(t, scenario.guidance)
			directory := t.TempDir()
			if scenario.guidance != manualNoOwnerGuidance {
				directory = buildManualOutcomePolicyFixture(t, scenario.guidance)
			}
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			uiDirectory := t.TempDir()
			reportPath := filepath.Join(t.TempDir(), manualFixtureManualUiJson)
			t.Setenv(appUITraceEnvironment, reportPath)
			t.Setenv(manualGuidanceEnvironment, scenario.guidance)
			writeUIExecutable(t, uiDirectory, manualFixtureManualUi)

			// Act through actual public UI operations.
			err := runWithPaths(t.Context(), paths, cli.Command{
				Mode:               cli.ModeUI,
				Headless:           headless.Command{UserText: "", ExtensionDirectory: directory},
				ExtensionDirectory: directory, UIDirectory: uiDirectory, UIID: manualFixtureManualUi, SocketPath: "",
			}, &bytes.Buffer{}, &bytes.Buffer{})

			// Assert the terminal result, original storage, and subsequent model input.
			require.NoError(t, err)
			encoded, err := os.ReadFile(reportPath)
			require.NoError(t, err)
			var report manualPublicReport
			require.NoError(t, json.Unmarshal(encoded, &report))
			require.Equal(t, scenario.committed, report.Committed, report.Error)
			require.Equal(t, scenario.canceled, report.Canceled, report.Error)
			if scenario.cause != "" {
				require.Contains(t, report.Error, scenario.cause)
			} else {
				require.Empty(t, report.Error)
			}
			if scenario.guidance != manualNoOwnerGuidance {
				assertManualOutcome(t, outcomePath, scenario.guidance, scenario.committed, report.Error)
				require.Equal(t, 1, report.Originals)
			}
			if scenario.committed {
				require.Equal(t, 1, report.Markers)
				actual := bodies.snapshot()
				require.Contains(t, string(actual[len(actual)-1]), manualReferenceSummary)
				require.NotContains(t, string(actual[len(actual)-1]), manualOriginalText)
			} else {
				require.Zero(t, report.Markers)
			}
		})
	}
}

// TestScenario checks explicit owner cardinality through registration.
func (s *programmaticManualCompactionRejectsAmbiguousOwnersSuite) TestScenario() {
	const manualFixtureAnotherPolicy = "another-policy"

	const manualFixtureAmbiguousPolicy = "ambiguous-policy"

	t := s.T()
	// Arrange two real registered policy processes with separate extension identities.
	directory := buildManualPolicyFixture(t)
	executable, err := os.ReadFile(filepath.Join(directory, manualFixtureReferencePolicy))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, manualFixtureAnotherPolicy), executable, 0o700))
	paths := testPaths(t, configuredRequestSettings)
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)

	// Act through the public manual operation.
	terminal := requestProgrammaticCompaction(t, fixture, manualFixtureAmbiguousPolicy, "")

	// Assert that Host does not choose an owner by registration order.
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetMessage(), "multiple compaction policy owners are registered")
	fixture.closeOwner(t)
}
