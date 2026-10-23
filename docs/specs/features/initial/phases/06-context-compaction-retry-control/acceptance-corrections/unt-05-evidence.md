# UNT-05 installed retry-wait cancellation evidence

## Result and scope

[FRQ-05](requirements.md#functional-requirements), [A5](target-picture.md#acceptance-scenarios), and [D5](decisions.md#phs-06-correction-decisions) have complementary deterministic layer and public lifecycle observations. The uncommitted change adds `host/internal/usecase/host/modelexecution/retry_installed_wait_test.go`, with `TestServiceCancellationInterruptsInstalledRetryWait`, and this report. Production code and the existing cancellation tests remain unchanged. No commit, dependency, toolchain, API, journal, architecture, or production injection seam change forms part of UNT-05. F17 and F18 were excluded. Acceptance of the revised evidence and user acceptance remain pending; this report claims neither approval nor whole-phase completion.

## Deterministic synchronization

The test calls `Service.RequestConfigured` from a worker inside `testing/synctest.Test`. The generated catalog and provider mocks return synchronously. The provider returns a transient failure. Retry policy allows one retry after one hour. The progress callback sends to a bubble-local channel with capacity one and returns nil. The controlling goroutine receives that signal and calls `synctest.Wait`.

The progress callback has only one possible invocation under this policy. Its first send fits in the channel even before the receiver runs. The callback neither cancels nor waits for permission from the test. After it returns, `Service.execute` returns `backoff.RetryAfter` to declared backoff v7.0.0. `backoff.Retry` starts the timer and selects on its timer channel and the owning context. Between the callback and that select, the worker has no other durable blocking operation. The worker closes the completion channel only after the request returns; it never waits on that channel. Together, durable blocking, the unfinished completion channel, and unchanged fake time establish that cancellation occurs during the installed production wait.

Before cancellation the controller checks one provider attempt, unfinished response, and the original fake time. It then cancels with the custom cause `owning operation stopped` and joins the worker. The final assertions check zero fake elapsed time, exactly one provider attempt, both provider and cancellation identities via `ErrorIs`, their full cause strings, current-cancellation identity via `errtree.IsCurrentCancellation`, absent outcome, and an empty `model.Response`.

The repeated generated-mock response uses an explicit attempt counter. This permits the sensitivity control to finish and report a forbidden repeat, rather than fail on an unexpected mock call. With the canceled original context captured by the production operation, a second attempt terminates through the existing concurrent-failure branch. There is no wall-clock sleep, timing estimate, test gate in the worker, or public timer acknowledgement.

Existing pre-wait callback-cancellation and concurrent-provider-cancellation tests remain intact. Those tests exercise different cancellation boundaries.

## Public lifecycle boundary

The uncached race-enabled integration run executed both `TestPublicAbortReleasesAgent` subcases, `pending-retry=true` and `pending-retry=false`, and `TestUIAbortPendingRetryReleasesAgent`. All passed.

In `abort_idle_integration_test.go`, `waitForAbortCompletion` requires target cancellation before cancellation-operation completion, preserves provider text for pending retry, and rejects competing target terminals. The test then observes public idle, an empty active operation ID, and successful recovery on the retained stream. It checks one target attempt and one recovery attempt.

In `abort_idle_ui_integration_test.go`, `runAbortRetryUIFixture` checks a cancellation result containing provider diagnostics, canceled target state, target-terminal ordering and cardinality, public idle, and a successful second submit through the retained UI Host connection. The parent checks one target attempt, one recovery attempt, and the fixture receipt.

Public retry progress synchronizes lifecycle cancellation only. It does not acknowledge installation of the backoff timer. The deterministic model-execution test supplies that observation, while the public tests supply terminal ordering, idle, and retained-connection recovery. This is the complementary evidence permitted by A5.

## Source and sensitivity identities

The acceptance-corrections delivery baseline is `7238582309a8abd2750898ac0c0d8bbc07e78a18`. The UNT-05 unit baseline is `d769d3004619c7b31d2669d49642ea78d75011eb`, after UNT-01 through UNT-04.

SHA-256 identities:

| Source | SHA-256 |
| --- | --- |
| Initial compiling test before formatter | `a14d10913bb014cd70511a864940f3d28f15f1a1514cd333418f07cbe8f351dc` |
| Final formatted test | `5612c9b564a0e1f8cad463bd83a59c43e2362cff0feb2c589246521321973ed2` |
| Original and restored `modelexecution/retry.go` | `6d6afd706cd731216df2fb6191092b4721b6ad080a7f28249e2546687308488a` |
| Isolated mutant `modelexecution/retry.go` | `c497185554b506641b76d346f2975fa03d5685d0cc4eba41f90eff56ef00b065` |

The initial test compiled and passed unchanged production before formatting. A temporary source copy supplied an isolated 0/1/0 baseline, mutant, restoration sequence. After formatting, the control sequence was repeated with the final test bytes. Both sequences passed baseline and restoration and failed the mutant at runtime.

The final baseline and restored CONTROL manifests both have SHA-256 `493aa7928614ace2673cb14223be5f9fcb7d1327d44223d7828c1b782e0ac2d0`. This is also the ROOT pre-evidence manifest identity for the full checks after formatting and before this document was added. EXTERNAL records cover only that invocation's subdirectory; their manifest identity is `0c84ff7e48ccc22f025d9ad3a6b7474b6eaf22969457f3dab506f18d76045ef2`. The final mutant CONTROL manifest is `3d378fd328f5e345b486dfa09f2b4fdded69e7a4f67d7d1cdc008f5a96b136e1`. The immutable final mutant archive is `b3f3517856166d442270f506c7fd6b53461e280290b87ae85b3add59b7a9a72b`.

The complete mutation is:

```diff
- response, retryErr := backoff.Retry(ctx, func() (model.Response, error) {
+ response, retryErr := backoff.Retry(context.WithoutCancel(ctx), func() (model.Response, error) {
```

Only the retry scheduler receives the cancellation-free context. The provider operation still captures the original owning context. Immutable control archives retain the whole mutant source, the exact mutant file, and the diff. Each execution has contemporaneous before/after source manifests including the new test. The final restored source manifest equals the final baseline manifest byte for byte. The user checkout never contained the mutation.

Material final-control output:

```text
baseline: PASS, exit 0
mutant: expected 0s, actual 1h0m0s
        owning cancellation must interrupt the retry delay
mutant: expected 1, actual 2
        owning cancellation must prevent a provider repeat
mutant: IsCurrentCancellation assertion: Should be true
        FAIL, exit 1
restored: PASS, exit 0
```

The mutant compiled and finished. The test's elapsed-time assertion reported one hour of fake elapsed time, its attempt-count assertion reported two provider attempts, and its current-cancellation assertion failed. Both cause assertions passed. This is assertion-sensitivity evidence for existing behavior, rather than original behavior-correction RED chronology.

## Verification execution record

All times below are UTC on 2026-10-23. `ROOT` means the repository checkout at the UNT-05 baseline plus the new test and pre-existing untracked task documents; `EXTERNAL` means `ROOT/testdata/external-plugins`. `CONTROL` means the isolated complete temporary source copy. Commands below are literal except for the explicitly abbreviated temporary `BUILD_ROOT` argument. Each completed invocation records cwd, full command, start/end, exit, output, before/after source manifests, and checksums generated after metadata was written.

For each invocation, the recorder requests tracked and nonignored untracked files with `git ls-files --cached --others --exclude-standard -z` in that invocation's cwd. It sorts and deduplicates the returned paths and hashes regular files relative to that cwd. If Git returns no usable file list, the recorder enumerates files beneath the cwd and excludes `.git`. ROOT manifests therefore cover the checkout's selected files, including the new test and pre-existing untracked task documents. EXTERNAL manifests cover the selected external-plugin subtree, rather than the complete ROOT tree. CONTROL manifests cover files in the isolated source copy. The pre-evidence manifests omit this document because it had yet to be created.

The formatter changed only the new test's struct-literal layout. No source changes followed `task fmt`. `task fix_dry_run` emitted no proposals, so neither `task fix` nor manual corrections were necessary. Standard task test commands may use Go's result cache; separate focused and integration-event commands below are uncached.

| Record | Cwd | Command | Start | End | Exit |
| --- | --- | --- | --- | --- | --- |
| initial | ROOT | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:03:44.220435 | 05:03:46.994581 | 0 |
| control-baseline | CONTROL | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:04:01.296293 | 05:04:11.750069 | 0 |
| control-mutant | CONTROL | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:04:22.188863 | 05:04:23.988576 | 1 |
| control-restored | CONTROL | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:04:33.003434 | 05:04:34.551589 | 0 |
| fmt | ROOT | `TMPDIR=/var/tmp task fmt` | 05:04:50.485437 | 05:04:56.087603 | 0 |
| fix-dry-run | ROOT | `TMPDIR=/var/tmp task fix_dry_run` | 05:04:56.153782 | 05:04:56.509660 | 0 |
| lint | ROOT | `TMPDIR=/var/tmp task lint` | 05:05:01.753325 | 05:05:10.912066 | 201 |
| external-lint | EXTERNAL | `TMPDIR=/var/tmp $(cd ../.. && go tool -n golangci-lint) run --config ../../.golangci.yml --build-tags=integration ./...` | 05:05:16.478688 | 05:05:16.944411 | 0 |
| unit-lint | ROOT | `TMPDIR=/var/tmp go tool golangci-lint run --config ./.golangci.yml ./host/internal/usecase/host/modelexecution/...` | 05:05:16.989935 | 05:05:17.991580 | 1 |
| public | ROOT | `TMPDIR=/var/tmp go test -race -count=1 -tags=integration -p 1 -parallel 1 -run "^(TestPublicAbortReleasesAgent\|TestUIAbortPendingRetryReleasesAgent)$" -v ./host/internal/app` | 05:05:18.057745 | 05:05:22.279028 | 0 |
| test | ROOT | `TMPDIR=/var/tmp task test` | 05:05:33.292684 | 05:05:36.594475 | 0 |
| itest | ROOT | `TMPDIR=/var/tmp task itest` | 05:05:36.662079 | 05:05:45.574027 | 0 |
| coverage | ROOT | `TMPDIR=/var/tmp task test-coverage` | 05:05:50.740337 | interrupted at runner limit | unrecorded |
| coverage-complete | ROOT | `TMPDIR=/var/tmp task test-coverage` | 05:08:02.051832 | 05:12:17.841542 | 0 |
| build | ROOT | `TMPDIR=/var/tmp task build BUILD_ROOT=<temporary BUILD_ROOT>` | 05:12:17.910021 | 05:12:20.576784 | 0 |
| integration-events | ROOT | `TMPDIR=/var/tmp go test -json -race -count=1 -tags=integration -p 1 -parallel 1 ./...` | 05:12:28.639019 | 05:19:47.283834 | 0 |
| external-events | EXTERNAL | `TMPDIR=/var/tmp go test -json -race -count=1 -tags=integration -p 1 -parallel 1 ./...` | 05:19:47.334699 | 05:19:54.485495 | 0 |
| final-control-baseline | CONTROL | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:20:13.476396 | 05:20:16.264903 | 0 |
| final-control-mutant | CONTROL | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:20:30.226106 | 05:20:32.033892 | 1 |
| final-control-restored | CONTROL | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:20:32.129025 | 05:20:33.680265 | 0 |
| baseline-unit-lint | CONTROL | `TMPDIR=/var/tmp go tool golangci-lint run --config ./.golangci.yml ./host/internal/usecase/host/modelexecution/...` | 05:20:46.367124 | 05:20:50.288307 | 1 |
| final-focused | ROOT | `TMPDIR=/var/tmp go test -race -count=1 -run "^TestServiceCancellationInterruptsInstalledRetryWait$" -v ./host/internal/usecase/host/modelexecution` | 05:20:50.363564 | 05:20:51.887791 | 0 |

### Original post-evidence final audit

The original final audit ran in ROOT on 2026-10-23 from 05:23:09.163452 to 05:23:09.173593 UTC and returned exit 0. It performed `git diff --check`, verified an empty tracked `git diff --name-only HEAD`, hashed production `retry.go`, the new test, and the original evidence document, compared production `retry.go` with the retained original bytes, and compared the final-control baseline and restored source manifests. The new test and evidence were untracked; the empty tracked diff did not assert their absence.

That audit observed production hash `6d6afd706cd731216df2fb6191092b4721b6ad080a7f28249e2546687308488a`, test hash `5612c9b564a0e1f8cad463bd83a59c43e2362cff0feb2c589246521321973ed2`, and original evidence hash `380fd718b29bdc704db434f27622dc554c114b145bed7b04db30433c08f8eaf8`. Its ROOT post-evidence source manifest has SHA-256 `79eb57d5620d04567bded1c515e29b60b6316a3c283c0cef0990942a59dc3efe`. That manifest includes the original evidence bytes and differs from the pre-evidence ROOT manifest. These are original recorded observations. They do not audit the revised evidence text or establish its hash. Any later documentation-only hash audit is a separate observation of revised bytes and does not reconstruct or replace the original records.

Material full-check output:

```text
lint: 0 issues.
ifaceguard: no errors found
Vulnerability #1: GO-2026-6809
Found in: crypto/x509@go1.27.2
Fixed in: crypto/x509@go1.27.3
Your code is affected by 1 vulnerability from the Go standard library.
This scan found no other vulnerabilities in packages you import or modules you require.
task: Failed to run task "lint": exit status 3
external lint: 0 issues.
Coverage: 84.2%, required: 80.0%
```

`task lint` returned task exit 201 because govulncheck returned 3 for the sole accepted GO-2026-6809 exception. Its nilness, easyp, integration-tag golangci-lint, and ifaceguard steps completed before the scanner. The external-plugin lint step was then executed separately with the ROOT-resolved golangci-lint, as prescribed by Taskfile. No scanner suppression was used.

The first coverage recorder was interrupted by the command runner's 120-second timeout before it recorded an exit. Its partial output and start metadata are retained separately. It supplies no passing check claim. A fresh isolated recorder reran the identical coverage command with a 900-second runner limit and completed with exit 0. Build then completed with a temporary output root. No shared artifact directory was removed.

## Actual skips and limits

The uncached race integration-event runs completed with exit 0 in ROOT and EXTERNAL. Seven named test scenarios were skipped:

| Package relative to ROOT | Test | Runtime reason |
| --- | --- | --- |
| `host/internal/app` | `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Directory permission failure injection requires Darwin permission enforcement. |
| `host/internal/app` | `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Immutable-file recovery injection requires Darwin chflags. |
| `host/internal/app` | `TestLifecycleObserverThroughStandardTUI` | Real PTY acceptance runs on Darwin arm64. |
| `host/internal/app` | `TestStandardTUIHostRuntimePersistenceFailures` | Real PTY acceptance runs on Darwin arm64. |
| `host/internal/app` | `TestStandardTUIHostSmoke` | Real PTY acceptance runs on Darwin arm64. |
| `internal/testsupport/tui` | `TestEarlyFailureCleanup` | Real PTY cleanup runs on Darwin arm64. |
| `plugins/ui/tui/internal/app` | `TestStandardTUIPTY` | Real PTY acceptance runs on Darwin arm64. |

Package-only skip events for packages with no eligible test files are not executed scenarios. EXTERNAL had no named skipped test. Neither public cancellation root skipped. PTY skips therefore limit broader terminal acceptance; they do not remove the executed UI-plugin lifecycle fixture.

## F25 disposition: supplementary unit-tag lint

The supplementary non-integration-tag model-execution lint command returned 1 with 21 diagnostics: dupl 7, errname 1, gofumpt 1, lll 5, paralleltest 2, testifylint 4, and unused 1. Its output is byte-identical to the same command against the UNT-05 baseline `d769d3004619c7b31d2669d49642ea78d75011eb`, with the new UNT-05 test removed. This establishes that all 21 predate UNT-05. No diagnostic names `retry_installed_wait_test.go`.

The overall delivery began at `7238582309a8abd2750898ac0c0d8bbc07e78a18`. UNT-02 commit `da9455f9afa4832acb29972c468cb1f0d92b0748` introduced `ownerless_overflow_test.go`, which accounts for three diagnostics: `lll` at line 27 and `testifylint` `require-error` at lines 156 and 158. Those three belong to this delivery. The delivery owner will correct them in a separately reviewed UNT-02 corrective commit, with applicable checks, after UNT-05 and before phase acceptance. That correction remains pending. Correcting this delivery's own errors requires no new user decision.

### Deviation description: 18 report-only diagnostics

The remaining 18 diagnostics name files unchanged between the delivery and UNT-05 baselines: `context_preparation_test.go`, `retry_cancellation_test.go`, `retry_test.go`, `service_test.go`, `observation_test.go`, and `captured_policy_test.go`. Examples include `typedPreparationFailure` naming and formatting, duplicated test blocks, and unused `newTestPreservingPolicies`. These 18 remain report-only; this documentation correction neither designs nor performs unrelated cleanup.

Consequences are MEDIUM for developers running this supplementary configuration because the command remains failing. The original outputs and Git file provenance establish the diagnostic count and attribution; lint output establishes no production behavior defect. The established disposition is to report the 18 outside-delivery diagnostics and separately correct the three delivery-owned diagnostics. Changing the 18 is outside this assignment and would require separate authority. The defect-outside-task reporting exit applies only to those 18. The supplementary failure is separate from the sole accepted GO-2026-6809 exception in required `task lint`; it is not another accepted scanner exception.

## Handoff state

The original required task commands completed after the final UNT-05 source change, subject only to the named scanner exception. The supplementary unit-tag lint failure and coverage recorder interruption remain explicit above. Existing untracked task documents and plan were preserved. No production defect was found on the exercised cancellation path. The UNT-05 addition introduced no new technical debt, deprecation, or unresolved implementation question. The three delivery-owned UNT-02 diagnostics still require the separate corrective commit before phase acceptance.

The accepted text-review findings were resolved through this document alone. Test code and comments remain frozen at SHA-256 `5612c9b564a0e1f8cad463bd83a59c43e2362cff0feb2c589246521321973ed2`; production `modelexecution/retry.go` remains at `6d6afd706cd731216df2fb6191092b4721b6ad080a7f28249e2546687308488a`. No formatter, build, test, or lint command was rerun for this documentation-only correction. The original execution records remain historical observations of their recorded source states. Review of the revised evidence and final user acceptance remain pending; this correction claims neither approval nor completion of the later UNT-02 work.
