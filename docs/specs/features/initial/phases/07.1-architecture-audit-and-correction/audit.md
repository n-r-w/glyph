# Review results: Implemented product architecture

The audit covers the whole implemented product, including partial PHS-07, against the [ticket](ticket.md). Production source is at `86be985c47cb3719cd98c7e611af6173c5692cfc`. Architecture authority includes the contract-import clarification committed as `9461f0436fbd3700455e782441ad175f670bb9f6`. That commit changes no production source.

[Coverage](#package-and-contract-coverage) accounts for all 72 production Go packages, 273 handwritten production Go files, and 16 protobuf sources. Experiments, fixtures, generated bodies, and test support have explicit dispositions there. New product capabilities and cosmetic cleanup are outside this review.

## Key definitions and abbreviations

- Existing import edge. A package import present at the source baseline.
- Runtime edge. A call through a concrete method, interface, callback, or transport. Its direction can differ from the import direction.
- Proposed assertion edge. An absent implementation-to-consumer import used to test the feasibility of a correction.
- Logical independence. Independence from another component's concrete implementation, application state, and policy. Importing its consumer interface and provider-neutral contract types does not by itself violate this boundary.

## Summary

Outcome: Request changes.

- FND-01 and FND-02 require correction of assembly, input, execution, and output responsibilities across complete paths.
- FND-03 and FND-04 require ownership of interface-specific types and storage metadata at their consumers and storage implementation.
- FND-05 through FND-09 cover lost error information, text-dependent TUI state, misplaced execution policy, incomplete contracts, and a forwarding owner with no behavior.

The user approved the original [correction plan](solution.md), then authorized revised U5 after its documentation update. Its [implementation evidence](solution.md#implementation-evidence) records completed correction units. FND-01 through FND-09 describe the audit baseline. Subsequent findings identify their reviewed implementation commits. Passing compilation or tests alone does not close them. [U8 accounting](u8-evidence.md) records the resulting dispositions for all nine findings and 72 baseline packages. Main-agent U8 inspection passed. Independent integrated review of `9a275b2b2bc5964e5ecd1f0cefc4f7444c971c2a` found FND-10 and FND-11. Their U6 correction is committed as `b03095a839ba1a02d9c5c97217545436aeb3197e`. Review of that commit found FND-12 through FND-14. The O72-1 [U6 retention implementation and local checks](solution.md#u6-retention-correction) are committed as `ded154978ff2a3e17ba0cd3d7cf231af2b6f05d1`. Independent review of that commit found FND-15 through FND-17. The user approved the [D1/D2 corrections](solution.md#qst-05-runtime-completion-meaning-and-tui-submission-causality) after alignment. The phase remains blocked and explicit final acceptance remains pending. The [TUI ownership gap](tui-ownership-gap.md) supplements the baseline: the initial FND-03/FND-09 disposition did not establish application-state, event-contract, SDK-output, and rendering owners. Revised U5 is implemented and independently verified. The user authorized the remaining units after committing the assertion-placement correction.

## Issues overview

- **Major FND-01**. Startup code implements services that remain on runtime request and delivery paths.
- **Major FND-02**. Some input components also provide outgoing application services. Callbacks hide reverse dependencies, and the coordinator infers Core settlement state from history rather than its owner.
- **Major FND-03**. Commands and response aggregates live in domain or interface-free shared packages, or belong to another boundary's consumer.
- **Major FND-04**. Domain and application code select the repository's file-format version.
- **Major FND-05**. SDK, provider, and session boundaries discard parts of source errors.
- **Major FND-06**. TUI state cleanup depends on diagnostic wording after the supplied error category has been discarded.
- **Major FND-07**. Filesystem catalogues decide startup acceptance, and the bash input controller owns execution timeout.
- **Minor FND-08**. Several consumed error contracts and handwritten implementations lack assertions; two UI port methods have no use at their declared consumer.
- **Minor FND-09**. The baseline presentation usecase only forwards to `State.Apply`. Removing that wrapper alone does not establish the correct owner of TUI application behavior. Revised U5 resolves that ownership through the real application usecase and separate input/output owners.

- **Major FND-10**. Codex failed SSE branches truncate source diagnostics before returning the cause and failed response.
- **Major FND-11**. Failed Programmatic terminal delivery and startup diagnostic writes discard the source cause.
- **Major FND-12**. The generic operation owner discards failed work after delivery cancellation prevents `Terminal`.
- **Major FND-13**. Rejection, queued error output and SDK completion paths do not retain their source causes through connection completion.
- **Major FND-14**. Compatible Responses ignores a top-level provider error event and substitutes an incomplete-stream message.

## Findings

### Major

#### FND-01: Assembly implements outgoing services and delivery transformations

- Location: [app/sessions.go](../../../../../../host/internal/app/sessions.go), `modelRequesterBinding` and `pricingCatalogBinding`, lines 40 through 99; [app/lifecycle.go](../../../../../../host/internal/app/lifecycle.go), lines 9 through 17; [app/ui.go](../../../../../../host/internal/app/ui.go), `runUIWithPaths` and `mapUIExtensionLoadReport`; [app/programmatic.go](../../../../../../host/internal/app/programmatic.go), `runProgrammaticWithPaths`.
- Issue: The two binding objects implement session consumer ports and forward calls to the provider catalogue. Assembly also implements lifecycle issue delivery, converts startup reports to UI projections, and supplies the UI activation callback. FRQ-03, FRQ-06, and FRQ-09 require those responsibilities to have concrete runtime owners outside assembly.
- Impact: Mutable late binding and mode-specific closures determine runtime behavior. Moving these wrappers unchanged would retain the cause.
- Scenario: Session accounting calls `app.pricingCatalogBinding.Pricing`; navigation calls `app.modelRequesterBinding.Request`. Lifecycle issue delivery calls an app implementation before reaching a client recipient.
- Assessing realism: High. All three modes construct these bindings. The UI initialization path also changes warning-delivery state and activates extensions through the app callback.
- Recommendation: Construct actual providers and consumers in dependency order. Put runtime delivery and initialization transformations with their responsible input, output, or application owner. Keep process construction, startup invocation, shutdown, and resource cleanup in assembly.
- Verification: Trace first model-result accounting, branch summary requests, initialization warnings, observer issues, and runtime failures in all three modes. Preserve the storage failure boundary specified by PHS-04 FLR-01, which requires failure before dependent provider work and client initialization, not a forwarding object.

#### FND-02: Input, application execution, and output dependencies are mixed

- Location: [headless/renderer.go](../../../../../../host/internal/controller/cli/headless/renderer.go), lines 16 through 35; [events/coordinator.go](../../../../../../host/internal/usecase/host/events/coordinator.go), lines 22 through 114; [events/service.go](../../../../../../host/internal/usecase/host/events/service.go), lines 13 through 56; [UI operations transport](../../../../../../host/internal/infra/plugins/ui/runtime/operations.go), `receiveOperations`; [Programmatic controller](../../../../../../host/internal/controller/programmatic/service.go), `PublishSessionEntry` and `PublishExtensionIssue`; [interactions/service.go](../../../../../../host/internal/usecase/host/interactions/service.go), lines 8 through 54.
- Issue: The outgoing boundary ledger below identifies undeclared service dependencies and mixed consumers/implementations. Sessioncontrol forwards to session/navigation/gate owners without owning session state or reservation policy. The headless input package implements `startup.Reporter`. UI infrastructure is the actual command controller, but invokes Host preparation through a callback on an outgoing Host port. Host interactions import concrete Codex infrastructure. These are FRQ-02, FRQ-03, FRQ-07, and FRQ-09 violations, not defects in assertions that expose the relationships.
- Impact: Local replacement of a callback with an interface can close an import cycle without resolving responsibility. The named UI controller's interface covers session startup, not receipt of UI commands. The coordinator's separate settlement inference can leave Core and Programmatic output active after a canceled operation has ended.
- Scenario: A UI command reaches `Session.Prepare` from infrastructure without passing through `controller/ui`. An extension message reaches Programmatic output through a callback into the same package that consumes the Host input interface. Canceling a run during an `AgentStart` observer, before the first history append, follows the settlement mismatch traced below.
- Assessing realism: High. These are normal request, event, authentication, commit and targeted-cancellation paths. The cancellation consequence is source-derived; this audit did not add an executable reproduction. No existing compiler cycle is inferred merely from runtime call direction.
- Recommendation: Separate actual input consumers, application orchestration, and output implementations. Define each service dependency at its caller. Core must report settlement need from its own transition to awaiting settlement, not from history length or error classification. Core can implement Host-owned contracts under APC-13; remove the existing reverse edges before adding its assertions. Keep local transformations, release functions, construction callbacks, and asynchronous acknowledgements that do not conceal a service owner. The observable cancellation-recovery correction is approved under [QST-03](solution.md#qst-03-cancellation-settlement-recovery). [U2 evidence](solution.md#u2-run-control-events-and-admission) records its implementation and executable RED/GREEN results. [U8 accounting](u8-evidence.md#finding-dispositions) records the resulting FND-02 owners.
- Verification: Trace imports and runtime calls for every ledger row. Add a failing observer-cancellation regression before the behavioral correction. Preserve reservation lifetime through settlement, client delivery before observers, one terminal result, navigation/append enqueue order, and waits outside the session commit lock.

##### Settlement-state mismatch

[Core Run](../../../../../../host/internal/usecase/agent/run/service.go), lines 76 through 93, begins the run and delivers `AgentStart` before appending user history. [Lifecycle observation](../../../../../../host/internal/usecase/host/lifecycle/service.go), lines 149 through 204, returns targeted caller cancellation while an observer is running. Core then returns through `finish`, which sets `StatusAwaitingSettlement` even when added history is empty, at lines 647 through 672.

[Coordinator.RunPrepared](../../../../../../host/internal/usecase/host/events/coordinator.go), lines 100 through 115, skips Core settlement and settled delivery when history is empty and the error is not the persistence sentinel. Its deferred gate release still runs. [Programmatic Delivery.finish](../../../../../../host/internal/usecase/host/programmatic/delivery.go), lines 125 through 139, does not clear the active association; the skipped `DeliverSettled` owns that transition at lines 235 through 245. A subsequent Programmatic request can therefore remain Busy. UI prepared cleanup can announce Idle while Core still rejects another run. Core's actual transition, not the coordinator's history inference, must determine settlement.

The mismatch above describes the audit baseline. The implemented run-control result now reports Core's settlement transition directly. The [U2 evidence](solution.md#u2-run-control-events-and-admission) records direct client gate consumption and dispatcher/output assertions. The [U3 evidence](solution.md#u3-session-queries-navigation-and-publication) records direct session and navigation contracts, sessions-owned entry publication, and removal of sessioncontrol. [U8 accounting](u8-evidence.md#finding-dispositions) records the resulting owners for the other ledger responsibilities.

##### Outgoing boundary ledger

Paths in the consumer column are under `host/internal/usecase/host`.

| Consumer and source | Runtime implementation or hidden responsibility |
| --- | --- |
| [events.Coordinator](../../../../../../host/internal/usecase/host/events/coordinator.go), `execute`, `settle`, `tryAcquire` | Core execution and settlement; operation-gate acquisition. |
| [events.Dispatcher](../../../../../../host/internal/usecase/host/events/service.go), `deliverAgent`, `deliverSettled` | Headless renderer, UI delivery, and Programmatic run-correlated delivery. |
| [lifecycle.IssueDelivery](../../../../../../host/internal/usecase/host/lifecycle/interfaces.go) | App forwarding implementation, followed by mode-specific output. |
| [extensionruntime.Service](../../../../../../host/internal/usecase/host/extensionruntime/service.go), `reportFailure` | UI/headless output and Programmatic logging selected in assembly. |
| [extensioncontext.Service](../../../../../../host/internal/usecase/host/extensioncontext/service.go), `messagePublisher` | Publisher passed into the active-session commit path. |
| [sessions.Service](../../../../../../host/internal/usecase/host/sessions/service.go), `AppendExtensionMessage` | Stored client service dependency reaches commit through an externally bound publisher; acknowledgement wait follows lock release. |
| [programmatic.Service](../../../../../../host/internal/usecase/host/programmatic/service.go), `stateSnapshot`, `historySnapshot`, `connectionPublisher` | Core state, session history, and unsolicited output through the input controller. |
| [sessioncontrol.Service](../../../../../../host/internal/usecase/host/sessioncontrol/service.go), `tryAcquire` | Operation-gate ownership supplied as a method value. |
| [ui.Session](../../../../../../host/internal/usecase/host/ui/session.go), `afterInitialization` | App changes warning-delivery state and activates runtime monitoring. |
| [ui.Channel](../../../../../../host/internal/usecase/host/ui/interfaces.go), `RunOperations` | Infrastructure receives commands and invokes Host preparation through a callback. |
| [interactions.Service](../../../../../../host/internal/usecase/host/interactions/service.go), `present` | UI authorization presentation; the same service asserts concrete-provider-owned `codex.Interaction`. |
| [startup.Reporter](../../../../../../host/internal/usecase/host/startup/interfaces.go) | Output implemented in the headless input-controller package. |

##### Existing paths versus proposed assertion edges

The baseline import graph is acyclic. In each row, only the last edge is absent today. These paths are diagnostics, not proposed target imports.

| Existing import path | Proposed edge that would close a cycle |
| --- | --- |
| `host/events → agent/run` | `agent/run → host/events` for execution/settlement. |
| `host/events → host/ui`, and independently `host/events → host/programmatic` | Each client delivery implementation back to `host/events`. |
| `host/events → controller/cli/headless` | Headless event output back to `host/events`. |
| `host/lifecycle → host/events → host/ui` | UI issue delivery back to `host/lifecycle`. |
| `host/lifecycle → host/events → host/programmatic → controller/programmatic` | Programmatic controller issue delivery back to `host/lifecycle`. |
| `host/programmatic → controller/programmatic` | Controller connection publication back to `host/programmatic`. |
| `infra/plugins/ui/runtime → host/ui` | Host command implementation back to the infrastructure command consumer. |
| `host/extensioncontext → host/lifecycle → host/events → host/ui` | UI message publication back to `host/extensioncontext`. |
| `host/extensionruntime → host/lifecycle → host/events → host/ui` | UI runtime-failure output back to `host/extensionruntime`. |

`host` in this table means `host/internal/usecase/host`; `agent/run` means `host/internal/usecase/agent/run`. Source imports and bindings are in the ledger's linked files and the three mode files under [app](../../../../../../host/internal/app).

The operation-scoped navigation reporter in [CommitNavigation](../../../../../../host/internal/usecase/host/sessions/navigation.go) is different from the stored append publisher. It projects and enqueues one committed snapshot under the lock. Its function form is retained; FND-03 addresses its payload ownership. No publisher interface is required solely to replace that function.

#### FND-03: Method types have no owning consumer at their location

- Location: audit-baseline `host/internal/domain/ui`, `Command`, `Frame`, `Initialization`, `Discovery`, and navigation projections; audit-baseline `host/internal/usecase/host/sessionnavigation`, `Request`, `Progress`, `Result`, and `OperationIssue`; [domain/session/session.go](../../../../../../host/internal/domain/session/session.go), `Replacement`, `Summary`, and `InformationSnapshot`; [extensionruntime/interfaces.go](../../../../../../host/internal/usecase/host/extensionruntime/interfaces.go), `ExtensionRuntime`; audit-baseline TUI commands in `plugins/ui/tui/internal/domain/presentation/presentation.go`, lines 458 through 516, and audit-baseline `TreeCommand` in `plugins/ui/tui/internal/domain/presentation/tree.go`, lines 91 through 101.
- Issue: UI and TUI command unions describe input/output contracts, not shared domain entities. Navigation types occupy an interface-free shared package. Session query/replacement aggregates are domain members. Runtime-manager method signatures reuse startup, session-tree, and lifecycle consumer aggregates across another interface boundary. FRQ-04 requires commands and response aggregates beside their consuming interfaces.
- Impact: A client response change alters domain or shared-contract packages. Runtime transport signatures depend on another capability's response aggregate rather than their actual consumer's contract.
- Scenario: UI preparation consumes a command created by transport; session replacement returns an information aggregate through client usecases; registration passes `startup.PendingRegistration` through the runtime manager's outgoing interface; terminal input sends `presentation.Command` to Host dispatch.
- Assessing realism: High. The declarations and producers/consumers exist on every corresponding path. Shared `model.Selection`, tool values, session entries, and provider-neutral lifecycle facts are not violations merely because several consumers use them.
- Recommendation: Assign each implemented command/response boundary to its actual consumer. Remove interface-free shared contract ownership. Map genuinely different boundary representations at their owners; do not clone types or preserve old names through aliases. Keep shared domain concepts with their behavior.
- Verification: Follow each declaration to its producer, consumer, mapper, and implementation assertion. General entry-to-history projection in [sessiontree/history.go](../../../../../../host/internal/usecase/host/sessiontree/history.go) has only [sessions/history.go](../../../../../../host/internal/usecase/host/sessions/history.go) as its production caller and belongs with session history, not navigation orchestration.

#### FND-04: Storage schema selection leaks into domain and application code

- Location: [domain/session/session.go](../../../../../../host/internal/domain/session/session.go), `Header.Version`, lines 34 through 44; [sessions/service.go](../../../../../../host/internal/usecase/host/sessions/service.go), lines 27 and 104 through 115; [replacement_operations.go](../../../../../../host/internal/usecase/host/sessions/replacement_operations.go), lines 90 through 94; [repository service](../../../../../../host/internal/infra/persistence/sessions/service.go), lines 26 and 203 through 205.
- Issue: Both the active-session usecase and the repository select persisted schema version 2. A domain header carries that codec selector. FRQ-05 and persistence ownership require storage-format selection at the repository boundary.
- Impact: Changing file encoding requires changes to domain and application construction as well as the codec.
- Scenario: Session creation, fork, and clone construct headers with the repository version before the repository encodes them.
- Assessing realism: High for storage maintenance. No incompatible file format is proposed by this finding.
- Recommendation: Make the repository select and validate the wire version. Retain domain session identity and metadata without its storage schema selector.
- Verification: Creation, append, replay, fork, and clone must retain the version-2 serialized format and recovery behavior. This is ownership correction, not a migration or compatibility layer.

The [U3 evidence](solution.md#u3-session-queries-navigation-and-publication) records the implemented session portion of FND-03 and the repository-only version ownership from FND-04. Host clients own the consumed navigation and stored-list results. Sessions projects validated loaded state into those results. Public row and entry projection remains at the Host clients. Version-2 replay and replacement tests passed uncached. Main-agent U3 inspection passed as recorded in the linked evidence. Independent whole-product verification and user acceptance remain pending.

#### FND-05: Error boundaries discard source causes

- Location: audit-baseline `sdk/plugins/extension/v1/external_error.go`, lines 9 through 37, removed by U6; [host.go](../../../../../../sdk/plugins/extension/v1/host.go), `mapTerminalExtensionEvent` and peer-error handling; [Codex provider.go](../../../../../../host/internal/infra/providers/openai/codex/provider.go), `Driver.streamError`, lines 410 through 421; [sessiontree/handlers.go](../../../../../../host/internal/usecase/host/sessiontree/handlers.go), lines 44 through 52 and 82 through 105; [sessions/replacement_operations.go](../../../../../../host/internal/usecase/host/sessions/replacement_operations.go), lines 59 through 62.
- Issue: The SDK truncates errors above 65,536 bytes, including Failed, Rejected, ordinary handler errors, protocol-error context, and gRPC statuses. Codex HTTP 401 replaces the SDK error with `ErrSignInRequired`. Navigation action validation reduces source errors to a boolean and reports generic invalid-action text. Label mutation replaces the domain cause with a sentinel. These violate the complete-error rule in AGENTS.md and NFQ-02.
- Impact: Outer layers cannot recover discarded text. Error codes remain available in some paths but cannot substitute for the cause.
- Scenario: A plugin sends a long diagnostic; Codex returns HTTP 401 with detail; an extension supplies an invalid navigation target; a client labels an unknown entry.
- Assessing realism: High for authentication and invalid targets; medium for oversized plugin diagnostics. All inputs can reach production boundaries. Existing SDK tests explicitly expect truncation.
- Recommendation: Preserve category and original cause together across these paths. Remove lossy non-secret truncation from error transport. The changed observable error text is approved under [QST-01](solution.md#qst-01-complete-error-text-corrections) and still requires a failing executable regression test before implementation.
- Verification: The three uncached SDK tests listed below pass because they assert the lossy baseline. Corrected tests must check intact suffixes and wrapped causes, stable classifications, ordinary-handler continuation, and the absence of unintended commits.

The [U6 evidence](solution.md#u6-complete-error-text) records the implemented FND-05 correction. Oversized SDK diagnostics, Codex HTTP 401, invalid navigation handler data, and absent label targets now retain their source text and classifications. The evidence includes executable RED/GREEN, ordinary-handler continuation, no-mutation checks, both Host client mappings, and the required project checks. Main-agent source inspection and repeated project and affected uncached race checks passed.

#### FND-06: TUI persistence cleanup depends on diagnostic wording

U7 closes this baseline finding and [BLK-05](tui-ownership-gap.md#blk-05-persistence-cause-cleanup-still-targets-the-old-presentation-boundary). [Source traces and executable RED/GREEN](solution.md#u7-run-persistence-cause-and-tui-cleanup) cover both client contracts and the TUI application owner. The observations below describe the audited baseline.

- Location after U5: [request_mapping.go](../../../../../../plugins/ui/tui/internal/controller/plugin/request_mapping.go), `mapConnectionError`; [application commands](../../../../../../plugins/ui/tui/internal/usecase/presentation/commands.go), `operationErrorEvent`; [application state](../../../../../../plugins/ui/tui/internal/usecase/presentation/state.go), `projection.applyError`.
- Issue: Connection mapping discards the category, and application operation-failure policy reduces `FailureError` to text. Application state still clears provisional model and tool output only for text beginning with `session persistence failed`. The public category becomes an implicit English-text protocol, contrary to FRQ-04, FRQ-05, and NFQ-02.
- Impact: Adding error context can change projection cleanup while leaving the supplied failure category unchanged. Matching unrelated text can also trigger cleanup.
- Scenario: A session operation supplies `PERSISTENCE_UNAVAILABLE`, but the TUI drops that category. An accepted agent run instead always supplies `INTERNAL` through [prepareSubmit](../../../../../../host/internal/usecase/host/ui/prepared_operations.go), lines 208 through 226. Its Core persistence sentinel has the text used by the domain prefix test, so retaining the existing run category alone cannot replace that test.
- Assessing realism: High for the information loss and maintenance dependency. The audit does not claim an uncached end-to-end reproduction of every cleanup path.
- Recommendation: Carry a source-backed persistence distinction to a presentation-owned semantic cause and apply cleanup to that cause. Preserve diagnostic text separately. The baseline still emits only `INTERNAL` for accepted-run failures. The public-contract change is approved under [QST-02](solution.md#qst-02-narrow-run-persistence-distinction) and is implemented in U7; moving the text comparison into a mapper would not correct the cause.
- Verification: Exercise operation and connection mappings and the source-to-client run path. Equal persistence causes with different text must produce equal cleanup; unrelated causes with the old prefix must not trigger persistence cleanup. Keep Programmatic Control and UI run-failure semantics aligned.

#### FND-07: Input and filesystem adapters own application execution policy

- Location: [extension catalogue](../../../../../../host/internal/infra/plugins/extension/catalog/service.go), lines 33 through 44 and 77 through 91; [UI catalogue](../../../../../../host/internal/infra/plugins/ui/catalog/service.go), lines 39 through 73; [tool controller bash.go](../../../../../../plugins/extension/tools/internal/controller/extension/bash.go), `bashExecutionContext`, lines 73 through 100; [bash usecase](../../../../../../plugins/extension/tools/internal/usecase/tools/bash/service.go), lines 22 through 53.
- Issue: The catalogue adapters decide default-versus-explicit failure handling and duplicate-ID acceptance. The bash input controller starts the timer and chooses the timeout cancellation cause before calling a usecase that has no timeout argument. FRQ-03 places these continuation and execution decisions with application owners.
- Impact: Filesystem implementation changes can change startup acceptance. Bash usecase callers must reproduce controller logic to obtain the supported execution timeout.
- Scenario: Duplicate normalized plugin IDs, an unreadable configured directory, or a bash command exceeding its requested timeout.
- Assessing realism: High. These are ordinary configuration and tool inputs.
- Recommendation: Move catalogue acceptance decisions to extension startup/runtime orchestration and UI selection. Keep filesystem inspection in adapters. Pass validated timeout intent through the controller-owned bash command and let the bash usecase own the timer and its cleanup.
- Verification: Preserve the distinct approved UI and extension duplicate/failure outcomes. Preserve bash timeout text, duration interpretation, parent cancellation, process-group termination, output spill, and progress delivery through existing behavioral tests.

The [U4 evidence](solution.md#u4-runtime-boundaries-discovery-and-startup) records the implemented runtime/startup portions of FND-01, FND-02, FND-03, and FND-07. Runtime management owns filtered process payloads and trusted identity binding. Catalog adapters return filesystem observations; Host consumers own their distinct acceptance rules. App forwarding and Host interactions are removed. UI output owns startup reporting, warnings, and authorization presentation. Required project checks and uncached affected tests passed. Main-agent U4 inspection passed as recorded in the linked evidence. U5 implemented the bundled-tool timeout correction. [U8 accounting](u8-evidence.md) records the combined implementation; independent whole-product verification and user acceptance remain pending.

#### FND-10: Codex streaming failures truncate source text

- Location: `host/internal/infra/providers/openai/codex/assembler.go`, `semanticAssembler.consume`; `provider.go`, `providerFailureMessage` and `failedResponseFromSDK`; `transport.go`, `errorCaptureTransport.RoundTrip` and the removed `boundedDetail`.
- Issue: At integrated-review commit `9a275b2b2bc5964e5ecd1f0cefc4f7444c971c2a`, `response.failed`, non-token-limit `response.incomplete` and `error` cut provider detail to 4000 runes. `safeError` also removed trailing periods. The original HTTP 401 correction did not repair these streaming paths. The HTTP capture transport also accepted failed responses but restored only their first 65,536 bytes for the SDK. That truncation could turn valid error JSON into incomplete JSON; it was not a size-admission rule.
- Impact: Core, persisted failed responses and client terminal errors cannot recover the discarded suffix.
- Scenario: A supported SSE failure contains a non-secret explanation beyond rune 4000, or an HTTP 401 or 500 response contains a valid diagnostic beyond 64 KiB.
- Assessing realism: Medium. Supported provider input reaches the branch without malformed input or a race. No occurrence rate is assumed.
- Recommendation: Preserve the entire source message at both adapter boundaries without increasing a presentation limit or adding provider categories.
- Verification: [U6 follow-up evidence](solution.md#u6-follow-up-complete-source-and-delivery-causes) records assertion RED and GREEN for all three SSE branches and complete HTTP 401 and 500 diagnostics beyond 64 KiB, plus Core and client contract checks. The correction is implemented; independent acceptance remains pending.

#### FND-11: Failed error output discards the source cause

- Location: `host/internal/controller/programmatic/delivery.go`, `streamDelivery.Terminal`; `service.go`, `Service.open`; `host/internal/usecase/host/startup/service.go`, `Service.Start`; `host/internal/infra/headless/renderer.go`, `Renderer.ReportIssue`; `host/internal/infra/plugins/ui/runtime/startup.go`, `Service.flushSelectionWarnings`.
- Issue: At the integrated-review commit, terminal enqueue or acknowledgement failure returned only delivery error. The writer-result branch could complete independently with that same loss. Startup reporting and fallback warning writes likewise discarded their source issue after output failed.
- Impact: RPC and application completion can explain failed output without explaining the operation or startup issue that required output.
- Scenario: A failed operation reaches a disconnected stream, or an excluded plugin diagnostic reaches a closed stderr writer.
- Assessing realism: High for disconnected operation output; medium for failed startup diagnostics. Both use implemented paths.
- Recommendation: Retain undelivered source causes at the existing output owners and join them into actual completion errors. Preserve delivery categories, cancellation, ordering and one-attempt warning output.
- Verification: [U6 follow-up evidence](solution.md#u6-follow-up-complete-source-and-delivery-causes) records assertion RED and GREEN through actual operation completion and startup reporting boundaries. The correction is implemented; independent acceptance remains pending.

#### FND-12: Failed work is discarded before terminal delivery

- Location: [operation owner](../../../../../../internal/operation/operation.go), `Owner.run`, `Owner.Fail` and `Owner.Err`; [Programmatic prepared work](../../../../../../host/internal/usecase/host/programmatic/prepared.go), `runPrepared.Run`; [Programmatic completion](../../../../../../host/internal/controller/programmatic/service.go), `Service.open`.
- Issue: At `b03095a839ba1a02d9c5c97217545436aeb3197e`, `Owner.run` receives the failed work outcome, calls `Release`, then drops `outcome.Err` when `deliveryContext` is canceled. It never calls `Terminal`. `Owner.Err` retains only the first delivery cause. NFQ-02 requires the source cause to reach local completion even when delivery cannot proceed.
- Dependency and runtime path: Core provider failure and failed progress delivery reach run control and `runPrepared.Run` together. The generic owner discards the work cause before the Programmatic `Terminal` source map can store it. Waiting for the owner cannot recover a value it discarded.
- Impact: RPC and `SessionCompletion` omit the distinct provider/work failure. A disconnected peer is not expected to receive a response; the defect is in retained local completion.
- Scenario and realism: High. A provider failure followed by a stopped or full progress writer reaches this path during an accepted run.
- Recommendation: Give the operation owner responsibility for failed work sources until final terminal-send confirmation. Keep release, cancellation and identifier lifetime unchanged. Collect retained sources after work and output cleanup.
- Verification: Source inspection establishes the branch. A new executable RED has not been run. The [approved U6 revision](solution.md#complete-error-retention-revision) requires actual local-completion regression evidence before correction.

#### FND-13: Error output loses sources outside selected terminal paths

- Location: [Programmatic output](../../../../../../host/internal/controller/programmatic/delivery.go), `reject`; [Host UI operation output](../../../../../../host/internal/infra/plugins/ui/runtime/operations.go), `Reject`, `AttachOutput` and `enqueueAcknowledged`; [UI connection publication](../../../../../../host/internal/infra/plugins/ui/runtime/publication.go), `ReportError`; [Extension SDK output](../../../../../../sdk/plugins/extension/v1/server.go), `extensionDelivery.Terminal`; [Extension SDK Host output](../../../../../../sdk/plugins/extension/v1/host_owner.go), `hostDelivery.Terminal` and `rejectHostRequest`; [UI SDK output and cleanup](../../../../../../sdk/plugins/ui/v1/service.go), `uiDelivery.Terminal` and `finishServerOpen`.
- Issue: At the same commit, these error outputs encode a source but do not retain it through every enqueue, write and cleanup branch. FRQ-03 and NFQ-02 apply to the actual output and completion owners, not just error-returning helpers.
- Dependency and runtime paths: Rejections have no accepted operation to retain their cause. UI Failed sources are retrieved only by the actual send callback, which `Writer.resolveQueued` skips after an earlier write fails. UI asynchronous authentication reporting has no result channel. SDK terminal sources remain only in message payloads; their final collectors gather transport/cleanup errors instead. Extension `Connection.recordError` keeps only its first error.
- Impact: Local completion can omit the admission, authentication, initialization or extension-operation failure that required output. Successful sending is not defective. A source still present only in a skipped message or inaccessible map does not satisfy the completion contract.
- Scenarios and realism: High for occupied admission or error output queued behind another failed write; medium for initialization or extension failure combined with output failure. All use implemented operations and streams.
- Recommendation: Separate failed-work retention from error-message retention and collect both at the connection owner. Cover immediate enqueue failure and messages never passed to `Send`. Stop and join error-producing work before final collection. Preserve successful rejection and ordinary-handler continuation.
- Verification: The [approved U6 revision](solution.md#complete-error-retention-revision) defines the complete output inventory, including declared `HandlerError` completed payloads and startup/error diagnostics. Source traces establish the registered gaps; executable RED/GREEN for the revision remains pending. Existing passing Programmatic Failed-terminal checks cover neither pre-Terminal cancellation nor the other output directions.

#### FND-14: Compatible Responses discards a provider error event

- Location: [compatible Responses decoding](../../../../../../host/internal/infra/providers/openai/compatible/responses.go), `responsesAccumulator.consume` and `Driver.streamResponses`; [compatible driver](../../../../../../host/internal/infra/providers/openai/compatible/service.go), `Driver.Stream`.
- Issue: `consume` has no branch for top-level `type="error"`. After clean EOF without another terminal response, `streamResponses` returns only "responses stream ended without a terminal response". NFQ-02 prohibits replacing the supplied error explanation.
- Dependency and runtime path: The selected OpenAI SDK's `ResponseErrorEvent` supplies the top-level message to Glyph's accumulator. The SDK does not treat that event as a stream error merely because its `type` is `error`. Glyph discards it before Core, failed-response history or either client can receive it. The Codex regression exercises this event shape through the same SDK.
- Impact: Failed model responses and operation errors contain the replacement rather than the original provider message.
- Scenario and realism: Medium. A Responses endpoint sends HTTP 200 with a typed error SSE event followed by EOF. No malformed envelope or new provider capability is required.
- Recommendation: Normalize the event through the adapter's failed-response path and preserve its complete non-secret message without new retries or categories.
- Verification: Source inspection and the selected SDK event definition establish the path. A new compatible-adapter regression has not been executed. The [U6 revision](solution.md#complete-error-retention-revision) requires empty/partial-stream RED/GREEN and Core/client preservation checks.

#### FND-15: Runtime cleanup discards SDK completion errors

- Location: [runtime adapter](../../../../../../host/internal/infra/plugins/extension/runtime/runtime.go), `Runtime.Close` and `isBenignCancellationError`; [runtime contract](../../../../../../host/internal/usecase/host/extensionruntime/interfaces.go), `ExtensionRuntime.Close`; [Host runtime owner](../../../../../../host/internal/usecase/host/extensionruntime/service.go), `runtimeState.closeTransport` and `Service.Close`.
- Issue: At `ded1549`, SDK `Connection.Close` collects undelivered sources and independent cleanup failures, but the production adapter logs the result and returns void. The Host owner returns reporting errors without the discarded SDK completion result. A broad cancellation match can also suppress a mixed cause tree.
- Impact: Application completion can omit an undelivered rejection or work diagnostic and an independent cleanup failure. Logging does not preserve the error across this runtime contract.
- Scenario and realism: Medium. A stale-context request is rejected, the extension disconnects before rejection delivery, and final cleanup drops the SDK-retained source. Both stale contexts and disconnection are supported conditions. An independent `CloseSend` failure reaches the same missing boundary.
- Recommendation: Apply [D1](solution.md#runtime-completion-propagation-correction). Preserve undelivered sources and independent cleanup errors without treating every connection-stop cause or successfully reported runtime failure as fatal.
- Verification: [D1 implementation evidence](solution.md#u6-runtime-completion-evidence) records actual runtime/application assertion RED and GREEN, source and cleanup identity checks, early collection and nonfatal delivered reporting. Main-agent source inspection and repeated required, uncached and actual-app checks passed. Independent integrated review and final acceptance remain pending.

#### FND-16: TUI dependent output can precede the submitted user line

- Location: [presentation command state](../../../../../../plugins/ui/tui/internal/usecase/presentation/commands.go), `Service.Notify` and `Service.Complete`; [dispatch result transition](../../../../../../plugins/ui/tui/internal/usecase/presentation/update.go), `interaction.applyEmissionResult`.
- Issue: At `ded1549`, Host notifications update the transcript before a delayed local dispatch result adds the submitted user line. A terminal notification does not prevent that later insertion. Event-loop serialization does not order the two independent producers.
- Impact: A failure or model response can appear above the user request that caused it. Stored session history is not changed by this presentation defect.
- Scenario and realism: Medium. A fast local persistence failure reaches the notification pump before the command goroutine supplies its dispatch result. The existing terminal-before-acknowledgement test already permits this schedule but uses an empty payload.
- Recommendation: Apply [D2](solution.md#tui-submission-causality-correction). Record the user line before dependent output, then consume the late dispatch result without duplicate insertion or draft loss. Preserve asynchronous I/O and one state-mutation loop.
- Verification: The [U5 causality evidence](solution.md#u5-causality-verification) records six content-bearing assertion RED/GREEN cases and main-agent checks. Submission order, exactly-once insertion and late-result draft preservation are implemented. Fresh whole-product review remains pending.

#### FND-18: Navigation drops accumulated diagnostics on later failure

- Location: [handler composition](../../../../../../host/internal/usecase/host/sessiontree/handlers.go), `runRequestHandlers` and `runResultHandlers`; [navigation](../../../../../../host/internal/usecase/host/sessiontree/service.go), `navigate`; both methods in `client_navigation.go`.
- Issue: At `cb3a8f6`, later summary, validation, persistence or cancellation failures discard earlier ordinary-handler and invalid-action issues. Cancellation branches also discard the accumulated slice.
- Impact and realism: High. Supported navigation failures reach both clients without preceding extension diagnostics, before an operation owner can retain them.
- Resolution: [Producer preservation](solution.md#u6-producer-preservation) retains original handler causes with their issues and joins them on failure. Successful completion, explicit handler cancellation and committed publication behavior are unchanged.
- Verification: Twelve assertion RED/GREEN cases cover both clients and summary, final validation, pre-commit, request cancellation, result cancellation and final cancellation failures. Local checks do not imply integrated acceptance.

#### FND-19: Runtime cancellation replaces received errors

- Location: [extension runtime](../../../../../../host/internal/infra/plugins/extension/runtime/runtime.go), `Execute`, `executionFailure` and `executionError`; [handler mapping](../../../../../../host/internal/infra/plugins/extension/runtime/handler.go), `handlerOperationError`, also consumed by lifecycle operations.
- Issue: At `cb3a8f6`, caller cancellation replaces received SDK and progress errors. A diagnostic-bearing gRPC Canceled status is reduced to `context.Canceled`.
- Impact and realism: Medium. Cancellation and received terminal/progress results are independent producers. D1 cannot recover a received peer source that this mapper drops.
- Resolution: Retain received and independent progress causes alongside cancellation. Preserve the original transport status and text without changing cancellation admission or process lifetime.
- Verification: Assertion RED/GREEN covers received-before-cancel ordering, real runtime callback cancellation and full gRPC cancellation diagnostics. Pure cancellation and ordinary handler continuation remain in the test set.

#### FND-20: SDK cancellation classification discards mixed sources

- Location: [Extension SDK](../../../../../../sdk/plugins/extension/v1/service.go), `operationOutcome`; its six prepared-work callers in `service.go` and `host_owner.go`.
- Issue: At `cb3a8f6`, an ordinary joined error containing cancellation becomes source-free Canceled even when it contains an independent failure.
- Impact and realism: Medium. Public operation implementations can return ordinary mixed errors, including bundled tool failures. Owner retention never receives their sources.
- Resolution: Only pure cancellation trees produce source-free Canceled. Ordinary mixed failures use the established INTERNAL category with the original source. Explicit failure categories and Completed handler diagnostics keep their established paths.
- Verification: Assertion RED/GREEN covers the classifier and actual SDK prepared work plus Owner collection. Pure cancellation remains source-free.

#### FND-21: Bash cancellation can replace a callback failure

- Location: [bash process output](../../../../../../plugins/extension/tools/internal/infra/process/bash/service.go), `streamWriter.Write`, `flush` and `outputSink.result`.
- Issue: At `cb3a8f6`, a callback error is passed only to `cancel(err)`. If parent cancellation wins first, that call cannot replace the context cause, and process completion omits the callback error. This corrects the earlier review's blanket claim that bash always preserves mixed causes before SDK classification.
- Impact and realism: Medium. A real stdout callback can cancel its caller before returning an independent output-consumer failure.
- Approval and resolution: O74-1 authorizes retaining callback failures in the existing output owner. `outputSink` records them under its lock before cancellation and joins them after process I/O and flush complete. `outputStore` retains its output-file error responsibility. No process termination policy changes.
- Verification: Main repeated the real-process assertion RED. The corrected real bash usecase/process test passes with both cancellation and original callback identity. See [producer evidence](solution.md#u6-producer-preservation).

#### FND-22: Bash group cancellation can leave a child running

- Location: [bash process lifetime](../../../../../../plugins/extension/tools/internal/infra/process/bash/service.go), `Service.Run`, `watchCancellation` and `killProcessGroup`.
- Issue: One successful group SIGKILL does not establish that every child stopped. A replay of `printf started; sleep 30` left an orphaned `sleep` in the owned group with stdout and stderr pipes open. `Cmd.Wait` waited for output-copy completion until that child exited.
- Impact and realism: High. Cancellation of an ordinary shell command can leave work running and hold operation completion. The bounded probe returned after 30.01 seconds and failed a prompt-completion assertion. It did not reach the test deadline. The exact fork/signal ordering is not established.
- Disposition: Unresolved. O75-1 authorizes a separate correction after the verified FND-18 through FND-21 commit. Its process-lifetime design requires approval before implementation. The retained callback error reaches the caller after the delayed completion, so this is distinct from FND-21.
- Verification: Process and descriptor observations showed the surviving child after the successful signal. A separate probe returned after an additional test-only group signal. Diagnostic instrumentation was removed and production restored. No termination correction or whole-product acceptance is claimed.

### Minor

#### FND-17: Architecture persistence-cleanup status is stale

- Location: [architecture CMP-16](../../architecture.md#components), final sentence.
- Issue: At `ded1549`, the component description keeps persistence-cause cleanup open although BLK-05 and the category-based reducer are implemented.
- Impact and realism: High for documentation use. The authority gives an engineer an incorrect implementation status.
- Recommendation: Record BLK-01 through BLK-05 closure while keeping unresolved phase findings and final acceptance separate.
- Verification: CMP-16 now records BLK-01 through BLK-05 as implemented and keeps unresolved phase findings separate. It matches the [ownership record](tui-ownership-gap.md) and presentation `projection.applyError`. This documentation correction needs no behavioral test.

#### FND-08: Consumer contracts and implementation assertions are incomplete

- Location: [UI interfaces](../../../../../../host/internal/usecase/host/ui/interfaces.go), `AgentRunner.Run` and `Channel.Close`; [UI preparation](../../../../../../host/internal/usecase/host/ui/prepared_operations.go), selection-error classification; [sessiontree/summarizer.go](../../../../../../host/internal/usecase/host/sessiontree/summarizer.go), `selectionFailure`; [providers/catalog.go](../../../../../../host/internal/usecase/host/providers/catalog.go), `SelectionError`; [UI SDK](../../../../../../sdk/plugins/ui/v1/sdk.go), `grpcUIPlugin`; [bash adapter](../../../../../../plugins/extension/tools/internal/infra/process/bash/service.go), `streamWriter`; [grep adapter](../../../../../../plugins/extension/tools/internal/infra/filesystem/project/grep.go), `boundedLine`.
- Issue: Host UI does not call its `AgentRunner.Run` or `Channel.Close` methods. UI and session-tree consumers structurally classify provider errors without their own accessible assertion contracts. Handwritten `grpcUIPlugin`, `streamWriter`, and `boundedLine` lack assertions for the external interfaces they implement. FRQ-07 requires implementation-package assertions; consumer ports must describe the consumer's actual need.
- Impact: Port implementations and mocks carry unused requirements. Structural error classification and plugin capability discovery lack the required declared conformance.
- Scenario: A consumed error method or handwritten method set changes independently of its real consumer.
- Assessing realism: High as a source/maintenance condition. Existing `io.Writer` and `io.RuneReader` assignment sites still check compatibility; no present runtime failure is claimed.
- Recommendation: Trim unused methods at the consumer, retain concrete cleanup at its owner, expose consumed error interfaces, and assert them at implementations after checking transitive dependencies. Assert `plugin.Plugin`, `plugin.GRPCPlugin`, `io.Writer`, and `io.RuneReader` on the listed handwritten types. Correct UI preparation's anonymous error consumer together with FND-02, not through a reverse import into infrastructure.
- Verification: Inspect production method usage and all cross-package implementations, including structural error classification. The listed external assertions require no new package-level edge. The baseline `ifaceguard` pass below does not establish complete assertion coverage.

#### FND-09: A presentation usecase forwards behavior without owning it

- Audit-baseline location: `plugins/ui/tui/internal/usecase/presentation/service.go`, lines 6 through 17; `plugins/ui/tui/internal/app/app.go`, lines 13 through 16; `plugins/ui/tui/internal/controller/tui/model.go`, `Model.apply` and `NewModel`; `plugins/ui/tui/internal/domain/presentation/state.go`, `State.Apply`.
- Issue: An empty service forwards every call to `state.Apply(event)`. It owns no state, policy, transformation, or outgoing port. NFQ-04 does not justify the extra owner.
- Impact: Assembly and tests carry an abstraction that isolates no behavior.
- Scenario: Initialization and every projected Host event pass through the forwarding function.
- Assessing realism: High. The service is wired into the normal TUI path.
- Recommendation: Replace the empty forwarding body with the real state and interaction policy identified in the [TUI ownership gap](tui-ownership-gap.md). Do not keep direct controller mutations around a domain reducer or add an interface around the empty service. The [revised solution](solution.md#tui) assigns input contracts, application transitions, SDK work, and rendering to their consumers and owners.
- Verification: Preserve projection, interaction, and terminal behavior tests at those owners. Close gap BLK-01 through BLK-04 in U5 and the separately approved cause-based cleanup in U7. No test of an absent forwarding call is needed.

### TUI follow-up status

The [revised U5 evidence](solution.md#u5-bundled-tool-and-tui-ownership) records implemented application, input, SDK, terminal, and tree responsibilities. Source traces and migrated behavior tests close [BLK-01 through BLK-04](tui-ownership-gap.md#blockers). The record includes executable RED/GREEN for the local `/name` snapshot regression. The retained bash ownership and external-assertion changes pass the complete unit's checks. Main-agent source inspection and repeated project checks passed. [U6](solution.md#u6-complete-error-text) closes FND-05. [U7](solution.md#u7-run-persistence-cause-and-tui-cleanup) closes BLK-05 and FND-06. [U8 implementation evidence](u8-evidence.md) is ready for main-agent inspection. Independent integrated verification and user acceptance remain pending.

## Package and contract coverage

Each production package received responsibility, state, interface/type, import, and runtime-boundary inspection. The table records baseline inspection and findings. The later [TUI ownership gap](tui-ownership-gap.md) corrects the incomplete disposition of its presentation and controller responsibilities without changing the baseline package count. A dash means no separate finding was recorded at that baseline.

### Host packages

Paths below are relative to `host`. These 47 packages contain 187 handwritten production Go files.

| Package | Actual responsibility or owned state | Findings |
| --- | --- | --- |
| [cmd/glyph](../../../../../../host/cmd/glyph) | Process entry, CLI invocation, exit-code mapping. | FND-02 output dependency |
| [internal/app](../../../../../../host/internal/app) | Concrete assembly, startup/shutdown, plus runtime bindings and delivery transformations. | FND-01, FND-02 |
| [internal/config/codingagent](../../../../../../host/internal/config/codingagent) | Embedded coding instructions. | — |
| [internal/controller/cli](../../../../../../host/internal/controller/cli) | Command syntax and mode arguments. | — |
| [internal/controller/cli/headless](../../../../../../host/internal/controller/cli/headless) | One-shot command execution and stream output state. | FND-02 |
| [internal/controller/extension](../../../../../../host/internal/controller/extension) | Public request mapping, bound identity, context-operation input contracts. | — |
| [internal/controller/programmatic](../../../../../../host/internal/controller/programmatic) | Stream input, command/response contracts, cancellation tracking, and connection output. | FND-02 |
| [internal/controller/ui](../../../../../../host/internal/controller/ui) | Starts a UI session; does not receive its commands. | FND-02, FND-03 |
| [internal/domain/agent](../../../../../../host/internal/domain/agent) | Provider-neutral history alternatives and run outcomes. | — |
| [internal/domain/extension](../../../../../../host/internal/domain/extension) | Runtime/session binding identity and runtime-failure values. | — |
| [internal/domain/model](../../../../../../host/internal/domain/model) | Descriptors, selection, content, usage, and opaque provider-context values. | — |
| [internal/domain/pluginid](../../../../../../host/internal/domain/pluginid) | Plugin identity normalization. | — |
| [internal/domain/session](../../../../../../host/internal/domain/session) | Entries, tree invariants, summary provenance, accounting, plus query/schema aggregates. | FND-03, FND-04 |
| [internal/domain/tool](../../../../../../host/internal/domain/tool) | Provider-neutral tool descriptors, results, constraints, and progress. | — |
| [internal/domain/ui](../../../../../../host/internal/domain/ui) | UI command, initialization, output, discovery, and tree-response values. | FND-03 |
| [internal/infra/browser](../../../../../../host/internal/infra/browser) | Browser-launcher process. | FND-02 boundary participant |
| [internal/infra/logging](../../../../../../host/internal/infra/logging) | Structured log files and permissions. | — |
| [internal/infra/persistence](../../../../../../host/internal/infra/persistence) | User-data paths and directory setup. | — |
| [internal/infra/persistence/credentials](../../../../../../host/internal/infra/persistence/credentials) | Credential storage and API-key source resolution. | — |
| [internal/infra/persistence/sessionfilesystem](../../../../../../host/internal/infra/persistence/sessionfilesystem) | Confined filesystem and descriptor lifetime. | — |
| [internal/infra/persistence/sessions](../../../../../../host/internal/infra/persistence/sessions) | JSONL encoding, writes, replay, recovery, private reconstruction state. | FND-04 |
| [internal/infra/persistence/settings](../../../../../../host/internal/infra/persistence/settings) | Strict settings parsing and configured-model validation. | — |
| [internal/infra/plugins/extension/catalog](../../../../../../host/internal/infra/plugins/extension/catalog) | Filesystem discovery plus application acceptance decisions. | FND-07 |
| [internal/infra/plugins/extension/runtime](../../../../../../host/internal/infra/plugins/extension/runtime) | Process/SDK operation transport and capability payload mapping. | FND-03 |
| [internal/infra/plugins/ui/catalog](../../../../../../host/internal/infra/plugins/ui/catalog) | UI filesystem discovery plus catalogue rejection policy. | FND-03, FND-07 |
| [internal/infra/plugins/ui/runtime](../../../../../../host/internal/infra/plugins/ui/runtime) | UI process, input command mapping, operation transport, ordered writer, close. | FND-02, FND-03, FND-08 |
| [internal/infra/programmatic/socket](../../../../../../host/internal/infra/programmatic/socket) | Unix listener and created-path lifetime. | — |
| [internal/infra/providers](../../../../../../host/internal/infra/providers) | Provider request-identification constants. | — |
| [internal/infra/providers/openai/codex](../../../../../../host/internal/infra/providers/openai/codex) | OAuth, refresh, wire mapping, streaming, replay, source error classification. | FND-02, FND-05 |
| [internal/infra/providers/openai/compatible](../../../../../../host/internal/infra/providers/openai/compatible) | API-key access, Chat/Responses mapping, reasoning format and streaming. | — |
| [internal/infra/sessionruntime](../../../../../../host/internal/infra/sessionruntime) | Cryptographic IDs and UTC time. | — |
| [internal/usecase/agent/run](../../../../../../host/internal/usecase/agent/run) | Run state, model/tool loop, cancellation, lifecycle events, settlement. | FND-02 input contract |
| [internal/usecase/host/events](../../../../../../host/internal/usecase/host/events) | Run reservations and settlement; client delivery before observers. | FND-02 |
| [internal/usecase/host/extensioncontext](../../../../../../host/internal/usecase/host/extensioncontext) | Issued context bindings, stale checks, configured requests, append/recovery orchestration. | FND-02 |
| [internal/usecase/host/extensionruntime](../../../../../../host/internal/usecase/host/extensionruntime) | Runtime instances, availability, operation counts, invalidation, monitoring, invocation. | FND-02, FND-03, FND-07 |
| [internal/usecase/host/interactions](../../../../../../host/internal/usecase/host/interactions) | Authorization presentation/browser routing. | FND-02 |
| [internal/usecase/host/lifecycle](../../../../../../host/internal/usecase/host/lifecycle) | Observer registrations, invocation order, ordinary observer-error policy. | FND-01, FND-02 |
| [internal/usecase/host/operationgate](../../../../../../host/internal/usecase/host/operationgate) | One occupied reservation and idempotent release. | FND-02 |
| [internal/usecase/host/programmatic](../../../../../../host/internal/usecase/host/programmatic) | Admission, prepared application work, active-run delivery correlation, projections. | FND-02, FND-03 |
| [internal/usecase/host/providers](../../../../../../host/internal/usecase/host/providers) | Configured catalogue, atomic selection, preflight, pricing, configured requests. | FND-01, FND-08 |
| [internal/usecase/host/sessioncontrol](../../../../../../host/internal/usecase/host/sessioncontrol) | Client session operations and mutation reservation. | FND-02, FND-03 |
| [internal/usecase/host/sessionnavigation](../../../../../../host/internal/usecase/host/sessionnavigation) | Interface-free navigation command/response definitions. | FND-03 |
| [internal/usecase/host/sessions](../../../../../../host/internal/usecase/host/sessions) | Active tree/incarnation, history, durable mutation, accounting, ordered publication. | FND-01 through FND-05 |
| [internal/usecase/host/sessiontree](../../../../../../host/internal/usecase/host/sessiontree) | Navigation handler policy, summarization, final validation, observers; history helper. | FND-01, FND-02, FND-03, FND-05, FND-08 |
| [internal/usecase/host/startup](../../../../../../host/internal/usecase/host/startup) | Registration validation, capability partitioning, acceptance and startup reports. | FND-02, FND-03 |
| [internal/usecase/host/tools](../../../../../../host/internal/usecase/host/tools) | Tool registry, schemas, conflicts, runtime ownership, argument/result validation. | — |
| [internal/usecase/host/ui](../../../../../../host/internal/usecase/host/ui) | UI selection/readiness, authentication admission, prepared operations and projections. | FND-01, FND-02, FND-03, FND-07, FND-08 |

### Plugins and shared operation code

These 21 handwritten packages contain 86 production Go files. The two SDK rows are public process-support code, not Host application policy.

| Package | Actual responsibility or owned state | Findings |
| --- | --- | --- |
| [plugins/extension/tools/cmd/glyph-tools](../../../../../../plugins/extension/tools/cmd/glyph-tools) | Extension process entry. | — |
| [plugins/extension/tools/internal/app](../../../../../../plugins/extension/tools/internal/app) | Tool service, adapter, controller, and SDK construction. | — |
| [plugins/extension/tools/internal/controller/extension](../../../../../../plugins/extension/tools/internal/controller/extension) | JSON input, registration, dispatch, result mapping, plus bash timer. | FND-07 |
| [plugins/extension/tools/internal/core/textbudget](../../../../../../plugins/extension/tools/internal/core/textbudget) | Model-visible byte/line budget values. | — |
| [plugins/extension/tools/internal/infra/filesystem/project](../../../../../../plugins/extension/tools/internal/infra/filesystem/project) | Filesystem access, path locks, bounded reads/search, atomic mutation. | FND-08 |
| [plugins/extension/tools/internal/infra/process/bash](../../../../../../plugins/extension/tools/internal/infra/process/bash) | Process groups, cancellation, output streaming and spill files. | FND-07 boundary participant, FND-08 |
| [plugins/extension/tools/internal/usecase/tools/bash](../../../../../../plugins/extension/tools/internal/usecase/tools/bash) | Tool execution orchestration and process result mapping. | FND-07 |
| [plugins/extension/tools/internal/usecase/tools/edit](../../../../../../plugins/extension/tools/internal/usecase/tools/edit) | Exact-match uniqueness, overlap checks, and replacement decisions. | — |
| [plugins/extension/tools/internal/usecase/tools/read](../../../../../../plugins/extension/tools/internal/usecase/tools/read) | Text/image result projection and continuation guidance. | — |
| [plugins/extension/tools/internal/usecase/tools/search](../../../../../../plugins/extension/tools/internal/usecase/tools/search) | Typed search commands and returned output. | — |
| [plugins/extension/tools/internal/usecase/tools/write](../../../../../../plugins/extension/tools/internal/usecase/tools/write) | Complete-file write operation and contextual errors. | — |
| [plugins/ui/tui/cmd/glyph-tui](../../../../../../plugins/ui/tui/cmd/glyph-tui) | UI process entry. | — |
| [plugins/ui/tui/internal/app](../../../../../../plugins/ui/tui/internal/app) | Terminal, rendering program, endpoint, and SDK assembly. | FND-09 |
| [plugins/ui/tui/internal/controller/plugin](../../../../../../plugins/ui/tui/internal/controller/plugin) | SDK endpoint, initialization, terminal/program lifetime, notification mapping, correlation. | FND-03, FND-06 |
| [plugins/ui/tui/internal/controller/tui](../../../../../../plugins/ui/tui/internal/controller/tui) | Bubble Tea input/rendering, editor/selectors/tree state, asynchronous commands. | FND-03, FND-09 |
| `plugins/ui/tui/internal/domain/presentation`, removed by U5 | Projection state, transcript/tree behavior, plus Host command payloads. | FND-03, FND-06 |
| [plugins/ui/tui/internal/infra/terminal](../../../../../../plugins/ui/tui/internal/infra/terminal) | Controlling-terminal files and cleanup. | — |
| [plugins/ui/tui/internal/usecase/presentation](../../../../../../plugins/ui/tui/internal/usecase/presentation) | Empty forwarding service. | FND-09 |
| [sdk/plugins/extension/v1](../../../../../../sdk/plugins/extension/v1) | Bootstrap, bidirectional operation namespaces, protocol validation, tracking and delivery. | FND-05 |
| [sdk/plugins/ui/v1](../../../../../../sdk/plugins/ui/v1) | UI bootstrap, initialization lifecycle, correlation, notifications, close. | FND-08 |
| [internal/operation](../../../../../../internal/operation) | Generic admission, cancellation, lifecycle tracking and ordered bounded writes. | — |

### Public protobuf sources and generated packages

The four generated packages complete the 72-package inventory. Their handwritten consumers and source contracts were inspected; generated bodies were excluded from handwritten architecture review.

| Generated package | Protobuf sources under the corresponding `api` directory | Boundary |
| --- | --- | --- |
| [pkg/operation/v1](../../../../../../pkg/operation/v1) | [operation.proto](../../../../../../api/operation/v1/operation.proto) | Common accepted/running/terminal lifecycle, errors, cancellation and close. |
| [pkg/plugins/extension/v1](../../../../../../pkg/plugins/extension/v1) | [extension.proto](../../../../../../api/plugins/extension/v1/extension.proto), [context.proto](../../../../../../api/plugins/extension/v1/context.proto), [lifecycle.proto](../../../../../../api/plugins/extension/v1/lifecycle.proto), [model.proto](../../../../../../api/plugins/extension/v1/model.proto), [session.proto](../../../../../../api/plugins/extension/v1/session.proto), [session_tree.proto](../../../../../../api/plugins/extension/v1/session_tree.proto), [tool.proto](../../../../../../api/plugins/extension/v1/tool.proto) | Registration, tools, contexts, configured models, lifecycle observers, extension state and navigation handlers. |
| [pkg/plugins/ui/v1](../../../../../../pkg/plugins/ui/v1) | [ui.proto](../../../../../../api/plugins/ui/v1/ui.proto), [agent.proto](../../../../../../api/plugins/ui/v1/agent.proto), [model.proto](../../../../../../api/plugins/ui/v1/model.proto), [session.proto](../../../../../../api/plugins/ui/v1/session.proto) | UI initialization, commands, events, authorization presentation, model and session projections. |
| [pkg/programmatic/v1](../../../../../../pkg/programmatic/v1) | [programmatic.proto](../../../../../../api/programmatic/v1/programmatic.proto), [agent.proto](../../../../../../api/programmatic/v1/agent.proto), [model.proto](../../../../../../api/programmatic/v1/model.proto), [session.proto](../../../../../../api/programmatic/v1/session.proto) | Programmatic commands, correlated outcomes, connection events, model and session projections. |

All 16 sources use edition 2023. Counts, positions, sizes, and token quantities use `int64`; prices use `double`; closed discriminators use enums. No `int32` data field or compatibility reservation was found. Public response contracts omit credentials and opaque provider reasoning payloads. Extension recovery alone carries the requesting extension's stored payload bytes; client extension-entry projections carry identity/type instead.

### Scope dispositions

- The baseline TUI disposition was incomplete. The [TUI ownership gap](tui-ownership-gap.md) supersedes it. Initialization admission, pending-command correlation, foreground Stop, and display transitions belong to the presentation application owner. SDK and terminal implementations own I/O and framework lifetime. Host retains authoritative run/session state.
- [internal/operation](../../../../../../internal/operation) consumes its own `Prepared` and `Delivery` interfaces and owns their generic lifecycle behavior. It is not an interface-free contract package. Its release, progress, and acknowledgement mechanisms are not replaced solely because they use functions or generics.
- The edit usecase owns replacement decisions; the filesystem adapter executes that transformation while holding its mutation lock. This callback is an atomic mutation operation, not a hidden substitute for its existing consumer-owned filesystem interface.
- [experiments/codex-oauth-spike](../../../../../../experiments/codex-oauth-spike) and [experiments/plugin-runtime-spike](../../../../../../experiments/plugin-runtime-spike) are separate non-product modules. Their source, spike protobuf, and generated spike protocol are not production modules.
- [testdata/external-plugins](../../../../../../testdata/external-plugins) is a fixture module depending on the product SDKs, not the reverse. Other `testdata` content is test input.
- [internal/testsupport/operationmock](../../../../../../internal/testsupport/operationmock), [pluginmock](../../../../../../internal/testsupport/pluginmock), and [tui](../../../../../../internal/testsupport/tui) are the three test-support packages returned by root `go list`. No production import or source-referenced runtime dependency on these roots, experiments, or testdata was found. Arbitrary user-selected plugins and bash commands are outside that dependency statement.
- `sdk/plugins/ui/v1.TestClient` imports `testing` from a production SDK source file and is used by integration tests. Its placement is recorded, but this audit does not infer an application-state ownership defect or add unrelated test-support cleanup.
- Missing logical model execution, retry, and compaction capabilities belong to PHS-06. Moving in-process provider drivers to extension processes belongs to PHS-12. Missing selection handlers and selection lifecycle events are remaining PHS-07 work. Their absence does not constitute an implemented-boundary finding.
- The Core event backpressure and client-before-observer ordering are approved behavior. Synchronous waits within an admitted asynchronous operation do not by themselves imply blocked input handling.

## Assumptions

The baseline traced catalogue failure policy, bash timer ownership, input mapping, output delivery, and state mutation. The [TUI follow-up](tui-ownership-gap.md) found that its presentation disposition still lacked a justified owner for interaction policy and boundary payloads. Proposed cycles distinguish absent assertion imports from the acyclic inspected source.

## Open questions

QST-01, QST-02 and QST-03 are closed by the [approved behavior decisions](solution.md#approved-behavior-decisions). The [TUI gap](tui-ownership-gap.md) records the source-backed closure of BLK-01 through BLK-05. [U8 accounting](u8-evidence.md) records all resulting package and finding dispositions. Independent integrated verification and explicit user acceptance remain pending.

## Next steps

- Inspect [U8](u8-evidence.md) and prepare its separate local commit.
- Complete independent integrated verification and obtain explicit user acceptance before PHS-07 resumes.

## Verification evidence

| Command | Result | Limitation |
| --- | --- | --- |
| `go list -json ./...` | 75 root-module packages, of which 72 are production and 3 are test support | Nested experiments and fixtures are accounted for separately in the coverage section. |
| `go tool ifaceguard --config ifaceguard.cfg` | Exit 0, `ifaceguard: no errors found` | Does not expose undeclared service relationships or all structural error-interface uses. |
| `task test` | Exit 0 | Test-bearing package results were cached. Includes the external-plugin fixture unit task. |
| `task itest` | Exit 0 | Test-bearing package results were cached. Includes the external-plugin fixture integration task. |
| `go test ./host/internal/usecase/host/events ./host/internal/usecase/agent/run ./internal/operation -count=1` | Exit 0, uncached during independent review | Baseline tests do not reproduce the exact observer-cancellation path in FND-02. |
| Targeted SDK error-ingress tests, command below | Exit 0, uncached | These tests assert the lossy baseline behavior in FND-05. |

```sh
go test ./sdk/plugins/extension/v1 -run 'TestMapExtensionEventBoundsExternalErrorText|TestExternalErrorIngressBoundsEveryOutcome|TestPeerStreamErrorsBoundExternalStatusText' -count=1
```

Formatting, fixes, generation, full lint, and coverage were not run for this read-only source audit. The correction plan requires the project verification sequence after implementation. No test result here establishes architectural acceptance.

## References

- [Ticket](ticket.md) defines audit scope, requirements, and acceptance criteria.
- [Architecture](../../architecture.md) defines logical owners and the approved contract-import rule.
- [Project rules](../../../../../../AGENTS.md) define complete error preservation and implementation verification.
- [Coverage](#package-and-contract-coverage) records package, public-contract, and non-product accounting.
- [PHS-04 solution](../04-persistent-linear-sessions/solution.md) defines persistence ordering and startup failure boundaries.
- [Delivery plan](../../delivery-plan.md) identifies future capability work excluded from this audit's corrections.
