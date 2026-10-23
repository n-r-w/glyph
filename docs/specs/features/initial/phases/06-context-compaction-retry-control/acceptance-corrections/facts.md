# Analysis Results: PHS-06 acceptance correction paths

Historical fact-collection snapshot: source branches, observations and forecasts below refer to `7238582309a8abd2750898ac0c0d8bbc07e78a18`, before the corrective units. Present-tense descriptions identify that baseline's behavior and assertions. This record supplied facts for the correction requirements and design under the [confirmed target](target-picture.md).

The [correction dispositions](../acceptance-report.md#findings) and [unit status](plan.md#plan-units) record the later implementation and reviews. The environment-only baseline integration/coverage reruns completed successfully as recorded in [baseline results](baseline.md#results) and [environment and recorder failures](baseline.md#environment-and-recorder-failures). Those unchanged-source reruns are separate from the later [correction observations](unt-06-evidence.md#correction-requirement-assessment). Original source limits and forecasts remain below; later execution does not reconstruct missing historical records.

## Scope

Scenarios A1 through A5, corresponding to F23, F24, F19, F20, and F21 in the [phase report](../acceptance-report.md). F17 and optional F18 work remain excluded.

## Key definitions and abbreviations

Use the [domain glossary](../../../../../../terms.md) and [ticket definitions](../ticket.md#key-definitions-and-abbreviations).

## Executive Summary

The Codex driver returns on a semantic terminal event. Host returns ownerless overflow before retry-handler dispatch. Bundled result composition validates replacements before assignment and commits after the handler loop. The repeated-history restart test checks summary presence without comparing the retained suffix. Existing cancellation tests initiate cancellation at retry progress, before backoff starts its timer.

## Background and Context

The [ticket](../ticket.md) owns the phase requirements. The [existing report](../acceptance-report.md#findings) records the five correction subjects. The [baseline](baseline.md) records whole-project commands separately from behavior observations.

## Method and Data Sources

Four independent source investigations traced Codex errors, Host retry and timer behavior, bundled result composition, and retained-history restoration. The delivery owner also read the principal branches and the named assertion bodies. Source inspection, test assertion inventory, and executed observations remain distinct below. Source revision is `7238582309a8abd2750898ac0c0d8bbc07e78a18`.

## Observations

### A1: Codex terminal refusal and stream iteration

In `host/internal/infra/providers/openai/codex/provider.go`, `Driver.executeRequest` returns immediately when `semanticAssembler.consume` reports terminal. The code following the loop reads `stream.Err()`. Its defer calls `stream.Close()` and joins a close error. The failure branches of `semanticAssembler.consume` in `assembler.go` return a typed providerResponseError with the received code and explanation.

Declared OpenAI SDK v3.49.0, `packages/ssestream/ssestream.go`, `eventStreamDecoder.Next`, returns a complete event at its terminating empty line. It copies the scanner error after iteration ends. `Stream.Next` copies the decoder error after decoder iteration. `Stream.Close` calls the underlying body Close without continuing iteration.

`Driver.Stream` emits the terminal response and calls `classifyCodexFailure`. That classifier checks the structured provider error before transport classification and retains the complete supplied error as Cause. Ordinary `Service.Stream` and configured `Service.RequestConfigured` in `host/internal/usecase/host/modelexecution/service.go` call the same `Service.execute` with the resolved provider.

Existing `TestTerminalRefusalAcrossPublicClients` in `host/internal/app/terminal_failure_modes_integration_test.go` serves terminal refusal plus a truncated HTTP body and asserts complete causes, category, and attempts through Programmatic, UI, configured SDK, and headless callers. Its settings select OpenAI-compatible Responses. Existing Codex terminal tests assert provider refusal and cleanup causes separately. No new Codex refusal-plus-later-read runtime observation was produced during fact collection.

### A2: Overflow and retry-handler dispatch

`host/internal/usecase/host/modelexecution/retry.go`, `Service.execute`, snapshots enabled retry handlers, executes one provider attempt, and acquires ProviderFailureError. For context overflow, it returns CONTEXT_LIMIT when retry is disabled or recoverOverflow is nil. This return precedes retryDecision and the handler loop. Configured `Service.RequestConfigured` supplies nil recovery; `selectCapturedOverflowRecovery` supplies nil for an absent ordinary capture.

The later handler loop passes fixed Original and evolving Current values in snapshot order. Handler errors and invalid actions return EXTENSION_FAILED. Explicit Cancel returns RETRY_CANCELED before later handlers. Preserve retains Current. Final decision validation and the provider-delay maximum precede repeat scheduling. `prepareOverflowRecovery` invokes the recovery function and requires a committed result before replacing history.

Existing `TestAbsentPolicySuppressesCompactionOverflow` checks CONTEXT_LIMIT, source identity, and one ordinary provider attempt with no handlers. Public refusal tests pair overflow with no handler and pair handler Cancel or failure with transient refusal. The specific ownerless/configured overflow and terminal-handler combinations were not executed during this investigation.

### A3: Bundled result composition

`plugins/extension/compaction/internal/usecase/policy/service.go`, `Service.complete`, fixes the original result before iterating result participants. Invocation errors and invalid actions return immediately. Explicit Cancel sets the canceled flag. Replacement validation occurs before assigning state.Result. Commit occurs after the result loop and a final context check. `Service.Compact` returns the acquired state with the outcome when complete returns an error.

`host/internal/app/manual_result_terminal_integration_test.go`, `manualResultHandlersSuite.runTerminalScenario`, checks public terminal text, before/after entry equality, no committed receipt, accepted result and requests, and absence of the last-handler artifact. It assembles the reference policy. `TestStandardManualComposition` in `standard_compaction_composition_integration_test.go` assembles bundled ownership and includes explicit result Cancel and successful result replacement. Its table does not select the reference result-failure/rejected-update modes.

The reference coordinator explicitly converts a live-context SDK CanceledError to canceled=true. The bundled Host adapter returns the participant error to the bundled coordinator. Explicit Cancel and an error containing context.Canceled therefore have separate source branches. Existing reference participants provide both ordinary failure and a classified joined cancellation/source failure.

### A4: Restored retained suffix

`host/internal/usecase/host/sessions/entry_history.go`, `compactedHistoryFromEntries`, finds the latest marker and its first-kept entry, prepends the latest summary, and traverses entries from that boundary in branch order while skipping compaction markers. Shared history projection and Codex serialization then process the ordered history.

`host/internal/app/standard_compaction_history_public_integration_test.go`, `TestStandardPreparedHistoryPublic`, performs two compactions and application-owner recreation in summarized-span and default-installed. It compares the restored active leaf with the second receipt ID. After submitting after restoration, it checks the latest summary's presence and exclusion of the earlier summary and original prefix. That assertion block has no decoded ordered suffix comparison. The second receipt contains FirstKeptEntryId, and preparedStoredEntries obtains the active branch through the public session-entry operation.

The existing test was executed once during fact collection with `go test -tags=integration ./host/internal/app -run '^TestStandardPreparedHistoryPublic$' -count=1 -v`. Its summarized-span, retained-span, and default-installed cases passed. This execution ran the existing assertions; it did not print a concrete second boundary or resumed input sequence.

### A5: Retry timer cancellation

`Service.execute` delivers retry progress synchronously before returning backoff.RetryAfter. In declared backoff v7.0.0, `Retry` checks context cancellation, starts its timer, and selects on timer completion and ctx.Done. Timer Stop is deferred.

`TestServiceCustomCancellationDuringRetryDelayPreservesCause` in `host/internal/usecase/host/modelexecution/retry_cancellation_test.go` uses synctest but calls cancel directly inside the progress callback. It checks custom cancellation and provider causes, current-cancellation identity, and an empty response. It does not call synctest.Wait.

The existing public `TestPublicAbortReleasesAgent` and `TestUIAbortPendingRetryReleasesAgent` synchronize on received retry progress. They assert terminal ordering, one target provider attempt, idle, and recovery through the retained connection. Their selected executions in an isolated copy passed with no selected skips. Those executions supply progress-boundary observations, without recording timer installation.

## Analysis and Interpretations

This stage records source branches and observations only. Problem assessment and implementation choices belong to subsequent stages.

## Hypotheses and Tests

The baseline's long temporary socket paths produced AF_UNIX path too long in direct OS bind probes. An initial shorter rerun still used 109-byte paths. A further baseline run used the existing short /var/tmp root directly; the focused scenario, full integration task, and coverage task passed. No F17 source was inspected.

## Options and Trade-offs

Implementation options are deferred to technical design.

## Recommendation

No implementation recommendation is part of fact collection.

## Action Plan

No implementation plan is part of fact collection.

## Assumptions

No new runtime success is inferred from source tracing. Existing phase evidence retains the boundaries in the phase report.

## Open Questions

- What complete causes, categories, and attempt counts do new Codex terminal-refusal and ownerless-overflow regression cases expose? Source branches are identified; obtain compiling RED/GREEN observations during implementation before accepting the corrections.
- Which bundled result-rejection cases execute the complete accepted-state, stopped-chain, cause, and no-commit assertions? Existing assertion sources are identified; obtain runtime evidence during implementation.
- What exact saved suffix and decoded resumed input occur after the second receipt in each repeated-compaction case? Obtain them from the saved branch and receipt in implementation tests.
- Which execution deterministically reaches the installed retry wait before cancellation? Resolve the test synchronization design before implementation, then execute it during implementation.
The baseline temporary-path question is resolved by passing unchanged-source full integration and coverage runs with TMPDIR=/var/tmp. No source correction or exclusion expansion was needed.

## References

- [Target picture](target-picture.md): acceptance scenarios and authority.
- [Baseline](baseline.md): command results and recorder limitations.
- [Phase report](../acceptance-report.md): original findings and accepted evidence boundaries.
- Source file and symbol references accompany each observation above.
