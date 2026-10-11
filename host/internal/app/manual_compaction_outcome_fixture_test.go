//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// configureManualOutcomeFixture selects public outcome capture and observer failure.
func configureManualOutcomeFixture(t *testing.T, guidance string) string {
	// manualFixtureOutcomeJson names the public outcome artifact file.
	const manualFixtureOutcomeJson = "outcome.json"

	// manualFixtureGlyphReferenceSeparateObserver names the separate observer process setting.
	const manualFixtureGlyphReferenceSeparateObserver = "GLYPH_REFERENCE_SEPARATE_OBSERVER"

	// manualFixtureGlyphReferenceOutcome names the outcome artifact path setting.
	const manualFixtureGlyphReferenceOutcome = "GLYPH_REFERENCE_OUTCOME"

	// manualFixtureGlyphReferenceObserverFailure names the observer failure setting.
	const manualFixtureGlyphReferenceObserverFailure = "GLYPH_REFERENCE_OBSERVER_FAILURE"

	t.Helper()
	path := filepath.Join(t.TempDir(), manualFixtureOutcomeJson)
	t.Setenv(manualFixtureGlyphReferenceOutcome, path)
	if guidance == manualFixtureLoseStateAfterCommit {
		t.Setenv(manualFixtureGlyphReferenceSeparateObserver, manualFixtureEnabled)
	}
	if guidance == manualFixtureObserverFailure || guidance == manualFixtureLoseStateAfterCommit ||
		guidance == manualSelectionRejectionObserverFailureGuidance {
		t.Setenv(manualFixtureGlyphReferenceObserverFailure, manualFixtureEnabled)
	}
	if guidance == manualFixtureObserverFailure {
		t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	}
	return path
}

// assertManualOutcome checks acquired diagnostic presence and the independent receipt.
func assertManualOutcome(t *testing.T, path, guidance string, committed bool, terminalError string) {
	t.Helper()
	if guidance == manualFixtureLoseSharedOwnerAfterCommit {
		require.Contains(t, terminalError, "extension runtime unavailable")
		require.Contains(t, terminalError, "issue compaction observer")
		require.Contains(t, terminalError, "rpc error:")
		return
	}
	encoded, err := os.ReadFile(path)
	require.NoError(t, err, "public compaction outcome observer must run")
	outcome := new(extensionpb.CompactionOutcome)
	require.NoError(t, protojson.Unmarshal(encoded, outcome))
	require.Equal(t, committed, outcome.GetCommitted() != nil)
	if guidance == manualFixtureLoseStateAfterCommit || guidance == manualFixtureInvalidStateAfterCommit ||
		guidance == manualFixtureCancelBeforeCommit ||
		guidance == manualNoOwnerGuidance {
		require.Nil(t, outcome.GetState(), "Host must not invent unavailable policy state")
	} else {
		require.NotNil(t, outcome.GetState())
		require.NotNil(t, outcome.GetState().GetOriginalRequest())
		require.NotNil(t, outcome.GetState().GetCurrentRequest())
		require.Equal(t, manualReferenceSummary, outcome.GetState().GetResult().GetSummary())
	}
	if guidance == manualFixtureObserverFailure || guidance == manualFixtureLoseStateAfterCommit ||
		guidance == manualSelectionRejectionObserverFailureGuidance {
		require.Contains(t, terminalError, "original observer cause")
	}
	if guidance == manualFixtureLoseStateAfterCommit {
		require.Contains(t, outcome.GetError(), "extension runtime unavailable")
		require.Contains(t, terminalError, outcome.GetError())
		require.Contains(t, outcome.GetError(), "rpc error:")
	}
	if guidance == manualFixtureInvalidStateAfterCommit {
		require.Contains(t, terminalError, "original policy failure cause")
		require.Contains(t, terminalError, "policy diagnostic original and current requests are required")
	}
	if guidance == manualSelectionRejectionGuidance || guidance == manualSelectionRejectionObserverFailureGuidance {
		require.NotNil(t, outcome.GetCommitted())
		require.Empty(t, outcome.GetCommitted().GetCompaction().GetDetails())
		require.Empty(t, outcome.GetState().GetResult().GetDetails())
		if guidance == manualSelectionRejectionObserverFailureGuidance {
			require.Contains(t, terminalError, manualSelectionPreparationCause)
			require.Contains(t, terminalError, "original observer cause")
		} else {
			require.Empty(t, terminalError)
		}
	}
	if committed {
		require.Equal(t, manualReferenceSummary, outcome.GetCommitted().GetCompaction().GetSummary())
	}
}

// buildManualOutcomePolicyFixture separates loss-prone policy work from public callers and observers.
func buildManualOutcomePolicyFixture(t *testing.T, guidance string) string {
	// manualFixtureReferenceObserver names the independent outcome observer executable.
	const manualFixtureReferenceObserver = "reference-observer"

	// manualFixtureReferenceCaller names the independent Extension caller executable.
	const manualFixtureReferenceCaller = "reference-caller"

	t.Helper()
	directory := buildManualPolicyFixture(t)
	if guidance == manualFixtureLoseStateAfterCommit {
		executable, err := os.ReadFile(filepath.Join(directory, manualFixtureReferencePolicy))
		require.NoError(t, err)
		for _, name := range []string{manualFixtureReferenceObserver, manualFixtureReferenceCaller} {
			require.NoError(t, os.WriteFile(filepath.Join(directory, name), executable, 0o700))
		}
	}
	return directory
}
