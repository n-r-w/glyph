# Problem Statement

Historical baseline snapshot: the observations and forecasts below describe the problem-definition stage at `7238582309a8abd2750898ac0c0d8bbc07e78a18`, before the corrective units. Present-tense behavior descriptions refer to that baseline. The [correction dispositions](../acceptance-report.md#findings) and [unit status](plan.md#plan-units) record the later implementation and review outcomes. Initial integration/coverage failures were followed by successful unchanged-source short-root reruns, recorded in [baseline results](baseline.md#results) and [environment and recorder failures](baseline.md#environment-and-recorder-failures). Those baseline reruns are separate from the later corrective-unit and assembled verification.

## Key definitions and abbreviations

Use the existing [domain glossary](../../../../../../terms.md) and [phase definitions](../ticket.md#key-definitions-and-abbreviations). This correction introduces no domain terms.

## Context

PHS-06 supplies extension-owned compaction, preserved session history, and Host-owned retry control. Its [independent acceptance report](../acceptance-report.md) records two implementation findings and three evidence findings. The [fact collection](facts.md) traces their current paths.

## Observed Problem

Users can receive incomplete Codex failure causes after a terminal refusal, and enabled retry handlers are bypassed for context overflow without recovery ownership. Maintainers also lack direct regression evidence for bundled result rejection, the complete restored suffix, and cancellation during an installed retry wait. These conditions leave whole-phase acceptance blocked.

## Affected Audience

- Ordinary and configured-model callers can lose the later read cause accompanying a Codex refusal, which limits their ability to explain a failed request.
- Extension authors can configure retry handlers whose terminal actions are skipped for ownerless overflow, so their cancellation or failure outcome does not reach the caller.
- Maintainers cannot use the current assertions to distinguish several required failure and restoration outcomes from regressions in the bundled coordinator and retained history.

## Evidence

- F23: `host/internal/infra/providers/openai/codex/provider.go`, `Driver.executeRequest`, returns at the semantic terminal event before the next SDK iteration. The declared SDK acquires scanner errors while continuing iteration and closes without draining. These independently inspected source branches explain the missing later-cause path. No new public Codex runtime reproduction is claimed at this stage.
- F24: `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute`, returns CONTEXT_LIMIT for enabled ownerless overflow before constructing and dispatching a retry decision. `Service.RequestConfigured` always supplies nil recovery.
- F19: Public terminal-result assertions in `manual_result_terminal_integration_test.go` assemble the reference coordinator. The bundled public matrix in `standard_compaction_composition_integration_test.go` selects successful replacement and explicit result Cancel, while its detailed result-error and rejected-update observables remain unexecuted.
- F20: `TestStandardPreparedHistoryPublic` performs repeated compaction and owner recreation, but its restored-body block checks summary presence and old-prefix exclusion without a positive ordered retained-suffix comparison.
- F21: `TestServiceCustomCancellationDuringRetryDelayPreservesCause` cancels inside retry progress. Host emits progress before declared backoff starts the timer. Public abort fixtures also synchronize on progress.

## Impact

F23 and F24 have HIGH consequences for caller-visible diagnostics and extension authority. F19 through F21 have MEDIUM consequences for acceptance evidence. They establish incomplete regression observations, rather than present defects in production result rejection, suffix restoration, or interruption of an installed timer.

## Current State

The bundled coordinator validates a result replacement before installing it and commits after the result loop. Session reconstruction traverses the suffix from the latest marker's first-kept entry. Declared backoff selects cancellation during its timer wait. Those inspected production paths coexist with the evidence limits above.

Existing passing history and abort tests retain the scope of their actual assertions. The [baseline](baseline.md) records incomplete whole-project integration and coverage runs separately from these source observations. The baseline timeout alone establishes no product cause.

## Desired State

Callers receive complete failure explanations and the terminal outcomes of their enabled handlers. Maintainers can determine phase readiness from executable observations of the required composition, restoration, and cancellation behavior.

## Problem Boundary

The affected cases include ordinary and configured-model Codex refusal streams, enabled retry-handler dispatch for context overflow without a recovery owner, bundled result-composition failures before mutation, repeated compaction followed by owner recreation, and abort while a retry delay is pending.

The [confirmed boundaries](target-picture.md#decision-authority-and-boundaries) retain accepted architecture, API, journal format, dependencies, generator limits, and prior debt. F17 and optional F18 work remain excluded. Live-provider summary quality, physical TUI interaction, and new performance guarantees remain outside these cases.

## Assumptions

No present production failure is inferred solely from the missing evidence in F19 through F21. Runtime RED/GREEN observations for the source-backed F23/F24 branches remain subsequent implementation evidence.

## Open Questions

At the problem-definition stage, no unresolved fact prevented describing the five correction subjects. The [fact-collection questions](facts.md#open-questions) retain the planned runtime observations. The environment-only baseline reruns subsequently completed with the [results recorded in baseline.md](baseline.md#results). Later correction observations and their source limits are in the [requirement assessment](unt-06-evidence.md#correction-requirement-assessment). These later results preserve the historical scope and forecasts above.
