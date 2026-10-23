# PHS-06 independent acceptance review

Status: Corrective implementation is complete at `5375876f770dc3eb4a7b4010bea472ff55a31539`. Independent acceptance-conclusion review approves all 40 phase clauses and correction FRQ-01 through FRQ-05/NRQ-01 within the recorded boundaries, with no required gap. UNT-01 through UNT-05 and the UNT-02 test-style follow-up have independent unit approval and local commits. The phase is delivery-ready on implementation and acceptance evidence. Final independent acceptance-conclusion and text reviews approved the delivered documentation, with both Minor text findings resolved. UNT-06 is complete in documentation commit `e7dd665cfc2dd50ab00182ad5be188dbe5619d27`. Final user acceptance remains pending.

This assembled record incorporates the independent acceptance-conclusion verdict described under [review outcome](#independent-review-outcome). The independent reviewers granted the recorded conclusion/text approvals. Final user acceptance remains the user's decision. The [correction plan](acceptance-corrections/plan.md#plan-units) records unit commits. The [UNT-06 delivery record](acceptance-corrections/unt-06-evidence.md) owns assembled check results, provenance, correction-requirement verification, and documentation validation. The executable baseline has Git tree `221491f013e9109d2d9682ff2d616be09c38c831`. UNT-06 changes documentation only.

## Scope and method

The [ticket](ticket.md) owns all 25 parent acceptance clauses. The [standard-extension requirements](phs-06.2_prd.md) own 12 clauses, and the [prepared-history requirements](../../../../issues/compaction-history-projection/prd.md) own three. The historical Host-owned checkpoint `690827f` remains comparison evidence rather than target behavior.

The author read current production branches and actual assertions for the five correction subjects. The author also reconciled inherited accepted [system evidence](system-integration-evidence.md), [prepared-history evidence](a4-prepared-history-evidence.md), [retry evidence](a5-retry-acceptance-evidence.md), and [abort evidence](a6-abort-idle-evidence.md) with the corrected source and fresh uncached event records. Those records establish different levels of evidence: production application assembly, mocked dependency ports, source traces, original behavioral RED, and later assertion-sensitivity controls. A passing command alone establishes no missing scenario.

The original independent review at `70da6a9f301ce0911956803ee9b03c7d995bc0a6` found F23/F24 behavior defects and F19/F20/F21 evidence gaps. The authorized [correction requirements](acceptance-corrections/requirements.md) preserve that review's scope. The dispositions below concern the corrected implementation; original unit handoff records remain immutable.

## Findings

### F23: Codex stops before a later stream error

Disposition: Corrected by UNT-01, commit `7f10ace03153388cc66705b42121e3c0e2587f1d`. In `host/internal/infra/providers/openai/codex/provider.go`, `Driver.executeRequest` continues context-bound SDK iteration after a typed refusal whose semantic delivery succeeded. It joins the subsequent SDK error with acquired refusal causes. Success, assembler-only failure, and failed semantic delivery retain immediate exits.

`TestTerminalRefusalAcrossPublicClients` in `host/internal/app/terminal_failure_modes_integration_test.go` asserts refusal text, `unexpected EOF`, `CONTEXT_LIMIT`, and one attempt for ordinary Programmatic and configured SDK Codex calls. Configured `Wait` and `WaitWithProgress` each make one logical request. Driver assertions separately cover clean end, conversion/merge causes, delivery/close causes, later-event isolation, immediate exits, and owning cancellation. See [UNT-01 evidence](acceptance-corrections/unt-01-evidence.md#behavior-observations). SDK iteration exposes its first subsequent failure; this mechanism promises no acquisition beyond SDK termination. The original RED snapshot limitation remains in [chronology](acceptance-corrections/unt-06-evidence.md#chronology-and-historical-records).

### F24: Ownerless overflow bypasses enabled retry handlers

Disposition: Corrected by UNT-02, commit `da9455f9afa4832acb29972c468cb1f0d92b0748`. In `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute` composes and validates enabled retry handlers before rejecting overflow without recovery. Disabled retry retains its early exit. The absent-recovery guard still precedes recovery invocation, progress, and scheduling.

`TestServiceOwnerlessOverflowHandlers` checks seven outcomes through ordinary and configured entry points: Cancel, failure, invalid action, invalid decision, Preserve, valid repeat override, and disabled retry. Ordered mocks check immutable Original, accepted Current, stopped later handlers, one provider call, complete causes, category, and zero progress. The public terminal fixture adds five ordinary/configured outcomes with refusal plus EOF. A valid override still cannot repeat unchanged overflowing input. See [UNT-02 observations](acceptance-corrections/unt-02-evidence.md#green-and-behavioral-observations). The separately approved [style follow-up](acceptance-corrections/unt-02-follow-up-evidence.md), commit `5375876f770dc3eb4a7b4010bea472ff55a31539`, removes the three delivery-owned supplementary lint diagnostics without changing asserted properties.

### F19: Result-handler rejection tests use another coordinator

Disposition: Required evidence supplied by UNT-03, commit `e18ee60b5c229ebf24c48834adae010ebfc1b24e`. `TestStandardResultTerminalActions` in `host/internal/app/standard_result_terminal_integration_test.go` runs seven terminal cases under bundled policy ownership. Its shared `runManualResultTerminalScenario` compares complete accepted request/result state, every pre/post stored entry, absent receipt, complete causes, and absence of the last-handler artifact.

`TestCompactStopsBeforeInstallingRejectedResult` in `plugins/extension/compaction/internal/usecase/policy/result_rejection_test.go` invokes bundled `Compact` with generated Host mocks. Unexpected later-handler and Commit calls fail the test. These assertions distinguish an attempted rejected commit from durable no-mutation. A live-context cancellation-containing error remains a failure; explicit Cancel has its own successful canceled outcome. The [three isolated sensitivity controls](acceptance-corrections/unt-03-evidence.md#evidence-sensitivity-controls) compile and fail for rejected-state installation, continuation after error, and attempted commit. This unit changes evidence rather than production behavior.

### F18: Optional independent-field control for request replacement

Disposition: Optional strengthening, excluded from required correction work. In `TestStandardManualComposition`, successful supplied-result cases execute the original-derived complete replacement and next-handler observations through the bundled coordinator. `Service.Compact` installs a validated returned request as a complete value. Existing evidence meets ACC-01.2. A second independent accepted-field mutation remains outside this delivery. No new merge defect or mandatory test obligation is asserted.

### F20: Restart assertions omit the retained suffix

Disposition: Required evidence supplied by UNT-04, commit `d769d3004619c7b31d2669d49642ea78d75011eb`. `TestStandardPreparedHistoryPublic` in `host/internal/app/standard_compaction_history_public_integration_test.go` saves the branch after the second receipt and recreates the application owner. `assertPreparedRestoredSuffix` in `standard_compaction_restored_suffix_integration_test.go` derives the expected suffix from that branch and the second receipt's first-kept identifier. It compares all decoded roles and texts in order, including two distinct retained items and the new request.

Both summarized-span and default-installed cases assert the latest leaf, summary, and complete suffix. Summary-wrapper wording is outside the equality contract. The retained-span case keeps its earlier return and separate tool-span assertions. [Omission and reordering controls](acceptance-corrections/unt-04-evidence.md#assertion-sensitivity-controls) fail at the new equality. Owner recreation occurs within the test process; no fresh Host OS-process restart is claimed. The [recorder limitations](acceptance-corrections/unt-04-evidence.md#recorder-deviations) remain historical limits.

### F21: Retry progress does not establish an installed timer

Disposition: Required evidence supplied by UNT-05, commit `4ac49d0bc9e7f97146eb0061886b7fb3e3a25726`. `TestServiceCancellationInterruptsInstalledRetryWait` in `host/internal/usecase/host/modelexecution/retry_installed_wait_test.go` uses synchronous generated ports, a buffered progress signal, `synctest.Wait`, unfinished completion, and unchanged fake time before owner cancellation. Between progress return and durable blocking, the worker's production wait is backoff's timer/context select. Assertions require zero elapsed fake time, one provider attempt, complete provider/cancellation causes, current-cancellation identity, and an empty response.

The [isolated cancellation-free scheduler control](acceptance-corrections/unt-05-evidence.md#source-and-sensitivity-identities) compiles and fails with one hour of fake elapsed time and two attempts. Existing public Programmatic active-HTTP/pending-retry and UI pending-retry tests separately establish terminal order, public idle, and retained-connection recovery. Public progress itself still supplies no timer-installation acknowledgement. This unit adds evidence without a production timer API or behavior change.

## Acceptance matrix

Every row below has an author assessment of **Source-supported**. This means the inspected implementation follows the clause and named executable assertions exercise its required behavior within the stated boundary. Independent acceptance-conclusion review approved these assessments within the stated boundaries. The verdict makes no claim that every provider/client/state combination ran. The inventory contains exactly 25 parent rows, 12 standard rows, and three prepared-history rows.

Evidence abbreviations: [System](system-integration-evidence.md#user-visible-contract), [History](a4-prepared-history-evidence.md#public-assembly-regressions), [Retry](a5-retry-acceptance-evidence.md#public-executable-observations), [Abort](a6-abort-idle-evidence.md#scope-and-exercised-paths). Correction dispositions above provide current code and assertion locations.

| Parent clause | Review result | Main evidence and boundary |
| --- | --- | --- |
| ACC-01 | Source-supported | Standard automatic/overflow continuation, retained originals and summary/suffix assertions in History and System; F19-F21 now have bundled rejection, ordered restoration, and installed-wait evidence. |
| ACC-01.1 | Source-supported | Public request-handler input assertions and bundled composition check equal initial Original/Current, immutable Original, registration order, detached input and accepted Current. System request-handler evidence retains transport scope. |
| ACC-01.2 | Source-supported | `TestStandardManualComposition` supplied-result cases exercise original-derived replacement and next-handler validation. F18 remains optional. |
| ACC-01.3 | Source-supported | The same test bypasses absent/multiple generators for supplied results and checks ordered result transformation; public participant inputs retain original-result observations. |
| ACC-01.4 | Source-supported | Clear-to-standard/custom and ambiguous-generator cases in bundled composition; no-generator and final-model/reasoning assertions in `standard_compaction_integration_test.go` and captured conversation-budget evidence in `standard_compaction_model_integration_test.go`. |
| ACC-01.5 | Source-supported | Bundled request failures plus F19's seven result-terminal cases. Full state/entry equality, absent later artifact and no expected Commit distinguish stopped composition from merely rejected persistence. |
| ACC-01.6 | Source-supported | Bundled explicit request/result Cancel yields zero markers and one failure observer invocation; success yields one success invocation. System nested-operation tests retain cancellation/join coverage. |
| ACC-01.7 | Source-supported | `TestStandardPreparedRetention`, `TestStandardPreparedHistoryPublic`, legal source-span and tool-group assertions cover default/changed retention and complete groups. Estimates remain approximate. |
| ACC-01.8 | Source-supported | Public repeated generation asserts preceding summary once, excludes superseded original prefix, and uses the second receipt for next projection; F20 adds positive suffix equality. |
| ACC-01.9 | Source-supported | Default-installed executable discovery plus standard/custom generator and complete-result composition. Tests use controlled provider HTTP behavior. |
| ACC-01.10 | Source-supported | Session commit guards and atomic persistence in System; public full original-entry equality, repeated projection, restart and navigation in History/F20. Fresh Host OS-process restart remains unexecuted. |
| ACC-01.11 | Source-supported | Accepted target ownership and System cleanup; bundled policy owns estimates, trigger, boundary, generation and composition. Core consumes shared prepared history; Host retains runtime/session mechanics. |
| ACC-02 | Source-supported | Standard Programmatic, UI and Extension manual adapters pass guidance through common services; F19 directly exercises bundled result rejection through Programmatic assembly and coordinator ports. No new per-client Cartesian matrix is claimed. |
| ACC-03 | Source-supported | F20 compares latest leaf and complete summary/suffix after application-owner recreation; accounting restart test retains saved source costs. |
| ACC-04 | Source-supported | Retry public default schedule asserts four attempts, delays 1/2/4 seconds and actual arrival lower bounds; F23/F24 cover formerly unmet refusal/overflow branches. |
| ACC-04.1 | Source-supported | Retry public composition records originals/current across failed attempts. F24 layer assertions add absent-owner/configured overflow order and terminal stopping. |
| ACC-04.2 | Source-supported | Retry public HTTP 400 override changes retryability, delay and final allowance; F24's valid override still cannot admit unchanged ownerless overflow. |
| ACC-04.3 | Source-supported | Public transient and overflow Cancel/failure/invalid-decision paths plus layer invalid-action tests assert complete causes, category, stopped later handlers and one attempt. |
| ACC-04.4 | Source-supported | Core's consumed logical stream remains neutral; retry owner, handlers and scheduler stay in Host. Accepted APC-13 contract/assertion imports retain their boundary below. |
| ACC-04.4.1 | Source-supported | Existing `TestRetryAcrossUIAndProgrammaticModes`, `TestConfiguredModelSDKFailuresPreserveLogicalIdentity` and terminal suites execute Codex and compatible drivers behind the consumed provider interface with compile-time assertions. No dedicated swap fixture or caller rewrite was required. |
| ACC-04.5 | Source-supported | Compatible terminal suite covers Programmatic/UI/configured/headless exhaustion, Cancel, handler failure and nonretryable failures. F23 adds ordinary Programmatic/configured Codex refusal plus EOF; F24 adds overflow handler categories. Broader Codex/client matrix remains unexecuted. |
| ACC-04.6 | Source-supported | Retry default/configured/disabled rows assert attempt counts, delay sequence, actual arrivals and absence of disabled handler invocation/progress. |
| ACC-04.7 | Source-supported | Retry provider-delay rows assert accepted 10/60-second lower bounds, default 30-second maximum, raised/lowered maxima, complete source and duration text, and no repeat on rejection. |
| ACC-05 | Source-supported | UI/Programmatic reset fixture asserts four stored entries, one completed tool result and repeated request bodies. Standard overflow checks actual completed-tool report and counts. |
| ACC-06 | Source-supported | F21's installed-wait layer test complements Abort's public active HTTP/pending retry, target-terminal ordering, idle and same-connection recovery. Physical TUI Stop remains unexecuted. |

Identifiers in the next table belong to [standard requirements](phs-06.2_prd.md).

| Standard clause | Review result and specific evidence or limit |
| --- | --- |
| FRQ-01 | Source-supported: default-installed executable case and normal startup registration. |
| FRQ-02 | Source-supported: `TestStandardAutomaticExactThreshold` and automatic/final-history suites assert equality/one-token excess, reported/missing/zero usage, model/reasoning changes and restart fallback; observation unit assertions cover detached anchors and binding/model isolation. No tokenizer-accuracy guarantee. |
| FRQ-03 | Source-supported: standard manual adapter guidance and shared composition, with F19 closing bundled rejection evidence. |
| FRQ-05 | Source-supported: default/configured prepared retention, indivisible tool groups and legal original-order source spans; response reserve remains separate. |
| FRQ-06 | Source-supported: repeated public source contains preceding summary once and only new prefix; complete original-entry equality preserves the journal. |
| FRQ-07 | Source-supported: standard ordered composition and immutable inputs, plus F19 accepted-state equality and no-Commit expectations on rejected results. |
| FRQ-08 | Source-supported: generator source tests assert 2,000 Unicode code points shared across text blocks, image omission, one truncation marker and unchanged originals; response tests check unusable terminals. Standard generation remains one request and may fail on excessive source. |
| FRQ-09 | Source-supported: standard overflow recovery/allowance/completed-tool cases preserve Host attempt budget and completed actions. F24 preserves enabled handler authority when recovery is absent. |
| FRQ-10 | Source-supported: F19 precommit terminal assertions, System nested joins, and standard postcommit observer/final-input failure tests retain receipt and complete causes. |
| FRQ-11 | Source-supported: independently disabled bundled generator, supplied-result bypass and zero/multiple cardinality failures in bundled acceptance/composition tests. |
| FRQ-12 | Source-supported: `TestCompactionAccountingPersistsRequestsAcrossClientsAndRestart` checks separate request pricing, zero/missing usage or pricing, repeated markers and saved costs after pricing removal. Failed/canceled-generation billing remains outside scope. |
| NRQ-01 | Source-supported: asynchronous operation ownership and public queries/cancellation during blocked work. No numerical latency bound is claimed. |

Identifiers in the final table belong to the [prepared-history issue](../../../../issues/compaction-history-projection/prd.md).

| Prepared-history clause | Review result and specific evidence or limit |
| --- | --- |
| FRQ-01 | Source-supported: `TestStandardCompactionExcludesFailedSource`, independent stored-result/skipped-result transformations and repeated-source assertions use prepared conversation content; standard generator limits remain applicable. |
| FRQ-02 | Source-supported: manual/automatic/overflow `TestStandardPreparedRetention` and default case exclude failed/aborted weight; shared source-span and tool-group tests retain valid boundaries. |
| FRQ-03 | Source-supported: full original-entry equality, summary/prefix exclusions and navigation in History; F20 adds complete ordered second-receipt suffix after owner recreation. Compacted fork/clone provider-input assembly remains unobserved as recorded below. |

## Verification

The [UNT-06 verification record](acceptance-corrections/unt-06-evidence.md#assembled-verification) contains actual commands and exits from 2026-10-23 at the corrected baseline. Mandatory formatting, dry run, unit tests, integration tests, coverage, build and two generation runs completed with exit 0. Required `task lint` returned 201 because the sole accepted GO-2026-6809 scanner finding returned inner exit 3. Root-resolved external-module lint completed with exit 0. Supplementary non-integration lint remains a separate outside-task failure.

Four uncached race JSON runs establish executed test-event inventories and actual skips. These events include parent tests; they are not unique scenario counts. No full project check was rerun for this documentation-only unit. Primary raw records were read without modification.

### Failed checks and recorder corrections

The [current build recorder correction](acceptance-corrections/unt-06-evidence.md#recorder-correction) preserves both successful build exits and distinguishes unintended ignored checkout output from the authoritative temporary-root build. The canonical 1,395-path comparison derives specifically from the second build record. Each command also has its own source manifests; the canonical digest is not retroactively attributed to every command.

Earlier archive/resource, parser and manifest failures remain historical verification limits. [UNT-04 recorder deviations](acceptance-corrections/unt-04-evidence.md#recorder-deviations) include lost early logs, invalid empty manifests, incomplete focused provenance and a stale descriptive checksum entry. [UNT-05 verification](acceptance-corrections/unt-05-evidence.md#verification-execution-record) retains an interrupted coverage attempt without a recorded exit; its later completed rerun supplies coverage evidence. Fresh assembled checks establish corrected-source execution rather than reconstructing historical records.

## Evidence boundaries and retained debt

The delivery preserves the following inherited boundaries and accepted exceptions. No new limitation is accepted by this report.

- Public fixtures use production application, SDK, extension processes, storage and provider adapters with controlled provider HTTP behavior. They establish neither live-model summary quality nor tokenizer accuracy.
- History restart fixtures recreate application/plugin owners in the test process. A fresh Host OS-process restart is unexecuted.
- Fork/clone share inspected reconstruction and persistence paths. Compaction-specific tests use mocked repository ports; real repository replay fixtures use `BranchSummaryEntry`. Public compacted fork/clone followed by provider-input inspection remains unobserved.
- Compaction-specific public navigation uses `NO_SUMMARY`. Generic optional-summary/replay tests are separate. Optional branch summary, compaction, restart and provider dispatch were not executed as one combined scenario.
- Standard overflow tests cover markers, completed-tool report, selected content and causes. Some exact categories, intermediate entry cardinality, suffix ordering and post-cancellation idle rely on shared Host/reference tests or source tracing.
- Public abort directly covers Programmatic active HTTP, Programmatic pending retry and UI pending retry. Ordinary UI active HTTP, physical TUI Stop and abort after a completed tool remain matrix limits. The requirements require behavior evidence rather than every combination.
- Core and model execution retain provider-neutral consumed interfaces. The previously inspected direct imports numbered 17 and 23 respectively. Transitive dependency closures include transport through contract/assertion imports allowed by APC-13. No transport-free compiled-closure claim is made.
- Parent NFQ-01 retains original behavioral RED/GREEN obligations and named historical exceptions. The original A4 exception applies only to `TestStandardPreparedRetention` and `TestStandardPreparedTransformations`. Later controls supply sensitivity evidence. The UNT-01 missing original RED snapshot remains a provenance limit, with original compiling failures retained as the implementation account. No original chronology is reconstructed.
- GO-2026-6809 in `crypto/x509@go1.27.2` remains the sole required vulnerability exception. No suppression or package/toolchain update occurred.
- Previously accepted parent-context storage debt and the separate production-panic issue retain their [system scope](system-integration-evidence.md#remaining-limits-and-historical-failures).
- F17 remains absolutely excluded from inspection, reproduction, design and correction. F18 remains optional and excluded from strengthening work.
- The remaining 18 supplementary lint diagnostics have a [full outside-task disposition](acceptance-corrections/unt-06-evidence.md#outside-task-deviation-18-supplementary-diagnostics). They remain an observed failing developer check, without a runtime-effect conclusion, cleanup authorization, specific user debt acceptance, or second scanner exception.

No measured responsiveness failure or approved numerical performance threshold was established. Asynchronous structure and blocked-operation observations remain distinct from measured latency. Generator input limits, original journal format, architecture and APIs remain unchanged by the corrections.

## Documentation changes

UNT-06 updates the authorized phase/status documents and adds the assembled delivery record. The main report owns the complete 40-clause assessment; the delivery record owns check details and correction-requirement verification. Historical per-unit evidence remains immutable even where it says uncommitted or pending review. The [plan](acceptance-corrections/plan.md#plan-units) identifies the later approved commits, and the [chronology note](acceptance-corrections/unt-06-evidence.md#chronology-and-historical-records) explains those handoff-time statuses.

The [documentation validation results](acceptance-corrections/unt-06-evidence.md#documentation-validation) record local links/anchors, exact inventory membership, whitespace and frozen source scope. The owner committed UNT-06 as `e7dd665cfc2dd50ab00182ad5be188dbe5619d27` after final acceptance-conclusion and text approval. Earlier uncommitted/no-commit statements described the author handoff before that event. This status-only follow-up records completed events; it claims no approval or commit identity for its own revised metadata.

### Independent review outcome

Independent acceptance-conclusion review approved all 25 parent, 12 standard and three prepared-history assessments, together with correction FRQ-01 through FRQ-05/NRQ-01. The reviewer found no required gap and accepted no new limitation or debt. The verdict preserves the [evidence boundaries](#evidence-boundaries-and-retained-debt), historical chronology limits and sole required scanner exception.

Separate text review returned Approve with changes for two Minor findings: historical framing in the initial problem/fact records and temporary references in the assembled delivery record. The author corrected both within documentation scope. Final text review approved the corrected bytes, and the owner then created the UNT-06 documentation commit. The [delivery record](acceptance-corrections/unt-06-evidence.md#review-outcomes-and-text-corrections) records the dispositions. Final user acceptance remains a separate decision.

## Open questions

No unsupported phase clause or new product decision blocks delivery. Independent acceptance-conclusion review is approved. Final text review approved both Minor corrections and the actual documentation commit completes UNT-06. Review of this procedural metadata follow-up remains separate from those completed events. Final user review remains separate: the user may accept the assembled result or request corrections. This record chooses neither outcome.

## References

- [Ticket](ticket.md): parent requirements and all 25 acceptance clauses.
- [Migration plan](migration-plan.md#phase-phs-064---independent-acceptance): acceptance exit criteria and inherited units.
- [Target solution](solution.md#target-ownership): ownership and integration boundaries.
- [Standard requirements](phs-06.2_prd.md) and [solution](phs-06.2_solution.md): standard policy and accepted generator limits.
- [Prepared-history requirements](../../../../issues/compaction-history-projection/prd.md) and [solution](../../../../issues/compaction-history-projection/solution.md): shared projection and journal preservation.
- [Correction plan](acceptance-corrections/plan.md) and [UNT-06 delivery record](acceptance-corrections/unt-06-evidence.md): corrected revision, unit commits, current checks and limits.
- [A3](a3-terminal-failure-evidence.md), [A4.1](a4.1-run-history-evidence.md), [A4](a4-prepared-history-evidence.md), [A5](a5-retry-acceptance-evidence.md), and [A6](a6-abort-idle-evidence.md): inherited accepted evidence and chronology.
