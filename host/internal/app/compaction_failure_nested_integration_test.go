//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// nestedCompactionReportEnvironment selects the legal selection observer caller.
	nestedCompactionReportEnvironment = "GLYPH_NESTED_COMPACTION_REPORT"
	// nestedCompactionReportName identifies the public SDK outcome report.
	nestedCompactionReportName = "nested-compaction.json"
	// nestedCompactionLaterSuffix identifies the later selected observer signal.
	nestedCompactionLaterSuffix = ".later"
	// nestedCompactionOperationID identifies the parent selection request.
	nestedCompactionOperationID = "nested-compaction-selection"
	// nestedCompactionHealthyOperationID identifies independent reuse of the same policy extension.
	nestedCompactionHealthyOperationID = "nested-compaction-healthy"
	// nestedCompactionStoredOperationID identifies the durable-state query.
	nestedCompactionStoredOperationID = "nested-compaction-stored"
	// nestedCompactionFailureGuidance selects the postcommit policy failure.
	nestedCompactionFailureGuidance = "fail-after-commit"
	// nestedCompactionFailureCause identifies the complete original policy cause.
	nestedCompactionFailureCause = "reference policy failure after durable replacement: original producer cause"
)

// TestConsumedPostcommitCompactionFailsSelection verifies Host retention when a legal observer consumes child failure.
func TestConsumedPostcommitCompactionFailsSelection(t *testing.T) {
	// Arrange history and an ordinary selection observer that consumes its compaction child failure.
	reportPath := filepath.Join(t.TempDir(), nestedCompactionReportName)
	t.Setenv(nestedCompactionReportEnvironment, reportPath)
	outcomePath := configureManualOutcomeFixture(t, nestedCompactionFailureGuidance)
	directory := buildManualOutcomePolicyFixture(t, nestedCompactionFailureGuidance)
	paths := testPaths(t, selectionCompositionSettings())
	writeProgrammaticCredentials(t, paths)
	transport, bodies := newManualCompactionProviderTransport(t)
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureOriginal, manualOriginalText))
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureRetained, manualRetainedText))

	// Act through the public selection entry while the operation gate permits nested manual compaction.
	require.NoError(t, fixture.stream.Send(selectReasoningRequest(
		nestedCompactionOperationID, programmaticpb.ReasoningChoice_REASONING_CHOICE_HIGH,
	)))
	terminals, receiveErrors := receiveManualTerminal(fixture, nestedCompactionOperationID)
	var terminal *programmaticpb.HostEvent
	select {
	case terminal = <-terminals:
	case err := <-receiveErrors:
		require.NoError(t, err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}

	// Assert the child SDK receives failure plus its actual receipt and Host fails the consuming parent.
	encoded, err := os.ReadFile(reportPath)
	require.NoError(t, err)
	var report manualPublicReport
	require.NoError(t, json.Unmarshal(encoded, &report))
	require.True(t, report.Failed, report.Error)
	require.True(t, report.Committed, report.Error)
	require.Equal(t, manualReferenceSummary, report.Summary)
	require.Contains(t, report.Error, nestedCompactionFailureCause)
	require.NotNil(t, terminal.GetFailed())
	require.Contains(t, terminal.GetFailed().GetFailure().GetMessage(), nestedCompactionFailureCause)
	require.NoFileExists(t, reportPath+nestedCompactionLaterSuffix)
	require.Len(t, bodies.snapshot(), 2)
	assertManualOutcome(t, outcomePath, nestedCompactionFailureGuidance, true, report.Error)
	stored := sendProgrammaticOperation(t, fixture, nestedCompactionStoredOperationID,
		func(request *programmaticpb.OpenRequest) {
			programmaticRequest(request).SetGetSessionEntries(new(programmaticpb.GetSessionEntries))
		}).GetSessionEntries().GetEntries()
	originals, markers := countManualStoredEntries(stored)
	require.Equal(t, 1, originals)
	require.Equal(t, 1, markers)
	completeProgrammaticRequest(t, fixture, userRequest(manualFixtureNext, manualNextText))
	require.Len(t, bodies.snapshot(), 3)
	healthy := requestProgrammaticCompaction(t, fixture, nestedCompactionHealthyOperationID,
		manualFixturePreserveTheRecentUserTurn)
	require.NotNil(
		t,
		healthy.GetCompleted().GetCompaction().GetCommitted(),
		healthy.GetFailed().GetFailure().GetMessage(),
	)
}
