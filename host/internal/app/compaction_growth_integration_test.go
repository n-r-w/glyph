//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestRecoveryRetainsCommitAndRejectsRealObserverContextGrowth checks public overflow settlement after observer growth.
func TestRecoveryRetainsCommitAndRejectsRealObserverContextGrowth(t *testing.T) {
	// Arrange an ordinary conversation and an overflow owner whose success observer appends visible context.
	outgoingPath := filepath.Join(t.TempDir(), "overflow-outgoing.json")
	outcomePath := filepath.Join(t.TempDir(), "overflow-outcome.json")
	t.Setenv(conversationProviderOverflowEnvironment, manualFixtureEnabled)
	t.Setenv(automaticFixtureModeEnvironment, automaticFixtureRejectFinalMode)
	t.Setenv(automaticFixtureTargetEnvironment, "unsubmitted automatic target")
	t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
	t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
	t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
	directory := buildManualPolicyFixture(t)
	settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 4000")
	settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 500")
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("growth-original", manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest("growth-retained", manualRetainedText))

	// Act through public submission, provider overflow, durable replacement and the owner's final rejection.
	require.NoError(t, fixture.stream.Send(userRequest("growth-overflow", manualNextText)))
	terminal := receiveAutomaticTerminal(t, fixture, "growth-overflow")

	// Assert no replacement attempt follows the failure and both provider and final-input sources survive.
	failed := terminal.GetEvent().GetFailed().GetFailure()
	require.NotNil(t, failed)
	require.Equal(t, "COMPACTION_FAILED", failed.GetCode())
	require.Contains(t, failed.GetMessage(), "provider context limit source")
	require.Contains(t, failed.GetMessage(), automaticFixtureFinalCause)
	require.Len(t, bodies.snapshot(), 3)
	encoded, err := os.ReadFile(outgoingPath)
	require.NoError(t, err)
	outgoing := new(extensionpb.CompactionOutgoingInput)
	require.NoError(t, protojson.Unmarshal(encoded, outgoing))
	require.Contains(t, string(encoded), "observer growth")
	require.NotContains(t, string(encoded), manualOriginalText)
	outcomeBytes, err := os.ReadFile(outcomePath)
	require.NoError(t, err)
	outcome := new(extensionpb.CompactionOutcome)
	require.NoError(t, protojson.Unmarshal(outcomeBytes, outcome))
	require.NotNil(t, outcome.GetCommitted())
	require.Equal(
		t,
		extensionpb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW,
		outcome.GetState().GetOriginalRequest().GetTrigger(),
	)
	require.True(t, outcome.GetState().GetOriginalRequest().GetRetryIntent())
	stored := sendProgrammaticOperation(t, fixture, "growth-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	markers := 0
	originalFound := false
	appendText := ""
	for _, entry := range stored {
		if entry.GetCompaction() != nil {
			markers++
			require.Equal(t, outcome.GetCommitted().GetId(), entry.GetId())
		}
		for _, content := range entry.GetUser().GetContent() {
			originalFound = originalFound || content.GetText() == manualOriginalText
		}
		if text := entry.GetExtensionMessage().GetText(); strings.HasPrefix(text, automaticFixtureObserverText) {
			appendText = text
		}
	}
	require.Equal(t, 1, markers)
	require.True(t, originalFound)
	require.Contains(t, appendText, "observer growth")
	require.Greater(t, int64(len(appendText)), outgoing.GetModel().GetContextWindow()*4)
}
