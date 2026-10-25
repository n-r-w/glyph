# Technical Solution: Extension Failure Propagation

## Problem Statement

The [ticket](ticket.md) and [extension failure rule](../../features/initial/prd.md#extension-failure-rule) define the problem and required behavior.

## Proposed Solution

### Ordinary invocation ownership

UNT07 extends child-lifetime ownership to ordinary Host extension invocations. Host uses the operation identifier it already allocates for each invocation, scoped to that runtime connection. Session authorization remains a separate check.

`Runtime.beginInvocation` in `host/internal/infra/plugins/extension/runtime/invocation.go` registers the parent before sending the request and returns its settlement callback. The entry points use that owner:

- `Runtime.Execute` in `host/internal/infra/plugins/extension/runtime/runtime.go`.
- `Runtime.Handle` in `host/internal/infra/plugins/extension/runtime/handler.go`.
- `Runtime.ObserveLifecycle` in `host/internal/infra/plugins/extension/runtime/lifecycle.go`.
- `Runtime.runCompactionHandler` in `host/internal/infra/plugins/extension/runtime/compaction.go`.

At settlement, the callback closes admission, cancels remaining accepted work, waits for child resource release, and removes the parent record. Host owns this authoritative join; the SDK does not duplicate settlement accounting.

The Extension Contract carries `ExtensionRequest.parent_invocation_id` in `api/plugins/extension/v1/extension.proto`. In `sdk/plugins/extension/v1/server.go`, `server.handleRequest` preserves the envelope identifier during preparation. In `sdk/plugins/extension/v1/context.go`, `invocationContext` creates a separate SDK `ExtensionContext` for that invocation while retaining the session identity. `ExtensionContext.start` in `sdk/plugins/extension/v1/invocation.go` puts the captured parent identifier on every ordinary child request. The supplied context controls caller cancellation; Host retains invocation ownership and parent cancellation independently of that context.

### Admission and resource release

`Service.BeginInvocationChild` in `host/internal/usecase/host/extensionruntime/context.go` validates the connected runtime generation before addressing its connection-local parent. `Runtime.BeginInvocationChild` in `host/internal/infra/plugins/extension/runtime/invocation.go` admits work only while that parent remains live and open. A completed invocation cannot use its old SDK context to start more Host work, even when its session binding remains current. Host returns `STALE_CONTEXT` and preserves the admission cause.

`Service.Prepare` in `host/internal/controller/extension/context_admission.go` validates session access, reserves runtime work, and acquires parent ownership before preparing the action. Rejected admission releases the runtime reservation. Preparation failure records the qualifying source before cleanup. `contextOperation.Run` in `host/internal/controller/extension/service.go` links accepted execution cancellation to the parent and retains execution failure independently of the plugin's subsequent result.

`contextOperation.Release` in `host/internal/controller/extension/service.go` releases selection ownership, compaction admission, and runtime accounting before settling its invocation child. A once-only release guard also covers an accepted action whose `Run` is skipped. The `InvocationChild.Fail` and `InvocationChild.Release` contract in `host/internal/controller/extension/interfaces.go` separates immediate failure retention from ownership release. `Fail` closes admission and cancels affected work before cleanup finishes; `Release` ends accepted ownership after caller cleanup. Runtime `invocationChild.Release` in `host/internal/infra/plugins/extension/runtime/invocation.go` performs the once-only ownership decrement; it does not perform resource cleanup itself. Parent success therefore cannot race a still-owned child reservation.

### Failure propagation

`qualifyingExtensionFailure` in `host/internal/controller/extension/invocation_failure.go` inspects current error causes for explicit extension failure meaning. When a current error qualifies, the helper returns the original complete cause unchanged, including diagnostic history; otherwise it returns nil. A failure retained only as diagnostic history does not establish eligibility. Ordinary validation rejection, provider failure categories, and tool-returned execution-error results do not acquire fatal meaning merely because they contain error text.

`handlerFailureError` in `host/internal/infra/plugins/extension/runtime/handler_failure.go` identifies independent participating-handler failures. `PolicyOperationError` in `host/internal/usecase/host/extensioncontext/policy_operation_error.go` and `LogicalFailureError` in `host/internal/usecase/host/modelexecution/contracts.go` expose extension-failure meaning for their existing categories. Original causes remain wrapped throughout these mappings.

When Host settles the parent, the callback returned by `Runtime.beginInvocation` in `host/internal/infra/plugins/extension/runtime/invocation.go` returns `fatalInvocationError` for a retained qualifying child failure, even if the plugin consumed the child error and returned success. This error implements the provider-neutral `run.FatalOperationFailure` contract in `host/internal/usecase/agent/run/interfaces.go`. Host tools preserve the error across `tools.Service.Execute` in `host/internal/usecase/host/tools/service.go`; Core does not infer fatal meaning from error text or `ToolResult.IsError`.

`Service.executeCalls` in `host/internal/usecase/agent/run/service.go` persists the necessary tool result and delivers the tool terminal history before applying `classifyToolFailure` from `host/internal/usecase/agent/run/tool_failure.go`. Explicit fatal failure stops the run before another tool or provider request. It takes precedence over cleanup cancellation. `Service.observe` in `host/internal/usecase/host/lifecycle/service.go` preserves already-retained fatal nested failure meaning through ordinary lifecycle failure classification. Ordinary handler errors retain extension availability and the healthy connection.

### Invalid participant output

`Service.InvokePolicyGenerator` in `host/internal/usecase/host/extensioncontext/manual_generator.go`, `Service.InvokePolicyRequest` in `host/internal/usecase/host/extensioncontext/manual_request.go`, and `Service.InvokePolicyResult` in `host/internal/usecase/host/extensioncontext/manual_result.go` validate returned producer attribution against the selected extension. Each producer mismatch now returns the original cause through `NewPolicyOperationError` in `host/internal/usecase/host/extensioncontext/policy_operation_error.go` with `FailureExtensionFailed` from `host/internal/usecase/host/modelexecution/contracts.go`.

Before this correction, these checks returned plain errors after participant invocation had finished. A policy could consume the rejection and return CANCELED, leaving Host without the explicit failure meaning needed for parent retention. Source classification makes that invalid extension output authoritative without promoting ordinary client validation, provider errors, or all `COMPACTION_FAILED` results. The producer comparison and existing compaction authority checks are unchanged.

### Cancellation and compaction authority

`classifyCompactionFailure` in `host/internal/infra/plugins/extension/runtime/compaction_failure.go` distinguishes a received SDK `Canceled` terminal from an independently classified failure. `PolicyCancellationError` in `host/internal/usecase/host/extensioncontext/policy_cancellation.go` preserves that explicit cancellation through policy adapters. A mixed current failure remains a failure. Explicit policy and fatal failures also expose their stable category through `errtree.CodedError` in `internal/errtree/current_errors.go`, so a cancellation-only underlying sentinel cannot erase the failure's meaning. `mapCompactionParticipantFailure` in `host/internal/controller/extension/compaction_participant_failure.go` preserves every distinct current source category beside the complete text before public mapping; an enclosing category does not hide the received SDK failure category.

Ordinary invocation ownership grants no compaction mutation authority. `parent_compaction_id`, policy authority, and captured participant validation remain separate. `Service.prepareCompactionAdmission` in `host/internal/controller/extension/context_admission.go` still requires compaction lifetime admission for candidate operations. The existing compaction operations retain their captured runtime, session, participant, and mutation checks.

### Public fixture lifetime

The old external manual-compaction caller retained an `agent_settled` invocation context after returning from that observer. The delayed socket request consequently depended on detached work. The fixture now exposes an ordinary live tool in `testdata/external-plugins/cmd/compaction-policy/caller_tool.go`, whose invocation remains active through the socket exchange and child settlement.

`startLiveCallerFixture` in `host/internal/app/live_caller_fixture_test.go` uses production components and the existing Host tool entry point while the operation gate is idle. Programmatic Control still creates conversation history and reads the resulting session state. This fixture changes test ownership, not the application's operation gate: `Coordinator.RunPrepared` in `host/internal/usecase/host/runcontrol/coordinator.go` continues to retain that gate through settled delivery.

### Model and reasoning selection

UNT08 adds selection failure propagation and typed results included with Failed under the [extension failure rule](../../features/initial/prd.md#extension-failure-rule). `Service.compose` in `host/internal/usecase/host/modelselection/handlers.go` captures eligible handlers before dispatch, stops on the first handler error, invalid action, or selected runtime loss, and reaches neither final validation nor commit after that failure. Initially unavailable registrations are excluded. A valid explicit Reject keeps its rejection category and does not acquire extension-failure meaning.

`Service.selectionObservers` and `Service.ObserveSelection` in `host/internal/usecase/host/lifecycle/service.go` capture eligible selection observers before invoking either observation group. Reasoning observation precedes model observation. A failure stops later observers and the transition to the remaining group. `Service.commitSelection` in `host/internal/usecase/host/modelselection/service.go` retains the actual committed selection and the observer error. An unchanged selection emits no selection observations. Ordinary agent lifecycle observation remains outside UNT08.

`NewExtensionFailure` and `extensionFailureError` in `host/internal/usecase/host/modelselection/extension_failure.go` preserve explicit current extension-failure meaning for UNT07 retention. `qualifyingExtensionFailure` in `host/internal/controller/extension/invocation_failure.go` remains the admission to that retention policy. Parent-run public mapping uses current `errtree.CodedError` categories in `failureCode` in `host/internal/usecase/host/programmatic/prepared.go` and `runFailureCode` in `host/internal/usecase/host/ui/prepared_operations.go`. These categories supplement existing typed compaction and logical-model classifications. Historical diagnostic categories cannot establish this new mapping. Host retains authoritative child settlement; the SDK adds no second settlement layer.

`Outcome.Result` and `FailedWithResult` in `internal/operation/operation.go` separate typed result availability from terminal state. Each Host-event protocol owns `HostFailed`, containing the required shared `operation.Failed` and optional existing `HostCompleted` representation: `api/programmatic/v1/programmatic.proto`, `api/plugins/ui/v1/ui.proto`, and `api/plugins/extension/v1/extension.proto`. No opaque payload, compatibility envelope, or second terminal is introduced.

`commandPrepared.runSelection` in `host/internal/usecase/host/programmatic/prepared.go`, `preparedUIOperation.Run` in `host/internal/usecase/host/ui/prepared_operations.go`, and `contextOperation.runSelection` in `host/internal/controller/extension/context_actions.go` retain committed selection data with failure. `hostPrepared.Run` in `host/internal/controller/ui/operations.go` and `hostPrepared.Run` in `sdk/plugins/extension/v1/host_owner.go` preserve that outcome through the respective owners. `streamDelivery.Terminal` in `host/internal/controller/programmatic/delivery.go`, `operationDelivery.Terminal` in `host/internal/infra/plugins/ui/runtime/operations.go`, and `hostDelivery.Terminal` in `sdk/plugins/extension/v1/host_owner.go` send protocol-owned Failed envelopes.

The incoming SDK boundaries validate failure data and whether `HostFailed.result` matches the initiating request. UI uses `validateHostPayload` in `sdk/plugins/ui/v1/request_kind.go`; Extension uses `mapHostEvent` in `sdk/plugins/extension/v1/host_events.go` and `hostRequestKind.validateHostFailed` in `sdk/plugins/extension/v1/host_failed.go`. `Operation.waitResult` in `sdk/plugins/ui/v1/host.go` and `contextOperation.wait` in `sdk/plugins/extension/v1/context_initiator.go` return the result alongside the complete remote failure. `ModelSelectionOperation.Wait` and `ReasoningSelectionOperation.Wait` in `sdk/plugins/extension/v1/context.go` preserve both values. `isSelectionCancellationOnly` in `host/internal/controller/extension/selection.go` uses the shared current-cause classifier, so an independently canceled participant with explicit failure meaning is not treated as owning cancellation. UI `operationNotification` in `sdk/plugins/ui/v1/notification.go` retains the same data for event-driven consumers.

`Controller.Notify` in `plugins/ui/tui/internal/controller/plugin/controller.go` decodes the result included with a failed notification. `Service.Notify` in `plugins/ui/tui/internal/usecase/presentation/commands.go` applies Host-confirmed selection, reports the complete error, and settles pending work in either acknowledgement order. The TUI does not present a success or roll back committed state. Post-commit pure owning cancellation retains the prior committed-completion behavior; an independent extension failure remains Failed when cancellation is also present.

The [selection verification record](selection-evidence.md) owns UNT08 baseline, RED/GREEN evidence, check results, and evidence limits. The verification below remains the original UNT07 record.

### Ordinary agent lifecycle

UNT09 applies the failure rule to `Service.Observe`, `Service.observe`, and `Service.ObserveSettled` in `host/internal/usecase/host/lifecycle/service.go`. `Service.captureObservers` shares the selection observer eligibility mechanism: Host snapshots registrations and availability before any dispatch. Initially unavailable registrations stay excluded. Context issuance failure, selected runtime loss, and observer invocation failure stop the chain. `Service.invokeObserver` applies context issuance, invocation, selected-unavailability, cancellation, and issue-delivery policy for both ordinary and selection observation. The ordinary entry points and `Service.ObserveSelection` construct their respective events. `Service.ObserveSelection` projects ordered observer and delivery issues. The service returns `modelselection.NewExtensionFailure` with the complete original cause and any issue-delivery error. Issue diagnostics supplement that failure. Current-cause cancellation classification preserves pure owning cancellation while independent and mixed failures retain their meaning.

`Dispatcher.Deliver` and `Dispatcher.DeliverSettled` in `host/internal/usecase/host/events/service.go` retain client-first delivery and join client and observer errors. Each event has its own observer chain. A failed chain does not suppress required client terminal/history delivery or Host settlement. No global lifecycle failure state is introduced.

`Service.execute` in `host/internal/usecase/host/modelexecution/retry.go` retains nonterminal callback failure at the callback boundary. It stops before nested provider-cause extraction, retry handlers, overflow recovery, response reset, retry progress, or another provider attempt. It preserves callback and adapter causes, terminal response data, and pure owning cancellation. Provider failures without callback failure retain their retry policy.

In `host/internal/usecase/agent/run/service.go`, `Service.runTurn` returns the streaming delivery failure to Core's caller. `Service.executeCalls` retains completed tool-result history and attempts the mandatory client tool-result event even when tool-end observation fails; it joins both delivery causes before stopping later tool/model work. `Service.applyOutcome` and `Service.executeCalls` retain the failed result returned by `Service.endTurn` instead of discarding it at a continuation boundary.

After provider cancellation, `Service.finalizeProviderError` keeps the persisted model response Aborted. If MessageEnd or TurnEnd observation returns a current classified delivery failure, it selects Failed for the logical run before `Service.finish` constructs the Core result and AgentEnd summary. `RunOutcomeFailed` in `host/internal/domain/agent/model.go` includes delivery failure; `Response.Outcome` in `host/internal/domain/model/model.go` describes the earlier provider response. Earlier untyped late client-delivery behavior remains unchanged. If AgentEnd's own observer fails, `Service.finish` returns Failed after the client has already received the summary with its earlier outcome. If AgentSettled later fails, Host likewise returns a Failed final operation terminal without changing the already-sent summary. For pure owning cancellation, `Service.finish` sets the outcome to Aborted and preserves the complete existing error message.

`StreamEvent.lifecycleContent` in `host/internal/usecase/agent/run/stream_lifecycle.go` gives text content boundaries explicit empty text so valid streaming events reach observers.

`Coordinator.RunPrepared` in `host/internal/usecase/host/runcontrol/coordinator.go` retains Core settlement, settled delivery, and deferred gate release. A settlement or settled-delivery failure changes the returned outcome to Failed and joins the complete causes. Core becomes idle through its existing settlement handoff; public owners release their existing session and operation reservations.

`runPrepared.Run` and `failureCode` in `host/internal/usecase/host/programmatic/prepared.go` expose the returned failure through the existing Failed envelope. `Session.prepareSubmit`, `preparedUIOperation.Run`, and `runFailureCode` in `host/internal/usecase/host/ui/prepared_operations.go` do the same for UI. A generic model-execution `INTERNAL` wrapper yields to explicit current extension failure meaning. Established persistence, compaction, and other model categories retain their precedence. Extension runtime `Runtime.ObserveLifecycle` in `host/internal/infra/plugins/extension/runtime/lifecycle.go` continues to use Host-owned invocation settlement and its existing SDK failed handler completion; no duplicate SDK child settlement or transport is added. Already published history and completed effects remain available through existing client events and session queries.

For baseline, RED/GREEN results, public-contract evidence, check results, and limits, see the [ordinary lifecycle verification record](lifecycle-evidence.md).

### Session-tree navigation

UNT10 changes `Service.runRequestHandlers`, `Service.runResultHandlers`, and `Service.runObservers` in `host/internal/usecase/host/sessiontree/handlers.go` to return the first participant failure. The registration snapshot in `Service.handlersFor` in `host/internal/usecase/host/sessiontree/registration.go` remains unchanged. Context issuance and response validation in `host/internal/usecase/host/sessiontree/runtime.go` feed the same failure path. `participantError` and `classifyParticipantFailure` in `host/internal/usecase/host/sessiontree/participant_failure.go` preserve original causes, current extension-failure meaning, and pure owning cancellation. Host retains consumed child failures through its existing invocation ownership.

`Service.navigate` in `host/internal/usecase/host/sessiontree/service.go` records the actual commit independently of optional destination and leaf identifiers. After a committed publication failure, it still attempts mandatory postcommit observation and joins publication and observer causes. `navigationResult.uiCompletion` and `navigationResult.programmaticCompletion` in `host/internal/usecase/host/sessiontree/client_completion.go` project only actual commits. `Service.NavigateUI` and `Service.NavigateProgrammatic` in `host/internal/usecase/host/sessiontree/client_navigation.go` return that projection alongside the error.

`Service.navigateSessionTree` and `Service.navigationFailure` in `host/internal/usecase/host/programmatic/service.go` retain precommit validation categories and committed navigation data. `commandPrepared.runNavigation` in `host/internal/usecase/host/programmatic/prepared.go` emits Failed with the typed result after independent postcommit failure. `Session.runSessionOperation`, `preparedUIOperation.Run`, and `sessionOperationFailureCode` in `host/internal/usecase/host/ui/prepared_operations.go` provide the corresponding UI projection. Both terminal producers preserve the committed-completion exception only for pure owning cancellation. The shared Failed transport, UI SDK result-plus-error, and TUI apply-and-display paths described under model and reasoning selection require no navigation-specific transport change.

For UNT10 baseline, behavioral RED/GREEN, public-process acceptance, check results, and limits, see the [navigation verification record](navigation-evidence.md).

### Postcommit compaction

UNT11 changes the committed compaction producers in Programmatic Control, UI, and Extension to emit Failed with the existing typed result after independent failure. The producers preserve committed completion for pure owning cancellation and retain independent failure priority when cancellation is also present. Both manual Extension compaction and policy replacement callers retain mapped results alongside errors. The existing typed compaction SDK waits and D3 transport already preserve those values.

The TUI applies the saved entry to its confirmed displayed session and shows the complete failure. While session ownership is unresolved, the TUI shows diagnostics and settles correlation immediately, then applies the retained entry when ownership resolves. After confirmed session replacement, the TUI omits an older session's receipt while preserving its failure and settlement. For local association, receipt application, topology limits, and verification, see the [consumer correction evidence](compaction-consumer-evidence.md).

Host retains compaction receipt, diagnostic state, observer stopping, child admission, cancellation, and joining through its existing owners. The legal consumed-child regression enters through a live reasoning-selection observer while the operation gate is idle and adds no session mutation authority. Source paths, exact-input baseline RED/GREEN results, fixture corrections, reused coverage, project checks, and limits are recorded once in the [compaction verification record](compaction-evidence.md).

## Verification

### Baseline

The pre-UNT07 baseline is repository revision `ca3215a6e036dcdcf4714c43c4f50b9fcdd4a74e`, using Go 1.27.3. `task fix_dry_run`, `task lint`, `task test`, and `task build` passed. `task itest` and `task test-coverage` passed with a short command-local temporary directory; combined coverage was 84.3%, above the 80.0% threshold. The initial longer temporary path timed out in `TestUIManualCancellationStopsObserverModel` in `host/internal/app/manual_compaction_integration_suites_test.go`; the short-path rerun changed neither source nor dependency versions.

At that baseline, SDK `server.prepareInvocation` in `sdk/plugins/extension/v1/server.go` omitted ordinary envelope ownership, controller `Service.Prepare` in `host/internal/controller/extension/context_admission.go` admitted only runtime and optional compaction lifetimes, and Core `Service.executeCalls` in `host/internal/usecase/agent/run/service.go` converted an execution error to tool history before continuing. These baseline source locations remain the comparison points for the regressions below.

### RED and GREEN evidence

The public regressions were run against the baseline Host/Core source with their test and external fixture additions. The Core fatal-error test also used generated mock support. Their RED results below were observed executions, not inferred outcomes.

These historical named runs used captured fixture variants. They are not source-identical baseline replays of every final fixture. In `testdata/external-plugins/cmd/compaction-policy/policy_run.go`, `policyOperation.runReplacement`, the wait-before-commit model-error branch now returns `nil, modelErr` directly. The earlier fixture returned `o.attachAcquiredState(newPolicyFailure(modelErr)), nil`. Those branches differ in cancellation/error propagation. Earlier named RED/GREEN observations apply to their captured sources; they do not prove a baseline runtime RED for every final fixture variant.

| Regression and repository symbol | RED result | Corrected result |
| --- | --- | --- |
| `host/internal/app/consumed_configured_public_integration_test.go`, `TestPublicConsumedExtensionFailureFailsParent` | Parent terminal was not `Failed` after a real tool consumed its configured child's retry-handler failure. | Parent emits `Failed` with both `retry handler full unique failure text` and the original provider failure. The plugin's later Host request is rejected; an independent run and ordinary tool succeed on the same extension connection. |
| `host/internal/app/consumed_child_integration_test.go`, `TestConsumedNestedCompactionFailureStopsCore` | Core attempted another provider snapshot after the real tool consumed a manual-compaction handler failure. | Core fails with the handler cause, retains tool-result history, starts neither the next tool nor another provider request, releases admission, and accepts an independent invocation. |
| `host/internal/app/stale_invocation_public_integration_test.go`, `TestPublicStaleInvocationCannotReuseLiveSession` | Reusing the completed invocation's context succeeded with an empty rejection category. | The same session binding remains valid, but the old parent is rejected with `STALE_CONTEXT`. The current ordinary tool and run still complete. |
| `host/internal/usecase/agent/run/fatal_tool_test.go`, `TestConsumedChildFailureStopsLaterTools` | Core invoked the second tool; concurrent cancellation changed the outcome to aborted. | Explicit fatal meaning stops later calls and remains failed under cleanup cancellation. |
| `host/internal/infra/plugins/extension/runtime/invocation_test.go`, `TestInvocationLinkObservesCancellationBeforeRun` | Linking an already-canceled parent initially returned a live child context. | Linking observes parent cancellation before child execution. |
| `host/internal/controller/extension/invocation_test.go`, `TestInvocationRunAndSkippedRelease` | Parent ownership was released before runtime resource release. | Runtime cleanup precedes once-only parent child settlement, including skipped execution. |
| `host/internal/usecase/host/extensioncontext/policy_failure_classification_test.go`, `TestExplicitPolicyFailureWinsCancellation`, and `host/internal/infra/plugins/extension/runtime/fatal_failure_classification_test.go`, `TestExplicitFatalFailureWinsCancellation` | Explicit classified failures were treated as cancellation when their only underlying cause was a cancellation sentinel. | Explicit failure meaning survives; separately received canceled participant terminals retain cancellation semantics. |

`TestConsumedChildFailureStopsLaterTools` in `host/internal/usecase/agent/run/fatal_tool_test.go` asserts source identity with `ErrorIs`, failed run outcome, and mock expectations that prevent later tool/provider calls. It does not inspect history. History retention is asserted by `TestConsumedNestedCompactionFailureStopsCore` through `assertConsumedCompactionStopsCore` in `host/internal/app/consumed_child_integration_test.go`.

Focused commands:

```sh
go test -count=1 ./host/internal/usecase/agent/run -run '^TestConsumedChildFailureStopsLaterTools$'
go test -race -count=1 ./host/internal/infra/plugins/extension/runtime -run 'TestInvocation|TestParentSuccess|TestChildCaller'
go test -race -count=1 ./host/internal/controller/extension -run '^TestInvocation'
go test -race -count=1 -tags=integration ./sdk/plugins/extension/v1 -run '^TestInvocationContextOwnsDifferentCallerContext$'
go test -count=1 ./host/internal/usecase/host/extensioncontext ./host/internal/infra/plugins/extension/runtime -run '^TestExplicit.*FailureWinsCancellation$'
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^(TestConsumedNestedCompactionFailureStopsCore|TestPublicConsumedExtensionFailureFailsParent|TestPublicStaleInvocationCannotReuseLiveSession)$' -v
```

The ownership tests in `host/internal/infra/plugins/extension/runtime/invocation_test.go` and `host/internal/infra/plugins/extension/runtime/invocation_cleanup_test.go` cover concurrent invocation isolation, connection-local identifier reuse, asynchronous child joins, cancellation before resource release, caller cancellation, and once-only release. `TestInvocationAdmissionChecksExactRuntimeGeneration` in `host/internal/usecase/host/extensionruntime/invocation_test.go` checks runtime generation validation. `TestInvocationAdmissionRejectionReleasesRuntime` in `host/internal/controller/extension/invocation_admission_test.go` and `TestInvocationPreparationFailureReleasesAdmission` and `TestInvocationRunAndSkippedRelease` in `host/internal/controller/extension/invocation_test.go` cover rejected admission, preparation failure, skipped execution, ordinary errors, cancellation, and accepted cleanup. `TestInvocationContextOwnsDifferentCallerContext` in `sdk/plugins/extension/v1/invocation_integration_test.go` checks concurrent parent association with an unrelated caller context.

Additional helper probes against the baseline failed compilation because the ordinary invocation API, protocol parent accessors, generated mocks, or explicit policy cancellation type did not exist. The five packages were runtime ownership, controller admission, runtime-generation admission, policy cancellation, and SDK invocation context. These API-absence compilation RED results do not establish a separate executed baseline behavioral RED for every helper test. The recorded command forms, run after copying the helper tests into the baseline, were:

```sh
for package in \
  host/internal/infra/plugins/extension/runtime \
  host/internal/controller/extension \
  host/internal/usecase/host/extensionruntime \
  host/internal/usecase/host/extensioncontext
do
  go test -count=1 "./$package" -run 'TestInvocation|TestParentSuccess|TestChildCaller|TestPolicy'
done
go test -count=1 -tags=integration ./sdk/plugins/extension/v1 -run '^TestInvocationContextOwnsDifferentCallerContext$'
```

Each package returned exit 1 with build failure, including missing `beginInvocation`, `BeginInvocationChild`, `InvocationChild`, `PolicyCancellationError`, and `GetParentInvocationId`. The named cancellation-link, release-order, and explicit-failure development regressions in the table are observed behavioral failures during implementation, separate from these helper compilation probes.

`TestTerminalRefusalAcrossPublicClients` in `host/internal/app/terminal_failure_modes_integration_test.go` now expects a failed parent after a configured child extension failure, with no second configured request or later provider report. Its provider-error and retry-cancellation cases retain their prior behavior. Existing generator and result-participant cancellation, mixed-failure, and classified-failure scenarios remain part of the focused integration checks. `TestGeneratorClassifiedCancellation` in `host/internal/app/manual_generator_integration_test.go` exposed loss of the original `INTERNAL` source category after the enclosing error gained a category. The corrected participant mapping retains both categories, and that scenario passed with pure and mixed cancellation cases.

### Consumed invalid-producer correction

`TestConsumedInvalidProducerFailsPublic` and `TestConsumedInvalidProducerStopsCore` in `host/internal/app/consumed_invalid_output_integration_test.go` cover generator, request, and result producers through real plugin processes. The policy fixture consumes participant rejection and returns CANCELED without an error. These six cases were executed against the current UNT07 implementation before the three producer-source corrections, rather than against an inferred baseline outcome.

All three public RED cases received `completed:{compaction:{canceled:true}}` instead of Failed. All three Core RED cases attempted another `MockModelRuntime.Snapshot` from `host/internal/usecase/agent/run/interfaces_mock.go` after the ordinary tool consumed the successful canceled child. After source classification, all six cases passed. Each public case checks the complete producer mismatch, preserved original history, no persisted replacement, and a healthy independent request on the same connection. Each Core case uses `assertConsumedCompactionStopsCore` in `host/internal/app/consumed_child_integration_test.go` to check failed outcome, full cause, rejected later owned Host work, retained first tool-result history, no second tool/provider turn, released gate admission, and an independent successful tool on the same runtime.

The focused command was identical before and after the source correction, with a short command-local `TMPDIR`:

```sh
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestConsumedInvalidProducer(FailsPublic|StopsCore)$' -v
```

This RED/GREEN comparison preserves its exact pre-correction test/fixture source and corrected source outside tracked documentation. No diagnostic-only overlay or source probe is required by the regression tests.

### Project checks

The final whole-project checks passed on the implemented source:

| Check | Result |
| --- | --- |
| `task generate` | Passed; protobuf and mock generation used the repository tools. |
| `task fmt` | Passed. |
| `task fix_dry_run` | Passed with no proposed changes; no `task fix` was needed. |
| `task lint` | Passed with zero root and external-plugin lint issues, no ifaceguard errors, and no reported vulnerabilities. |
| `task test` | Passed, including the external-plugin module. |
| `task itest` with short command-local `TMPDIR` | Passed, including the external-plugin module. |
| `task test-coverage` with short command-local `TMPDIR` | Passed: combined coverage 84.4%, required 80.0%. |
| `task build` | Passed for Host and standard extension and UI plugins. |

The executed command set is:

```sh
task generate
task fmt
task fix_dry_run
task lint
task test
short_tmp="$(mktemp -d /tmp/g7XXXX)"
TMPDIR="$short_tmp" task itest
TMPDIR="$short_tmp" task test-coverage
task build
```

The first whole-project integration run exposed two cancellation classification mismatches and three configured-SDK expectations for continuation after extension failure. Those paths and expectations were corrected. A subsequent integration run exposed loss of the original generator failure category; the source-category preservation correction is recorded above. The final whole-project integration run passed. Library versions remain unchanged.

## Overengineering and Overspecification Considerations

The mechanism uses the existing invocation identifiers, runtime generation checks, operation lifecycle, and compaction authorization paths. The connection-local owner is shared by the implemented ordinary invocation entry points. The separate failure-retention and release operations are necessary to cancel affected work immediately while still joining accepted cleanup. No session-binding redesign, global workflow engine, invocation serialization, generic payload framework, or new provider feature is introduced.

## Limits and Remaining Scope

UNT07 covers ordinary invocation ownership and consumed failures that existing child APIs return as errors. Its verification above remains scoped to that unit. UNT08 adds the selection correction and shared typed Failed transport described above; the [selection verification record](selection-evidence.md) records its evidence separately.

UNT09 implements ordinary agent lifecycle chain failure propagation and terminal settlement, with scoped evidence in the [ordinary lifecycle verification record](lifecycle-evidence.md). UNT10 implements session-tree chain failure propagation and committed Failed producers, with scoped evidence in the [navigation verification record](navigation-evidence.md). UNT11 implements postcommit compaction Failed producers with retained typed results; see the [compaction verification record](compaction-evidence.md). Both scoped issues are verified and complete; see the [final cross-cutting verification](../../../artefacts/pre-phs08-corrections-verification.md). This work does not reopen the accepted earlier phase baselines. PHS-08 remains unstarted.

The manual-compaction regression invokes an ordinary Host tool directly while the application operation gate is idle. The assembled Programmatic regression uses a configured-model child and an existing retry-handler failure, so it verifies the public failed terminal without changing run-gate behavior.

Integration checks use short command-local temporary paths for Unix sockets. The baseline timeout and its passing short-path rerun are identified in Verification above; this correction makes no broader claim about unrelated cancellation or provider behavior. The active LSP index excluded integration-tagged files from reference queries; those queries were attempted, and primary source inspection plus compiled integration tests checked the affected callers without changing session-wide build flags.

## Open Questions

None for the implemented ownership, selection, ordinary lifecycle, navigation, and compaction semantics. The remaining issue scope is identified above.

## References

- [Ticket](ticket.md): full issue scope and acceptance criteria.
- [Extension failure rule](../../features/initial/prd.md#extension-failure-rule): normative failure and cancellation exceptions.
- [Roadmap](../../../roadmap.md#extension-failures-stop-the-invoking-operation): delivery status before PHS-08.
