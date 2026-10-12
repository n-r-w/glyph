//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestTerminalActions checks stopped composition and last accepted observer state.
func (s *manualResultHandlersSuite) TestTerminalActions() {
	// Arrange exclusive cancellation, invalid-action, rejected-candidate and participant-failure cases.
	cases := []struct {
		mode     string
		cause    string
		canceled bool
	}{
		{mode: "cancel", cause: "", canceled: true},
		{mode: "canceled", cause: "", canceled: true},
		{mode: "mixed", cause: "cancellation must be exclusive", canceled: false},
		{mode: "clear", cause: "result disposition is invalid", canceled: false},
		{mode: "missing", cause: "replacing result requires a candidate", canceled: false},
		{mode: "unexpected", cause: "preserving result cannot supply a replacement", canceled: false},
		{mode: "invalid", cause: "result disposition is invalid", canceled: false},
		{mode: "boundary", cause: "compaction boundary is outside its branch", canceled: false},
		{mode: "producer", cause: "does not match the selected extension", canceled: false},
		{mode: "failure", cause: "result participant source failure", canceled: false},
		{mode: "classified", cause: "result participant source failure", canceled: false},
	}
	// Act through each public terminal scenario.
	// Assert stopped composition and the last accepted state in the scenario helper.
	for _, scenario := range cases {
		s.Run(scenario.mode, func() { s.runTerminalScenario(scenario.mode, scenario.cause, scenario.canceled) })
	}
}

// runTerminalScenario checks a public participant outcome against unchanged durable entries.
func (s *manualResultHandlersSuite) runTerminalScenario(mode, cause string, canceled bool) {
	t := s.T()
	// Arrange a later terminal participant after one accepted replacement.
	inputs := t.TempDir()
	t.Setenv(manualResultInputsEnvironment, inputs)
	t.Setenv("GLYPH_REFERENCE_RESULT_MODE", mode)
	outcomePath := configureManualOutcomeFixture(t, "")
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, _ := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest("result-stop-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("result-stop-retained", manualRetainedText))
	before := sendProgrammaticOperation(t, fixture, "result-stop-before", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	// Act through the public manual operation.
	terminal := requestProgrammaticCompaction(t, fixture, "result-stop-compaction", "")
	after := sendProgrammaticOperation(t, fixture, "result-stop-after", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	// Assert terminal classification, complete causes and absence of partial mutation.
	if canceled {
		require.NotNil(t, terminal.GetCompleted())
		require.True(t, terminal.GetCompleted().GetCompaction().GetCanceled())
	} else {
		require.NotNil(t, terminal.GetFailed())
		require.Contains(t, terminal.GetFailed().GetMessage(), cause)
	}
	require.Len(t, after, len(before))
	for index := range before {
		require.True(t, proto.Equal(before[index], after[index]))
	}
	encoded, err := os.ReadFile(outcomePath)
	require.NoError(t, err)
	outcome := new(extensionpb.CompactionOutcome)
	require.NoError(t, protojson.Unmarshal(encoded, outcome))
	require.Nil(t, outcome.GetCommitted())
	require.Equal(t, canceled, outcome.GetCanceled())
	acceptedInput := readManualResultInput(t, inputs, "result-a-second")
	require.True(
		t,
		proto.Equal(acceptedInput.GetCurrentResult(), outcome.GetState().GetResult()),
		"failure observers must retain the complete accepted result",
	)
	require.True(t, proto.Equal(acceptedInput.GetOriginalRequest(), outcome.GetState().GetOriginalRequest()))
	require.True(t, proto.Equal(acceptedInput.GetCurrentRequest(), outcome.GetState().GetCurrentRequest()))
	require.Equal(t, manualResultSummary, outcome.GetState().GetResult().GetSummary())
	if !canceled {
		require.Contains(t, outcome.GetError(), cause)
	}
	require.NoFileExists(t, filepath.Join(inputs, "result-m-last.json"))
	fixture.closeOwner(t)
}
