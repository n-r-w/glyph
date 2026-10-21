# Completion-plan unit A4: Prepared-history implementation evidence

Status: A4 is accepted after code-quality, text-quality, and independent unit review under the two scoped exceptions below. Full PHS-06 acceptance remains pending. The project verification section retains the actual failed lint result.

## Scope and prerequisites

Completion-plan A4 implements the approved [history-source solution](../../../../issues/compaction-history-projection/solution.md), D1 and D2, and [FRQ-01 through FRQ-03](../../../../issues/compaction-history-projection/prd.md). It is distinct from PHS-06.2 slice A4, which implements admitted overflow recovery. The [migration plan](migration-plan.md#phase-phs-064---independent-acceptance) owns completion-plan progress.

The review baseline is `31b295c67b7b161265877e4fa473af7c652a8e44`. That commit contains accepted [A4.1 run-owned history accounting](a4.1-run-history-evidence.md). A4 preserves that accounting and the earlier accepted A3 terminal-refusal correction. In `host/internal/usecase/agent/run/service.go`, A4 changes only the `Service.ProjectHistory` return adaptation.

The extension retains policy ownership. Host retains coherent session capture, structural validation, operation-bound authority, concurrent-mutation rejection, durable replacement, source accounting, and receipt reporting. Generator source limits, the estimator formula, retention configuration, explicit Cancel semantics, retry policy, and dependencies are unchanged. No persistent provenance format or raw-source fallback is added.

## Source inventory

| Source | Implemented responsibility |
|---|---|
| `host/internal/usecase/agent/run/history.go`, `ProjectHistory` | Shared exclusion, result ordering, and skipped-result synthesis return caller-relative input positions alongside prepared content. Agent Core receives no session identities or compaction policy. |
| `host/internal/domain/session/prepared_history.go` | Source-associated prepared records contain original entry identity, original branch position, original kind, and the rendered-summary role. `PreparedHistory.Content` supplies detached content-only views. |
| `host/internal/usecase/host/sessions/entry_history.go`, `historyFromEntries`, `compactedHistoryFromEntries`, `projectSessionHistory` | Session conversion binds original records before shared projection. Synthetic results inherit the producing response's source. Rendered summaries inherit their marker's source. |
| `host/internal/usecase/host/sessions/manual_compaction.go`, `Service.CaptureCompaction` | One detached original-branch capture supplies both replacement authority and prepared source association. Restoration and navigation rebuild that association from original records. |
| `host/internal/controller/extension/compaction_history.go`, `compaction_history_source.go`, and request codecs | Public prepared records retain source correspondence, private byte counts, and extension-supplied estimates through composition. |
| `api/plugins/extension/v1/compaction.proto` and generated Go contract | Policy history and request prefixes/suffixes share `CompactionHistoryRecord`. `CompactionHistorySourceKind` identifies the original record kind. The parallel raw-entry contracts are removed. Ordinary observations, outgoing input, and candidate projections retain their content-only purposes. |
| `plugins/extension/compaction/internal/domain/compaction/entry.go`, `retention.go`, `request.go` | The bundled policy selects prepared content. Legal split points keep call/result groups intact and separate original source positions into a prefix and suffix. The rendered preceding summary contributes to complete-input estimation, while recent-history retention and the newly summarized prefix exclude it. |
| `plugins/extension/compaction/internal/transport/extensionwire` | Policy, request, observation, and candidate mappings use the prepared representation. Source metadata stays outside content-only usage-continuation comparisons. |
| `sdk/plugins/extension/v1/manual_policy_*test.go` and reference compaction fixtures | SDK input detachment and request/current/original composition carry source kinds. The reference policy selects the latest original user through captured source kind. It does not unconditionally reuse the preceding boundary. |

## Public assembly regressions

The public tests use production application assembly, extension executables, SDK transport, session persistence, and provider adapters. Controlled HTTP transport supplies provider responses. These tests inspect model input and operation outcomes; they do not measure live-model summary quality.

| Test | Observed boundary |
|---|---|
| `TestStandardCompactionExcludesFailedSource` | A streamed provider-failure response persists partial assistant text. A continuation and manual summary generation exclude that text. The original journal retains it. |
| `TestStandardPreparedTransformations` | Independent cases check that stored results precede an intervening message in generation source and that a missing result becomes a synthesized skipped result. Neither case contains excluded responses, so an exclusion assertion cannot mask these failures. |
| `TestStandardPreparedHistoryPublic` | Persisted interleaved and interrupted-call fixtures exercise summarized and retained legal spans, shared synthetic sources, original-record preservation, repeated summaries, restart, navigation before and after markers, and compaction after navigation. The default-installed case discovers the real bundled executable under the default data-directory catalog without an override. |
| `TestStandardPreparedRetention` | Manual, automatic, and admitted-overflow paths retain the older usable original user group. Failed and aborted responses contribute neither source text nor retained-history weight. A separate default-retention case uses the declared default configuration and retains the older large usable group. |
| `TestReferenceCompactionKeepsLatestOriginalUser` | Repeated reference-policy compaction advances to a newer original user and ignores later user-role extension messages and rendered summaries. |
| `TestStandardManualComposition` | Complete supplied results bypass absent or multiple generators. Clearing a supplied result enforces generator cardinality. A custom generator works with bundled generation disabled. Request/result Cancel cases persist no marker and observe one failure callback; successful persistence observes one success callback. |

The interrupted-call fixtures are arranged through the ordinary session persistence adapter while the application is closed, then loaded through public session resume. They contain a stored result across an intervening message, a second call without a stored result, and optional failed/aborted original responses. The compaction, generation, continuation, restart, and navigation operations use public Programmatic Control. The fixtures do not claim that the test executed the interrupted tools.

The composition fixture records each actual success/failure observer invocation in an append-only artifact. The tests compare the entire invocation sequence with one success or one failure. They also count persisted markers and provider requests. Overwriting a single outcome payload is not used as callback-cardinality evidence.

## Focused logic and contract evidence

- `TestProjectHistorySourceCorrespondence` checks caller positions for reordered stored results, synthesized results, duplicate message text, and excluded terminal responses.
- `TestCaptureCompactionBindsPreparedSources` checks source identities, original positions, original kinds, and the rendered-summary role in one session-owned capture. A synthetic result retains Model origin; extension messages and branch summaries remain distinguishable from original users.
- `TestSelectRequestPreparedSourceSpans` checks both summarization and retention of an interleaved original span. Several prepared messages can share a source identifier.
- `TestSelectRequestCountsPreviousSummaryOnce` checks complete-input estimation, recent-history selection, and the separately supplied preceding summary.
- `TestSourceMetadataStaysControlOnly` checks unchanged generation source and token estimates when source identity, position, kind, and supplied estimates change.
- `TestObservationContinuation/source-metadata` checks that changed source metadata preserves content-only usage-continuation matching.
- `TestPreparedSourceKindsRoundTrip` and `TestPreparedHistorySourceKinds` check all original source kinds through Host and bundled-extension codecs. Unknown-kind tests retain explicit request-validation failures.
- Runtime replacement round trips, participant-value detachment, and SDK scoped-input tests check metadata preservation and immutable original values. Existing public request/result suites retain generator bypass, cancellation exclusivity, complete causes, and producer/cardinality behavior.
- Existing session and Host authority tests retain invalid-boundary, concurrent-mutation, one-commit, durable receipt, and accounting coverage. The full suites include the accepted A3 and A4.1 regressions.

## Test-first chronology and sensitivity repeats

### Original source-selection RED

Before the first A4 production edit, this command compiled against `99dcf27596b90c882e58a660bcd048f2ee9bdf58` and exited 1:

```sh
go test -tags=integration ./host/internal/app -run '^TestStandardCompactionExcludesFailedSource$' -count=1 -timeout=90s
```

The generator request contained `discarded-assistant-source`, while the test required that discarded response text to remain excluded. The continuation request already excluded it. After the prepared-source migration, the same test passed.

Later supplementary baseline probes exposed the separate run-history indexing panic. That panic is recorded and resolved by [A4.1](a4.1-run-history-evidence.md). It is not source-selection or retained-weight RED evidence.

### Fresh accepted-baseline repeat

After A4.1 acceptance, the compatible public tests were copied into an isolated extraction of `31b295c67b7b161265877e4fa473af7c652a8e44`. No A4 production code was included. Each command ran separately:

```sh
go test -race -tags=integration -count=1 -p 1 -parallel 1 ./host/internal/app -run '^TestStandardCompactionExcludesFailedSource$'
go test -race -tags=integration -count=1 -p 1 -parallel 1 ./host/internal/app -run '^TestStandardPreparedRetention$'
go test -race -tags=integration -count=1 -p 1 -parallel 1 ./host/internal/app -run '^TestStandardPreparedTransformations$'
go test -race -tags=integration -count=1 -p 1 -parallel 1 ./host/internal/app -run '^TestReferenceCompactionKeepsLatestOriginalUser$'
```

| Test | Baseline result |
|---|---|
| Excluded failed source | Exit 1: generation source contained discarded assistant text. |
| Retention: manual, automatic, overflow | Exit 1 in all three cases: the selected boundary was `excluded-aborted`, rather than the older usable original user entry. |
| Ordered stored result | Exit 1: result position 4 did not precede intervening-message position 3. |
| Synthesized skipped result | Exit 1: no source result existed for the missing call; the search returned -1. |
| Reference latest-original-user selection | Exit 0: the original raw contract preserved the required policy semantics. |

All four commands compiled. None panicked. These later runs check assertion sensitivity against the accepted prerequisite baseline. The earlier failed-source run establishes the original test-first chronology for failed-source exclusion.

`TestStandardPreparedRetention` and `TestStandardPreparedTransformations` were added after the first A4 production edits. Their later baseline failures establish assertion sensitivity only. Code-quality review identified the original test-first deviation for retained weight, independent stored-result ordering, and skipped-result synthesis. Decision O14-1 accepts that deviation for these two tests and these three behaviors only. Original failed-source exclusion and source-kind correction RED remain separately valid. All other test-first obligations, including A5, A6, and subsequent behavior corrections, remain in effect.

### Original-kind correction

Before adding source-kind metadata, `TestReferenceCompactionKeepsLatestOriginalUser` compiled against the provisional migration and exited 1. The second compaction retained the previous user's boundary instead of the newly appended original user. After captured source kinds replaced unconditional preceding-boundary reuse, the test passed. The positive control above also passes against the original public contract.

New-contract compilation failures are excluded from behavioral RED evidence. Source-kind transport and control-only unit tests supplement the public semantic-preservation regression.

## Project verification

The pre-review run covers the implementation and test bytes after source-kind migration and fixture cleanup. Later text-review verification is recorded separately below.

| Command | Pre-review result |
|---|---|
| `task fmt` | Exit 0. |
| `task fix_dry_run` | Exit 0; no proposed changes, so no fix was applied. |
| `task generate`, twice | Exit 0 on both runs; all 72 generated protobuf/mock files were byte-identical. |
| `task lint` | Failed: shell exit 201; Task reports underlying exit status 3 from `govulncheck`. Preceding code linters report zero issues and ifaceguard reports no errors. |
| `task test` | Exit 0, including the external module. |
| `task itest` | Exit 0, including the external module. |
| `task test-coverage` | Exit 0; combined root-module coverage 84.2%, required 80.0%. |
| `task build` | Exit 0, with an isolated output root selected through `BUILD_ROOT`. |
| `go list ./...`, root and external module | Exit 0 in both modules. |
| `git diff --check` | Exit 0. |

Because `task lint` stops at the vulnerability scan, external-plugin lint was also run directly from its module with `go tool`'s declared golangci-lint executable, `--config ../../.golangci.yml --build-tags=integration ./...`. It exited 0 with zero issues.

Both pre-review generated-file manifests have SHA-256 `7ba1dc59152a6c0eb60141a22f343d72ef6bf36bcaf0993346cf0e42113eb116`. This compares file content, including generated contracts and mocks.

An earlier intermediate verification found two test assertions that compared source-associated snapshots directly with content-only outgoing input. After those assertions were migrated to `PreparedHistory.Content()`, the focused tests and final full integration/coverage runs passed. The intermediate failures are not the final check result.

### Text-review verification

Text-quality review corrected prose and comments only. The protobuf input `api/plugins/extension/v1/compaction.proto` was regenerated into `pkg/plugins/extension/v1/compaction.pb.go`. Comparing all 64 A4 Go files before and after text review found identical non-comment tokens, including test assertions and generated code. The protobuf declarations and field values also remained identical after removing line comments.

After the comment edits, both `task generate` runs exited 0. The 70-file protobuf/mock content manifests matched. `task fmt`, `task fix_dry_run`, direct external-plugin lint, `task test`, `task itest`, `task test-coverage`, and `git diff --check` exited 0. The fix dry run proposed no changes. Coverage remained 84.2%, with 80.0% required. `task lint` again failed with shell exit 201 and underlying `govulncheck` exit 3 for GO-2026-6809 alone. The preceding code linters reported zero issues and ifaceguard reported no errors.

The test tasks reused cached results where Go permitted; the coverage task ran uncached unit and integration tests. The pre-review build, package-loading results, and named-skip census below remain separate observations. Text review also checked 314 local Markdown links and anchors across nine documents; every target resolved. No executable correction or new RED run was required. Independent unit review followed this text pass, as recorded below. No commit was created during text review.

### Actual skips and evidence limits

Pre-review uncached integration censuses ran in each module:

```sh
go test -race -count=1 -tags=integration -p 1 -parallel 1 -json ./...
```

Both commands exited 0. Root output contains seven named test skips and 55 package-level no-test entries. External-module output contains zero named skips and two package-level no-test entries. These counts use fresh JSON events, separately from cached task output. A package-level no-test entry does not imply the package has no unit tests.

| Named skipped test | Reported condition |
|---|---|
| `host/internal/app`, `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Directory permission failure injection requires Darwin permission enforcement. |
| `host/internal/app`, `TestLifecycleObserverThroughStandardTUI` | Real PTY acceptance runs on Darwin arm64. |
| `host/internal/app`, `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Immutable-file recovery injection requires Darwin `chflags`. |
| `host/internal/app`, `TestStandardTUIHostSmoke` | Real PTY acceptance runs on Darwin arm64. |
| `host/internal/app`, `TestStandardTUIHostRuntimePersistenceFailures` | Real PTY acceptance runs on Darwin arm64. |
| `internal/testsupport/tui`, `TestEarlyFailureCleanup` | Real PTY cleanup runs on Darwin arm64. |
| `plugins/ui/tui/internal/app`, `TestStandardTUIPTY` | Real PTY acceptance runs on Darwin arm64. |

No A4 public regression was skipped.

### Finding outside A4

FND-01: `go.mod` declares Go 1.27.2. The required `govulncheck` scan reports GO-2026-6809 in `crypto/x509@go1.27.2` and names Go 1.27.3 as the fixed version. The same scan on the isolated accepted baseline `31b295c` exits 3 with the same finding. This blocks `task lint`; it is not introduced by the A4 diff. A4 changes no toolchain or dependency version and includes no scan suppression. The baseline comparison alone does not waive the failure. The user subsequently accepted GO-2026-6809 as an exception for the current development work and directed continuation on Go 1.27.2. The exception applies to this finding; all other mandatory checks and review requirements remain in effect.

## Review and remaining acceptance

Code-quality review found no executable-code defect and reported the original test-first deviation above. The two approved acceptance exceptions are separate:

- Decision O14-1 accepts only the [named original test-first deviation](#fresh-accepted-baseline-repeat).
- The development exception accepts only the [GO-2026-6809 finding on Go 1.27.2](#finding-outside-a4), with the failed lint result retained.

All other checks, test-first obligations, ownership boundaries, and review requirements remain in effect. Text-quality review is complete. Independent unit review accepts A4 under the two exceptions and the recorded Linux/platform evidence limits. It found no additional A4 defect or unmet A4 requirement.

The independent reviewer repeated uncached race-enabled shared-projector, session, controller, runtime, Host-context, bundled-extension, public-application, and SDK tests. All repeated commands exited 0. Public repeats cover source exclusion, independent transformations, retention, legal spans, reference-user selection, composition, UI/Extension manual entry points, unavailable generation, final outgoing history, and accepted A4.1 run completion. These focused repeats supplement the whole-project checks above.

A5 retry override/delay evidence, A6 pending-retry cancellation/public-idle evidence, and full PHS-06 acceptance remain pending. A4 includes no suppression, dependency upgrade, or scan bypass.

## Open Questions

None for A4 acceptance. A5, A6, and full PHS-06 acceptance remain subsequent work.

## Verification boundaries

Public regressions use controlled provider HTTP responses and real local application/plugin/persistence paths. Unit and mock-backed Host tests establish their own layer contracts. Live-provider summary quality, tokenizer accuracy, and physical terminal presentation are unobserved. The project verification section records the actual platform skips.
