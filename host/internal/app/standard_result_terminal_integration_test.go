//go:build integration

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Bundled result terminal modes exercise the reference participants without reference policy ownership.
const (
	// standardResultMixedMode selects Cancel combined with a result disposition.
	standardResultMixedMode = "mixed"
	// standardResultMissingMode selects Replace without a result payload.
	standardResultMissingMode = "missing"
	// standardResultUnexpectedMode selects Preserve with an unexpected result payload.
	standardResultUnexpectedMode = "unexpected"
	// standardResultBoundaryMode selects a replacement boundary outside the captured branch.
	standardResultBoundaryMode = "boundary"
	// standardResultFailureMode selects an ordinary participant failure.
	standardResultFailureMode = "failure"
	// standardResultClassifiedMode selects a participant error joining cancellation and its source cause.
	standardResultClassifiedMode = "classified"
	// standardResultFailureCause identifies the participant's original failure text in public diagnostics.
	standardResultFailureCause = "result participant source failure"
)

// TestStandardResultTerminalActions observes rejected updates and participant errors under bundled ownership.
func TestStandardResultTerminalActions(t *testing.T) {
	for _, scenario := range []struct {
		mode     string
		causes   []string
		canceled bool
	}{
		{mode: resultCancelMode, causes: nil, canceled: true},
		{mode: standardResultMixedMode, causes: []string{"cancellation must be exclusive"}, canceled: false},
		{
			mode:   standardResultMissingMode,
			causes: []string{"compaction result replacement payload does not match disposition"}, canceled: false,
		},
		{
			mode:   standardResultUnexpectedMode,
			causes: []string{"compaction result replacement payload does not match disposition"}, canceled: false,
		},
		{mode: standardResultBoundaryMode, causes: []string{"compaction boundary is outside its branch"}, canceled: false},
		{mode: standardResultFailureMode, causes: []string{standardResultFailureCause}, canceled: false},
		{
			mode:   standardResultClassifiedMode,
			causes: []string{standardResultFailureCause, context.Canceled.Error()}, canceled: false,
		},
	} {
		t.Run(scenario.mode, func(t *testing.T) {
			// Arrange bundled ownership with independently registered reference result handlers and observer.
			t.Setenv(policyDisabledEnvironment, manualFixtureEnabled)
			t.Setenv(separateObserverEnvironment, manualFixtureEnabled)
			t.Setenv(separateRequestsEnvironment, manualFixtureEnabled)
			t.Setenv(separateGeneratorEnvironment, manualFixtureEnabled)
			t.Setenv(separateResultsEnvironment, manualFixtureEnabled)
			t.Setenv(manualResultsEnvironment, manualFixtureEnabled)
			t.Setenv(standardRetainedEnvironment, "1")
			directory := buildStandardCompaction(t)
			reference := buildManualPolicyFixture(t)
			binary, err := os.ReadFile(filepath.Join(reference, manualFixtureReferencePolicy))
			require.NoError(t, err)
			for _, executable := range []string{referenceObserverExecutable, standardResultExecutable} {
				require.NoError(t, os.WriteFile(filepath.Join(directory, executable), binary, 0o700))
			}
			// Act through public manual compaction with a live owning context.
			// Assert accepted diagnostics, complete causes, absent receipt and later artifact, and entry equality.
			runManualResultTerminalScenario(t, directory, scenario.mode, scenario.causes, scenario.canceled)
		})
	}
}
