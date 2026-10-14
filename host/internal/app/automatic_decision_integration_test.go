//go:build integration

package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestAutomaticDecisionRetainsDurableOutcome checks rejected decisions and retained receipts.
func TestAutomaticDecisionRetainsDurableOutcome(t *testing.T) {
	const canceledDiagnosticMode = "canceled-diagnostic"
	const diagnosticCause = "independent policy diagnostic"
	for _, scenario := range []struct {
		mode      string
		committed bool
		cause     string
	}{
		{
			mode: automaticFixtureUnspecifiedMode, committed: false,
			cause: "compaction policy disposition is unspecified or unknown",
		},
		{
			mode: automaticFixtureReplacedWithoutReceiptMode, committed: false,
			cause: "compaction policy finished without replacement or cancellation",
		},
		{
			mode: automaticFixtureCanceledBeforeMode, committed: false,
			cause: "compaction policy canceled automatic preparation",
		},
		{
			mode: automaticFixturePreserveAfterCommitMode, committed: true,
			cause: "compaction policy preserve conflicts with replacement intent or receipt",
		},
		{
			mode: automaticFixtureCanceledAfterMode, committed: true,
			cause: "compaction policy canceled after durable replacement",
		},
		{mode: canceledDiagnosticMode, committed: false, cause: diagnosticCause},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			// Arrange completion reports before starting the public-only policy fixture.
			scratch := t.TempDir()
			outgoingPath := filepath.Join(scratch, "outgoing.json")
			outcomePath := filepath.Join(scratch, "outcome.json")
			t.Setenv(automaticFixtureOutcomeEnvironment, outcomePath)
			t.Setenv(automaticFixtureModeEnvironment, scenario.mode)
			t.Setenv(automaticFixtureTargetEnvironment, automaticFixtureTarget)
			t.Setenv(automaticFixtureOutgoingEnvironment, outgoingPath)
			directory := buildManualPolicyFixture(t)
			paths := testPaths(t, configuredRequestSettings)
			writeProgrammaticCredentials(t, paths)
			transport, bodies := newManualCompactionProviderTransport(t)
			previous := http.DefaultTransport
			http.DefaultTransport = transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
			defer fixture.closeOwner(t)
			completeProgrammaticRequest(t, fixture, userRequest("decision-seed", manualOriginalText))
			// Act through automatic preparation without a manual command or provider failure.
			require.NoError(t, fixture.stream.Send(userRequest("decision-next", automaticFixtureTarget)))
			terminal := receiveAutomaticTerminal(t, fixture, "decision-next").GetEvent()
			// Assert preparation fails before provider dispatch and retains only the actual durable outcome.
			require.NotNil(t, terminal.GetFailed(), automaticUnexpectedTerminalFormat, terminal)
			require.Contains(t, terminal.GetFailed().GetMessage(), scenario.cause)
			if scenario.mode == canceledDiagnosticMode {
				encoded, readErr := os.ReadFile(outcomePath)
				require.NoError(t, readErr)
				outcome := new(extensionpb.CompactionOutcome)
				require.NoError(t, protojson.Unmarshal(encoded, outcome))
				require.True(t, outcome.GetCanceled())
				require.Contains(t, outcome.GetError(), diagnosticCause)
				require.Nil(t, outcome.GetCommitted())
			}
			require.Len(t, bodies.snapshot(), 1)
			_, err := os.Stat(outgoingPath)
			require.True(t, os.IsNotExist(err))
			stored := sendProgrammaticOperation(
				t,
				fixture,
				"decision-stored",
				func(request *programmaticpb.OpenRequest) {
					programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
				},
			).GetSessionEntries().
				GetEntries()
			originals, markers := countManualStoredEntries(stored)
			require.Equal(t, 1, originals)
			if scenario.committed {
				require.Equal(t, 1, markers)
			} else {
				require.Zero(t, markers)
			}
		})
	}
}
