# UNT-02 acceptance evidence

## Result and scope

Implemented [FRQ-02](requirements.md#functional-requirements) and target [A2](target-picture.md#acceptance-scenarios) using decision D2. In `modelexecution.Service.execute`, disabled overflow retains its early CONTEXT_LIMIT exit. Enabled overflow traverses ordered retry handlers using the existing initial decision. After composition and validation, Host rejects overflow without recovery before recovery invocation, progress delivery, or retry scheduling.

The diff is uncommitted. Repository behavior and code-quality reviews remain the delivery owner's next step. This record does not assert a review outcome or phase completion. The pre-existing task documents and plan remain unchanged. No public API, journal, dependency, toolchain, scanner suppression, F17 investigation, or optional F18 work was added.

Changed implementation and regression files:

- `host/internal/usecase/host/modelexecution/retry.go`: move the absent-recovery admission boundary after handler composition.
- `host/internal/usecase/host/modelexecution/ownerless_overflow_test.go`: ordinary absent-owner and configured entry-point regressions with generated mocks.
- `host/internal/app/terminal_failure_modes_integration_test.go`: extend the existing production-assembly fixture with five overflow handler scenarios, limited to ordinary programmatic and configured SDK calls. Preserve the UNT-01 Codex cases and other public clients.
- `testdata/external-plugins/cmd/extension/retry_handler.go`: add an invalid replacement decision to the existing external retry fixture.

## Source identity and provenance

Base commit: `7f10ace03153388cc66705b42121e3c0e2587f1d`.

Before the production edit, an original RED source copy was saved in temporary storage and made read-only. The copy contains the original production owner, both regression sources, and the external fixture. The recorder retained the base commit, SHA-256 manifest, exact commands, working directory, UTC start/end, process exits, and original diagnostic output. Raw artifact locations belong to the desk handoff rather than repository documentation.

| File | Original RED SHA-256 | Final checked SHA-256 |
| --- | --- | --- |
| `modelexecution/retry.go` | `ba29eae9758b2d37cd7152443b4f7b06925d47653f5e0701de538a1ac99221f0` | `6d6afd706cd731216df2fb6191092b4721b6ad080a7f28249e2546687308488a` |
| `modelexecution/ownerless_overflow_test.go` | `1e84889396f8c5dee71c71d8d8a38b9b1006c7a7dec22ce6c4f393b85c2f4423` | `0f4a21390ef73751d3878ccf9e7fe71fe9dd7594bd3dce0d78303628097328d5` |
| `app/terminal_failure_modes_integration_test.go` | `abc14c987076df90b00fa96942bafa647e189befd61c525b937a79ba2813412e` | `6dc044588f54b4837f052420ce0d27e81f8f209e92aa3aee5c6b51e8b47be26d` |
| `external-plugins/cmd/extension/retry_handler.go` | `47067e78dfecb48344a5334b345136b60d59bec8a64c5ef0ce59d0819c5a2402` | same |

The abbreviated paths above identify the full changed paths listed in Scope. The final layer test differs from the original RED copy only through `task fmt` formatting of struct literals and chained expectations. The public test differs from the immutable RED source only by the accepted FND-01 constant-reuse correction: the compose row uses same-package `retryComposeMode`, the marker assertion uses `retryInvokedSignal`, and their duplicate local definitions were removed. Both shared constants have the same string values as the removed definitions. `preserveHandler` retains its distinct value. Assertions, cases, and fixture behavior are equivalent. The external fixture retains its original RED bytes. Original RED output is retained separately from all GREEN and readiness output.

A whole-checkout SHA-256 manifest, including tracked files and pre-existing untracked task documents, was captured before and after the original full batch. Both original manifest digests are `8809ab9e5b6653c25e32aeb99d8af5f82004eaa876cc9efa78f065406d3d069e`. The evidence document was added after that batch; it is outside the original checked-source manifest. The corrected-source batch below has separate manifests. Original source copies, logs, metadata, and manifests remain preserved.

## Compiling RED before production correction

All times below are UTC on 2026-10-23. Commands ran from the repository root with child `TMPDIR=/var/tmp`.

| Observation | Command after `TMPDIR=/var/tmp` | Start | End | Exit |
| --- | --- | --- | --- | --- |
| Layer RED | `go test -count=1 ./host/internal/usecase/host/modelexecution -run TestServiceOwnerlessOverflowHandlers -v` | 01:34:48.687097 | 01:34:49.479694 | 1 |
| Public RED | `go test -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run TestTerminalRefusalAcrossPublicClients/overflow -v` | 01:34:49.498434 | 01:34:53.445014 | 1 |

Material layer excerpt:

```text
TestServiceOwnerlessOverflowHandlers/cancel/ordinary
expected: "RETRY_CANCELED"
actual  : "CONTEXT_LIMIT"
missing call(s) to *modelexecution.MockRetryHandlers.HandleRetry(... first ...)
missing call(s) to *modelexecution.MockRetryHandlers.HandleRetry(... second ...)
```

Material public excerpt:

```text
TestTerminalRefusalAcrossPublicClients/overflow_cancellation/programmatic
unable to find file ".../retry-handler-invoked"
expected: "RETRY_CANCELED"
actual  : "CONTEXT_LIMIT"
TestTerminalRefusalAcrossPublicClients/overflow_handler_failure/programmatic
"OpenAI-compatible request failed: responses request failed: provider terminal rejection full unique text attempt 1\nunexpected EOF"
does not contain "retry handler full unique failure text"
```

Both runs compiled and executed. The category mismatch, absent handler signal, and missing ordered mock invocations establish skipped handler authority. Neither failure was a compilation or fixture startup error. The disabled layer rows passed during RED. Existing ordinary and Codex overflow rows in the public run also passed.

## GREEN and behavioral observations

| Observation | Command after `TMPDIR=/var/tmp` | Start | End | Exit |
| --- | --- | --- | --- | --- |
| Layer GREEN | `go test -count=1 -race ./host/internal/usecase/host/modelexecution -v` | 01:35:12.331484 | 01:35:15.150714 | 0 |
| Public GREEN | `go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run TestTerminalRefusalAcrossPublicClients -v` | 01:35:15.169663 | 01:35:45.417671 | 0 |

Material GREEN excerpts:

```text
--- PASS: TestServiceOwnerlessOverflowHandlers
--- PASS: TestServiceRecoversAdmittedOverflowsWithinRetryAllowance
--- PASS: TestServiceDisabledRetryClassifiesOverflowWithCapturedOwner
--- PASS: TestServiceOverflowRecoveryCancellationMatrix
--- PASS: TestTerminalRefusalAcrossPublicClients/overflow_cancellation/programmatic
--- PASS: TestTerminalRefusalAcrossPublicClients/overflow_cancellation/configured_SDK
--- PASS: TestTerminalRefusalAcrossPublicClients/overflow_repeat_override/programmatic
--- PASS: TestTerminalRefusalAcrossPublicClients/overflow_repeat_override/configured_SDK
```

Coverage of FRQ-02 and A2:

| Required observation | Executed evidence |
| --- | --- |
| Cancel, handler failure, rejected action, Preserve, valid repeat override | Public ordinary and configured production assembly executes all five outcomes. Public invalid replacement uses negative delay. Layer rows separately cover missing replacement decision and invalid negative-delay decision. |
| Complete acquired causes and category | Public assertions retain the provider explanation and unexpected EOF for every outcome, handler failure text for failure, validation cause for invalid replacement, and the expected terminal category. Layer assertions use `ErrorIs` for provider and handler causes and check validation text. |
| Immutable Original and evolving Current | Layer handlers first accept a changed stopped decision, then assert initial Original and changed Current. A third handler observes the preserved or repeat-replacement Current after successful composition. |
| Stop later handlers on terminal results | Generated mock expectations prohibit the third handler after Cancel, handler failure, invalid action, and invalid decision. |
| One attempt and no retry progress without recovery | Every new layer row expects exactly one provider call and zero retry-output calls. Configured callbacks fail on progress. Public ordinary event and configured progress-report assertions require zero updates; configured Wait and WaitWithProgress each make one provider attempt. |
| No unchanged repeat after valid override | The public compose fixture supplies valid replacements. The layer repeat override requests an hour delay and a larger allowance. Both return CONTEXT_LIMIT after all accepted handlers, with one attempt and zero progress. |
| Disabled retry and captured recovery | New disabled ordinary/configured rows prohibit handler activity. Existing captured-owner disabled, allowance/recovery, and recovery-cancellation regressions pass in the layer package and full checks. |

## Original readiness batch

The initial formatter, fix dry run, and lint also ran before the final batch: 01:35:56.657166 to 01:36:02.149772 (exit 0), 01:36:02.168972 to 01:36:06.215149 (exit 0), and 01:36:06.234182 to 01:36:20.052732 (exit 201), respectively. Their raw metadata and output remain separate. The formatter changed only the new layer test. Both fix dry runs produced empty output, so there was no proposal to apply and `task fix` was unnecessary.

The original batch used child `TMPDIR=/var/tmp` directly for every task. The external lint binary was resolved in ROOT using `go tool -n golangci-lint`, then executed from `testdata/external-plugins` with `run --config ../../.golangci.yml --build-tags=integration ./...` as the Taskfile prescribes.

| Command | Start | End | Exit and result |
| --- | --- | --- | --- |
| `task fmt` | 01:36:42.191338 | 01:36:47.813050 | 0 |
| `task fix_dry_run` | 01:36:47.831893 | 01:36:48.137150 | 0, empty proposals |
| `task lint` | 01:36:48.156186 | 01:36:55.244602 | 201; inner govulncheck exit 3, accepted GO-2026-6809 only |
| External-module lint, root-resolved binary | 01:36:55.263286 | 01:36:56.264080 | 0 |
| `task test` | 01:36:56.282752 | 01:37:06.240798 | 0, root and external module |
| `task itest` | 01:37:06.259755 | 01:42:57.637859 | 0, root and external module |
| `task test-coverage` | 01:42:57.656751 | 01:47:22.880739 | 0, 84.2% against 80.0% minimum |
| `task build BUILD_ROOT=<temporary build directory>` | 01:47:22.899544 | 01:47:25.462838 | 0, Host and standard plugins |

Material lint excerpt:

```text
0 issues.
ifaceguard: no errors found
Vulnerability #1: GO-2026-6809
Found in: crypto/x509@go1.27.2
Fixed in: crypto/x509@go1.27.3
Your code is affected by 1 vulnerability from the Go standard library.
This scan found no other vulnerabilities in packages you import or modules you require.
task: Failed to run task "lint": exit status 3
```

The existing [UNT-01 record](unt-01-evidence.md) and [baseline](baseline.md) retain the prior accepted vulnerability evidence. This unit preserves that narrow exception. `task lint` stopped before its external-module task; the separate external lint executed successfully. No suppression or version change was applied.

## Executed skip inventory and limitations

An additional uncached JSON integration run executed `go test -count=1 -json -tags=integration -p 1 -parallel 1 ./...` with child `TMPDIR=/var/tmp`, from 01:47:36.562277 to 01:51:40.215066, exit 0. The same command in the external module ran from 01:51:40.233708 to 01:51:45.196192, exit 0.

The root run reported these seven actual test skips:

- `host/internal/app.TestRunWithPathsHeadlessPersistenceFailurePreservesContext`: directory permission injection requires Darwin permission enforcement.
- `host/internal/app.TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath`: immutable-file injection requires Darwin `chflags`.
- `host/internal/app.TestLifecycleObserverThroughStandardTUI`: real PTY acceptance runs on Darwin arm64.
- `host/internal/app.TestStandardTUIHostRuntimePersistenceFailures`: real PTY acceptance runs on Darwin arm64.
- `host/internal/app.TestStandardTUIHostSmoke`: real PTY acceptance runs on Darwin arm64.
- `internal/testsupport/tui.TestEarlyFailureCleanup`: real PTY cleanup runs on Darwin arm64.
- `plugins/ui/tui/internal/app.TestStandardTUIPTY`: real PTY acceptance runs on Darwin arm64.

Package-level JSON skip events for packages without integration tests are retained as package events in the raw inventory. The external module reported no individual test skips. All new UNT-02 scenarios executed. The focused matrix deliberately adds no UI/headless or Codex Cartesian expansion for this unit.

## FND-01 correction and refreshed readiness

Accepted Minor FND-01 from review `6dbeba88-2fa8-46bb-a2ec-ba73a3e8849a` is resolved by reusing the existing same-package integration constants from `retry_acceptance_integration_test.go`. The correction changes only the public test's constant definitions and references. `preserveHandler` remains `"preserve"`; the existing `retryPreserveMode` represents a different value. No behavior change or additional refactor was made. This disposition records the implemented correction and does not assert a subsequent review outcome.

After the last source edit, the complete readiness set ran again. Before and after the corrected-source batch, whole-checkout manifests had the same SHA-256 digest: `ca5dff5f7b886113b861f7f8c37544b1a3ca35dd82d173d527c95d804cd1d6a9`. These manifests include the evidence document's pre-refresh version. Only this evidence document was edited after the batch. The source hashes in the provenance table identify the frozen corrected code.

All times in this batch are UTC on 2026-10-23. Every task used child `TMPDIR=/var/tmp` directly. Commands ran from the repository root except the external lint binary, which was resolved in ROOT and executed in the external module using the Taskfile's declared command.

| Command | Start | End | Actual exit and result |
| --- | --- | --- | --- |
| `task fmt` | 02:11:25.972901 | 02:11:31.585334 | 0, source unchanged |
| `task fix_dry_run` | 02:11:31.603822 | 02:11:31.917362 | 0, empty proposals; no fix required |
| `task lint` | 02:11:44.077652 | 02:11:54.055434 | 201; inner govulncheck exit 3, accepted GO-2026-6809 only |
| External-module lint, root-resolved binary | 02:11:54.074082 | 02:11:54.558888 | 0 |
| `task test` | 02:11:54.577642 | 02:11:55.868361 | 0, cached passing root and external results |
| `task itest` | 02:11:55.887628 | 02:17:27.610455 | 0, root and external module |
| `task test-coverage` | 02:17:27.629371 | 02:21:38.047562 | 0, 84.2% against 80.0% minimum; uncached unit and integration execution |
| `task build BUILD_ROOT=<temporary build directory>` | 02:21:38.066088 | 02:21:40.419378 | 0, Host and standard plugins |
| `go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run TestTerminalRefusalAcrossPublicClients -v` | 02:21:40.437581 | 02:21:50.261190 | 0, all public terminal cases executed |

The exact external lint invocation was `LINT=$(go tool -n golangci-lint); cd testdata/external-plugins && TMPDIR=/var/tmp "$LINT" run --config ../../.golangci.yml --build-tags=integration ./...`. Each execution has separate command, working-directory, UTC start/end, actual process exit, and original output records. The build used a separate temporary build root. `git diff --check` passed after the source correction.

The refreshed lint output again reported `0 issues.` and `ifaceguard: no errors found` before the sole accepted GO-2026-6809 scanner finding. Main lint stopped before external lint; the explicit external-module execution above passed. No scanner suppression or version change was applied. The earlier seven-skip JSON inventory remains original evidence from the times listed above; it was not rerun as part of this correction. The corrected public regression executed without skips. Platform-limited scenarios remain unexecuted evidence limits.

## Handoff boundaries

No new defect outside UNT-02 was investigated or corrected. Remaining debt is the accepted GO-2026-6809 scanner exception and the platform-specific unexecuted scenarios listed above. No workaround or deprecation was added. No commit or push occurred. Independent repository reviews and final acceptance remain with the delivery owner; the plan and phase statuses were preserved for that process.
