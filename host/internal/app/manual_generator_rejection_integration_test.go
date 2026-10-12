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

// manualGeneratorInvalidBoundaryMode selects a candidate outside the captured branch.
const manualGeneratorInvalidBoundaryMode = "invalid-boundary"

// TestRejectedGeneratorPreservesComposedState checks observer state after session-owned candidate rejection.
func (s *manualRequestHandlersSuite) TestRejectedGeneratorPreservesComposedState() {
	const (
		originalOperation   = "generator-rejection-original"
		retainedOperation   = "generator-rejection-retained"
		beforeOperation     = "generator-rejection-before"
		compactionOperation = "generator-rejection-compaction"
		afterOperation      = "generator-rejection-after"
		lastInputFile       = "request-v-last.json"
		boundaryCause       = "compaction boundary is outside its branch"
	)
	t := s.T()
	// Arrange a correctly attributed generator with an out-of-branch retained boundary.
	t.Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	t.Setenv(manualGeneratorModeEnvironment, manualGeneratorInvalidBoundaryMode)
	t.Setenv(manualRequestInputsEnvironment, t.TempDir())
	outcomePath := configureManualOutcomeFixture(t, manualRequestClearGuidance)
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	transport, _ := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	completeProgrammaticRequest(t, fixture, userRequest(originalOperation, manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest(retainedOperation, manualRetainedText))
	before := sendProgrammaticOperation(t, fixture, beforeOperation, func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	require.Len(t, before, 4)

	// Act through public manual compaction and read the resulting original session entries.
	terminal := requestProgrammaticCompaction(t, fixture, compactionOperation, manualRequestClearGuidance)
	after := sendProgrammaticOperation(t, fixture, afterOperation, func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()

	// Assert complete rejection text, no commit and unchanged original entry projections.
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetMessage(), boundaryCause)
	require.Len(t, after, len(before))
	for index := range before {
		require.True(t, proto.Equal(before[index], after[index]))
		require.Nil(t, after[index].GetCompaction())
	}
	encoded, err := os.ReadFile(outcomePath)
	require.NoError(t, err)
	outcome := new(extensionpb.CompactionOutcome)
	require.NoError(t, protojson.Unmarshal(encoded, outcome))
	require.Contains(t, outcome.GetError(), boundaryCause)
	require.Nil(t, outcome.GetCommitted())
	require.NotNil(t, outcome.GetState())
	lastEncoded, err := os.ReadFile(filepath.Join(os.Getenv(manualRequestInputsEnvironment), lastInputFile))
	require.NoError(t, err)
	lastInput := new(extensionpb.CompactionRequestInvocation)
	require.NoError(t, protojson.Unmarshal(lastEncoded, lastInput))
	require.Nil(t, lastInput.GetCurrentResult())
	require.True(t, proto.Equal(lastInput.GetOriginal(), outcome.GetState().GetOriginalRequest()))
	require.True(t, proto.Equal(lastInput.GetCurrent(), outcome.GetState().GetCurrentRequest()))
	require.Nil(t, outcome.GetState().GetResult(), "failure observers must receive the preceding absent result")
	fixture.closeOwner(t)
}
