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
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestStandardAutomaticCompaction checks automatic compaction of growing conversations.
func TestStandardAutomaticCompaction(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		usage   string
		large   bool
		change  string
		compact bool
	}{
		{"missing-usage", "", true, "", true},
		{"reported-usage-preserves", "low", true, "", false},
		{"reported-usage-compacts", "high", false, "", true},
		{"model-change", "high", false, "model", false},
		{"reasoning-change", "high", false, "reasoning", false},
		{"restart-fallback", "low", true, "restart", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange the bundled compaction extension with a captured 7,000-token input budget.
			t.Setenv(standardRetainedEnvironment, "1")
			directory := buildStandardCompaction(t)
			settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 8000")
			settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
			settings = strings.Replace(settings, "defaultModel: gpt-active", "defaultModel: gpt-test", 1)
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			response := finalResponseSSE
			if scenario.usage != "" {
				response = conversationResponseSSE
			}
			if scenario.usage == "high" {
				response = strings.ReplaceAll(response, `"input_tokens":7`, `"input_tokens":12000`)
			}
			bodies := installStandardConversationTransport(t, response)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			original, recent := manualOriginalText, manualRetainedText
			if scenario.large {
				original += strings.Repeat("a", 16000)
				recent += strings.Repeat("b", 16000)
			}
			completeProgrammaticRequest(t, fixture, userRequest("automatic-original", original))
			switch scenario.change {
			case "model":
				sendProgrammaticOperation(t, fixture, "change-model", func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetSelectModel(programmaticpb.SelectModel_builder{
						ProviderId: new("openai-codex"), ModelId: new("gpt-active"),
					}.Build())
				})
			case "reasoning":
				sendProgrammaticOperation(t, fixture, "change-reasoning", func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetSelectReasoningChoice(programmaticpb.SelectReasoningChoice_builder{
						Choice: new(programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH),
					}.Build())
				})
			case "restart":
				info := sendProgrammaticOperation(
					t,
					fixture,
					"session-info",
					func(request *programmaticpb.OpenRequest) {
						programmaticRequest(request).SetGetSessionInfo(new(programmaticpb.GetSessionInfo))
					},
				).GetSessionInfo().
					GetInfo()
				fixture.closeOwner(t)
				fixture = startProgrammaticFixtureWithExtension(t, paths, directory)
				sendProgrammaticOperation(t, fixture, "resume", func(request *programmaticpb.OpenRequest) {
					programmaticRequest(
						request,
					).SetResumeSession(programmaticpb.ResumeSession_builder{SessionId: new(info.GetId())}.Build())
				})
			}
			defer fixture.closeOwner(t)
			// Act through the public ordinary-conversation entry point.
			completeProgrammaticRequest(t, fixture, userRequest("automatic-recent", recent))
			stored := sendProgrammaticOperation(t, fixture, "stored", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
			}).GetSessionEntries().GetEntries()
			// Assert preservation or one durable replacement and the provider-visible history.
			markers := 0
			summary := ""
			originalFound := false
			for _, entry := range stored {
				if entry.GetCompaction() != nil {
					markers++
					summary = entry.GetCompaction().GetSummary()
				}
				for _, block := range entry.GetUser().GetContent() {
					originalFound = originalFound || block.GetText() == original
				}
			}
			require.True(t, originalFound)
			actual := bodies.snapshot()
			if scenario.compact {
				require.Equal(t, 1, markers)
				require.Len(t, actual, 3)
				require.Contains(t, string(actual[2]), summary)
				require.NotContains(t, string(actual[2]), original)
			} else {
				require.Zero(t, markers)
				require.Len(t, actual, 2)
				require.Contains(t, string(actual[1]), original)
			}
			require.Contains(t, string(actual[len(actual)-1]), recent)
		})
	}
}

// TestStandardAutomaticFinalHistory checks post-compaction conversation history.
func TestStandardAutomaticFinalHistory(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		name := "fits"
		if oversized {
			name = "exceeds-budget"
		}
		t.Run(name, func(t *testing.T) {
			// Arrange an independent public success observer and bundled policy/generator.
			t.Setenv(standardRetainedEnvironment, "1")
			t.Setenv("GLYPH_REFERENCE_POLICY_DISABLED", "1")
			t.Setenv("GLYPH_REFERENCE_SEPARATE_OBSERVER", "1")
			t.Setenv("GLYPH_REFERENCE_SEPARATE_REQUESTS", "1")
			t.Setenv("GLYPH_REFERENCE_SEPARATE_GENERATOR", "1")
			t.Setenv("GLYPH_REFERENCE_SEPARATE_RESULTS", "1")
			t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
			if oversized {
				t.Setenv(automaticFixtureModeEnvironment, automaticFixtureRejectFinalMode)
			}
			outcomePath := filepath.Join(t.TempDir(), "outcome.json")
			t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
			directory := buildStandardCompaction(t)
			reference := buildManualPolicyFixture(t)
			binary, err := os.ReadFile(filepath.Join(reference, manualFixtureReferencePolicy))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(directory, "reference-observer"), binary, 0o700))
			settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 8000")
			settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 1000")
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			bodies := installStandardConversationTransport(t, finalResponseSSE)
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			original := manualOriginalText + strings.Repeat("a", 16000)
			recent := manualRetainedText + strings.Repeat("b", 16000)
			completeProgrammaticRequest(t, fixture, userRequest("final-original", original))
			// Act when automatic replacement is followed by an observer append.
			require.NoError(t, fixture.stream.Send(userRequest("final-recent", recent)))
			terminal := receiveAutomaticTerminal(t, fixture, "final-recent")
			// Assert one generation, a persisted compaction entry, and whether the final history fits the input budget
			// without recomposition.
			encoded, err := os.ReadFile(outcomePath)
			require.NoError(t, err)
			outcome := new(extensionpb.CompactionOutcome)
			require.NoError(t, protojson.Unmarshal(encoded, outcome))
			require.NotNil(t, outcome.GetCommitted())
			require.Equal(
				t,
				extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD,
				outcome.GetState().GetOriginalRequest().GetTrigger(),
			)
			stored := sendProgrammaticOperation(t, fixture, "final-stored", func(request *programmaticpb.OpenRequest) {
				programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
			}).GetSessionEntries().GetEntries()
			markers := 0
			for _, entry := range stored {
				if entry.GetCompaction() != nil {
					markers++
					require.Equal(t, outcome.GetCommitted().GetId(), entry.GetId())
				}
			}
			require.Equal(t, 1, markers)
			actual := bodies.snapshot()
			if oversized {
				require.NotNil(t, terminal.GetEvent().GetFailed())
				require.Contains(
					t,
					terminal.GetEvent().GetFailed().GetFailure().GetMessage(),
					"conversation input budget exceeded",
				)
				require.Contains(
					t,
					terminal.GetEvent().GetFailed().GetFailure().GetMessage(),
					"budget 7000 (context 8000, response reserve 1000)",
				)
				require.Len(t, actual, 2)
			} else {
				require.NotNil(t, terminal.GetEvent().GetCompleted())
				require.Len(t, actual, 3)
				require.Contains(t, string(actual[2]), automaticFixtureObserverText)
				require.Contains(t, string(actual[2]), outcome.GetCommitted().GetCompaction().GetSummary())
				require.NotContains(t, string(actual[2]), original)
			}
		})
	}
}

// installStandardConversationTransport lets tests capture provider requests and choose the response.
func installStandardConversationTransport(t *testing.T, response string) *requestBodies {
	t.Helper()
	bodies := &requestBodies{}
	transport := NewMockHTTPRoundTripper(gomock.NewController(t))
	transport.EXPECT().
		RoundTrip(gomock.Any()).
		AnyTimes().
		DoAndReturn(func(request *http.Request) (*http.Response, error) {
			encoded, err := io.ReadAll(request.Body)
			if err != nil {
				return nil, err
			}
			bodies.append(encoded)
			return &http.Response{
				Status:           "",
				StatusCode:       http.StatusOK,
				Proto:            "",
				ProtoMajor:       0,
				ProtoMinor:       0,
				Header:           make(http.Header),
				Body:             io.NopCloser(bytes.NewBufferString(response)),
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
	return bodies
}
