# UNT-06 assembled delivery evidence

## Result and scope

Independent acceptance-conclusion review approves source-supported closure of all 40 phase clauses in the [whole-phase acceptance matrix](../acceptance-report.md#acceptance-matrix) and correction FRQ-01 through FRQ-05/NRQ-01, within the recorded boundaries. No required gap remains in that review. The phase is delivery-ready on implementation and acceptance evidence. Final acceptance-conclusion and text reviews approved the corrected documentation, with both Minor findings resolved. The owner committed UNT-06 as `e7dd665cfc2dd50ab00182ad5be188dbe5619d27`, satisfying the unit's completion definition. Final user acceptance remains pending. At the first author handoff, the documents were uncommitted; the owner performed the commit after review.

The code baseline is `5375876f770dc3eb4a7b4010bea472ff55a31539`, Git tree `221491f013e9109d2d9682ff2d616be09c38c831`. The [plan](plan.md#plan-units) identifies the five independently approved corrective commits and the approved UNT-02 follow-up. Source, tests, protobufs, generated outputs, module files, task configuration, linter configuration and dependencies remain read-only during UNT-06. The author changed only the 12 authorized status documents, this delivery record, and the task planning documents needed for accurate review status and decisions.

At the first handoff, the eight planning documents were untracked: `baseline.md`, `decisions.md`, `facts.md`, `plan.md`, `problem.md`, `requirements.md`, `target-picture.md`, and `technical-solution.md` in this directory. Existing committed per-unit reports remain immutable. Together with the new UNT-06 record and 12 authorized status documents, those planning documents formed the 21-document closure scope. Commit `e7dd665cfc2dd50ab00182ad5be188dbe5619d27` includes all 21 documents. Documentation identities remain separate from the checked executable baseline.

## Correction requirement assessment

Local identifiers below belong to [requirements.md](requirements.md). The main phase report alone owns the 40 parent/standard/prepared-history assessments.

| Requirement | Author result | Specific verification and remaining boundary |
| --- | --- | --- |
| FRQ-01 | Behavior corrected; unit independently approved | [UNT-01 observations](unt-01-evidence.md#behavior-observations) and current `Driver.executeRequest` typed-refusal continuation. Public ordinary Programmatic and configured SDK Codex cases assert refusal plus EOF, category and one attempt per logical call. Driver tests assert immediate exits, acquired conversion/delivery/close causes and owning cancellation. Original RED snapshot remains absent. |
| FRQ-02 | Behavior corrected; unit and style follow-up independently approved | [UNT-02 observations](unt-02-evidence.md#green-and-behavioral-observations), `Service.execute` handler-before-admission order, and `TestServiceOwnerlessOverflowHandlers`. Fourteen layer rows and five ordinary/configured public outcomes check Original/Current, terminal stopping, full causes/categories, one attempt and zero progress. Disabled and captured-owner paths retain regression evidence. |
| FRQ-03 | Required evidence supplied; unit independently approved | [UNT-03 observations](unt-03-evidence.md#passing-observations-and-assertion-coverage): bundled public terminal cases compare accepted state and all stored entries. Coordinator mocks prohibit later-handler/Commit calls. Seven outcomes distinguish cancellation-containing failure from explicit Cancel. Three isolated controls fail at intended assertions, with no production correction. |
| FRQ-04 | Required evidence supplied; unit independently approved | [UNT-04 positive comparison](unt-04-evidence.md#positive-comparison-and-independent-expected-data): saved branch plus second receipt defines the full ordered suffix oracle after owner recreation. Omission and swap controls fail. The scenario recreates owners in-process, and summary-wrapper wording remains outside equality. |
| FRQ-05 | Required evidence supplied; unit independently approved | [UNT-05 synchronization](unt-05-evidence.md#deterministic-synchronization): durable wait and unchanged fake time precede cancellation. Assertions retain causes, empty response and one attempt. Public lifecycle tests separately check terminal order, idle and retained-connection recovery. The scheduler control fails; public progress itself acknowledges no timer. |
| NRQ-01 | Final acceptance-conclusion and text reviews approved; UNT-06 committed | All 40 phase rows have specific evidence and boundaries. The two behavior corrections retain original compiling failures and GREEN records; evidence-only additions retain separate controls. Current assembled checks, exact skips, exceptions, source identities and status changes appear below. Independent review approved the acceptance conclusion without a required gap. Final text approval and the actual UNT-06 documentation commit complete those procedural conditions. Final user acceptance remains outstanding. |

Requirement checkboxes mean separate user confirmation. Autonomous intermediate stages did not request that confirmation. Verification results above therefore do not change the unchecked boxes or invent requirement approval.

## Assembled verification

The collector ran checks on 2026-10-23 with child `TMPDIR=/var/tmp` directly. The author read primary command metadata, outputs, manifests and JSON events. The preserved records contain the per-command metadata and source manifests described below. Independent acceptance-conclusion review checked their correspondence with the corrected executable baseline and approved the bounded conclusions. No full project check was rerun for UNT-06's documentation-only edits.

Each command record contains actual command, cwd, UTC start/end, process exit, HEAD before/after and source manifests before/after. HEAD stayed at the code baseline in every record. Individual source manifests are equal before/after except record 11, whose ignored build outputs changed. The author inspected that difference separately from canonical nonignored source stability.

| Record | Command | UTC interval | Actual result |
| --- | --- | --- | --- |
| 01 | `task fmt` | 06:32:19Z to 06:32:27Z | 0; both output streams empty and source manifest unchanged. |
| 02 | `task fix_dry_run` | 06:32:34Z to 06:32:37Z | 0; both streams empty and source unchanged. No proposal required `task fix` or manual correction. |
| 03 | `task generate` | 06:32:42Z to 06:32:55Z | 0; source manifest unchanged. |
| 04 | `task generate` | 06:32:58Z to 06:33:11Z | 0; source manifest unchanged again. |
| 05 | `task lint` | 06:33:23Z to 06:33:32Z | Task exit 201, inner scanner exit 3 for sole accepted GO-2026-6809. Preceding code lint: `0 issues.`; ifaceguard: `no errors found`. |
| 06 | External lint command below | 06:33:38Z to 06:33:40Z | 0; root-resolved tool, external cwd and integration tag. |
| 07 | `go tool golangci-lint run --config ./.golangci.yml ./host/internal/usecase/host/modelexecution/...` | 06:33:44Z to 06:33:46Z | 1; exactly 18 outside-task diagnostics, detailed below. |
| 08 | `task test` | 06:33:51Z to 06:33:54Z | 0; root/external unit tasks, cached results present. |
| 09 | `task itest` | 06:33:57Z to 06:40:23Z | 0; root/external integration tasks. Task commands permit caching. |
| 10 | `task test-coverage` | 06:40:28Z to 06:44:41Z | 0; `Coverage: 84.2%, required: 80.0%`. |
| 11 | `env BUILD_ROOT= task build` | 06:44:45Z to 06:44:49Z | 0; recorder argument expanded empty, ignored default checkout outputs. |
| 12 | `go test -count=1 -race -json -tags=integration -p 1 -parallel 1 ./...` | 06:44:53Z to 06:52:09Z | 0; root uncached integration events. |
| 13 | Same integration command in `testdata/external-plugins` | 06:52:14Z to 06:52:22Z | 0; external uncached integration events. |
| 14 | `go test -count=1 -race -json ./...` | 06:52:26Z to 06:52:32Z | 0; root uncached unit events. |
| 15 | Same unit command in `testdata/external-plugins` | 06:52:36Z to 06:52:37Z | 0; external compilation/no-test package events. |
| 16 | `git diff --check` | 06:52:42Z to 06:52:44Z | 0 before documentation authoring. |
| 17 | `env BUILD_ROOT=<literal temporary output path> task build` | 06:53:27Z to 06:53:31Z | 0; authoritative second build. The placeholder abbreviates only the preserved literal path. |
| 18 | `git diff --check` | 06:53:36Z to 06:53:38Z | 0 before documentation authoring. |

The external lint invocation used the root's resolved `go tool -n golangci-lint` binary, then executed `run --config ../../.golangci.yml --build-tags=integration ./...` in `testdata/external-plugins`. Main lint stopped at the scanner before its external-module step, so record 06 supplies that check. The commands above explicitly abbreviate only the machine-local resolved binary and temporary build paths. Record 06 identifies the external working directory and declared lint arguments; record 17 identifies the literal temporary output argument. The output hashes and source-comparison scope appear below.

The required scanner reports GO-2026-6809 in `crypto/x509@go1.27.2`, fixed in Go 1.27.3, with no other vulnerability. The [baseline](baseline.md) and [approved A4 exception](../a4-prepared-history-evidence.md#finding-outside-a4) retain the same failure before this delivery. No scanner suppression, version change or second required-check exception was introduced.

### Source identity and generation

The canonical scope is tracked paths plus nonignored untracked paths from Git enumeration. It excludes `.git`, ignored files, external raw records and temporary build roots. The canonical comparison contains 1,395 paths and zero missing paths before and after. Both manifest SHA-256 values are:

```text
f806f36960952cd2e3809b6d63934d12dc4f8007b801c90ce65cff3ae9e9ee33
```

The supplement derives those manifests specifically from preserved full working-file manifests for authoritative second build record 17. They include all eight untracked planning documents and the ownerless-overflow test. This digest is evidence for that second-build comparison only. Each other command retains its own manifests, and records 03/04 separately establish two nonmodifying generation runs. Neither HEAD alone nor this one digest establishes uninterrupted historical provenance.

Documentation authoring follows those checks. Consequently, executable check results apply to the named code baseline, while documentation validation applies to the separately revised 21-document scope. Source/test/configuration identity remains tied to the corrected code baseline; no check result is attributed to future committed documentation bytes.

### Recorder correction

The initial recorder invocation assigned BUILD_ROOT for the child recorder process while expanding a later `$BUILD_ROOT` argument in the caller shell. The caller variable was empty, so record 11 captured `env BUILD_ROOT= task build`. Task's default selected `bin`, producing Host, tools, compaction and TUI binaries in ignored checkout output paths. The product build exited 0. The record is preserved with its changed full working-file manifest.

The collector then passed a literal temporary BUILD_ROOT directly through `env` in record 17. All four outputs exist, and their byte counts/hashes match the first outputs:

| Output relative to build root | Bytes | SHA-256 |
| --- | ---: | --- |
| `glyph` | 54105311 | `f8feeeb743e02bb9c30e7dde36595e934e4815964d100ce2f7846658cc01db30` |
| `plugins/extension/glyph-tools` | 24620926 | `2c8ef2d047331a0af88674cdc506a7b8f614322ffdf39d945ef6c980dfe29222` |
| `plugins/extension/glyph-compaction` | 23654881 | `e8bfe54905e1baeb693149c4d2ad7e069ae4d01895b7d41db07e4075f608938b` |
| `plugins/ui/glyph-tui` | 27408247 | `0fe163953c989136b381e69e253d83791a08c7f659484ea87ae185821cecc3ba` |

This correction addresses verification recording within authorized scope. Its consequence is MEDIUM for acceptance reasoning because the intended build-output isolation was absent from the first command. Actual command metadata and output hashes establish the two successful builds; no failed product build is inferred. The options were to retain only the unintended-output record or preserve it and add a correctly addressed temporary-root build. The collector chose the latter within verification authority, recorded as [D8](decisions.md#d8-preserve-and-correct-the-assembled-build-recorder). No product decision or source change was needed.

### Uncached event inventory and exact skips

The author parsed raw JSON rather than adding overlapping unit-suite counts. Test pass events include parent tests and descendants; these are event counts rather than unique scenarios. Package-level no-test skips remain separate.

| Run | Test pass events | Test failure events | Named test skips | No-test packages |
| --- | ---: | ---: | ---: | ---: |
| Root integration, record 12 | 1551 | 0 | 7 | 55 |
| External integration, record 13 | 4 | 0 | 0 | 2 |
| Root unit, record 14 | 2067 | 0 | 0 | 35 |
| External unit, record 15 | 0 | 0 | 0 | 3 |

The seven runtime reasons below come from each skipped test's own preceding `Action=output` event in record 12. Supplemental byte comparison reports `byte_equal=yes` for each reason. They are unexecuted scenarios, not successful acceptance cases.

| Package relative to module root | Test | Exact runtime reason |
| --- | --- | --- |
| `host/internal/app` | `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | `directory permission failure injection requires Darwin permission enforcement` |
| `host/internal/app` | `TestLifecycleObserverThroughStandardTUI` | `real PTY acceptance runs on Darwin arm64` |
| `host/internal/app` | `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | `immutable-file recovery injection requires Darwin chflags` |
| `host/internal/app` | `TestStandardTUIHostRuntimePersistenceFailures` | `real PTY acceptance runs on Darwin arm64` |
| `host/internal/app` | `TestStandardTUIHostSmoke` | `real PTY acceptance runs on Darwin arm64` |
| `internal/testsupport/tui` | `TestEarlyFailureCleanup` | `real PTY cleanup runs on Darwin arm64` |
| `plugins/ui/tui/internal/app` | `TestStandardTUIPTY` | `real PTY acceptance runs on Darwin arm64` |

## Chronology and historical records

The committed per-unit reports describe their own handoff state. Their uncommitted/pending-review statements remain accurate historical statements even though the delivery owner later approved and committed those units. The [plan's unit table](plan.md#plan-units) supplies the later commit/review state. This record does not rewrite the earlier reports or extend their recorder coverage.

- UNT-01 retains original compiling driver/public RED output and the implementation account of before-fix ordering. No immutable original RED source snapshot was retained. Later constants, comments and assertion refinements do not identify the original RED test bytes. Current GREEN and source identity do not reconstruct them.
- UNT-02 retains immutable original RED source and separate compiling layer/public failures before production correction. Its later constants/formatting and test-style follow-up preserve predicates without claiming original byte identity. The three delivery-owned lint diagnostics are fixed in `5375876f770dc3eb4a7b4010bea472ff55a31539`.
- UNT-03/04/05 evidence-only additions first pass unchanged production. Their isolated mutation failures are assertion-sensitivity controls, not original behavior RED. Each record distinguishes its baseline, mutant and restored observations.
- UNT-04 retains lost early logs, invalid initial empty manifests, incomplete focused-run provenance, large-value diagnostic rendering limits and a stale aggregate entry for a descriptive recorder file. Retained replacement observations support the specified assertions; they do not repair the historical handling or establish an uninterrupted original recorder chain.
- UNT-05 retains its interrupted first coverage attempt without a recorded exit. A separate completed rerun supplies passing coverage. Fresh assembled coverage adds current evidence and does not turn the interrupted attempt into a pass.
- The original A4 test-first exception and accepted development vulnerability remain scoped as in [A4 acceptance](../a4-prepared-history-evidence.md#review-and-remaining-acceptance). Historical system recorder failures and parent-context debt remain in [system acceptance](../system-integration-evidence.md#remaining-limits-and-historical-failures).

## Outside-task deviation: 18 supplementary diagnostics

When developers run `go tool golangci-lint run --config ./.golangci.yml ./host/internal/usecase/host/modelexecution/...` without the integration tag, the command exits 1 with 18 diagnostics. The current filename/rule multiset equals the retained UNT-02 follow-up multiset. Neither `ownerless_overflow_test.go` nor `retry_installed_wait_test.go` appears. The six diagnostic files remain unchanged from original delivery baseline `7238582309a8abd2750898ac0c0d8bbc07e78a18` to the corrected code baseline.

| File relative to `host/internal/usecase/host/modelexecution/` | Rules and counts |
| --- | --- |
| `captured_policy_test.go` | unused: 1 |
| `context_preparation_test.go` | dupl: 1; errname: 1; gofumpt: 1 |
| `observation_test.go` | lll: 2 |
| `retry_cancellation_test.go` | dupl: 2; lll: 1; paralleltest: 1 |
| `retry_test.go` | dupl: 2; lll: 1; paralleltest: 1; testifylint: 2 |
| `service_test.go` | dupl: 2 |

Consequences: MEDIUM developer-verification impact, because this supplementary configuration remains failing. Reliability: preserved current/follow-up lint outputs establish the 18-entry equality, and Git file identity establishes known source origin outside this delivery. Runtime effect remains unestablished. The author did not investigate, reproduce or design cleanup for those files.

Assignment boundary: cleanup lies outside UNT-06 and the authorized corrective delivery. Options are report the observed failure with no code change, or obtain separately authorized investigation/cleanup. Cleanup would change the six existing test files and require its own verification. The user owns authorization of that option. The author takes the defect-outside-task reporting exit and changes no source. This is report-only under the outside-task rule and general delivery boundaries, not specific user acceptance of the 18 diagnostics or acceptance of new product debt. Required `task lint` retains solely the GO-2026-6809 scanner exception. See [D7](decisions.md#d7-delivery-owned-lint-correction-and-outside-task-reporting).

## Documentation validation

The author used a temporary read-only Python checker and ran `git diff --check` after authoring. Both exited 0. The Python checker resolved local Markdown targets, generated heading anchors, checked inbound references, compared clause identifiers with their owning requirements and checked whitespace and path authorization. The checker inspected all 21 closure documents: 12 authorized status documents, eight planning documents and this new record. After the Minor text corrections and approved-conclusion status updates, it found 578 local links, 261 anchor references and 256 inbound anchor references from the documentation tree, with zero missing paths or anchors. It found zero trailing-whitespace, missing-final-newline or U+2014 issues. After final approval and commit-status finalization, the author repeated the read-only checks across the same 21 documents with the same counts and zero defects. The 40 assessment rows remain byte-identical to documentation commit `e7dd665cfc2dd50ab00182ad5be188dbe5619d27`; all 1,279 non-document source paths retain their checked identities.

Exact inventory comparison against the owning requirements found 25 parent rows, 12 standard rows and three prepared-history rows, each once and each with a Source-supported result. The checker found zero unauthorized changed/untracked paths. All six committed unit/follow-up evidence documents remain byte-identical to the code baseline. Comparison with the record-17 canonical manifest found all 1,279 non-document paths byte-identical; the 17 differing existing paths are authorized documentation edits. The new delivery record is the only added path beyond that prior canonical scope. The author also verified the four authoritative build-output hashes and the exact 18-entry current/follow-up diagnostic multiset equality. Git comparison found no change in any of the six supplementary-diagnostic files between original delivery baseline and corrected code baseline.

The author separately read the status diff and the assembled record to check meaning: all five correction subjects have dispositions; the 40-row matrix preserves material unexecuted scenarios; requirement checkboxes retain confirmation semantics; status text distinguishes completed implementation, final independent conclusion/text approvals and the actual UNT-06 commit from pending final user acceptance. Existing headings used by inbound links remain valid. These author checks do not pre-approve the procedural metadata follow-up. The executable-source comparison, command results, source hashes and their provenance boundaries remain stated above.

## Review outcomes and text corrections

Independent acceptance-conclusion review approved all 40 phase clauses and correction FRQ-01 through FRQ-05/NRQ-01 with no required gap. The reviewer preserved the recorded evidence boundaries and accepted no new debt or product limitation. This verdict supplies acceptance-conclusion approval only.

Separate text review returned Approve with changes for two Minor findings. FND-01 is corrected by prominent historical-baseline notes in [problem.md](problem.md) and [facts.md](facts.md). Both notes identify revision `7238582309a8abd2750898ac0c0d8bbc07e78a18`, preserve the initial observations/forecasts and source limits, distinguish completed unchanged-source [baseline reruns](baseline.md#results), and link to later correction dispositions and unit status.

FND-02 is corrected by removing temporary checker paths, desk identifiers and unresolved external handoff dependencies from this record. The author retained the actual checker method, 21-document scope, results, executable baseline, command metadata, hashes and provenance limits inline. The corrections change prose only. Final text review approved the corrected record, including removal of the remaining temporary lint-artifact phrase. Both Minor findings are resolved. The final precommit record had SHA-256 `d2f6c82b32a50ef720a99d72aa0a55a981bd86f995a3245c9d0511149222806f`; commit `e7dd665cfc2dd50ab00182ad5be188dbe5619d27` contains those bytes. Final acceptance-conclusion review also approved that final version. This status-only follow-up records those actual events and claims no approval or commit identity for the follow-up's revised metadata. Final user acceptance remains pending.

## Remaining steps and decisions

No new technical approach, architecture, API, journal, dependency, generator limit, product limitation, workaround or deprecation was introduced. Existing evidence boundaries remain in the [whole-phase report](../acceptance-report.md#evidence-boundaries-and-retained-debt). F17 remains absolutely excluded; optional F18 strengthening remains excluded.

Research, design, planning and this documentation implementation proceeded without separate user confirmation under final_only. Their transition gates required no confirmation. The author does not infer user acceptance from autonomous progression or from unchecked requirements. Independent acceptance-conclusion review is approved. Final text approval and commit `e7dd665cfc2dd50ab00182ad5be188dbe5619d27` complete UNT-06. Review of this procedural metadata follow-up remains separate. Final user review remains pending. The user can accept the assembled result or request corrections after review; this record chooses neither result.
