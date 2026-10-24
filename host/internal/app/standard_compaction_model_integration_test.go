//go:build integration

package app

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardComposedModelKeepsConversationBudget separates final summary selection from candidate capacity.
func TestStandardComposedModelKeepsConversationBudget(t *testing.T) {
	// standardSummaryModelEnvironment names the environment variable for the request fixture's composed summary model.
	const standardSummaryModelEnvironment = "GLYPH_REFERENCE_REQUEST_SUMMARY_MODEL"

	for _, oversized := range []bool{false, true} {
		name := "fits"
		if oversized {
			name = "exceeds-conversation-budget"
		}
		t.Run(name, func(t *testing.T) {
			// Arrange a real request participant selecting the second configured model.
			t.Setenv(standardRetainedEnvironment, "1")
			t.Setenv(standardSummaryModelEnvironment, configuredRequestModelID)
			directory := buildStandardCompaction(t)
			fixtureDirectory := buildManualPolicyFixture(t)
			binary, err := os.ReadFile(filepath.Join(fixtureDirectory, manualFixtureReferencePolicy))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(directory, standardRequestExecutable), binary, 0o700))
			settings := strings.Replace(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 8000", 1)
			settings = strings.Replace(settings, "maxTokens: 16384", "maxTokens: 1000", 1)
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			bodies := &requestBodies{}
			transport := NewMockHTTPRoundTripper(gomock.NewController(t))
			transport.EXPECT().
				RoundTrip(gomock.Any()).
				AnyTimes().
				DoAndReturn(func(request *http.Request) (*http.Response, error) {
					encoded, readErr := io.ReadAll(request.Body)
					if readErr != nil {
						return nil, readErr
					}
					bodies.append(encoded)
					response := finalResponseSSE
					if bytes.Contains(encoded, []byte(`"model":"`+configuredRequestModelID+`"`)) {
						response = configuredModelResponseSSE
						if oversized {
							response = strings.ReplaceAll(response, configuredResponseText, strings.Repeat("s", 40000))
						}
					}
					return &http.Response{
						Status:           "",
						StatusCode:       http.StatusOK,
						Proto:            "",
						ProtoMajor:       0,
						ProtoMinor:       0,
						Header:           make(http.Header),
						Body:             io.NopCloser(strings.NewReader(response)),
						ContentLength:    0,
						TransferEncoding: nil,
						Close:            false,
						Uncompressed:     false,
						Trailer:          nil,
						Request:          nil,
						TLS:              nil,
					}, nil
				})
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("standard-model-original", manualOriginalText))
			completeProgrammaticRequest(t, fixture, userRequest("standard-model-recent", manualRetainedText))
			// Act through manual composition, clearing supplied content before generation.
			terminal := requestProgrammaticCompaction(t, fixture, "standard-model", manualRequestClearGuidance)
			// Assert the actual configured-model request and the captured conversation budget.
			actual := bodies.snapshot()
			require.Len(t, actual, 3)
			require.Contains(t, string(actual[2]), `"model":"`+configuredRequestModelID+`"`)
			require.Contains(t, string(actual[2]), `"effort":"high"`)
			if oversized {
				require.NotNil(t, terminal.GetFailed())
				require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), "conversation input budget")
			} else {
				require.NotNil(
					t,
					terminal.GetCompleted(),
					"failure: %s",
					terminal.GetFailed().GetFailure().GetMessage(),
				)
				marker := terminal.GetCompleted().GetCompaction().GetCommitted().GetCompaction()
				require.Equal(t, configuredResponseText, marker.GetSummary())
				require.Len(t, marker.GetSource().GetModelRequests().GetRequests(), 1)
				modelRequest := marker.GetSource().GetModelRequests().GetRequests()[0]
				require.Equal(t, configuredRequestModelID, modelRequest.GetModelId())
				require.Equal(
					t,
					programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH,
					modelRequest.GetReasoningChoice(),
				)
				require.Equal(t, int64(11), modelRequest.GetUsage().GetTotalTokens())
			}
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"standard-model-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			_, markers := countManualStoredEntries(stored)
			if oversized {
				require.Zero(t, markers)
			} else {
				require.Equal(t, 1, markers)
			}
			fixture.closeOwner(t)
		})
	}
}
