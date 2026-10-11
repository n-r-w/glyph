//go:build integration

// Package app assembles the host application and its public integration fixtures.
package app

// manualSelectionRejectionGuidance selects a handled nested selection rejection.
const manualSelectionRejectionGuidance = "handled-selection-rejection"

// manualSelectionRejectionObserverFailureGuidance selects observer failure after a handled selection rejection.
const manualSelectionRejectionObserverFailureGuidance = "handled-selection-rejection-observer-failure"

// manualSelectionPreparationCause identifies the complete underlying catalog rejection.
const manualSelectionPreparationCause = "provider catalog selection failed: not_found"

// manualTerminalScenario contains one caller-observable policy outcome.
type manualTerminalScenario struct {
	// name identifies the Programmatic subtest.
	name string
	// guidance selects the reference policy behavior.
	guidance string
	// cause identifies the acquired failure text.
	cause string
	// committed reports a durable receipt.
	committed bool
	// canceled reports explicit policy cancellation.
	canceled bool
}

// newManualTerminalScenarios supplies the outcomes common to all public callers.
func newManualTerminalScenarios() []manualTerminalScenario {
	// manualFixtureFailAfterCommit selects policy failure after durable replacement.
	const manualFixtureFailAfterCommit = "fail-after-commit"

	// manualFixtureChangeBranchBeforeCommit selects a branch change before replacement.
	const manualFixtureChangeBranchBeforeCommit = "change-branch-before-commit"

	return []manualTerminalScenario{
		{
			name: "canceled", guidance: manualFixtureCancelBeforeCommit,
			cause: "", committed: false, canceled: true,
		},
		{
			name: "stale branch", guidance: manualFixtureChangeBranchBeforeCommit,
			cause:     "active branch changed before compaction commit",
			committed: false, canceled: false,
		},
		{
			name: "durable failure", guidance: manualFixtureFailAfterCommit,
			cause:     "reference policy failure after durable replacement: original producer cause",
			committed: true, canceled: false,
		},
		{
			name: "observer failure", guidance: manualFixtureObserverFailure,
			cause: "original observer cause", committed: true, canceled: false,
		},
		{
			name: "handled selection rejection observer success", guidance: manualSelectionRejectionGuidance,
			cause: "", committed: true, canceled: false,
		},
		{
			name:      "handled selection rejection observer failure",
			guidance:  manualSelectionRejectionObserverFailureGuidance,
			cause:     manualSelectionPreparationCause,
			committed: true,
			canceled:  false,
		},
		{
			name: "lost policy state", guidance: manualFixtureLoseStateAfterCommit,
			cause: "extension runtime unavailable", committed: true, canceled: false,
		},
		{
			name: "invalid diagnostic", guidance: manualFixtureInvalidStateAfterCommit,
			cause: "original policy failure cause", committed: true, canceled: false,
		},
	}
}

// newManualUnavailableObserverScenario supplies the shared-process loss outcome.
func newManualUnavailableObserverScenario() manualTerminalScenario {
	return manualTerminalScenario{
		name: "observer unavailable", guidance: manualFixtureLoseSharedOwnerAfterCommit,
		cause: "extension runtime unavailable", committed: true, canceled: false,
	}
}

// newManualAbsentOwnerScenario supplies explicit owner absence.
func newManualAbsentOwnerScenario() manualTerminalScenario {
	return manualTerminalScenario{
		name:      "absent",
		guidance:  manualNoOwnerGuidance,
		cause:     "no compaction policy owner is registered",
		committed: false,
		canceled:  false,
	}
}
