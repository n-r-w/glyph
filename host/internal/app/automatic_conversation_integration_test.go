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

const (
	// automaticFixtureReplaceMode selects replacement in the reference fixture.
	automaticFixtureReplaceMode = "replace"
	// automaticFixtureRejectFinalMode selects outgoing budget rejection.
	automaticFixtureRejectFinalMode = "reject-final"
	// automaticFixtureUnspecifiedMode selects an unspecified completion decision.
	automaticFixtureUnspecifiedMode = "unspecified"
	// automaticFixtureReplacedWithoutReceiptMode selects replacement acknowledgement without persistence.
	automaticFixtureReplacedWithoutReceiptMode = "replaced-without-receipt"
	// automaticFixtureCanceledBeforeMode selects cancellation before mutation.
	automaticFixtureCanceledBeforeMode = "canceled-before"
	// automaticFixturePreserveAfterCommitMode selects preservation acknowledgement after mutation.
	automaticFixturePreserveAfterCommitMode = "preserve-after-commit"
	// automaticFixtureCanceledAfterMode selects cancellation after mutation.
	automaticFixtureCanceledAfterMode = "canceled-after"
	// automaticFixtureAppendFinalMode selects visible growth during final validation.
	automaticFixtureAppendFinalMode = "append-final"
	// automaticFixtureWaitBeforeCommitMode selects configured work before persistence.
	automaticFixtureWaitBeforeCommitMode = "wait-for-caller-cancel"
	// automaticFixtureHoldFinalMode selects configured work during final validation.
	automaticFixtureHoldFinalMode = "hold-final"
	// automaticFixtureLoseAfterCommitMode selects process exit before receipt acknowledgement.
	automaticFixtureLoseAfterCommitMode = "lose-state-after-commit"
	// automaticFixturePreserveMode selects ordinary history preservation.
	automaticFixturePreserveMode = "preserve"
	// automaticFixtureSplitBoundaryMode selects a structurally invalid replacement proposal.
	automaticFixtureSplitBoundaryMode = "split-boundary"
	// automaticWireEffortNone is the provider's disabled reasoning effort value.
	automaticWireEffortNone = "none"
	// automaticUnexpectedTerminalFormat describes an unexpected public operation terminal.
	automaticUnexpectedTerminalFormat = "unexpected terminal: %v"
	// automaticFixtureModeEnvironment names the environment variable for the reference automatic preparation mode.
	automaticFixtureModeEnvironment = "GLYPH_REFERENCE_AUTOMATIC_MODE"
	// automaticFixtureTargetEnvironment names the environment variable that the reference fixture uses
	// to select a user request as the trigger for context replacement.
	automaticFixtureTargetEnvironment = "GLYPH_REFERENCE_AUTOMATIC_TARGET"
	// automaticFixtureOutgoingEnvironment identifies the final input report.
	automaticFixtureOutgoingEnvironment = "GLYPH_REFERENCE_AUTOMATIC_OUTGOING"
	// automaticFixtureAppendEnvironment names the environment variable for a visible success-observer append.
	automaticFixtureAppendEnvironment = "GLYPH_REFERENCE_AUTOMATIC_APPEND"
	// automaticFixtureOutcomeEnvironment identifies the captured compaction outcome report.
	automaticFixtureOutcomeEnvironment = "GLYPH_REFERENCE_OUTCOME"
	// automaticFixtureTarget is the user request preserved by replacement.
	automaticFixtureTarget = "automatic next request"
	// automaticFixtureSummary is the reference extension's supplied replacement.
	automaticFixtureSummary = "Reference policy replacement summary."
	// automaticFixtureObserverText is visible session growth from the success observer.
	automaticFixtureObserverText = "automatic observer append"
	// automaticFixtureFinalCause is the declared policy rejection cause.
	automaticFixtureFinalCause = "reference policy rejected actual outgoing input: " +
		"observer growth exceeds captured conversation budget"
)

// TestAutomaticConversationPreparation checks public replacement and validation of outgoing input
// after success notifications.
func TestAutomaticConversationPreparation(t *testing.T) {
	for _, mode := range []string{automaticFixtureReplaceMode, automaticFixtureRejectFinalMode} {
		t.Run(mode, func(t *testing.T) {
			// Arrange fixture reports before starting the real Host, SDK and session wiring.
			scratch := t.TempDir()
			outgoingPath := filepath.Join(scratch, "outgoing.json")
			outcomePath := filepath.Join(scratch, "outcome.json")
			t.Setenv(automaticFixtureModeEnvironment, mode)
			t.Setenv(automaticFixtureTargetEnvironment, automaticFixtureTarget)
			t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
			t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
			t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
			directory := buildManualPolicyFixture(t)
			settings := configuredRequestSettings
			if mode == automaticFixtureRejectFinalMode {
				settings = strings.ReplaceAll(settings, "contextWindow: 131072", "contextWindow: 4000")
				settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 500")
			}
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			completeProgrammaticRequest(t, fixture, userRequest("automatic-seed", manualOriginalText))
			// Act through ordinary conversation submission, without a manual compaction command.
			require.NoError(t, fixture.stream.Send(userRequest("automatic-next", automaticFixtureTarget)))
			terminal := receiveAutomaticTerminal(t, fixture, "automatic-next")
			// Assert that after durable replacement, Host skips dispatch when the policy owner rejects the final input.
			if mode == automaticFixtureRejectFinalMode {
				require.NotNil(
					t,
					terminal.GetEvent().GetFailed(),
					"terminal completion: %v",
					terminal.GetEvent().GetCompleted(),
				)
				require.Len(t, bodies.snapshot(), 1)
			}
			// Assert that the policy owner receives the committed summary, suffix and visible success-observer append.
			encoded, err := os.ReadFile(outgoingPath)
			require.NoError(t, err)
			outgoing := new(extensionpb.CompactionOutgoingInput)
			require.NoError(t, protojson.Unmarshal(encoded, outgoing))
			require.Contains(t, string(encoded), automaticFixtureSummary)
			require.Contains(t, string(encoded), automaticFixtureTarget)
			require.Contains(t, string(encoded), automaticFixtureObserverText)
			require.NotContains(t, string(encoded), manualOriginalText)
			outcomeBytes, err := os.ReadFile(outcomePath)
			require.NoError(t, err)
			outcome := new(extensionpb.CompactionOutcome)
			require.NoError(t, protojson.Unmarshal(outcomeBytes, outcome))
			require.NotNil(t, outcome.GetCommitted())
			require.Equal(
				t,
				extensionpb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD,
				outcome.GetState().GetOriginalRequest().GetTrigger(),
			)
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"automatic-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
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
				if text := entry.GetExtensionMessage().
					GetText(); strings.HasPrefix(
					text,
					automaticFixtureObserverText,
				) {
					appendText = text
				}
			}
			require.Equal(t, 1, markers)
			require.True(t, originalFound)
			require.NotEmpty(t, appendText)
			if mode == automaticFixtureRejectFinalMode {
				require.Greater(t, int64(len(appendText)), outgoing.GetModel().GetContextWindow()*4)
			} else {
				require.Equal(t, automaticFixtureObserverText, appendText)
			}
			actualBodies := bodies.snapshot()
			if mode == automaticFixtureRejectFinalMode {
				require.NotNil(t, terminal.GetEvent().GetFailed())
				require.Contains(t, terminal.GetEvent().GetFailed().GetMessage(), automaticFixtureFinalCause)
				require.Contains(t, terminal.GetEvent().GetFailed().GetMessage(), "budget=3500")
				require.Len(t, actualBodies, 1)
			} else {
				require.NotNil(t, terminal.GetEvent().GetCompleted())
				require.Len(t, actualBodies, 2)
				require.Contains(t, string(actualBodies[1]), automaticFixtureSummary)
				require.Contains(t, string(actualBodies[1]), automaticFixtureTarget)
				require.Contains(t, string(actualBodies[1]), automaticFixtureObserverText)
				require.NotContains(t, string(actualBodies[1]), manualOriginalText)
			}
		})
	}
}

// TestAbsentOwnerDispatchesOversizedHistory checks the public optional-owner path without Host threshold policy.
func TestAbsentOwnerDispatchesOversizedHistory(t *testing.T) {
	// Arrange a small configured capacity and an oversized conversation request.
	t.Setenv(conversationPolicyDisabledEnvironment, manualFixtureEnabled)
	settings := strings.ReplaceAll(configuredRequestSettings, "contextWindow: 131072", "contextWindow: 4000")
	settings = strings.ReplaceAll(settings, "maxTokens: 16384", "maxTokens: 500")
	directory := buildManualPolicyFixture(t)
	paths := testPaths(t, settings)
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	text := strings.Repeat("oversized ordinary input ", 3000)
	// Act through ordinary public submission without a policy owner.
	completeProgrammaticRequest(t, fixture, userRequest("oversized-ordinary", text))
	// Assert the provider receives the complete ordinary input with no replacement.
	actual := bodies.snapshot()
	require.Len(t, actual, 1)
	require.Contains(t, string(actual[0]), text)
	stored := sendProgrammaticOperation(t, fixture, "oversized-stored", func(request *programmaticpb.OpenRequest) {
		programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
	}).GetSessionEntries().GetEntries()
	for _, entry := range stored {
		require.Nil(t, entry.GetCompaction())
	}
}

// receiveAutomaticTerminal provides the submitted conversation's terminal operation event.
func receiveAutomaticTerminal(
	t *testing.T,
	fixture *programmaticFixture,
	operationID string,
) *programmaticpb.OpenResponse {
	t.Helper()
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		event := response.GetEvent()
		if response.GetOperationId() == operationID &&
			(event.GetCompleted() != nil || event.GetFailed() != nil || event.GetCanceled() != nil) {
			return response
		}
	}
}
