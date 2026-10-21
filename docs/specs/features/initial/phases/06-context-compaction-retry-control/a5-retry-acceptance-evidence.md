# A5 retry acceptance evidence

Status: A5 is accepted after code-quality, text-quality, and independent unit review under the specific `GO-2026-6809` exception and the evidence limits below. A6 and whole-phase ACC-01 through ACC-06 acceptance remain pending.

Scope: [ticket](ticket.md) ACC-04.1/04.2/04.6/04.7, the default schedule in ACC-04, and the corresponding FRQ-04 and FRQ-07/08 requirements under [D4 public-boundary acceptance](solution.md#d4-public-boundary-acceptance). The [migration plan](migration-plan.md#phase-phs-064---independent-acceptance) owns progress. Baseline: `afe75cf2f75aba37c2a27e29bd8a9a72b1aab460`.

## Public executable observations

All new cases are in `host/internal/app/retry_acceptance_integration_test.go`. They invoke Programmatic Control over its Unix-socket stream through the existing production application fixture. Host loads settings and the real external extension executable, then dispatches through the OpenAI-compatible Responses adapter to a controlled local HTTP server. When retry is enabled, Host invokes the extension's public retry handlers. Session persistence and operation reporting use the production assembly. This is IN PROCESS application-entry evidence with a real child extension process and local HTTP transport.

| Acceptance | Executable observation |
|---|---|
| ACC-04.1 | `TestPublicRetryOverrideComposition` reads each handler's received public invocation for all three failed attempts. On each failure, the first handler receives equal original and current retry decisions. Both handlers receive equal originals, with retryability false, retry false, zero delay, and total attempt limit 1. The second handler receives the first handler's current decision: retryability true, retry true, 40 ms delay, and total attempt limit 4. The fixture registers that handler second. |
| ACC-04.2 | The same test starts with HTTP 400 and configured `maxRetries: 0`. The first handler permits retries with a 40 ms delay and limit 4. The second derives its decision from current, adds 40 ms, and reduces the limit to 3. Public progress reports 80 ms and limit 3 for each of two repeats. Exactly three HTTP attempts arrive. On the third failure, the first handler still permits retry, but the second stops it at limit 3. The terminal category remains `MODEL_FAILED`, preserving the non-retryable source classification separately from the overridden retry decision and retaining the provider explanation. |
| ACC-04.6 and default ACC-04 schedule | `TestPublicRetryPolicySchedule/default` leaves retry configuration absent and observes four attempts with public delays 1000, 2000, and 4000 ms and total limit 4. `configured` supplies two retries with delays 40 and 90 ms and observes three attempts with total limit 3. Both exhaust with `RETRY_EXHAUSTED` and the provider explanation. `disabled` observes only the initial HTTP attempt, no retry progress, no retry-handler invocation marker, and `MODEL_FAILED` with the provider explanation. |
| ACC-04.7 | `TestPublicProviderRetryAfter/default_accepts_ten` receives HTTP 429 with `Retry-After: 10`, reports 10000 ms, then completes on its second attempt. `default_rejects_sixty` receives `Retry-After: 60`, produces `RETRY_DELAY_EXCEEDED` containing the provider explanation, requested `1m0s`, and maximum `30s`; it emits no retry progress and makes one attempt. `raised_maximum_accepts_sixty` configures 60 s, reports 60000 ms, and completes on its second attempt. `lowered_maximum_rejects_ten` configures 5 s and rejects 10 s with both durations and the source explanation, one attempt, and no retry progress. |

For every scheduled repeat, tests compare consecutive HTTP request arrival times with the corresponding minimum delay. These assertions establish provider-arrival lower bounds independently of progress fields. Exact scheduler wake-up latency and upper latency bounds remain unmeasured. In the accepted 60-second case, the second provider request arrives at least 60 seconds after the first.

In `compose` mode, the public fixture records received protobuf invocations and registers two retry handlers. Existing preserve, cancel, and fail modes retain their behavior. No production retry owner, provider adapter, public contract, dependency, toolchain, or compaction policy changed.

## Preserved authority and evidence limits

In `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute` validates the current retry decision after each successful preserve or replace action and validates the final decision. It checks the provider maximum after successful handler composition and schedules the final delay when retry is permitted. Explicit Cancel and handler errors terminate composition before the maximum check. The disabled-policy branch skips handler snapshot and dispatch. `RetryDecision.validate` enforces the provider-requested lower bound even when retry is stopped.

Existing `host/internal/usecase/host/modelexecution/retry_handler_test.go` tests exercise invalid decisions, lower-bound undercuts, stopped-decision undercuts, and handler outcomes before maximum enforcement through mocked provider/runtime ports. These are LAYER observations. The new public maximum cases use preserving handlers; they do not establish every invalid-handler path at the public boundary.

In `host/internal/app/retry_modes_integration_test.go`, `TestRetryAcrossUIAndProgrammaticModes` exercises the Codex adapter's assembled UI-plugin and Programmatic Control retry paths with a mocked HTTP round tripper. `TestPublicRetryHandlerAndConfiguredModelProgress` observes Extension SDK configured-model progress at 1 completed attempt, total limit 2, and zero delay. That test leaves provider-arrival timing unmeasured. The new scheduling and override cases cover Programmatic Control with the OpenAI-compatible Responses adapter. UI-plugin and Extension configured-model override and scheduling matrices remain unverified.

Linux fixtures establish neither physical-terminal presentation nor live-provider model quality. A6 pending-delay abort and public-idle evidence remains separate. This unit does not close whole-phase acceptance.

## Test-first chronology

1. The author added the public composition assertion before extending the executable fixture. The author ran `go test -tags=integration ./host/internal/app -run '^TestPublicRetryOverrideComposition$' -count=1`. The test compiled and failed: expected three HTTP attempts, observed one, terminal `MODEL_FAILED`. This is the original RED for the fixture's missing composition capability. Production retry behavior already existed.
2. The author added two ordered fixture handlers and public invocation recording, then ran all requested delay and attempt-count cases. Two assertions used incorrect expectations: a non-retryable source retains `MODEL_FAILED` after an override chain stops, and 60 seconds is formatted as `1m0s`. The author corrected those assertions to the existing category contract and duration formatting. These assertion-authoring failures are not production-defect RED evidence.
3. The author strengthened the fixture chain so the first handler supplies limit 4 and the second derives limit 3, independently demonstrating the final limit. All focused tests then passed with uncached race detection.
4. The external linter reported introduced fixture variable shadowing and unnamed numeric values. The author replaced the shadowed error name and named the fixture delay and initial limit, then repeated required checks after these executable edits.
5. Code-quality reviewers found that the policy test indexed progress limits after a nonfatal delay comparison. Reviewers used a temporary missing-progress control that retained three provider attempts but dropped recorded progress. Before correction, the test reported the delay mismatch and panicked on the empty limits slice. Reviewers added a fatal limits-length assertion. With the same control, the test reported the missing progress and stopped without a slice panic. Reviewers removed the control before verification. This is test-diagnostic evidence, separate from the original fixture RED.
6. Code-quality reviewers reused public failure-code constants and named stable fixture, protocol, and format strings. Reviewers removed redundant parent retry-handler environment assignments; each subtest owns its assignment. An intermediate version kept only the parent assignments, and full lint reported two `paralleltest` errors. The final version retains only subtest assignments and requires no lint suppression.

Default/configured/disabled retry and provider-delay tests are acceptance and preservation tests for behavior already implemented. Their first run required no production correction. No artificial RED or A4 test-first exception is claimed for them. There was no A5 production behavior correction.

## Project verification

The baseline passed fix dry-run, unit tests, integration tests, coverage, temporary-root build, package loading, diff checks, and direct external-module lint. Baseline coverage was 84.2% against 80.0%. `task lint` failed at the accepted `GO-2026-6809` scan.

After the last executable code-quality correction, reviewers reran the full verification batch. The following results apply to that batch:

| Command | Result |
|---|---|
| `task fmt` | Exit 0. |
| `task fix_dry_run` | Exit 0; no proposals. |
| `task lint` | Shell exit 201, underlying scan exit 3 for `GO-2026-6809`; preceding analyzers and ifaceguard passed. |
| Direct external-module lint with the root module's `go tool -n golangci-lint`, `--config ../../.golangci.yml --build-tags=integration ./...` | Exit 0; zero issues. |
| `task test` | Exit 0, root and external modules. |
| `task itest` | Exit 0, root and external modules. |
| `task test-coverage` | Exit 0; 84.2%, required 80.0%. |
| `task build` with temporary `BUILD_ROOT` | Exit 0. |
| Root and external-module `go list ./...` | Both exit 0. |
| `git diff --check` | Exit 0. |
| `task generate`, twice | Both exit 0. Tracked generated Go files had identical content before generation, after the first run, and after the second run. |

The aggregate SHA-256 of sorted per-file hashes for 96 tracked Go files containing `Code generated` was `73b978fc40d978c568c1cfe633dd6e34bc8432f6227c54691cf19364bb054e7a` at all three generation checkpoints. No generated artifact changed.

The focused command `go test -count=1 -race -tags=integration -v ./host/internal/app -run '^(TestPublicRetryOverrideComposition|TestPublicRetryPolicySchedule|TestPublicProviderRetryAfter)$'` passed after the implementation corrections described in steps 1 through 4. Composition took 0.61 s; policy cases took 7.63 s; provider-delay cases took 70.57 s. The individual accepted provider waits completed in 10.04 s and 60.09 s. These are test runtimes, separate from the per-arrival lower-bound assertions. The final code-quality batch reran all three tests with `-count=1 -race -tags=integration -p 1 -parallel 1` and passed in 79.856 s. No A5 case skipped.

An additional uncached race-enabled integration census used `go test -count=1 -json -race -tags=integration -p 1 -parallel 1 ./...` in each module. Both commands passed. The root census retained seven platform skips:

- `host/internal/app`: `TestRunWithPathsHeadlessPersistenceFailurePreservesContext`, `TestLifecycleObserverThroughStandardTUI`, `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath`, `TestStandardTUIHostSmoke`, and `TestStandardTUIHostRuntimePersistenceFailures`.
- `internal/testsupport/tui`: `TestEarlyFailureCleanup`.
- `plugins/ui/tui/internal/app`: `TestStandardTUIPTY`.

The external census had no named test skips. Separately, 55 root packages and two external fixture command packages reported no test files. The seven named platform skips remain evidence gaps. The vulnerability exception covers only `GO-2026-6809`; it does not cover skipped scenarios.

The retained vulnerability is in standard-library `crypto/x509@go1.27.2`, reported fixed in `go1.27.3`. The scan trace names `plugins/extension/tools/internal/infra/filesystem/project/service.go:114:21`. The approved development exception covers this specific scan failure; no suppression or toolchain change was applied.

The earlier verification batch passed tests, coverage, build, package loading, diff checks, and both unchanged generation comparisons, but direct external lint failed on the introduced fixture issues described above. The post-correction batch supersedes that earlier check state.

## Text-quality verification

After the prose and comment edits, reviewers compared executable Go tokens in all three A5 Go files with their pre-review copies. All tokens, assertions, and string literals were identical; `main.go` was byte-identical. Reviewers also compared the ticket and solution requirement lines and left those lines unchanged.

Reviewers reran `task fmt` and `task fix_dry_run`; both exited 0, with no fix proposals. `task lint` again exited 201 after the zero-issue analyzers and ifaceguard because govulncheck reported `GO-2026-6809` with child exit 3. Direct external-module lint exited 0 with zero issues. `task test` and `task itest` exited 0 with cached test results. `task test-coverage` ran uncached unit and integration tests and exited 0 at 84.2% against 80.0%.

The final whitespace check passed. All 260 local Markdown file targets across the five changed documents exist; document-wide anchors were outside that check. Reviewers did not rerun build, package loading, generation, the focused race command, or the JSON skip census during text review. The earlier results above remain the evidence for those checks.

## Verification provenance

The retained original RED output establishes compilation and the one-versus-three assertion failure. The author reports the command and its sequence before the first fixture composition edit. Reviewers did not inspect a contemporaneous fixture source snapshot or reconstruct the historical source bytes.

The post-text raw bundle contains lint, external lint, test, integration-test, and coverage output. The text reviewer reports a separate silent successful formatter and fix-dry-run invocation; that output was not saved in the raw bundle. The earlier complete batch supplies explicit formatter and fix-dry-run results after the last executable edit. Ordinary-comment and prose edits preserve executable tokens.

## Review and remaining acceptance

Code-quality, text-quality, and independent unit reviews accept A5 under the specific vulnerability exception and the recorded evidence limits. Independent uncached race-enabled public repeats passed, as did the existing lower-layer authority and assembled UI/configured-model tests within their stated boundaries.

Both corrected initial expectations fix errors in the new tests and preserve the required behavior. They are direct corrections, with no additional exception or workaround-debt obligation. The earlier A4 test-first exception remains limited to its named A4 cases.

A6 covers pending-retry abort and public idle. Phase acceptance still requires executable evidence for all ACC-01 through ACC-06 conditions and independent phase review.

## Open Questions

None for A5 acceptance. A6 and whole-phase acceptance remain subsequent work.
