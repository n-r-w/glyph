//go:build integration

package app

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/proto"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// preparedSource captures the generator's structured conversation data.
type preparedSource struct {
	// Previous contains the preceding summary supplied separately from new messages.
	Previous string
	// Messages contains the newly summarized prepared prefix.
	Messages []preparedSourceMessage
}

// preparedSourceMessage contains observable conversation source data.
type preparedSourceMessage struct {
	// Role identifies the prepared message kind.
	Role string
	// CallID identifies the call associated with a tool result.
	CallID string
	// Failed identifies an error result.
	Failed bool
	// Blocks contains prepared text and tool calls.
	Blocks []struct {
		// Text contains conversation text.
		Text string
		// CallID identifies a tool call.
		CallID string
	}
}

// TestStandardPreparedHistoryPublic checks original-branch boundaries, interrupted results and restoration.
func TestStandardPreparedHistoryPublic(t *testing.T) {
	const installedExecutable = "glyph-compaction"
	for _, scenario := range []string{"summarized-span", "retained-span", "default-installed"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange persisted interleaved results and an interrupted call with the real bundled executable.
			retained := "20"
			t.Setenv(standardRetainedEnvironment, "")
			recent := "recent prepared source" + strings.Repeat("r", 100)
			if scenario == "retained-span" {
				retained = "100"
			}
			if scenario == "default-installed" {
				recent += strings.Repeat("d", 80000)
			} else {
				t.Setenv(standardRetainedEnvironment, retained)
			}
			directory := buildStandardCompaction(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			if scenario == "default-installed" {
				installed := filepath.Join(paths.Directory, "plugins", "extension")
				require.NoError(t, os.MkdirAll(installed, 0o700))
				binary, err := os.ReadFile(filepath.Join(directory, installedExecutable))
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(installed, installedExecutable), binary, 0o700))
				directory = ""
			}
			allBodies, summaries := installPreparedHistoryTransport(t, nil)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("prepared-original", manualOriginalText))
			info := sendProgrammaticOperation(t, fixture, "prepared-info", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionInfo(new(programmaticpb.GetSessionInfo))
			}).GetSessionInfo().GetInfo()
			fixture.closeOwner(t)
			appendPreparedBranchFixture(t, paths, info.GetId(), recent, true)
			fixture = startProgrammaticFixtureWithExtension(t, paths, directory)
			resumePreparedSession(t, fixture, info.GetId())
			originals := preparedStoredEntries(t, fixture)
			// Act through public manual compaction after session restoration.
			first := requestProgrammaticCompaction(t, fixture, "prepared-first", "")
			require.NotNil(t, first.GetCompleted(), "%v", first)
			firstReceipt := first.GetCompleted().GetCompaction().GetCommitted()
			expectedBoundary := "recent-source"
			if scenario == "retained-span" {
				expectedBoundary = "calls"
			}
			require.Equal(t, expectedBoundary, firstReceipt.GetCompaction().GetFirstKeptEntryId())
			// Assert source order, shared synthetic correspondence and preservation of every original record.
			stored := preparedStoredEntries(t, fixture)
			require.Len(t, stored, len(originals)+1)
			for index := range originals {
				require.True(t, proto.Equal(originals[index], stored[index]))
			}
			generated := summaries.snapshot()
			require.Len(t, generated, 1)
			source := decodePreparedSource(t, generated[0])
			require.Empty(t, source.Previous)
			require.NotContains(t, string(generated[0]), "excluded-failed")
			require.NotContains(t, string(generated[0]), "excluded-aborted")
			require.NotContains(t, string(generated[0]), recent)
			if scenario == "retained-span" {
				require.Len(t, source.Messages, 2)
			} else {
				require.Len(t, source.Messages, 6)
				require.Equal(t, "source-a", source.Messages[2].Blocks[0].CallID)
				require.Equal(t, "source-a", source.Messages[3].CallID)
				require.Equal(t, "source-b", source.Messages[4].CallID)
				require.True(t, source.Messages[4].Failed)
				require.Contains(t, source.Messages[4].Blocks[0].Text, "skipped")
				require.Equal(t, "intervening source", source.Messages[5].Blocks[0].Text)
			}
			next := manualNextText + strings.Repeat("n", 100)
			if scenario == "default-installed" {
				next += strings.Repeat("n", 80000)
			}
			completeProgrammaticRequest(t, fixture, userRequest("prepared-next", next))
			outgoing := allBodies.snapshot()
			require.Contains(t, string(outgoing[len(outgoing)-1]), firstReceipt.GetCompaction().GetSummary())
			require.Contains(t, string(outgoing[len(outgoing)-1]), recent)
			if scenario == "retained-span" {
				require.Contains(t, string(outgoing[len(outgoing)-1]), "source-b")
				require.Contains(t, string(outgoing[len(outgoing)-1]), "intervening source")
				fixture.closeOwner(t)
				return
			}
			second := requestProgrammaticCompaction(t, fixture, "prepared-second", "")
			require.NotNil(t, second.GetCompleted(), "%v", second)
			secondReceipt := second.GetCompleted().GetCompaction().GetCommitted()
			savedBranch := preparedStoredEntries(t, fixture)
			generated = summaries.snapshot()
			require.Len(t, generated, 2)
			repeated := decodePreparedSource(t, generated[1])
			require.Equal(t, firstReceipt.GetCompaction().GetSummary(), repeated.Previous)
			require.Equal(t, 1, strings.Count(string(generated[1]), firstReceipt.GetCompaction().GetSummary()))
			require.NotContains(t, string(generated[1]), manualOriginalText)
			require.NotContains(t, string(generated[1]), "stored source")
			fixture.closeOwner(t)
			// Act after another restart, then navigate before and after the replacement markers.
			fixture = startProgrammaticFixtureWithExtension(t, paths, directory)
			resumePreparedSession(t, fixture, info.GetId())
			tree := sendProgrammaticOperation(t, fixture, "prepared-tree", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionTree(new(programmaticpb.GetSessionTree))
			}).GetSessionTree().GetTree()
			require.Equal(t, secondReceipt.GetId(), tree.GetActiveLeafId())
			const restoredRequest = "after restoration"
			completeProgrammaticRequest(t, fixture, userRequest("prepared-restored-next", restoredRequest))
			outgoing = allBodies.snapshot()
			restored := string(outgoing[len(outgoing)-1])
			require.Contains(t, restored, secondReceipt.GetCompaction().GetSummary())
			require.NotContains(t, restored, firstReceipt.GetCompaction().GetSummary())
			require.NotContains(t, restored, manualOriginalText)
			assertPreparedRestoredSuffix(t, outgoing[len(outgoing)-1], savedBranch, secondReceipt, restoredRequest)
			navigatePreparedSession(t, fixture, "recent-source")
			completeProgrammaticRequest(t, fixture, userRequest("prepared-before-marker", "before marker"))
			outgoing = allBodies.snapshot()
			before := string(outgoing[len(outgoing)-1])
			require.Contains(t, before, manualOriginalText)
			require.Contains(t, before, "source-b")
			require.NotContains(t, before, secondReceipt.GetCompaction().GetSummary())
			navigatePreparedSession(t, fixture, secondReceipt.GetId())
			completeProgrammaticRequest(t, fixture, userRequest("prepared-after-marker", next))
			outgoing = allBodies.snapshot()
			after := string(outgoing[len(outgoing)-1])
			require.Contains(t, after, secondReceipt.GetCompaction().GetSummary())
			require.NotContains(t, after, manualOriginalText)
			third := requestProgrammaticCompaction(t, fixture, "prepared-third", "")
			require.NotNil(t, third.GetCompleted(), "%v", third)
			generated = summaries.snapshot()
			require.Len(t, generated, 3)
			require.Equal(t, secondReceipt.GetCompaction().GetSummary(), decodePreparedSource(t, generated[2]).Previous)
			require.Equal(t, 1, strings.Count(string(generated[2]), secondReceipt.GetCompaction().GetSummary()))
			require.NotContains(t, string(generated[2]), manualOriginalText)
			fixture.closeOwner(t)
		})
	}
}

// installPreparedHistoryTransport captures real provider requests and returns distinct summary responses.
func installPreparedHistoryTransport(t *testing.T, overflow <-chan struct{}) (*requestBodies, *requestBodies) {
	const summaryFormat = "prepared-summary-%d"
	const sourceMessagesField = `\"Messages\"`
	t.Helper()
	all, summaries := &requestBodies{}, &requestBodies{}
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			all.append(body)
			if overflow != nil {
				select {
				case <-overflow:
					response := runtimeFailureHTTPResponse(
						`{"error":{"message":"prepared overflow source","type":"invalid_request_error",` +
							`"code":"context_length_exceeded"}}`,
					)
					response.StatusCode = http.StatusBadRequest
					return response, nil
				default:
				}
			}
			response := finalResponseSSE
			if strings.Contains(string(body), sourceMessagesField) {
				position := summaries.append(body)
				response = strings.ReplaceAll(
					response,
					"Request complete.",
					fmt.Sprintf(summaryFormat, position),
				)
			}
			return runtimeFailureHTTPResponse(response), nil
		})
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	return all, summaries
}

// decodePreparedSource extracts structured conversation data from one summary provider request.
func decodePreparedSource(t *testing.T, body []byte) preparedSource {
	t.Helper()
	var request struct {
		Input []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"input"`
	}
	require.NoError(t, json.Unmarshal(body, &request))
	require.NotEmpty(t, request.Input)
	require.NotEmpty(t, request.Input[0].Content)
	var source preparedSource
	require.NoError(t, json.Unmarshal([]byte(request.Input[0].Content[0].Text), &source))
	return source
}

// resumePreparedSession selects a persisted session through Programmatic Control.
func resumePreparedSession(t *testing.T, fixture *programmaticFixture, id string) {
	t.Helper()
	sendProgrammaticOperation(t, fixture, "prepared-resume", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetResumeSession(programmaticpb.ResumeSession_builder{SessionId: new(id)}.Build())
	})
}

// navigatePreparedSession selects an original branch position without creating a branch summary.
func navigatePreparedSession(t *testing.T, fixture *programmaticFixture, id string) {
	t.Helper()
	sendProgrammaticOperation(t, fixture, "prepared-navigate", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(
			request,
		).SetNavigateSessionTree(programmaticpb.NavigateSessionTree_builder{
			TargetEntryId: new(
				id,
			),
			SummaryMode: new(programmaticpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
			CustomFocus: nil,
		}.Build())
	})
}

// preparedStoredEntries reads the original journal through Programmatic Control.
func preparedStoredEntries(t *testing.T, fixture *programmaticFixture) []*programmaticpb.SessionEntry {
	t.Helper()
	return sendProgrammaticOperation(t, fixture, "prepared-stored-history", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
}

// TestStandardPreparedRetention excludes failed and aborted responses from every trigger's retained budget.
func TestStandardPreparedRetention(t *testing.T) {
	for _, purpose := range []string{"manual", "automatic", "overflow", "default"} {
		t.Run(purpose, func(t *testing.T) {
			// Arrange enough usable history to require an older complete group beyond excluded responses.
			t.Setenv(standardRetainedEnvironment, "600")
			usable := strings.Repeat("u", 4000)
			if purpose == "default" {
				t.Setenv(standardRetainedEnvironment, "")
				usable = strings.Repeat("u", 80000)
			}
			directory := buildStandardCompaction(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			overflow := make(chan struct{}, 1)
			_, summaries := installPreparedHistoryTransport(t, overflow)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("retention-oldest", strings.Repeat("o", 4000)))
			completeProgrammaticRequest(t, fixture, userRequest("retention-usable", usable))
			info := sendProgrammaticOperation(t, fixture, "retention-info", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionInfo(new(programmaticpb.GetSessionInfo))
			}).GetSessionInfo().GetInfo()
			original := preparedStoredEntries(t, fixture)
			fixture.closeOwner(t)
			appendPreparedBranchFixture(t, paths, info.GetId(), "latest prepared request", true)
			if purpose == "automatic" {
				settings := strings.ReplaceAll(
					configuredRequestSettings,
					"contextWindow: 131072",
					"contextWindow: 3000",
				)
				settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
				require.NoError(t, os.WriteFile(paths.SettingsFile, []byte(settings), 0o600))
			}
			fixture = startProgrammaticFixtureWithExtension(t, paths, directory)
			resumePreparedSession(t, fixture, info.GetId())
			// Act through the selected public compaction trigger.
			if purpose == "manual" || purpose == "default" {
				terminal := requestProgrammaticCompaction(t, fixture, "retention-manual", "")
				require.NotNil(t, terminal.GetCompleted(), "%v", terminal)
			} else {
				if purpose == "overflow" {
					overflow <- struct{}{}
				}
				completeProgrammaticRequest(t, fixture, userRequest("retention-trigger", "continue prepared history"))
			}
			// Assert the selected boundary retains enough usable content and generation sees only the old prepared prefix.
			markers := 0
			for _, entry := range preparedStoredEntries(t, fixture) {
				if marker := entry.GetCompaction(); marker != nil {
					markers++
					require.Equal(t, original[2].GetId(), marker.GetFirstKeptEntryId())
				}
			}
			require.Equal(t, 1, markers)
			generated := summaries.snapshot()
			require.Len(t, generated, 1)
			source := decodePreparedSource(t, generated[0])
			require.Len(t, source.Messages, 2)
			require.Equal(t, strings.Repeat("o", 4000), source.Messages[0].Blocks[0].Text)
			require.NotContains(t, string(generated[0]), "excluded-failed")
			require.NotContains(t, string(generated[0]), "excluded-aborted")
			fixture.closeOwner(t)
		})
	}
}

// TestStandardPreparedTransformations independently checks reordered source and synthesized skipped source.
func TestStandardPreparedTransformations(t *testing.T) {
	for _, scenario := range []string{"ordered-results", "skipped-result"} {
		t.Run(scenario, func(t *testing.T) {
			// Arrange an interrupted batch with an interleaved message and no excluded responses.
			t.Setenv(standardRetainedEnvironment, "1")
			directory := buildStandardCompaction(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			_, summaries := installPreparedHistoryTransport(t, nil)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			completeProgrammaticRequest(t, fixture, userRequest("transformation-original", manualOriginalText))
			info := sendProgrammaticOperation(
				t,
				fixture,
				"transformation-info",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionInfo(new(programmaticpb.GetSessionInfo))
				},
			).GetSessionInfo().
				GetInfo()
			fixture.closeOwner(t)
			appendPreparedBranchFixture(t, paths, info.GetId(), "transformation recent", false)
			fixture = startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			resumePreparedSession(t, fixture, info.GetId())
			// Act through public manual compaction and inspect the generator's structured source.
			terminal := requestProgrammaticCompaction(t, fixture, "transformation-compact", "")
			require.NotNil(t, terminal.GetCompleted(), "%v", terminal)
			generated := summaries.snapshot()
			require.Len(t, generated, 1)
			source := decodePreparedSource(t, generated[0])
			// Assert each transformation independently, so an exclusion failure cannot mask either regression.
			actual, intervening, skipped := -1, -1, -1
			for index, message := range source.Messages {
				if message.CallID == "source-a" {
					actual = index
				}
				if message.CallID == "source-b" {
					skipped = index
				}
				for _, block := range message.Blocks {
					if block.Text == "intervening source" {
						intervening = index
					}
				}
			}
			if scenario == "ordered-results" {
				require.NotEqual(t, -1, actual)
				require.NotEqual(t, -1, intervening)
				require.Less(t, actual, intervening)
			} else {
				require.NotEqual(t, -1, skipped)
				require.True(t, source.Messages[skipped].Failed)
				require.Contains(t, source.Messages[skipped].Blocks[0].Text, "skipped")
			}
		})
	}
}
