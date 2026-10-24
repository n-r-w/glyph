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

// TestRejectedCandidatesRemainUninstalled checks combined and direct candidates before acceptance.
func (s *manualResultHandlersSuite) TestRejectedCandidatesRemainUninstalled() {
	for _, mode := range []string{"request", "direct"} {
		s.Run(mode, func() {
			t := s.T()
			// Arrange a rejected new candidate with captured original state.
			t.Setenv("GLYPH_REFERENCE_CANDIDATE_REJECTION", mode)
			inputs := t.TempDir()
			t.Setenv(manualRequestInputsEnvironment, inputs)
			guidance := ""
			if mode == "request" {
				guidance = manualRequestCompositionGuidance
			}
			outcomePath := configureManualOutcomeFixture(t, guidance)
			directory := buildManualPolicyFixture(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, _ := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("candidate-original", manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest("candidate-retained", manualRetainedText))
			before := sendProgrammaticOperation(
				t,
				fixture,
				"candidate-before",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			// Act through public compaction and its terminal observer.
			terminal := requestProgrammaticCompaction(t, fixture, "candidate-compaction", guidance)
			after := sendProgrammaticOperation(
				t,
				fixture,
				"candidate-after",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			// Assert atomic rejection of policy state and durable state.
			require.NotNil(t, terminal.GetFailed())
			require.Contains(
				t,
				terminal.GetFailed().GetFailure().GetMessage(),
				"compaction boundary is outside its branch",
			)
			require.Len(t, after, len(before))
			for index := range before {
				require.True(t, proto.Equal(before[index], after[index]))
			}
			encoded, err := os.ReadFile(outcomePath)
			require.NoError(t, err)
			outcome := new(extensionpb.CompactionOutcome)
			require.NoError(t, protojson.Unmarshal(encoded, outcome))
			require.Nil(t, outcome.GetCommitted())
			if mode == "request" {
				require.FileExists(t, filepath.Join(inputs, "request-z-first.json"))
				require.NoFileExists(t, filepath.Join(inputs, "request-y-second.json"))
			}
			require.Nil(t, outcome.GetState().GetResult(), "a rejected candidate must not enter acquired state")
			require.True(
				t,
				proto.Equal(outcome.GetState().GetOriginalRequest(), outcome.GetState().GetCurrentRequest()),
				"combined rejection must retain both preceding values",
			)
			fixture.closeOwner(t)
		})
	}
}
