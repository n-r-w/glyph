# PHS-06 independent acceptance review

Status: Changes required. PHS-06 remains in progress. Independent behavior review found two Major implementation findings and three Moderate evidence findings. F18 remains an optional test-strengthening note. Neither existing unit acceptance nor passing commands closes these findings.

Reviewed implementation: `70da6a9f301ce0911956803ee9b03c7d995bc0a6`, Git tree `a53f5dd00d8e129698fff97cf3dfc761b5775293`. Checkpoint `690827f` is the migration comparison baseline, not the accepted target. This review changes documentation only. Production code, tests, generated files, dependencies, configuration, and the original session format remain unchanged.

## Scope and method

The [ticket](ticket.md) owns the requirements and acceptance conditions. The [target solution](solution.md#target-ownership), [standard-extension requirements](phs-06.2_prd.md), and [prepared-history requirements](../../../../issues/compaction-history-projection/prd.md) define the review boundaries. The historical Host-owned checkpoint is excluded from target requirements.

Ten independent behavior reviews traced public entry points through their production dependencies to client results, persisted records, and subsequent provider input. They covered manual and automatic compaction, overflow recovery, repeated compaction, restart, navigation, fork, clone, logical retries, and abort. Reviewers read concrete test assertions and fixture construction. They distinguished the bundled coordinator from the reference coordinator and real application assembly from mocked dependency ports. Separate reviewers checked the material source conclusions.

The acceptance inventory contains all 25 parent ACC clauses, all 12 standard-extension requirement clauses, and all three prepared-history requirement clauses. Each clause has a review result below. A recorded review result can identify an unmet requirement or incomplete evidence; it is not a passing runtime result. The scenario inventories of overlapping reviews are not added together.

Scripts checked source identity, generated-file hashes, test-event records, direct imports, package dependencies, and documentation links. Semantic review covered action/state transitions, complete causes, policy ownership, source correspondence, durable mutation, and the meaning of test assertions. No new benchmark, production abstraction, or test framework was introduced.

## Findings

### F23: Codex stops before a later stream error

Severity: Major. Consequences: HIGH, because a public error can omit a required independent cause.

- Requirement: Ticket FRQ-04.8.1 and ACC-04.5 require both the received terminal provider refusal and the subsequent stream error.
- Location: `host/internal/infra/providers/openai/codex/provider.go`, `Driver.executeRequest`; `assembler.go`, `semanticAssembler.consume`; `transport.go`, `errorCaptureTransport.RoundTrip` and `joinError`.
- Scenario: An ordinary or configured-model request selects Codex. The HTTP 200 response contains a complete `response.failed` event with `context_length_exceeded` and a provider explanation. The response body then ends before its declared length. If body close succeeds, the caller can receive the refusal without `unexpected EOF`.
- Mechanism: `executeRequest` returns immediately when the assembler reports a terminal event. The declared OpenAI SDK v3.49.0 acquires a later scanner error only while continuing iteration. Its stream `Close` closes the body without draining the decoder. Codex's error-capture transport buffers rejection bodies for HTTP 400 through 599; it does not read a successful streaming body on behalf of the decoder.
- Evidence: Primary production and SDK source inspection, independently rechecked. This review did not reproduce the scenario at runtime. Existing terminal-refusal public regressions use OpenAI-compatible Responses, whose continuation path is different.
- Exceptions: Preterminal read failures, non-nil close errors, and captured HTTP rejection-body errors have preservation paths. The finding concerns the later read failure after a received terminal refusal.
- Recommendation: Acquire and retain the subsequent stream failure while preserving refusal-based classification, complete delivery causes, and owning cancellation behavior.
- Verification needed: A Codex terminal-refusal/truncated-body regression through ordinary and configured-model entry points must assert the provider explanation, subsequent read cause, category, and attempt count. Applicable client mappings need explicit evidence.

The correction would change production code and tests. The alternatives are an authorized correction or explicit acceptance of incomplete Codex diagnostics. Neither alternative has been selected. The unchanged requirement prevents phase closure while F23 remains open.

### F24: Ownerless overflow bypasses enabled retry handlers

Severity: Major. Consequences: HIGH, because Host skips supported extension actions and changes the public outcome.

- Requirement: Ticket FRQ-04.1 through FRQ-04.4 and ACC-04.3 require ordered retry-handler participation and terminal handling of Cancel, invalid actions, and handler errors.
- Location: `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute` and `selectCapturedOverflowRecovery`; `service.go`, `Service.RequestConfigured`.
- Scenario: Retry is enabled and a retry handler returns Cancel. A configured-model request receives context overflow. Host returns `CONTEXT_LIMIT` without invoking the handler, although handler cancellation would produce `RETRY_CANCELED`. A handler failure with a distinct cause is also skipped. The same branch applies to an ordinary request without a captured compaction owner.
- Mechanism: The context-overflow/nil-recovery return precedes decision construction and the handler loop. Configured-model requests always pass nil recovery. The public runtime mapping supports context-overflow classification and Cancel; the skip occurs before that mapping is invoked.
- Evidence: Primary entry-point, retry-owner, runtime-mapping, and outcome-mapping source inspection, independently rechecked. No runtime reproduction was performed. Existing public Cancel and handler-failure cases use transient refusals, while their overflow case has no handler.
- Exceptions: Ordinary requests with a captured recovery owner reach the chain. Disabled retry intentionally skips handlers for every classification. This finding creates no permission to repeat unchanged overflowing input.
- Recommendation: Separate handler participation from admission of a provider repeat. Preserve terminal handler outcomes and enforce recovery constraints before any repeat.
- Verification needed: Configured-model and ordinary no-owner overflow cases must cover Cancel, handler failure, invalid action, and Preserve. Assertions must distinguish categories and complete causes, stop later handlers, and require one provider attempt when no recovery is possible.

The alternatives are an authorized correction or explicit acceptance of this exception to retry-handler authority. Neither alternative has been selected. The unchanged requirements prevent phase closure while F24 remains open.

### F19: Result-handler rejection tests use another coordinator

Severity: Moderate. Consequences: MEDIUM for acceptance and regression evidence.

- Requirement: ACC-01.5, ACC-02, FRQ-01.7.1, FRQ-01.8, and standard FRQ-07/10.
- Location: `host/internal/app/manual_result_terminal_integration_test.go`, `manualResultHandlersSuite.runTerminalScenario`; `manual_candidate_acceptance_integration_test.go`; `standard_compaction_composition_integration_test.go`, `TestStandardManualComposition`; `plugins/extension/compaction/internal/usecase/policy/service.go`, `Service.complete`.
- Scenario and evidence: The detailed public invalid-result, rejected-boundary, handler-error, last-accepted-state, no-later-handler, and no-mutation assertions execute `policyOperation.composeResults` in the reference extension. The bundled public matrix executes successful result composition and explicit Cancel. Its inspected policy unit tests reject request-handler updates, while component tests cover result-action decoding. These observations do not execute the bundled coordinator's result-error/rejected-update branches.
- Recommendation: Reuse the existing result participants under the bundled policy and retain the complete cause, accepted state, no-later-handler, and zero-marker assertions. Distinguish a participant error containing `context.Canceled` from explicit Cancel.
- Verification needed: The production-path assertions must detect installation before validation, continuation after a handler error, and an attempted commit after rejection.

No production result-handler failure was established. Test changes require separate authorization; this report retains the gap.

### F18: Optional independent-field control for request replacement

Disposition: Optional strengthening, excluded from required corrective work. The approved acceptance condition has executable evidence.

- Relevant condition: ACC-01.2 and FRQ-01.5 permit deriving a replacement from the immutable original and require the next handler to observe that replacement. They do not require a second independent accepted-field change.
- Location: `testdata/external-plugins/cmd/compaction-policy/request.go`, `referenceRequestOperation.newFirstRequestAction` and `newSecondRequestAction`; `host/internal/app/manual_request_handlers_integration_test.go`, `assertManualRequestInputs`; `plugins/extension/compaction/internal/usecase/policy/service_test.go`, `TestCompactComposesOrderedState`.
- Scenario and evidence: Handler A returns a request with changed guidance. Handler B clones the original and writes different guidance. Handler C checks B's guidance. Cloning A's current request and overwriting that same field would yield the same assertion result. A's local mutation of its original invocation occurs after constructing its accepted replacement, so that mutation checks detachment rather than discarding an independent accepted change.
- Existing evidence: B explicitly clones the original, returns a complete request replacement, and C validates the resulting guidance through the bundled coordinator. Both successful supplied-result bundled cases passed in the current uncached integration run. The production coordinator assigns the returned request as a complete value.
- Optional improvement: A second independent accepted-field change could distinguish another hypothetical fixture mutation. No approved clause makes that mutation control mandatory, and no production merge defect was established.

Independent conclusion review rejected F18 as a mandatory acceptance deficit. ACC-01.2 is source-supported with current execution. No additional F18 test work is required or authorized by this review.

### F20: Restart assertions omit the retained suffix

Severity: Moderate. Consequences: MEDIUM for restored-context evidence.

- Requirement: ACC-03, ACC-01.10, and prepared-history FRQ-03.
- Location: `host/internal/app/standard_compaction_history_public_integration_test.go`, `TestStandardPreparedHistoryPublic`, `summarized-span` and `default-installed`.
- Scenario and evidence: After two compactions and application-owner recreation, both cases assert the second marker's active leaf, the latest summary in the next provider body, and exclusion of the preceding summary and original prefix. They omit a positive ordered comparison of the suffix retained by the second marker. The `retained-span` case returns before this restart. A mocked-repository single-marker test checks summary and retained messages at the session snapshot layer.
- Recommendation: Extend the existing resumed-provider assertions with the suffix selected by the second receipt. Derive the expected content from the saved branch and that receipt's first-kept entry.
- Verification needed: The assertion must detect omission or reordering of the second marker's unsummarized suffix. Content retained by the first marker may legitimately be summarized by the second.

Source inspection supports restoration of the suffix; no present suffix-loss defect was established. A recreated application owner in these tests is not a fresh Host OS process.

### F21: Retry progress does not establish an installed timer

Severity: Moderate. Consequences: MEDIUM for pending-delay cancellation evidence.

- Requirement: FRQ-05, ACC-06, and NRQ-01.
- Location: `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute`; the same package's `retry_cancellation_test.go`, `TestServiceCustomCancellationDuringRetryDelayPreservesCause`; public `abort_idle_integration_test.go` and `abort_idle_ui_integration_test.go`; declared backoff v7.0.0 timer loop.
- Scenario and evidence: Model execution emits retry progress before returning `backoff.RetryAfter`. Backoff then checks cancellation and starts its timer. Public A6 helpers synchronize on progress; the inspected layer tests cancel synchronously inside progress callbacks. Consequently, those tests permit cancellation before timer installation. The Programmatic state query does not acknowledge timer installation either.
- Recommendation: Establish that the delay wait has begun before cancellation, then retain the existing complete-cause, no-repeat, terminal-order, idle, and same-connection recovery assertions.
- Verification needed: Deterministic execution of cancellation during the installed wait. The dependency source selects `ctx.Done()` during its timer wait and stops the timer; no production interruption defect was established.

A6 continues to demonstrate its explicitly recorded progress-boundary cancellation and public recovery behavior. This finding prevents promoting that observation into deterministic installed-delay execution evidence.

## Acceptance matrix

"Source-supported" means that the reviewed implementation follows the clause on the inspected path and the cited tests contain relevant assertions. It does not extend an executed fixture to other providers, client modes, or scenarios. "Incomplete evidence" and "Unmet branch" remain open for phase acceptance. Across the 40 clauses, 25 are source-supported, 10 have incomplete evidence and five have unmet branches. Several clauses share the same findings; the five unmet clauses concern F23/F24.

| Parent clause | Review result | Main evidence and boundary |
| --- | --- | --- |
| ACC-01 | Incomplete evidence | Bundled automatic/manual/overflow continuation and journal tests; F19-F21 bound result failures, restored context and installed-delay evidence. |
| ACC-01.1 | Source-supported | Request participant input assertions compare immutable originals, initial equality, registration order, detached inputs, and accepted current state. |
| ACC-01.2 | Source-supported with current execution | Handler B derives a complete replacement from original; the next participant validates its current guidance through successful bundled composition. F18 is optional strengthening. |
| ACC-01.3 | Source-supported | Bundled supplied-result cases bypass zero/multiple generators. Ordered result state has reference-public and bundled component assertions. |
| ACC-01.4 | Source-supported | Standard/custom generator, clear, zero/multiple cardinality, final model/reasoning, and captured conversation-budget assertions. |
| ACC-01.5 | Incomplete evidence | Bundled request failures and Host rejection paths have assertions; F19 separates reference result-coordinator evidence. |
| ACC-01.6 | Source-supported | Explicit request/result Cancel, notification counts, zero markers, and nested-generation stopping/joining assertions. |
| ACC-01.7 | Source-supported | Prepared retention/default cases, complete tool groups, reordered/shared source spans, and boundary validation. |
| ACC-01.8 | Source-supported | Repeated generation receives the preceding summary once and excludes superseded original prefixes. |
| ACC-01.9 | Source-supported | Default-installed executable and bundled/custom participant/generator cases through public contracts. |
| ACC-01.10 | Incomplete evidence | Original-entry equality, atomic commit source trace, repeated projection and public navigation; F20 bounds complete restored-suffix assertions. |
| ACC-01.11 | Source-supported | Policy owns sizing, triggers, boundaries, generation and composition. Host owns capture/runtime/session mutation; Core consumes prepared history. |
| ACC-02 | Incomplete evidence | Programmatic, UI and Extension manual entry paths share services and guidance; F19 affects their common result-composition evidence. |
| ACC-03 | Incomplete evidence | Latest leaf/summary after owner restart and saved accounting are asserted; F20 concerns the retained suffix. |
| ACC-04 | Unmet branch | Existing schedule, override, delay and terminal tests remain valid within their fixtures; F23/F24 affect supported failure branches. |
| ACC-04.1 | Unmet branch | Public original/current retry assertions exist; F24 bypasses decision/handler dispatch for ownerless overflow. |
| ACC-04.2 | Unmet branch | Public nonretryable override and final allowance assertions exist; F24 bounds handler authority for context overflow. |
| ACC-04.3 | Unmet branch | Transient Cancel/failure and layer invalid-action assertions exist; F24 skips those actions for configured/no-owner overflow. |
| ACC-04.4 | Source-supported | Core invokes one neutral logical stream; retry decisions, handlers and scheduling remain Host-owned. |
| ACC-04.4.1 | Source-supported with current execution | Codex ordinary UI/Programmatic and configured-model SDK tests, together with compatible Responses ordinary/configured tests, execute both concrete drivers behind the same consumed interface and unchanged production callers. |
| ACC-04.5 | Unmet branch | Compatible Responses has four-client refusal/truncation assertions; F23 concerns Codex and F24 concerns overflow-handler categories. |
| ACC-04.6 | Source-supported | Configured retry sequence, attempt counts, actual arrival lower bounds, and disabled-policy no-handler/no-repeat assertions. |
| ACC-04.7 | Source-supported | Provider delays 10/60 seconds, default maximum 30 seconds, changed maxima, actual arrival lower bounds, and complete rejection text. |
| ACC-05 | Source-supported | UI/Programmatic retry fixture checks response reset, four stored entries, one stored tool result and repeated request bodies. Standard overflow additionally checks the actual completed-tool report. |
| ACC-06 | Incomplete evidence | Three A6 client/state cases assert terminal order, idle and retained-connection recovery. F21 limits installed-delay execution. |

The 12 standard-extension clauses have the following results. Identifiers in this table belong to [phs-06.2_prd.md](phs-06.2_prd.md).

| Standard clause | Review result and evidence |
| --- | --- |
| FRQ-01 | Source-supported: default-installed bundled executable and startup registration. |
| FRQ-02 | Source-supported: exact threshold, reported/missing/zero usage, model/reasoning changes, restart fallback, anchor detachment and isolation. |
| FRQ-03 | Incomplete evidence: all manual adapters share guidance and composition; F19 applies. |
| FRQ-05 | Source-supported: configured/default retention, legal source spans and complete tool groups, independent of response reserve. |
| FRQ-06 | Source-supported: preceding summary once, new prefix only, original journal retained. |
| FRQ-07 | Incomplete evidence: ordered composition and original-derived replacement have execution evidence; F19 concerns bundled result-failure assertions. |
| FRQ-08 | Source-supported: generator assertions cover 2,000 Unicode characters across text blocks, image omission, one marker, unchanged originals, one request, and unusable responses. |
| FRQ-09 | Source-supported: admitted standard recovery retains the logical attempt allowance and completed tools. F24 concerns handler participation where recovery is absent. |
| FRQ-10 | Incomplete evidence: precommit stopping, committed receipts and nested joins are traced; F19 concerns bundled result failures. |
| FRQ-11 | Source-supported: independent generator disablement, supplied-result bypass and explicit cardinality failure. |
| FRQ-12 | Source-supported: separate request pricing, zero/missing usage/pricing, repeated markers and saved costs after restart with pricing removed. |
| NRQ-01 | Source-supported asynchronous operation ownership and public interaction during blocked work; no measured latency guarantee is claimed. |

The prepared-history issue's FRQ-01 and FRQ-02 are source-supported by public source-exclusion, retention and transformation assertions plus shared projection/source-span tests. Its FRQ-03 has incomplete evidence for the restored suffix under F20; original-journal and branch-sensitive context evidence remains applicable. The issue remains pending full phase acceptance.

## Verification

The current verification ran on 2026-10-23 with Go `go1.27.2`, Linux amd64, against isolated Git-archive copies of the reviewed commit. Archive SHA-256: `e4ad88eb2bf4e8ca0cf5387f222c3a6bf615d7c9b7ecb87758dfda7361e39ed7`. Comparison against the canonical Git tree found 1,374 paths, zero missing paths, zero blob mismatches and zero unexpected files. A separate unit-check rerun preserved identical before/after manifests for all 1,374 files. The checks exercise the reviewed executable source; subsequent working-tree changes are Markdown only.

The table uses `$build_root` for isolated build output and `$scratch` for a temporary directory on the filesystem with available space. These variables replace ephemeral absolute directories; the other command arguments match execution. Environment assignments applied only to individual child commands.

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0. Fresh before/after source manifests contain 1,374 identical entries. |
| `task fix_dry_run` | Exit 0. Both output streams are empty; no fix proposal or `task fix` execution. |
| `task lint` | Exit 201. Code linters report zero issues and ifaceguard reports no errors; govulncheck exits 3 for the accepted GO-2026-6809 finding. |
| `env TMPDIR="$scratch" task test` | Exit 0 for root and external-plugin race unit tasks. The initial resource failure is recorded below. The task permits Go test caching. |
| `task itest` | Exit 0 for root and external-plugin race integration tasks. |
| `task test-coverage` | Exit 0. Combined root unit/integration coverage is 84.2%, required 80.0%; this task uses uncached runs. |
| `task build BUILD_ROOT="$build_root"` | Exit 0 for Host, tools, compaction and TUI executables. |
| `go list ./...` | Exit 0 in root and external-plugin modules. |
| `cd testdata/external-plugins && $(cd ../.. && go tool -n golangci-lint) run --config ../../.golangci.yml --build-tags=integration ./...` | Exit 0. |
| `task generate`, twice | Both exit 0. A fresh recheck has 96 generated files identical before and after each run separately. |
| `go test -json -count=1 -race -tags=integration -p 1 -parallel 1 ./...` | Exit 0 in each module. Root has seven named skips; external module has none. |
| Focused A6 race command below | Exit 0. The three named A6 scenarios execute five repetitions each. |
| `go tool golangci-lint run --config ./.golangci.yml --build-tags=integration ./host/internal/app` | Exit 0. |
| `git diff --check` in the user checkout | Exit 0 for the analytical documentation changes. |

Focused A6 race command:

```sh
go test -race -tags=integration -count=5 -p 1 -parallel 1 ./host/internal/app -run 'TestPublicAbortReleasesAgent|TestUIAbortPendingRetryReleasesAgent'
```

The uncached integration JSON contains passing root events and every descendant for `TestRetryAcrossUIAndProgrammaticModes`, `TestConfiguredModelSDKFailuresPreserveLogicalIdentity`, and `TestTerminalRefusalAcrossPublicClients`: respectively 3, 1 and 25 pass events, with zero failure or skip events. These counts include parent tests. Together, the suites establish ACC-04.4.1 dispatch for Codex and compatible Responses through unchanged ordinary/Core and configured-model callers. They do not execute F23's Codex truncation or F24's overflow-with-handler scenarios.

The same JSON records `TestStandardManualComposition` with its nine descendants, `TestStandardAutomaticExactThreshold`, and `TestStandardAutomaticFinalHistory` with its two descendants as passing without skips. Packages reporting no applicable test files are excluded from scenario passes: 55 root-module package records and two external-module package records.

| Skipped root integration test | Recorded platform requirement |
| --- | --- |
| `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Darwin permission enforcement for directory failure injection. |
| `TestLifecycleObserverThroughStandardTUI` | Real PTY acceptance on Darwin arm64. |
| `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Darwin `chflags` for immutable-file recovery injection. |
| `TestStandardTUIHostSmoke` | Real PTY acceptance on Darwin arm64. |
| `TestStandardTUIHostRuntimePersistenceFailures` | Real PTY acceptance on Darwin arm64. |
| `TestEarlyFailureCleanup` | Real PTY cleanup on Darwin arm64. |
| `TestStandardTUIPTY` | Real PTY acceptance on Darwin arm64. |

### Failed checks and recorder corrections

The initial `task test` exited 201 because the separate temporary filesystem ran out of space. Compiler/linker output and test `TempDir` failures contain `no space left on device`. The same archived source passed the rerun with per-command scratch space on another filesystem. No source, dependency or expected-result adjustment was made.

An attempted `git diff --check` inside the Git-less archive exited 129. The documentation check subsequently ran in the actual checkout and exited 0. Initial manifest helpers also attempted `git ls-files` inside the archive and failed. Canonical path/blob comparison outside the archive and fresh filesystem-based formatter/generation manifests replace those invalid recorder outputs.

The first skip-census parser had a quoting `SyntaxError` after the test commands wrote their JSON. Their captured Go exit codes were 0. A corrected standalone parser read the preserved JSON; passing test suites were not rerun solely for the parser error. The failed recorder attempts remain part of the verification history.

The only remaining mandatory-command failure is the explicitly accepted GO-2026-6809 exception. Passing checks do not exercise the missing conditions in F19-F21 and do not disprove the source findings F23/F24.

The earlier retained 17-command bundle records coverage of 84.2% against 80.0%, two stable generation passes, passing integration commands, and `task lint` exit 201 for GO-2026-6809. Its raw sources lack a verifiable whole-tree revision. All four retained 96-file generated manifests and the three A6 executable-file hashes match current files. Those matches establish correspondence only for the named files. Historical results are not reported as current whole-tree verification.

## Evidence boundaries and retained debt

Direct-import inspection counted 17 imports for Agent Core and 23 for Host model execution, with no concrete provider, provider SDK, plugin SDK or transport imports in those direct lists. Their full Go dependency closures include transport packages through consumer-contract and compile-time assertion imports. The [accepted architecture](../../architecture.md#import-rules), APC-13, permits those contract directions. The source review checked that Core's consumed model interface and Host's provider-attempt interface remain provider-neutral; it makes no claim that the compiled transitive closure is transport-free.

Parent NFQ-01 retains the original RED/GREEN obligations and scoped exceptions. This review introduces no implementation behavior or new test, so it supplies no new RED/GREEN claim. NFQ-02 is assessed against the provider-neutral interfaces and accepted import rules. NRQ-01 remains unmet because of the open findings, despite current passing checks under the known scanner exception.

- Public fixtures use production application, SDK, extension processes, storage and provider-adapter wiring with controlled provider HTTP transport. They establish no live-provider summary quality or tokenizer accuracy.
- The public history restart fixtures recreate application and plugin owners in the test process. They do not launch a fresh Host OS process.
- Fork and clone share inspected reconstruction/persistence paths. Compaction-specific tests use mocked repository ports. Real repository replacement/replay fixtures use `BranchSummaryEntry`. Public compacted fork/clone followed by provider-input inspection remains unobserved.
- Public compaction-specific navigation cases use `NO_SUMMARY`. Generic optional-summary and navigation replay tests provide separate evidence. Combining optional branch summary, compaction markers, restart and provider dispatch was not executed as one scenario.
- Standard overflow tests assert marker/tool counts, selected content and acquired causes. Some exact failure categories, intermediate model-entry cardinality, complete suffix ordering and post-cancellation idle are covered only by shared Host/reference tests or source tracing.
- Public A6 directly covers Programmatic active HTTP, Programmatic pending retry, and UI pending retry. Ordinary UI active HTTP, physical TUI Stop input, and abort after a completed tool remain matrix limits. The requirements do not demand every client/state combination.
- The original A4 test-first exception remains limited to `TestStandardPreparedRetention` and `TestStandardPreparedTransformations`. Later assertion-sensitivity controls do not reconstruct original RED chronology.
- GO-2026-6809 in `crypto/x509@go1.27.2` remains the sole accepted vulnerability exception. No package/toolchain update or scanner suppression is authorized by this review.
- The previously accepted parent-context storage debt and separate production-panic issue retain the scope recorded in [system acceptance](system-integration-evidence.md#remaining-limits-and-historical-failures).
- F17 remains report-only under its existing decision. This review did not inspect, reproduce, design or correct that fixture concern.

No new performance requirement was introduced. Reviewers noted that coherent capture traverses the original journal, but supplied no measured responsiveness failure or approved numerical threshold. A new large-journal benchmark is not required to decide the source findings in this report. Asynchronous structure and existing public blocked-operation assertions remain distinct from measured latency.

## Documentation changes

This review adds this whole-phase record and updates phase/issue status links. F22 is closed by correcting the stale PHS-06.2 statement that the prepared-history correction remains unimplemented. A3 remains accepted for its OpenAI-compatible Responses scope; F23 prevents treating that unit acceptance as complete Codex refusal coverage. Historical per-unit reports and their chronology remain intact.

Documentation verification used `git diff --check` and a Python link/anchor and clause-inventory checker invoked as `python3 -` across the 12 changed Markdown files. The whitespace check passed. A documentation-inventory check failed because its parser omitted checkbox-prefixed PRD bullets. After the parser was corrected, the check passed. The Python link checker found 443 local links and 176 anchor references, with zero missing paths or anchors. All 25 parent ACC clauses have one result row each; all 12 standard rows are present. Independent conclusion review also checked the three prepared-history clauses, leaving zero clauses without a result.

No commit or push is part of this review.

## Open questions

The required product behavior is specified. The outstanding decision is whether to authorize production corrections for F23/F24 and evidence corrections for F19-F21, or explicitly retain those limitations. This analytical workflow grants no such implementation authority and accepts no new debt.

Existing assembled suites supply ACC-04.4.1 dispatch evidence for both implemented providers and the named unchanged callers. A dedicated swap test would duplicate those paths. Phase completion remains blocked independently by F23 and F24.

## References

- [Ticket](ticket.md): parent requirements and all 25 ACC clauses.
- [Migration plan](migration-plan.md#phase-phs-064---independent-acceptance): phase exit criteria and accepted unit history.
- [Target solution](solution.md#target-ownership): ownership and integration boundaries.
- [Standard requirements](phs-06.2_prd.md) and [solution](phs-06.2_solution.md): standard policy and accepted generator limits.
- [Prepared-history requirements](../../../../issues/compaction-history-projection/prd.md) and [solution](../../../../issues/compaction-history-projection/solution.md): shared projection, source correspondence and retained journal.
- [A3](a3-terminal-failure-evidence.md), [A4.1](a4.1-run-history-evidence.md), [A4](a4-prepared-history-evidence.md), [A5](a5-retry-acceptance-evidence.md), and [A6](a6-abort-idle-evidence.md): earlier unit results, exceptions and historical evidence limits.
