# UNT-02 test-style follow-up evidence

## Scope and disposition

This separate follow-up corrects the three UNT-02 diagnostics identified by the supplementary non-integration lint configuration. The starting commit is `4ac49d0bc9e7f97146eb0061886b7fb3e3a25726`, after the later units. The original UNT-02 implementation commit is `da9455f9afa4832acb29972c468cb1f0d92b0748`.

Only `host/internal/usecase/host/modelexecution/ownerless_overflow_test.go` changes code:

- Wrap the scenario slice declaration across lines to satisfy `lll`.
- Replace `assert.ErrorIs` with `require.ErrorIs` for the handler failure cause.
- Replace `assert.ErrorContains` with `require.ErrorContains` for the missing replacement decision diagnostic.

All seven scenario values and their order remain unchanged: cancel, failure, invalid action, invalid decision, preserve, repeat, and disabled. Both ordinary and configured entry points remain covered. Assertion arguments, categories, error causes, provider-attempt expectations, ordered handler expectations, and progress expectations remain unchanged. The two error assertions now stop their subtest on failure, as required by `testifylint`; their predicates and expected values remain equivalent. No production behavior or asserted property changes. No refactor or additional cleanup was performed.

The other changed path is this new evidence document. Original UNT-02 evidence, earlier snapshots, other unit documents, public Codex regressions, and the installed-wait test remain untouched. No commit or push was made. Independent reviews and a separate corrective commit remain with the delivery owner. This record does not assert review approval or phase acceptance.

## Source identity

SHA-256 identities:

| Source | Before follow-up | Frozen checked source |
| --- | --- | --- |
| `host/internal/usecase/host/modelexecution/ownerless_overflow_test.go` | `0f4a21390ef73751d3878ccf9e7fe71fe9dd7594bd3dce0d78303628097328d5` | `4be1a31d7680139b2afe8a10c6327ac702ae134c859409e9642b5d5eda6234b9` |
| `host/internal/app/terminal_failure_modes_integration_test.go` | `6dc044588f54b4837f052420ce0d27e81f8f209e92aa3aee5c6b51e8b47be26d` | unchanged |
| `host/internal/usecase/host/modelexecution/retry_installed_wait_test.go` | `5612c9b564a0e1f8cad463bd83a59c43e2362cff0feb2c589246521321973ed2` | unchanged |
| Original `unt-02-evidence.md` in this directory | `1868a88c55c550629fa3bb5eb86d49d651749c55dfb74a6aac8dabbdb82946ef` | unchanged |

A full checkout manifest, including pre-existing untracked task documents, was captured before editing: digest `527e47d503b772a3475060f5e366352bf662ad4d8e0ea57612586f6563e7aa64`. Before and after the complete corrected-source check batch, both manifest digests were `cbe83770d78e17f1cd3368dc0bf4e2e680252365976dee9ced283df64b04d2b1`. All code edits preceded the batch. This evidence document was added afterward and is outside those batch manifests. Command records retain exact commands, working directory, UTC start/end, actual process exits, and full original outputs. Source copies and manifests are retained separately from earlier unit records.

## Original RED chronology

The original behavior-correction RED remains the record in [UNT-02 evidence](unt-02-evidence.md#compiling-red-before-production-correction). On 2026-10-23, the layer run executed from 01:34:48.687097 to 01:34:49.479694 UTC, exit 1. The public run executed from 01:34:49.498434 to 01:34:53.445014 UTC, exit 1. Both compiled and failed because ownerless overflow skipped retry handler authority. The original immutable source and diagnostic records remain original; this follow-up neither recreates nor relabels them.

The later constant-reuse correction and formatter equivalence remain documented in the original unit record. This follow-up adds only the scenario-list wrapping and two `assert`-to-`require` substitutions described above. No new original RED is required for this test-style correction because production behavior and asserted properties remain unchanged. Existing regressions supply current execution evidence below.

## Required readiness and focused regressions

All times are UTC on 2026-10-23. Commands ran from the repository root with child `TMPDIR=/var/tmp` directly. The external lint command resolves its binary in ROOT before changing to the external module.

| Command | Start | End | Actual result |
| --- | --- | --- | --- |
| `TMPDIR=/var/tmp task fmt` | 05:49:30.679215 | 05:49:36.298293 | exit 0; no further source changes |
| `TMPDIR=/var/tmp task fix_dry_run` | 05:49:36.317892 | 05:49:36.671796 | exit 0; empty proposals |
| `TMPDIR=/var/tmp task lint` | 05:49:49.691956 | 05:49:58.827554 | task exit 201; inner scanner exit 3, GO-2026-6809 only |
| Root-resolved external lint command below | 05:49:58.847375 | 05:49:59.305263 | exit 0 |
| `TMPDIR=/var/tmp task test` | 05:50:00.254479 | 05:50:03.598452 | exit 0; affected layer package executed, other passing package results cached |
| `TMPDIR=/var/tmp task itest` | 05:50:03.617303 | 05:50:12.597647 | exit 0; passing root and external package results cached |
| `TMPDIR=/var/tmp task test-coverage` | 05:50:12.616656 | 05:54:29.420561 | exit 0; uncached unit/integration runs, coverage 84.2%, required 80.0% |
| `TMPDIR=/var/tmp task build BUILD_ROOT=<temporary build directory>` | 05:54:29.439475 | 05:54:32.066698 | exit 0; Host and standard plugins built |
| `TMPDIR=/var/tmp go test -count=1 -race ./host/internal/usecase/host/modelexecution -v` | 05:54:32.085642 | 05:54:33.657724 | exit 0 |
| `TMPDIR=/var/tmp go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run TestTerminalRefusalAcrossPublicClients -v` | 05:54:33.676225 | 05:54:43.592742 | exit 0 |
| `git diff --check` | 05:55:15.276610 | 05:55:15.280482 | exit 0 |

The build command above is explicitly abbreviated: `<temporary build directory>` replaces the actual temporary BUILD_ROOT retained in the execution metadata. No repository output directory was used.

Exact external lint command:

```sh
LINT=$(go tool -n golangci-lint); cd testdata/external-plugins && TMPDIR=/var/tmp "$LINT" run --config ../../.golangci.yml --build-tags=integration ./...
```

The fix dry run had no proposals to apply; `task fix` was unnecessary. Main lint reported `0 issues.` and `ifaceguard: no errors found`, then stopped at the sole accepted GO-2026-6809 finding in `crypto/x509@go1.27.2`, fixed in go1.27.3. The scanner reported no other vulnerabilities. External lint therefore ran explicitly and passed. No suppression, dependency, or toolchain change was applied.

Material focused GREEN excerpts:

```text
--- PASS: TestServiceCancellationInterruptsInstalledRetryWait
--- PASS: TestServiceOwnerlessOverflowHandlers
--- PASS: TestServiceOwnerlessOverflowHandlers/invalid_action/ordinary
--- PASS: TestServiceOwnerlessOverflowHandlers/failure/configured
--- PASS: TestTerminalRefusalAcrossPublicClients/overflow_repeat_override/programmatic
--- PASS: TestTerminalRefusalAcrossPublicClients/overflow_repeat_override/configured_SDK
```

All 14 ownerless rows passed. The full layer package also executed the installed-wait test and captured-owner/disabled-retry regressions. The uncached public terminal run preserved and passed the Codex cases and all ownerless terminal rows. Neither focused run reported an individual skip. The original platform-specific integration skip inventory remains in the linked unit evidence; this follow-up does not claim those platform-limited scenarios executed or provide a fresh whole-suite skip inventory.

## Supplementary lint comparison

The exact requested supplementary command was:

```sh
TMPDIR=/var/tmp go tool golangci-lint run --config ./.golangci.yml ./host/internal/usecase/host/modelexecution/...
```

It ran from 05:49:59.324118 to 05:50:00.235496 UTC and exited 1 with 18 diagnostics. This is a failing supplementary result with a report-only disposition, not a clean supplementary-lint claim.

The preserved original 21-diagnostic output has SHA-256 `ffee9fab8309d97fe77c50fa311fdcfa10dbbcd7c850f299443ce6ce3385abb1`. Comparing diagnostic filename/rule multisets removed exactly one `lll` and two `testifylint` diagnostics from `ownerless_overflow_test.go`. No diagnostic was added. Neither `ownerless_overflow_test.go` nor `retry_installed_wait_test.go` appears in the new diagnostic set.

Remaining diagnostics, with paths relative to `host/internal/usecase/host/modelexecution/`:

| File | Rules and counts |
| --- | --- |
| `captured_policy_test.go` | unused: 1 |
| `context_preparation_test.go` | dupl: 1; errname: 1; gofumpt: 1 |
| `observation_test.go` | lll: 2 |
| `retry_cancellation_test.go` | dupl: 2; lll: 1; paralleltest: 1 |
| `retry_test.go` | dupl: 2; lll: 1; paralleltest: 1; testifylint: 2 |
| `service_test.go` | dupl: 2 |

Total: 18. A filename-only Git comparison from `7238582309a8abd2750898ac0c0d8bbc07e78a18`, the parent of UNT-01, to starting HEAD found no changes in these six files. This confirms that the remaining diagnostic files were unchanged by UNT-01 through UNT-05. Comparison was limited to diagnostic filenames/rules and file-change identity; their code was not investigated and no cleanup was designed.

## Remaining report-only disposition and handoff

When the supplementary non-integration configuration runs, 18 diagnostics remain in the six unchanged files listed above. The observed consequence is a nonzero supplementary lint exit. No runtime consequence was established. The implementation agent reports these 18 diagnostics under the outside-task reporting rule and the approved follow-up boundaries. This correction addresses only the three UNT-02 diagnostics. Report-only treatment constitutes neither specific user acceptance of the 18 diagnostics nor acceptance of new product debt. Further investigation or correction outside these boundaries requires separate authorization. No suppression or cleanup was applied. The main required lint exception remains solely GO-2026-6809.

No new outside-scope defect was investigated. No production change, public contract change, workaround, deprecation, F17/F18 work, or other unit-document edit was made. The source is frozen for the delivery owner's independent reviews and separate corrective commit. Required checks, the supplementary failure boundary, original RED provenance, and platform evidence limits are recorded separately rather than represented as unconditional acceptance.
