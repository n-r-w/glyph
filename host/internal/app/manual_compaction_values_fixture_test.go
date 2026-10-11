//go:build integration

// Package app assembles the host application and its public integration fixtures.
package app

const (
	// manualFixtureEnabled enables reference fixture modes.
	manualFixtureEnabled = "1"
	// manualFixtureGlyphManualCancelFailure names the UI cancellation failure setting.
	manualFixtureGlyphManualCancelFailure = "GLYPH_MANUAL_CANCEL_FAILURE"
	// manualFixtureGlyphManualCancelSocket names the UI cancellation coordination socket setting.
	manualFixtureGlyphManualCancelSocket = "GLYPH_MANUAL_CANCEL_SOCKET"
	// manualFixtureGlyphReferenceCallerSocket names the ordinary Extension caller socket setting.
	manualFixtureGlyphReferenceCallerSocket = "GLYPH_REFERENCE_CALLER_SOCKET"
	// manualFixtureGlyphReferenceObserverModel names the observer model-work setting.
	manualFixtureGlyphReferenceObserverModel = "GLYPH_REFERENCE_OBSERVER_MODEL"
	// manualFixtureGlyphReferencePolicyDisabled names the policy registration disable setting.
	manualFixtureGlyphReferencePolicyDisabled = "GLYPH_REFERENCE_POLICY_DISABLED"
	// manualFixtureGlyphReferencePolicyInput names the captured policy input path setting.
	manualFixtureGlyphReferencePolicyInput = "GLYPH_REFERENCE_POLICY_INPUT"
	// manualFixtureCallerSock names the Extension caller coordination socket file.
	manualFixtureCallerSock = "caller.sock"
	// manualFixtureCancelBeforeCommit selects policy cancellation before replacement.
	manualFixtureCancelBeforeCommit = "cancel-before-commit"
	// manualFixtureCancelCommand identifies the public cancellation request.
	manualFixtureCancelCommand = "cancel-command"
	// manualFixtureCancelParent identifies the manual parent request to cancel.
	manualFixtureCancelParent = "cancel-parent"
	// manualFixtureCancelSock names the parent cancellation coordination socket file.
	manualFixtureCancelSock = "cancel.sock"
	// manualFixtureCanceledOriginals identifies original-history retrieval after cancellation.
	manualFixtureCanceledOriginals = "canceled-originals"
	// manualFixtureInputJson names the captured input artifact file.
	manualFixtureInputJson = "input.json"
	// manualFixtureInvalidStateAfterCommit selects incomplete diagnostics after durable replacement.
	manualFixtureInvalidStateAfterCommit = "invalid-state-after-commit"
	// manualFixtureLoseSharedOwnerAfterCommit selects shared policy and observer process loss after replacement.
	manualFixtureLoseSharedOwnerAfterCommit = "lose-shared-owner-after-commit"
	// manualFixtureLoseStateAfterCommit selects policy process loss after replacement.
	manualFixtureLoseStateAfterCommit = "lose-state-after-commit"
	// manualFixtureManualUi selects the manual-compaction UI fixture mode.
	manualFixtureManualUi = "manual-ui"
	// manualFixtureManualUiCompact identifies the UI manual-compaction request.
	manualFixtureManualUiCompact = "manual-ui-compact"
	// manualFixtureManualUiJson names the UI fixture report file.
	manualFixtureManualUiJson = "manual-ui.json"
	// manualFixtureNestedCancellationCheckpoint marks nested model input in cancellation scenarios.
	manualFixtureNestedCancellationCheckpoint = "nested cancellation checkpoint"
	// manualFixtureNext identifies the next public conversation request.
	manualFixtureNext = "next"
	// manualFixtureObserveCancellation selects observer work for parent cancellation.
	manualFixtureObserveCancellation = "observe-cancellation"
	// manualFixtureObserverFailure selects an outcome observer failure.
	manualFixtureObserverFailure = "observer-failure"
	// manualFixtureOriginal supplies the original conversation input.
	manualFixtureOriginal = "original"
	// manualFixturePreserveTheRecentUserTurn supplies manual replacement guidance.
	manualFixturePreserveTheRecentUserTurn = "preserve the recent user turn"
	// manualFixtureReferencePolicy names the reference policy executable.
	manualFixtureReferencePolicy = "reference-policy"
	// manualFixtureReportJson names the cancellation fixture report file.
	manualFixtureReportJson = "report.json"
	// manualFixtureRetained supplies the retained conversation turn.
	manualFixtureRetained = "retained"
	// manualFixtureUnix selects Unix-socket fixture coordination.
	manualFixtureUnix = "unix"
	// manualFixtureWaitForCallerCancel selects pending policy work until caller cancellation.
	manualFixtureWaitForCallerCancel = "wait-for-caller-cancel"
)

const (
	// manualRequestCompositionGuidance selects ordered request composition.
	manualRequestCompositionGuidance = "request-composition"
	// manualRequestClearGuidance selects clearing the composed result.
	manualRequestClearGuidance = "request-clear-result"
	// manualRequestCancelGuidance selects participant cancellation.
	manualRequestCancelGuidance = "request-cancel"
	// manualRequestFailGuidance selects participant failure.
	manualRequestFailGuidance = "request-fail"
	// manualRequestFailAfterChildGuidance selects failure after a handled child cause.
	manualRequestFailAfterChildGuidance = "request-fail-after-handled-child"
	// manualRequestInvalidGuidance selects an invalid participant action.
	manualRequestInvalidGuidance = "request-invalid"
	// manualRequestFinalSummary contains the final participant summary.
	manualRequestFinalSummary = "Ordered request replacement summary."
	// manualRequestSecondInstruction contains the second participant request change.
	manualRequestSecondInstruction = "second request change from original"
	// manualRequestResultDetails contains the opaque result-only change.
	manualRequestResultDetails = "result-only-details"
	// manualRequestFailureCause identifies the complete participant failure.
	manualRequestFailureCause = "reference request handler failure: complete source cause"
	// manualRequestInvalidCause identifies the invalid-action rejection.
	manualRequestInvalidCause = "invalid reference request action"
	// manualRequestPolicyInputFile names the captured policy input artifact.
	manualRequestPolicyInputFile = "policy.json"
)
