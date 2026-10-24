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

// TestReplacementProducerIdentity checks request result ownership through manual compaction.
func (s *manualRequestHandlersSuite) TestReplacementProducerIdentity() {
	for _, source := range []string{"extension", "model", "other-extension"} {
		s.Run(source, func() {
			t := s.T()
			const (
				sourceEnvironment = "GLYPH_REFERENCE_REQUEST_SOURCE"
				forgedProducer    = "request-forged-producer"
				thirdInputFile    = "request-x-third.json"
				terminalInputFile = "request-w-terminal.json"
				lastInputFile     = "request-v-last.json"
			)
			// Arrange a replacement after an accepted request and result composition.
			t.Setenv(sourceEnvironment, source)
			inputs := t.TempDir()
			t.Setenv(manualRequestInputsEnvironment, inputs)
			outcomePath := configureManualOutcomeFixture(t, manualRequestCompositionGuidance)
			directory := buildManualPolicyFixture(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, _ := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("source-original", manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest("source-retained", manualRetainedText))
			before := sendProgrammaticOperation(t, fixture, "source-before", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
			}).GetSessionEntries().GetEntries()
			require.Len(t, before, 4)

			// Act through the public manual operation and inspect session and observer output.
			terminal := requestProgrammaticCompaction(t, fixture, "source-compaction", manualRequestCompositionGuidance)
			after := sendProgrammaticOperation(t, fixture, "source-after", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
			}).GetSessionEntries().GetEntries()
			encoded, err := os.ReadFile(outcomePath)
			require.NoError(t, err)
			outcome := new(extensionpb.CompactionOutcome)
			require.NoError(t, protojson.Unmarshal(encoded, outcome))
			inputEncoded, err := os.ReadFile(filepath.Join(inputs, thirdInputFile))
			require.NoError(t, err)
			input := new(extensionpb.CompactionRequestInvocation)
			require.NoError(t, protojson.Unmarshal(inputEncoded, input))
			require.NotNil(t, input.GetCurrentResult())
			owner := input.GetCurrentResult().GetSource().GetExtensionId()
			require.NotEmpty(t, owner)

			// Assert Host rejects a mismatched producer before the policy applies the replacement or calls later handlers.
			if source == "other-extension" {
				require.NotNil(t, terminal.GetFailed())
				for _, identity := range []string{forgedProducer, owner} {
					require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), identity)
					require.Contains(t, outcome.GetError(), identity)
				}
				require.Nil(t, outcome.GetCommitted())
				require.NoFileExists(t, filepath.Join(inputs, terminalInputFile))
				require.NoFileExists(t, filepath.Join(inputs, lastInputFile))
				require.Len(t, after, len(before))
				for index := range before {
					require.True(t, proto.Equal(before[index], after[index]))
					require.Nil(t, after[index].GetCompaction())
				}
				require.NotNil(t, outcome.GetState())
				require.True(t, proto.Equal(input.GetOriginal(), outcome.GetState().GetOriginalRequest()))
				require.True(t, proto.Equal(input.GetCurrent(), outcome.GetState().GetCurrentRequest()))
				require.True(t, proto.Equal(input.GetCurrentResult(), outcome.GetState().GetResult()))
			} else {
				// Assert accepted producer metadata survives the public and stored projections.
				require.NotNil(
					t,
					terminal.GetCompleted(),
					"failure: %s",
					terminal.GetFailed().GetFailure().GetMessage(),
				)
				marker := terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction()
				require.NotNil(t, marker)
				require.True(t, marker.HasDetails())
				if source == "model" {
					model := marker.GetSource().GetModelRequests().GetRequests()[0]
					require.NotNil(t, model)
					require.Equal(t, "request-result-provider", model.GetProviderId())
					require.Equal(t, "request-result-model", model.GetModelId())
					require.Equal(t, programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH, model.GetReasoningChoice())
					usage := model.GetUsage()
					require.NotNil(t, usage)
					require.Equal(t, int64(4), usage.GetInputTokens())
					require.Equal(t, int64(4), usage.GetOutputTokens())
					require.Equal(t, int64(2), usage.GetCacheReadTokens())
					require.Equal(t, int64(1), usage.GetCacheWriteTokens())
					require.Equal(t, int64(3), usage.GetReasoningTokens())
					require.Equal(t, int64(11), usage.GetTotalTokens())
					require.Empty(t, marker.GetDetails())
				} else {
					require.Equal(t, owner, marker.GetSource().GetExtensionId())
					require.NotEmpty(t, marker.GetDetails())
				}
				_, markerCount := countManualStoredEntries(after)
				require.Equal(t, 1, markerCount)
				for _, entry := range after {
					if stored := entry.GetCompaction(); stored != nil {
						require.True(t, proto.Equal(marker, stored))
					}
				}
			}
			fixture.closeOwner(t)
		})
	}
}
