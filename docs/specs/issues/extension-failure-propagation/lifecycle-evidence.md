# Ordinary lifecycle failure verification

## Scope and source

This record covers ordinary agent lifecycle observation for UNT09 under the [extension failure rule](../../features/initial/prd.md#extension-failure-rule). For implementation behavior, see the [technical solution](solution.md#ordinary-agent-lifecycle). Earlier ownership and selection evidence remains qualified by its original verification records.

The directly checked baseline is revision `696171d97aaf92330e8ca51e71ee7ce6558eab55`, with Go 1.27.3 and a clean worktree. `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build` passed before edits. Combined baseline coverage was 84.6%, with an 80.0% requirement.

At that revision, `Service.observe` in `host/internal/usecase/host/lifecycle/service.go` continued after ordinary observer errors and skipped selected runtime loss. `Service.finish` in `host/internal/usecase/agent/run/service.go` returned an agent-end delivery error with the earlier outcome. `Coordinator.RunPrepared` in `host/internal/usecase/host/runcontrol/coordinator.go` returned settled delivery errors with the earlier outcome.

## Executed RED and GREEN checks

The initial ordinary-lifecycle regression tests and public fixture were executed against the baseline production source in an isolated copy, then against the first implementation. No dependency or generated interface change was needed to compile the behavioral regressions. Exact source manifests and execution logs are retained outside tracked documentation; the repository tests and fixture are sufficient to run the corrected checks.

| Regression | Observed baseline result | Corrected result |
| --- | --- | --- |
| `TestOrdinaryObserverFailureStopsChain` in `host/internal/usecase/host/lifecycle/ordinary_failure_test.go` | Ordinary errors and independently canceled observers admitted the next observer. Context issuance and unavailable participants did not produce the required classified stop. | The first failure stops the selected chain, retains original error identity, and exposes explicit current extension failure. Issue delivery failure joins the original cause. Mixed owning cancellation and classified cancellation remain failures. Settled observation follows the same rule. |
| `TestOrdinaryObserversCaptureEligibilityBeforeDispatch` in `host/internal/usecase/host/lifecycle/ordinary_participants_test.go` | Invocation started before the remaining eligibility checks. | Host selects all eligible participants before dispatch, excludes initially unavailable registrations, and fails on later selected loss even when the runtime supplies no error. |
| `TestSettledFailureOverridesEarlierOutcome` in `host/internal/usecase/host/runcontrol/terminal_failure_test.go` | Completed and Aborted remained unchanged after settled delivery failure. | Both become Failed, the original cause remains reachable, and the gate releases after settlement attempts. |
| `TestToolEndObserverFailureKeepsClientResult` in `host/internal/usecase/agent/run/observer_terminal_test.go` | Tool-end failure skipped the mandatory client tool-result event. | Host retains completed tool history, attempts the client tool-result event, and returns both observer and client delivery causes without a later tool or provider call. |
| `TestEndObserverFailureOverridesAbort` in the same file | Agent-end failure left the earlier Aborted outcome. | The final result is Failed and retains both the observer failure and the original cancellation. |
| `TestOwningObserverCancellationAbortsRun` in `host/internal/usecase/agent/run/observer_cancellation_test.go` | Owning cancellation at agent start produced Failed. | Pure owning cancellation produces Aborted, starts no provider work, and permits settlement to idle. |
| `TestPublicOrdinaryObserverFailure` in `host/internal/app/ordinary_observer_failure_integration_test.go` | All eleven lifecycle scenarios received an unexpected successful terminal instead of Failed. | Programmatic Control receives `EXTENSION_FAILED` and the full observer cause. No later observer runs in the failed chain. Start failures prevent model dispatch. Update and terminal failures prevent a later model request. Queries retain committed user, model, and tool-result history as applicable. A second request succeeds on the same extension and invokes its healthy observer chain. |
| `TestPublicUIOrdinaryObserverFailure` in `host/internal/app/ordinary_observer_ui_integration_test.go` | The real UI SDK process reported that the ordinary observer unexpectedly completed, for agent start, agent end, and agent settled. | SDK Wait and failed notification expose the same full failure. Message completion remains visible after end and settled failures. Host reports idle and accepts the next request through the same UI and extension processes. |

The ordinary public scenarios cover agent start, turn start, message start, message update, message end, turn end, agent end, agent settled, tool-execution start, tool-execution update, and tool-execution end. The external fixture uses the existing public SDK in `testdata/external-plugins/cmd/extension/ordinary_failure.go`, `ordinaryFailureObservers` and `handleOperation.runOrdinaryFailureObserver`. Its first participant fails once; the second participant records any invocation. A subsequent healthy request verifies continued extension availability.

The baseline text message-update path encountered a projection failure before entering the plugin: absent boundary delta text reached the lifecycle encoder as missing required public text. The old lifecycle service swallowed that error and still completed the public request. During correction, the healthy follow-up request reproduced the same mapping error as Failed. `StreamEvent.lifecycleContent` in `host/internal/usecase/agent/run/stream_lifecycle.go` now preserves text delta fragments and represents content boundaries with explicit empty text. The corrected public message-update scenario reaches the real plugin error and completes a healthy text-streaming follow-up request. This projection correction preserves boundary ordering and strips provider-only context as before.

The focused commands used for the baseline comparison and corrected run are:

```sh
go test -race -count=1 ./host/internal/usecase/host/lifecycle ./host/internal/usecase/host/runcontrol ./host/internal/usecase/agent/run -run 'TestOrdinary|TestSettledFailure|TestToolEndObserver|TestEndObserver|TestOwningObserver'
go test -race -count=1 -timeout=120s -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^(TestPublicOrdinaryObserverFailure|TestPublicUIOrdinaryObserverFailure)$' -v
```

The existing dispatcher tests `TestDispatcherObservesAfterClientFailureAndReturnsCompleteCauses` and `TestDispatcherSettledPreservesBothCauses` in `host/internal/usecase/host/events/service_test.go` verify client-first attempts and identity-preserving joins. They already passed before this correction. The first version of `TestObserverDeliveryFailurePreventsProviderRetry` in `host/internal/usecase/host/modelexecution/observer_delivery_test.go` also passed on the baseline. That callback rejected every event, including response reset, and the service had no retry output. Its pass established stopping at failed recovery delivery, not stopping before recovery. The strengthened test and observed correction results are recorded below. The unchanged dispatcher tests remain preservation evidence.

## Observer recovery and terminal-run correction

The correction baseline is revision `696171d97aaf92330e8ca51e71ee7ce6558eab55` plus the first ordinary-lifecycle implementation's 24 changed or new paths. The complete source was snapshotted before edits. Its 24 scoped files matched the first implementation's retained source manifest. All eight project tasks listed below passed on that baseline; combined coverage was 84.7%.

`Service.execute` in `host/internal/usecase/host/modelexecution/retry.go` previously searched the adapter error tree for `ProviderFailureError` after a streaming callback failed. A real ordinary observer can own a configured-model child whose transient provider error remains below its failed retry-handler error. The outer execution then entered recovery before Core returned its retained observer failure. Separately, `Service.finalizeProviderError` in `host/internal/usecase/agent/run/service.go` joined terminal observer failure but kept the earlier provider-derived Aborted logical run outcome. The [technical solution](solution.md#ordinary-agent-lifecycle) describes the corrected boundaries and summary timing.

The initial correction executions and the earlier captured behavioral replay reached the failures below. That earlier replay preceded the pure-owner preservation case and same-valued named selector refactors. The later handoff replay used an incomplete archive without root `go.sum`; its integration command failed during dependency setup and supplies no behavioral RED. Comparing its final inputs with source established input identity only.

For a new replay, the implementer extracted the complete pre-correction snapshot, including root and external-module dependency manifests. The implementer overlaid the exact final eight correction test/fixture inputs plus the two original ordinary public tests whose handler-kind inputs now use generated enum strings. Before and after execution, every baseline regular file outside that explicit input set matched the complete snapshot byte-for-byte, and every input matched final source. Production retained pre-correction behavior. Both focused commands below executed behavioral failures with exit 1 on that replay and passed with exit 0 on current source. Integration execution used a separate short command-local `TMPDIR`, outside the module root. Dependency manifests remained unchanged.

```sh
go test -mod=readonly -race -count=1 ./host/internal/usecase/host/modelexecution -run 'TestObserverDelivery|TestCurrentObserver|TestObserverOwning'
go test -mod=readonly -race -count=1 -timeout=180s -tags=integration -p 1 -parallel 1 ./host/internal/usecase/host/modelexecution ./host/internal/app -run 'TestTerminalObserverFailureSelectsFailedRun|TestPublicObserverNestedFailureStopsOuterRecovery|TestPublicTerminalObserverFailureAfterProviderCancel'
```

The observed pre-correction column below describes the newly executed complete final-input replay. The pure-owner and concurrent-cancellation cases already passed on the baseline and remain preservation evidence.

| Final regression and repository location | Observed pre-correction result | Corrected result |
| --- | --- | --- |
| `TestObserverDeliveryFailurePreventsProviderRetry`, `host/internal/usecase/host/modelexecution/observer_delivery_test.go` | Host invoked a forbidden second provider attempt after successful reset and retry-progress delivery. | Original-update failure stops before reset, progress, retry handlers, or another provider attempt; both original causes remain reachable. |
| `TestCurrentObserverFailureStopsRecovery`, `host/internal/usecase/host/modelexecution/observer_recovery_test.go` | Active-context transient and overflow cases recovered to success and lost the current observer error. Concurrent-cancellation cases already stopped. | Current extension failure retains nested provider and adapter identities, meaningful terminal response data, and concurrent owner cancellation. Healthy retry and overflow recipients remain unused. |
| `TestPublicObserverNestedFailureStopsOuterRecovery`, `host/internal/app/observer_nested_failure_integration_test.go` | Host emitted outer retry progress after the observer consumed its configured child's failed retry handler. | Public Failed retains extension and provider text, with zero outer retry progress and exactly the original outer and child provider requests. A healthy next request succeeds on the same connection. The retry fixture fails once, then permits recovery if called again. |
| `TestTerminalObserverFailureSelectsFailedRun`, `host/internal/usecase/host/modelexecution/terminal_observer_integration_test.go` | All four MessageEnd/TurnEnd and provider/owner-cancellation cases returned Aborted. | Core result and newly emitted summary are Failed. Committed provider response remains Aborted with its response identifier and usage. Both error identities survive; Core settles to idle. |
| `TestPublicTerminalObserverFailureAfterProviderCancel`, `host/internal/app/observer_aborted_response_integration_test.go` | Both public owner-canceled runs ended Failed but emitted an Aborted AgentEnd summary. | Failed summaries accompany Failed operation terminals. History remains provider-Aborted; a subsequent request succeeds. |

`TestObserverOwningCancellationRetainsDiagnostics` in `host/internal/usecase/host/modelexecution/observer_recovery_test.go` passes before and after correction. It checks pure owning callback cancellation after a recovered transient attempt, with the earlier provider identity retained as diagnostics. `TestCoreCurrentCancellationRetainsDiagnostics` in `host/internal/usecase/host/modelexecution/current_cause_integration_test.go` still passes, including its untyped late-terminal-delivery Aborted expectation. `TestOwningObserverCancellationAbortsRun` and `TestEndObserverFailureOverridesAbort` in the Core tests above preserve pure owning abort and later AgentEnd failure behavior. Existing provider retry tests remain part of the whole-project runs. `Service.invokeObserver` in `host/internal/usecase/host/lifecycle/service.go` now contains the shared ordinary/selection invocation policy; the lifecycle package tests retain participant eligibility, context issuance, unavailability, cancellation, diagnostic joins, first-stop, and selection-order checks.

## Whole-project checks

After the correction, the final source passed:

| Command | Result |
| --- | --- |
| `task generate` | Passed; generated output unchanged. |
| `task fmt` | Passed. |
| `task fix_dry_run` | Passed with no proposed changes; no `task fix` required. |
| `task lint` | Passed for root and external-plugin modules, with no ifaceguard errors or reported vulnerabilities. |
| `task test` | Passed, including the external-plugin module. |
| `task itest` | Passed, including the external-plugin module. |
| `task test-coverage` | Passed: 84.7%, required 80.0%. |
| `task build` | Passed for Host and standard plugins. |

Integration and coverage checks used short command-local temporary directories for Unix sockets:

```sh
short_tmp="$(mktemp -d /tmp/g9XXXX)"
TMPDIR="$short_tmp" task itest
TMPDIR="$short_tmp" task test-coverage
```

Development verification exposed an overly broad failure-code precedence change through `TestThresholdCompactionPreservesSplitBoundaryCause` in `host/internal/app/compaction_threshold_boundary_integration_test.go`. The final mapping preserves established model and compaction categories; only generic `INTERNAL` yields to an explicit current extension failure. `TestConversationObservation/TestCanceledObservation` in `host/internal/app/conversation_observation_integration_test.go` caught replacement of retained provider diagnostic text with a generic abort message. The final Core correction changes the outcome without replacing that complete message. Both scenarios passed in focused and whole-project reruns.

## Limits and remaining work

The UI check runs a real SDK/plugin process through the UI Plugin Contract. It is not a standard-TUI PTY acceptance claim. Existing platform-specific PTY qualifications remain unchanged. LSP reference queries for integration-tagged test symbols were attempted but excluded by the active Go build configuration; primary source inspection and compiled integration execution cover those references.

This record does not verify new navigation or compaction observer failure producers. UNT10 and UNT11 retain those producers, and UNT12 retains final issue verification. The issue remains in progress. No dependency, SDK settlement, protocol, session-wide environment, or commit change is part of this correction.

## Open questions

None for the implemented ordinary lifecycle behavior.
