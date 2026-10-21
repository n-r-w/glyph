# A6: General abort and public idle evidence

Status: A6 is accepted after code-quality, text-quality, and independent unit review against `68f39e9a3087b13821fa80100d050c0a1345f760`, under the specific GO-2026-6809 exception and the evidence limits below. Whole-phase acceptance remains pending.

## Scope and exercised paths

This supplement records ordinary conversation abort through Programmatic Control and the UI-plugin protocol under [FRQ-03 and FRQ-05](ticket.md#functional-requirements), [ACC-06](ticket.md#acceptance-criteria), and [D4 public-boundary acceptance](solution.md#d4-public-boundary-acceptance).

`TestPublicAbortReleasesAgent` in `host/internal/app/abort_idle_integration_test.go` uses the production Programmatic application, Unix socket/gRPC connection, executable reference extension, session services, Host retry owner, and OpenAI-compatible Responses adapter. A local HTTP server supplies controlled provider responses. Each subtest retains one client connection through cancellation, state queries, and recovery.

| Scenario | Observation before abort | Cancellation and recovery assertions |
|---|---|---|
| `pending-retry=true` | Provider returns HTTP 503 with the full source explanation. Public retry progress reports one completed attempt, limit two, and a configured one-hour delay. Public state reports running with the target operation ID. The provider has received one target request. | Public Cancel targets that operation. The target emits Canceled with the acquired provider explanation before cancellation completes with target state CANCELED. Public state reports IDLE with an empty active operation ID. A subsequent user request completes on the retained connection. After application-owner join, the server has received exactly one target request and one recovery request. |
| `pending-retry=false` | Provider accepts the target HTTP request and flushes SSE headers. A server signal establishes in-progress provider work. Public state reports running with the target operation ID. | Public Cancel produces the same target/cancellation ordering. The HTTP request context ends, and the server acknowledges cancellation. Public idle and a successful later request are observed before application shutdown. Final provider counts are one target request and one recovery request. |

`TestUIAbortPendingRetryReleasesAgent` in `host/internal/app/abort_idle_ui_integration_test.go` exercises an ordinary submitted conversation through the production UI application and a real UI-plugin child process. The fixture uses the public SDK on one retained connection. The local compatible HTTP server returns HTTP 503 for the target and a successful Responses stream for the later request.

The child observes public retry progress with one completed attempt, limit two, a one-hour delay, and the provider explanation. It calls `Host.Cancel` against the conversation target. The target wait returns an error matching `context.Canceled` with the acquired provider cause. The child checks that the target terminal notification precedes the cancellation-completed notification. Cancellation completes with target state CANCELED. The child observes `AVAILABILITY_IDLE` through a public availability connection event, either while waiting for the target or through `waitForIdle` after cancellation. It then submits another ordinary request through the retained `Host` and waits for success. The child records successful completion of these assertions before closing the connection. After application join, the server has received exactly one target request and one recovery request.

This is ordinary UI-protocol cancellation and public availability evidence. It exercises neither a physical TUI Stop key nor terminal rendering. Observer-owned and Extension configured-model operations are separate scenarios.

The one-hour delay is ordinary retry configuration. The test neither sleeps for that delay nor uses absence during an arbitrary observation window as its sole evidence. It observes public retry progress while the next provider attempt is pending. Progress delivery precedes `backoff.RetryAfter` in production, so this observation does not establish that the internal backoff timer has already started.

The target's provider input remains local to its logical execution. `Service.Stream` clones history before calling `Service.execute`; each retry clones that captured request. The later request's unique text identifies recovery HTTP input. The canceled execution cannot acquire that later text from mutable conversation history.

## Cancellation ownership and settlement

The source trace supporting the terminal and count assertions is:

1. `host/internal/controller/programmatic/service.go`, `Service.prepareCancellation`, admits cancellation against the active target.
2. `host/internal/controller/programmatic/cancellation.go`, `cancellationPrepared.Run`, invokes `Owner.CancelAndWait` and returns the target terminal state.
3. `internal/operation/operation.go`, `Owner.Cancellation`, cancels the owning context and waits for the target's `done` channel. `Owner.run` waits for `prepared.Run`, releases the operation, delivers its terminal result, and calls `Owner.finishOperation` to close that channel.
4. `host/internal/usecase/host/programmatic/prepared.go`, `runPrepared.Run`, waits for `Coordinator.RunPrepared`. `host/internal/usecase/host/runcontrol/coordinator.go`, `Coordinator.RunPrepared`, waits for Core execution and settlement before returning.
5. `host/internal/usecase/agent/run/service.go`, `Service.Run` and `Service.runTurn`, synchronously invoke the logical model stream with the owning context. `host/internal/usecase/host/modelexecution/service.go`, `Service.Stream`, synchronously invokes `Service.execute`.
6. `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute`, passes that context into provider streaming and `backoff.Retry`. In declared dependency `github.com/cenkalti/backoff/v7@v7.0.0`, `retry.go`, `Retry`, checks cancellation before delay setup and selects the context while waiting. It stops its timer on return. The retry loop creates no detached provider-attempt worker.
7. `host/internal/usecase/host/programmatic/service.go`, `Service.runState`, reports idle only when the public active operation and Core active state are both clear.

The ordinary UI path enters through SDK `Host.Cancel` in `sdk/plugins/ui/v1/host.go`. In `host/internal/controller/ui/operations.go`, `startHostCancellation` captures `Owner.Cancellation` for the target, and `hostCancellationPrepared.Run` waits for it. `Session.prepareSubmit` in `host/internal/usecase/host/ui/prepared_operations.go` waits for `Coordinator.RunPrepared`; its release publishes idle availability. The common operation owner completes release and target terminal delivery before signaling cancellation settlement. `operationDelivery.Terminal` in `host/internal/infra/plugins/ui/runtime/operations.go` maps the canceled target and cancellation-completed state to the public UI protocol. The ordinary provider and retry work uses the shared synchronous Core/model-execution path traced above.

Thus application shutdown is cleanup after observed target cancellation, public idle, and successful recovery. It is not the operation that first releases the agent in these scenarios.

## Existing evidence retained

- `ProgrammaticAppSuite.TestProgrammaticBlockedRunAllowsQueryAndTargetedCancellation` in `host/internal/app/programmatic_cancellation_test.go` exercises a blocked provider transport, public query, and targeted cancellation. The new HTTP in-progress case supplements it with provider-context acknowledgment, idle, and same-connection recovery.
- `TestProgrammaticObserverCancellationReleasesRun` and `TestUIObserverCancellationReleasesRun` retain observer-owned nested-work cancellation and subsequent public request coverage. The UI case uses SDK `Host.Cancel` over the UI-plugin protocol. Its later successful submission demonstrates reuse; it does not exercise public pending-retry cancellation or an ordinary conversation provider request.
- `TestConfiguredRetryCancellationThroughExtension` in `host/internal/app/configured_cancellation_integration_test.go` cancels a nested Extension configured-model operation. That operation remains distinct from general agent abort. It supplies no additional general-abort claim here.

## Chronology and test-first provenance

The initial test draft failed compilation because it guessed diagnostic accessors absent from `operation.Canceled`. The public contract carries diagnostic text directly in `message`. Correcting the test to read that field fixed the test's cause without changing production behavior. That compilation failure is neither behavioral RED nor acceptance evidence.

The author reports that the first compiling uncached `TestPublicAbortReleasesAgent` run passed. These are preservation/acceptance tests of implemented behavior; no production correction or behavioral RED is claimed. A later focused lint run found an overlong comment, missing parallel calls, and forbidden `require` calls inside HTTP handlers. Corrections shortened the comment, enabled parallel isolated cases, and used nonfatal assertions in handlers. No check suppression or production edit was introduced.

The first handoff covered Programmatic general abort and identified ordinary UI pending-retry cancellation as unobserved. The continuation adds assembled evidence for that supported client path. It reports no production defect. The author reports that its first compiling uncached `TestUIAbortPendingRetryReleasesAgent` run passed. The existing Programmatic integration test retains SHA-256 `6be19fed3d02430189de1feef05fe43112dffcdaf0d4466fad0fc7bd890c674f`.

The author reports these as the first compiling passes. The retained Programmatic success output records a pass without contemporaneous command, selection, or source hashes; independent review therefore does not certify the first-run ordinal. The final checks below apply to the captured final Go bytes and the later documentation-only changes. They establish captured-state correspondence, not continuous whole-tree byte provenance.

An early race command used an unmatched suite selector for the old blocked-run case. Its output covers the new test and both observer tests only. The final focused command uses `TestClientAppSuites` and records the actually executed suite paths.

## Prior Programmatic-only verification

The following results belong to the earlier Programmatic-only executable state. They remain historical evidence; UI-complete verification is recorded separately below. All commands in this table ran after the final executable edit of that prior state. The clean baseline also passed the mandatory checks with coverage 84.2% against 80.0%, except for the same root lint failure.

| Command | Result |
|---|---|
| `task fmt` | Exit 0. A final repeat preserved the integration test's SHA-256. |
| `task fix_dry_run` | Exit 0, empty proposal output. No fix was needed. Final repeat also exited 0. |
| `task lint` | Exit 201; underlying `govulncheck` exit 3. Earlier lint stages passed. |
| `(cd testdata/external-plugins && $(cd ../.. && go tool -n golangci-lint) run --config ../../.golangci.yml --build-tags=integration ./...)` | Exit 0, `0 issues.` Run directly because root lint stopped at scanning. |
| `task test` | Exit 0. |
| `task itest` | Exit 0; Host integration package 312.767 seconds. |
| `task test-coverage` | Exit 0; coverage 84.2%, required 80.0%. |
| `BUILD_ROOT=$(mktemp -d /tmp/glyph-build.XXXXXX) task build` | Exit 0; temporary build output. |
| `go list ./...` | Exit 0, 93 root packages. |
| `(cd testdata/external-plugins && go list ./...)` | Exit 0, 3 external packages. |
| `git diff --check` | Exit 0. New-file whitespace was also checked separately. |
| `task generate`, twice | Both exit 0. See the retained comparison below. |
| `go test -tags=integration ./host/internal/app -run '^TestPublicAbortReleasesAgent$' -count=1 -v -timeout=90s` | Exit 0; both new scenarios passed uncached. |
| Focused race command below | Exit 0, 27.203 seconds; both new scenarios, both observer tests, and the existing blocked-run test passed in every repetition. |
| `go test -race -tags=integration -v ./plugins/ui/tui/internal/app` | Exit 0; physical PTY skip described below. |

Focused race command:

```sh
go test -race -tags=integration ./host/internal/app -run '^(TestPublicAbortReleasesAgent|TestUIObserverCancellationReleasesRun|TestProgrammaticObserverCancellationReleasesRun|TestClientAppSuites)$' -count=5 -v -timeout=240s
```

The root scanner reports GO-2026-6809 in `crypto/x509@go1.27.2`, fixed in Go 1.27.3, and no other vulnerabilities. The specific development exception retains the failed result as `lint201/scan3`. No suppression, bypass, or version change was applied.

Two distinct named skips were observed in the historical Programmatic-only focused and TUI commands:

- `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath`: `immutable-file recovery injection requires Darwin chflags`, once per focused repetition.
- `TestStandardTUIPTY`: `real PTY acceptance runs on Darwin arm64`. `TestStandardTUIPTYInner` passed. That TUI output contains one named PTY skip. The post-correction full integration census appears below.

No new A6 scenario skipped. Unit output had 38 `[no test files]` package entries; integration output had 57. Integration packages `host/internal/controller/extension` and `internal/operation` reported `[no tests to run]`. These package statuses are separate from named test skips.

The first extraction report's generation manifests were overwritten during report preparation, so those aggregate hashes are discarded as provenance evidence. The implementer repeated generation twice using independently retained before/after manifests for 96 tracked generated Go files. Each manifest maps repository paths to SHA-256 content hashes. Both comparisons were equal to the original snapshot; the SHA-256 of the sorted JSON mapping was `1e4f64a8d35afc1e91ab40ae777fe3e587221ba00a068fb649aec9b164309e1e` before and after each run. The extractor's broader 97-file text-marker inventory is not used for this comparison.

Formatting was initially reported as having changed four documentation files. That attribution came from working-tree status while the implementer was editing those documents; it did not establish formatter changes. The final repeat explicitly checked integration-test byte stability. Only ordinary evidence prose and progress links changed after executable verification.

## UI-complete implementation verification

The following checks ran after the ordinary UI fixture and dispatch edits, before the code-quality corrections below. The existing Programmatic test's bytes remained unchanged. Formatting retained all three executable-file hashes: `abort_idle_integration_test.go`, `abort_idle_ui_integration_test.go`, and `ui_protocol_fixture_test.go`.

| Command | Result |
|---|---|
| `task fmt` | Exit 0; genuine before/after file hashes equal. |
| `task fix_dry_run` | Exit 0; empty proposals, no fixes applied. |
| `task lint` | Exit 201; underlying scan exit 3 for the accepted GO-2026-6809 finding. Repeat returned the same result. Earlier lint stages pass. |
| `(cd testdata/external-plugins && $(cd ../.. && go tool -n golangci-lint) run --config ../../.golangci.yml --build-tags=integration ./...)` | Exit 0, `0 issues.` |
| `task test` | Exit 0. |
| `task itest` | Exit 0; Host integration package 314.352 seconds. |
| `task test-coverage` | Exit 0; 84.2% against 80.0%. |
| `BUILD_ROOT=<temporary directory> task build` | Exit 0. |
| `go list ./...` and `(cd testdata/external-plugins && go list ./...)` | Both exit 0. |
| `git diff --check` | Exit 0. |
| `task generate`, twice | Both exit 0; 96 tracked generated Go files equal before and after each run. |
| Focused uncached integration command below | Exit 0; all five selected test groups pass. |
| Focused race command below | Exit 0; five repetitions pass with ordinary test parallelism, 32.277 seconds. |
| `go test -race -tags=integration -count=1 -v ./plugins/ui/tui/internal/app` | Exit 0; one named physical-PTY skip, same reason as above; inner fixture passes. |

Focused uncached command:

```sh
go test -count=1 -v -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^(TestPublicAbortReleasesAgent|TestUIAbortPendingRetryReleasesAgent|TestProgrammaticObserverCancellationReleasesRun|TestUIObserverCancellationReleasesRun|TestClientAppSuites)$'
```

Focused race command:

```sh
go test -race -tags=integration ./host/internal/app -run '^(TestPublicAbortReleasesAgent|TestUIAbortPendingRetryReleasesAgent|TestProgrammaticObserverCancellationReleasesRun|TestUIObserverCancellationReleasesRun|TestClientAppSuites)$' -count=5 -v -timeout=240s
```

A separate extractor race run with `-p 1 -parallel 1 -count=5` also passed in 34.556 seconds. The final implementation race command above exercised the test's ordinary parallel scheduling.

In those pre-CodeQuality focused and TUI commands, the observed named skips are `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` for Darwin `chflags` and `TestStandardTUIPTY` for Darwin arm64 physical PTY acceptance. Each focused repetition reports the former. The new UI and Programmatic cases all execute. Unit output has 38 no-test package entries; integration output has 57. The two integration packages with `[no tests to run]` remain separate package statuses.

Before generation, the implementer retained the 96-file content mapping. It retained fresh mappings immediately after each successful generation run, without recomputing earlier snapshots. All three sorted mappings have SHA-256 `1e4f64a8d35afc1e91ab40ae777fe3e587221ba00a068fb649aec9b164309e1e`. These are new before/after observations after the UI edit, independent of the earlier historical comparison.

Two command-authoring limits are retained. An initial external-module command tried resolving the root-only linter tool inside the external module and exited 2 with `no such tool`. The Taskfile's root-tool-resolution invocation above then passed. An extractor TUI invocation omitted the integration tag and returned `[no test files]`; it supplies no PTY evidence. The explicitly tagged uncached implementation command above supplies the named skip and inner-fixture result. Neither command error establishes a product defect or a new accepted check exception.

The original Programmatic-only handoff retains its narrower coverage boundary. The later UI-complete handoff preceded the code-quality corrections below. The post-correction checks below apply to the final executable state.

## Code-quality corrections and final verification

The SDK launches `MockUIService.Run` in a worker goroutine. In `sdk/plugins/ui/v1/service.go`, `server.Open` sends the callback's return value to `runDone`, and `finishServerOpen` waits for that result. `testing.T.FailNow` calls `runtime.Goexit`; therefore, a fatal fixture assertion can end that worker before it sends the result. The new `runAbortRetryUIFixture` and `waitForUIAbortRetryProgress` now return assertion failures through the callback. They retain checks for terminal rejection, attempt count, attempt limit, delay, provider cause, target cancellation identity, target state, recovery success, and target terminal count. Existing fixtures remain outside this correction.

The UI fixture also checks public notification order. Sequential target and cancellation waits use independent SDK result channels, so those waits alone do not establish receive order. The callback now records a failure if it receives cancellation completion before exactly one target terminal notification. The SDK receives stream messages serially and queues notifications in receive order. Recovery uses the same callback before the child writes its receipt.

Four temporary Go overlays exercised returned-error paths: a deliberately wrong progress count, a deliberately wrong cancellation identity, a deliberately wrong ordering predicate, and an HTTP 400 response before retry progress. Each public UI test exited with failure and its diagnostic in under one second of reported test execution. These are synthetic diagnostic controls, not production incidents or behavioral RED evidence. The working-tree tests retain their required expectations.

During correction, root lint reported one `errorlint` violation because an unexpected-terminal error used a non-wrapping formatting verb. The helper now handles successful completion separately and wraps the failed notification's error with `%w`. The correction preserves the original cause. Focused lint then reported `0 issues.`

All checks below ran after the last executable correction on Go 1.27.2, Linux amd64. Later text-quality corrections changed documentation only. The three Go files retain the SHA-256 values below.

| Command | Result |
|---|---|
| `task fmt` | Exit 0; all three executable hashes equal before and after. |
| `task fix_dry_run` | Exit 0; empty proposals, no fix applied. |
| `task lint` | Exit 201; underlying scanner exit 3, only the accepted GO-2026-6809. Earlier lint stages passed. |
| External lint with the root tool-resolution command above | Exit 0, `0 issues.` |
| `task test` and `task itest` | Both exit 0. |
| `task test-coverage` | Exit 0; 84.2% against 80.0%. |
| `task build BUILD_ROOT=<temporary directory>` | Exit 0. |
| Root and external `go list ./...` | Both exit 0. |
| `git diff --check` | Exit 0. |
| `task generate`, twice | Both exit 0; four separately retained snapshots each contain the same 96 tracked generated Go files and content hashes. |
| `go test -json -count=1 -race -tags=integration -p 1 -parallel 1 ./...`, in root and external modules | Both exit 0; integration skip census below. |
| Focused race command above, with ordinary test parallelism | Exit 0; five repetitions, 32.488 seconds. |
| Focused race selection of both new tests with `-p 1 -parallel 1 -count=5` | Exit 0. |
| `go tool golangci-lint run --config ./.golangci.yml --build-tags=integration ./host/internal/app` | Exit 0, `0 issues.` |

Final executable SHA-256 values:

- `abort_idle_integration_test.go`: `6be19fed3d02430189de1feef05fe43112dffcdaf0d4466fad0fc7bd890c674f`.
- `abort_idle_ui_integration_test.go`: `14092f02902b8e617f350721c369eadc53adb4c0ac925a084cb5ffe9d963fb31`.
- `ui_protocol_fixture_test.go`: `de5bb73efc5db62fb5578b2dc9ddfe960ad411c480847aeb9d850c9023cad192`.

The uncached full integration JSON run contains seven named root-module skips:

| Test | Reported platform condition |
|---|---|
| `host/internal/app.TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Directory-permission failure injection requires Darwin permission enforcement. |
| `host/internal/app.TestLifecycleObserverThroughStandardTUI` | Real PTY acceptance runs on Darwin arm64. |
| `host/internal/app.TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Immutable-file recovery injection requires Darwin `chflags`. |
| `host/internal/app.TestStandardTUIHostRuntimePersistenceFailures` | Real PTY acceptance runs on Darwin arm64. |
| `host/internal/app.TestStandardTUIHostSmoke` | Real PTY acceptance runs on Darwin arm64. |
| `internal/testsupport/tui.TestEarlyFailureCleanup` | Real PTY cleanup runs on Darwin arm64. |
| `plugins/ui/tui/internal/app.TestStandardTUIPTY` | Real PTY acceptance runs on Darwin arm64. |

The external-module integration run contains zero named skips. All three new A6 scenarios and `TestStandardTUIPTYInner` passed. This census covers integration tests; it makes no complete unit-test skip-inventory claim.

Code-quality review found no remaining in-scope code correction after these checks. Text-quality and independent unit reviews accept A6 under the named scan exception and the recorded evidence limits. A fresh independent uncached race run repeated all three A6 scenarios five times successfully. Whole-phase acceptance remains pending.

## Limits and remaining acceptance

- Pending-retry cancellation now exercises both Programmatic Control and the ordinary UI-plugin protocol. Ordinary in-progress HTTP cancellation has new Programmatic coverage. Physical TUI Stop input and terminal rendering remain outside these observations. Named platform skips are reported separately.
- The provider is controlled local HTTP. Live remote-provider behavior and the full provider/client combination set remain unobserved.
- Public retry progress establishes an accepted retry decision and a pending next attempt. Internal timer installation is not publicly observable in this test. Existing model-execution cancellation tests retain layer-level delay coverage.
- A6 is accepted after all three reviews under the specific vulnerability exception and the recorded evidence limits. Whole-phase ACC-01 through ACC-06 acceptance remains pending.
- Dependencies, toolchain, production interfaces, retry authority, compaction behavior, and the original journal remain unchanged.

## Findings outside A6

Both findings remain report-only. Global F17 is CodeQuality FND-04.

### DEVIATION DESCRIPTION: F16

What happens: a reader sees history-source work described as pending in the A3 paragraph while the A4 paragraph records acceptance. Consequences: LOW, a stale progress interpretation; accepted evidence remains accessible. Reliability: both source paragraphs read, no runtime reproduction or correction. Assignment level: outside task. Options: retain the discrepancy as reported, or separately authorize correction of the stale sentence in migration-plan.md. The user owns that scope decision. The matching rule is outside-task defect reporting. No fix design or reproduction was undertaken.

### DEVIATION DESCRIPTION: F17 / CodeQuality FND-04

What happens: if an existing sibling UI callback's fatal assertion fails in the SDK worker, runtime.Goexit can prevent the runDone result needed by cleanup. Consequences: MEDIUM, diagnostic cleanup can block; no product cancellation incident is established. Reliability: complete conditional source path read; no failing occurrence or frequency measured. Assignment level: outside task. Options: retain as a reported test-quality limitation, or separately authorize correction of the existing callback in observer_cancellation_ui_integration_test.go. The user owns that decision. The matching rule is outside-task defect reporting. No reproduction, design, or correction occurred.

## Open questions

None for A6 acceptance. Whole-phase acceptance remains a subsequent step. F16 and F17 remain report-only; any reproduction or correction requires separate authorization.
