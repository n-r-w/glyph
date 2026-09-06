# Technical solution: Architecture correction plan

## Problem statement

The [ticket](ticket.md) defines PHS-07.1. The [audit](audit.md) records nine groups of violations across the implemented product and distinguishes them from future capabilities.

## Proposed solution

### Status and scope

The user approved this correction plan and its implementation. Production evidence is commit `86be985c47cb3719cd98c7e611af6173c5692cfc`. Source extraction at `811af3ed8abdbd99e3c905588309fee8f0a809f2` found no production, test, protobuf, or build-configuration changes since that baseline. The governing architecture includes the contract-import clarification in `9461f0436fbd3700455e782441ad175f670bb9f6`. Commit `9b1f725` aligns the product PRD with complete external error-text preservation.

Core implements Host consumer contracts with implementation-package assertions. It remains logically independent of concrete Host implementations, Host state, and Host policy. No assertion exception or forwarding Core adapter is required.

All structural corrections preserve the public behavior defined by the implemented phases. Error-text corrections, the source-backed run-persistence distinction, and cancellation settlement recovery are [approved behavior decisions](#approved-behavior-decisions). Cancellation settlement recovery is implemented under U2. Error-text and persistence-category corrections remain open. Implementation remains subject to the verification and acceptance gates below. PHS-07 stays paused until the ticket's acceptance criteria pass. PHS-06 retry/compaction/model execution and PHS-12 provider migration are not brought into this plan.

### Responsibility changes

Existing source links identify the work to move. New paths below contain behavior, not shared contract definitions alone.

| Responsibility | Target owner | Source and scope |
| --- | --- | --- |
| Run IDs, prepared reservations, Core invocation and settlement ordering | New `host/internal/usecase/host/runcontrol` | Move [events.Coordinator](../../../../../../host/internal/usecase/host/events/coordinator.go), not the dispatcher. |
| Client-first event delivery and subsequent observers | [host/events](../../../../../../host/internal/usecase/host/events) | Keep dispatch; replace stored service callbacks with its own delivery interface. |
| One-shot input | [controller/cli/headless](../../../../../../host/internal/controller/cli/headless) | Keep parsing, `AgentRunner`, and terminal-outcome checking. |
| Headless presentation | New `host/internal/infra/headless` | Move [Renderer](../../../../../../host/internal/controller/cli/headless/renderer.go), its line state, errors, startup output, and diagnostics. |
| UI command input | [controller/ui](../../../../../../host/internal/controller/ui) | Move command validation, mapping, dispatch, preparation-error classification, and command-facing operation ownership from [UI runtime](../../../../../../host/internal/infra/plugins/ui/runtime). |
| UI readiness, admission, selection and activation policy | [host/ui](../../../../../../host/internal/usecase/host/ui) | Keep application policy; remove command receipt and output construction. |
| UI process/stream lifetime and output | [infra/plugins/ui/runtime](../../../../../../host/internal/infra/plugins/ui/runtime) | Own one selected process, its stream, ordered output, initialization/warnings, agent projection and authorization presentation. |
| Programmatic input and operation admission transport | [controller/programmatic](../../../../../../host/internal/controller/programmatic) | Retain input contracts, validation, cancellation registry and generic operation ownership. |
| Programmatic application work | [host/programmatic](../../../../../../host/internal/usecase/host/programmatic) | Retain admission, execution/cancellation/join of prepared work, session/model operations and public command projections. |
| Programmatic output | New `host/internal/infra/programmatic/output` | Move active run/output correlation and event projection from [Delivery](../../../../../../host/internal/usecase/host/programmatic/delivery.go), and unsolicited output from the input controller. |
| Session state, queries and durable publication | [host/sessions](../../../../../../host/internal/usecase/host/sessions) | Implement real client session/history ports directly. Own committed-entry publication and general history projection. |
| Navigation policy | [host/sessiontree](../../../../../../host/internal/usecase/host/sessiontree) | Implement client navigation ports directly; retain handlers, summarization and final validation. |
| Mutation reservation | [host/operationgate](../../../../../../host/internal/usecase/host/operationgate) | Implement the minimal gate interfaces of actual admission consumers. |
| Extension process payloads and catalogue acceptance | [host/extensionruntime](../../../../../../host/internal/usecase/host/extensionruntime) | Own process-facing invocation types, runtime binding/accounting and extension discovery outcomes. |
| Filesystem catalogue observations | [extension catalogue](../../../../../../host/internal/infra/plugins/extension/catalog) and [UI catalogue](../../../../../../host/internal/infra/plugins/ui/catalog) | Return candidates and failures, not application acceptance decisions. |
| Storage version and wire records | [session repository](../../../../../../host/internal/infra/persistence/sessions) | Encode/decode version 2 without a domain/usecase schema selector. |
| TUI command input and command contract | [controller/tui](../../../../../../plugins/ui/tui/internal/controller/tui) | Own the command-sender interface and move Host command payloads out of presentation domain. |
| TUI SDK dispatch and endpoint lifetime | [controller/plugin](../../../../../../plugins/ui/tui/internal/controller/plugin) | Directly implement command sending; retain initialization, operation correlation, foreground Stop and stream lifetime. |
| Bubble Tea program construction and framework I/O | [infra/terminal](../../../../../../plugins/ui/tui/internal/infra/terminal) | Receive the concrete factory/program behavior from [tui/program.go](../../../../../../plugins/ui/tui/internal/controller/tui/program.go). |
| Bash timeout | [bash usecase](../../../../../../plugins/extension/tools/internal/usecase/tools/bash) | Receive timer creation, cancellation cause and cleanup from the input controller. |

Remove the [sessioncontrol](../../../../../../host/internal/usecase/host/sessioncontrol), [sessionnavigation](../../../../../../host/internal/usecase/host/sessionnavigation), [interactions](../../../../../../host/internal/usecase/host/interactions), and [domain/ui](../../../../../../host/internal/domain/ui) packages after their behavior and contracts reach these owners. Remove the empty [TUI presentation usecase](../../../../../../plugins/ui/tui/internal/usecase/presentation). No aliases, duplicated old declarations, or replacement forwarding services remain.

### Core invocation, events and state queries

The complete runtime path is client input, client Host usecase, run control, Core, event dispatcher, mode output, then lifecycle observers.

- Run control owns an execution interface with the run ID and user text input. Its result contains the domain run outcome and whether settlement is required. Core reports whether it entered `StatusAwaitingSettlement`: every return through `finish` requires settlement, while a failed `begin` does not. Run control does not infer Core state from history length or error categories and does not receive its history slice or partial-response state. Recovery after cancellation before the first history append is approved under QST-03 and requires a failing regression before implementation.
- Core implements execution and settlement directly. After execution, run control calls Core settlement, then settled delivery, then releases the gate. Canceled preparation releases its reservation without relying on a run that never started.
- Programmatic owns a separate minimal Core state-query interface. Core answers under its state lock whether a run is running or awaiting settlement. Programmatic retains the operation-ID correlation needed for its public response.
- Move the single provider-neutral lifecycle event model from [run/event.go](../../../../../../host/internal/usecase/agent/run/event.go) to [domain/agent](../../../../../../host/internal/domain/agent). These are shared agent facts, not a client response aggregate. Keep Core's outgoing event interface at Core. Do not keep an alias or second event union.
- The event dispatcher owns its client-delivery and observer interfaces. Mode output implements client delivery; lifecycle implements observation. Client-delivery failure does not skip the observer attempt; the dispatcher preserves both errors.

This removes the coordinator's imports of UI, Programmatic and headless consumers from the event-dispatch package. UI and Programmatic also stop importing Core for event mapping and state snapshots. Those removals make Core's new consumer-contract assertions acyclic.

### Client input, output and initialization

#### UI

- The UI controller owns command/preparation contracts, command-specific progress/results, and a named preparation-failure interface. Host UI implements these contracts. The runtime supplies stream I/O through controller-owned transport contracts; it no longer calls a Host `Prepare` callback through an outgoing Host channel.
- Split the [domain UI frame union](../../../../../../host/internal/domain/ui/model.go) by its actual consumers. Operation commands/results belong to the controller. Host initialization, availability, selection and output method types belong beside the Host ports that consume them. Private wire projections stay with output. Do not relocate the whole union to another shared package.
- Retain one infrastructure owner for the selected UI process and stream. Host selection consumes candidate-start/close operations; the UI controller consumes stream acquisition. The same concrete owner implements both contracts. Selection returns identity/issues, not a runtime service handle passed through Host.
- Preserve [Selector](../../../../../../host/internal/usecase/host/ui/selection.go) behavior. Explicit/configured selection starts its matching candidate once. Automatic selection probes candidates sequentially, closes successful probes, rejects zero or multiple compatible candidates, then restarts the sole compatible candidate. Controller stream acquisition adds no restart or fallback.
- App opens the selected stream at the baseline startup point. After session/provider dependencies are assembled, the controller starts Host initialization and operation processing. Host UI has no transport receiver or receiver factory.
- Initialize output, attach the ordered writer and failure owner, then let Host UI invoke its runtime-activation port and start asynchronous authentication. Command receipt does not wait for authentication completion. Shutdown cancels and joins that work.
- Bind a per-operation reporter before work starts. Output maps agent events and authorization progress into that reporter. Run starts after Accepted is acknowledged. After Run returns, call `Prepared.Release` and finish operation-owned work before terminal enqueue. Keep the operation ID reserved until the writer acknowledges the terminal message.

#### Programmatic and headless

- Programmatic output owns one active run/output association and the attached ordered writer. Prepared application work owns execution, cancellation and joins. Do not move those execution responsibilities into output or retain a second authoritative correlation pointer.
- Output implements the controller's writer/reporter binding contracts and the Host consumer delivery ports. Remove controller `PublishSessionEntry`/`PublishExtensionIssue`, Host `BindConnectionPublisher`, and the Host publication forwarding method. Both operation and unsolicited messages use the connection's one writer.
- Settlement clears the active output association at the dispatcher step. Prepared-work cleanup finishes before terminal enqueue. The generic operation owner retains only the operation identifier until terminal acknowledgement; it does not retain application work or admission reservations for that wait.
- Programmatic runtime failures retain diagnostic-only reporting. Lifecycle observer issues retain their public connection events. The correction does not add a runtime-failure notification that this mode did not previously send.
- Headless output directly implements its consumed startup, event, lifecycle, runtime-failure and session-publication ports. Its acknowledged extension-entry publication deliberately produces no unsolicited session-entry text. Keep one-shot parsing and run invocation in the input controller.

### Session operations and commit ordering

[Session control](../../../../../../host/internal/usecase/host/sessioncontrol/service.go) forwards to state/navigation owners while its callers hold the reservation. Remove that extra owner. UI and Programmatic consume their own minimal gate, active-session and navigation interfaces, implemented directly by the gate, sessions and session-tree services.

- Client prepared operations acquire admission reservations before Accepted. Failed acceptance releases them without starting Run. After execution, release admission reservations and finish operation-owned work during prepared cleanup, before terminal enqueue. The operation-ID reservation remains until terminal acknowledgement. Do not move acquisition inside persistence and create an admission/execution gap.
- Delete the interface-free navigation contract package. Client consumers own their navigation intent and completion/progress aggregates. The tree owner maps those into private handler state and owns its outgoing commit command/result beside the active-session interface.
- Retain operation-scoped reporters and enqueue transformations. They carry one operation's immutable committed snapshot, not a stored dependency on a client service. Their payloads belong to the consuming operation contracts.
- A navigation commit validates the expected leaf, persists navigation and summary together, updates the active tree/history, and enqueues that committed snapshot while holding the session lock. It unlocks before wire acknowledgement and post-commit observers. An enqueue failure returns the committed snapshot and issue; it does not roll back or report an uncommitted navigation.
- Sessions owns the committed-entry publisher interface because sessions actually calls it. Inject the real mode output owner there. Remove the stored publisher from extension context and from its append method parameters.
- An extension append retains runtime/session revalidation and its commit guard. It persists, updates history, and enqueues under commit protection. It releases the guard and lock before waiting for the acknowledgement. Failure still reports the committed entry.
- Navigation progress and later entry notifications use the same ordered writer. Output never rereads active session state to reconstruct an earlier committed snapshot.

Remove domain `Replacement`, `Summary`, and `InformationSnapshot` response aggregates. Replacement can return domain session information and cloned entries from one committed state, plus fork's next-input text. Information can return domain metadata and statistics from one lock acquisition. Client list interfaces own their list-item results; sessions projects them directly from validated loaded sessions. This is a state-to-query projection, not a copied shared response passed through a wrapper.

Move general [entry-to-history projection](../../../../../../host/internal/usecase/host/sessiontree/history.go) to sessions, its only production consumer. Keep summary-request serialization in sessiontree. Keep `session.Info`, entries, trees and accounting values as shared domain concepts. The repository alone encodes and checks version 2; serialized files and replay behavior do not change.

### Extension runtime, discovery and startup output

#### Process-bound contracts

The runtime manager retains availability, incarnation, operation accounting and low-level invocation. Capability services retain registration order, handler policy, summary fallback, final validation and public error meaning.

| Runtime boundary | Consumer-owned representation and actual transformation |
| --- | --- |
| Registration | Process-reported declarations exclude trusted discovered ID/path. Runtime management attaches that identity before returning startup's pending registration. Transport only decodes raw declarations. |
| Tree handler request | Runtime management binds context and projects entries/content to the extension-visible request. Opaque provider context and hidden extension payload bytes are excluded. Transport encodes the runtime-owned payload. |
| Tree handler response | Transport validates response variant/correlation and returns raw process action. Runtime management maps it to the capability action without applying handler policy. Preserve original/current and preserve/clear/replace distinctions. |
| Lifecycle notification | Runtime management projects domain lifecycle facts to the bound extension-visible payload. Transport performs protobuf/SDK work. Observation order and failure policy remain at lifecycle. |
| Tool invocation | Runtime management binds availability/accounting. Domain tool results and progress remain shared values; transport owns their protobuf mapping. |

Move provider-neutral process filtering from [handler mapping](../../../../../../host/internal/infra/plugins/extension/runtime/handler_mapping.go) and [lifecycle mapping](../../../../../../host/internal/infra/plugins/extension/runtime/lifecycle_event.go) with that runtime boundary. Do not copy or embed whole startup, tree and lifecycle aggregates solely to preserve transport signatures.

#### Discovery acceptance

- Both catalogue adapters return all executable observations, normalized IDs, per-entry failures and the directory-read failure. They keep filesystem inspection, normalization and deterministic observation order.
- Extension runtime management decides missing-default-directory success, isolated default-directory failure, fatal explicit-directory failure, and exclusion of every member of a duplicate normalized-ID group.
- UI selection decides whole-catalogue rejection after its directory, candidate, empty-ID or duplicate failures. Preserve the distinct UI and extension outcomes and their complete filesystem causes.

#### Composition and authorization

- Delete app's pricing/model requester forwarding objects. Bind the actual provider catalogue once into the real session/tree consumers before activation. These are dependencies on stateful owners, not separate binding services. Preserve storage initialization before dependent provider work and client initialization.
- UI output implements startup reporting and constructs initialization content. Startup's complete load report is authoritative for that content; preceding issue reports do not create duplicate UI entries. Remove app's report conversion and Host UI's output-only initialization builder.
- UI output owns selection warnings and their stderr fallback. Successful initialization delivery transfers warning delivery once. Close flushes only warnings not delivered through initialization and returns writer errors for shutdown joining. App only constructs, invokes and closes owners.
- Remove the Host interactions forwarding service. UI output directly implements the Codex-consumed interaction contract with actual authorization progress and browser-output behavior. The browser adapter implements an interface owned by that output consumer. Codex retains OAuth protocol and credential work.
- Non-UI modes provide no interaction implementation. Codex returns the baseline unavailable-interaction cause at the presentation step, preserving preceding OAuth setup/cleanup and error context. URL presentation still precedes best-effort browser launch; browser failure does not stop OAuth after successful URL delivery.

### TUI command ownership and bash execution

#### TUI

- Move `CommandKind`, `Command` and `TreeCommand` from presentation domain beside a command-sender interface consumed by the terminal input Model. Change all producers, receivers and mappings together. Keep genuine presentation values and projection events/state in the domain.
- The plugin endpoint directly implements command sending through its SDK dispatch and correlation behavior. It retains the initialized connection and active run context during its endpoint lifetime. Program construction receives that initialized sender; shutdown cancellation reaches command dispatch.
- Move the concrete Bubble Tea factory/program into the existing terminal infrastructure package. Keep `ProgramFactory` and `Program` interfaces at the plugin endpoint, their lifecycle consumer. The factory passes the Model-owned command sender into the Model.
- This removes `controller/tui → controller/plugin`. The new assertion import is `controller/plugin → controller/tui`; terminal infrastructure imports both consumers. No reverse path remains through the factory implementation.
- Call `State.Apply` directly at the projection consumer and remove the empty usecase and its `Apply` callback. Preserve meaningful projection tests at the state or caller, rather than testing a replacement wrapper.
- Terminal input still emits a `tea.Cmd`. The endpoint remains the terminal-file cleanup owner; moving program construction does not add a second cleanup path or a new application service.

#### Bash

The extension input controller retains JSON parsing and validation. Its bash command carries text and validated optional timeout intent. The bash usecase starts/stops the timer and supplies its cancellation cause. The process adapter still owns process-group execution/termination and output spill.

Preserve absent timeout, positive fractional seconds, the one-nanosecond clamp, maximum-duration validation, timeout text and wrapping, parent cancellation, progress order and bounded output. Keep per-operation progress callbacks; they represent the execution stream, not a hidden service dependency. Reuse [bash tests](../../../../../../plugins/extension/tools/internal/controller/extension/bash_test.go) across their corrected owners. No behavior change is intended.

### Approved error corrections

#### Complete source text

FND-05 requires these source-to-client changes:

- Extension SDK preserves complete Failed, Rejected and ordinary HandlerError text, protocol-error context and gRPC error causes. Remove non-secret truncation, including the bounded replacement status that discards its original cause. Transport message/queue limits remain separate from error-text policy.
- Codex HTTP 401 retains `ErrSignInRequired` classification together with the original SDK error. Authentication availability handling must still recognize the category through wrapping.
- Invalid navigation actions retain the exact validation cause, preserve preceding state and continue later handlers under the established ordinary-error policy.
- Unknown-label mutation returns the entry-not-found category together with the domain cause and performs no repository mutation.

#### Source-backed persistence classification

[APC-20](../../../../issues/blocking-contract-operation-processing/solution.md#operation-inventory) defines the approved run-failure mapping, including joined failures. PHS-07.1 implements the history-persistence distinction for both client contracts. The production baseline still emits only `INTERNAL` for accepted-run failures, so retaining that category in TUI alone cannot replace the text test. Ordinary tool errors remain tool results. The [agent-run failure issue](../../../../issues/agent-run-failure-semantics/problem.md) retains broader model/provider classification in product PHS-06/PHS-12.

Move the single history-recording failure identity from Core to `domain/agent`. Core detects that failure and wraps the shared identity together with the original cause. Both client Host usecases classify it with `errors.Is`, without importing Core or matching text. Update all producers and consumers together; retain no alias or duplicate declaration. Update Programmatic's [command-specific failure allowlist](../../../../../../host/internal/controller/programmatic/delivery.go), which otherwise replaces the new accepted-run category with `INTERNAL`.

Map that source-backed category to a presentation-owned persistence cause. Keep text separately and remove prefix-based domain behavior. Connection-error mapping also retains semantic classification instead of deriving it from text; this plan adds no new connection-event kind. Implement the approved APC-20 amendment tracked by the cross-phase issue. Do not add retry, provider source categories, or a general error taxonomy.

### Contract and assertion closure

- Remove the UI consumer's unused direct `AgentRunner.Run` and output `Channel.Close` requirements. Concrete process/probe cleanup remains with the owner that calls it.
- Give UI preparation, UI selection failure, tree selection failure, and client navigation failure classification named interfaces at their actual consumers. Implementations assert them in their own packages and retain complete causes.
- Client navigation errors no longer import sentinels from the deleted shared contract package. Tree-owned errors implement the client classification contracts. Preserve model/credential/extension failure categories; domain session errors remain domain values.
- Add the missing external assertions for UI SDK `grpcUIPlugin`, bash `streamWriter`, and grep `boundedLine`. Their contracts are `plugin.Plugin`, `plugin.GRPCPlugin`, `io.Writer` and `io.RuneReader` respectively. Required package-level imports already exist.
- Regenerate mocks and public generated contracts from their sources. Do not manually edit generated bodies or add assertions in assembly to substitute for implementation-package checks.

### Target import constraints

Arrows here are Go imports. Runtime direction can be opposite. `H` means `host/internal/usecase/host`; `C` means `host/internal/controller`; `I` means `host/internal/infra`; Core means `host/internal/usecase/agent/run`.

| Importing owner | Contract dependencies that determine cycle safety |
| --- | --- |
| `C/ui`, `C/programmatic`, `C/cli/headless`, `C/extension` | Their public transport/SDK and domain values, not concrete Host or infrastructure implementations. |
| `H/ui`, `H/programmatic` | Their respective input-controller contracts, domain values and generic operation code. No Core, session, event, runtime or output implementation imports. |
| New `H/runcontrol` | Headless, UI and Programmatic consumer contracts. No Core/event implementation import. |
| Core | `H/runcontrol` and `H/programmatic` consumer contracts; provider-neutral domain/utilities. |
| `H/events` | Core's event contract and run-control settlement contract. No UI/Programmatic/headless implementation imports. |
| `H/sessiontree` and `H/operationgate` | Their actual UI/Programmatic/run-control consumers and domain values. |
| `H/lifecycle` | Event and startup consumer contracts. |
| `H/extensioncontext` | Extension controller and capability context-consumer contracts. |
| `H/sessions` | Core, context, tree and UI/Programmatic consumer contracts. |
| `H/extensionruntime` | Startup, context, tool, tree, lifecycle, extension-controller and UI activation contracts. |
| `H/providers` | Core and Host consumer contracts; no output implementation. |
| Mode output implementations | Their controller, events, lifecycle, sessions, startup/runtime consumers as actually used. UI output also implements the Codex interaction contract. |
| Extension process transport | Runtime-manager process contracts, domain, SDK and protobuf. No borrowed startup/tree/lifecycle aggregates. |
| Browser implementation | UI output's browser-consumer contract, not the removed Host interactions service. |
| TUI plugin endpoint | Terminal-input command contract. |
| TUI terminal infrastructure | Plugin lifecycle contracts and input Model. Input Model no longer imports the plugin endpoint. |

The target owner graph was checked against the retained baseline package imports and has no cycle. This is a design-graph check, not a compilation result. Key removed edges are the coordinator's consumer imports in events, client imports of Core, headless input imports for output, the Host-to-Codex interaction import, and all imports of deleted contract/forwarding packages. During implementation, compare every actual new import and generated mock dependency with the full graph before adding its assertion.

### Execution order

The corrections below are dependency slices, not feature releases. No compatibility path, feature flag or dual-run implementation is introduced. Each slice updates the complete affected input-to-result path and reuses behavioral tests. Source-only restructuring does not need an artificial failing test. The approved behavioral parts of run control and the error slices require RED, GREEN, then structural cleanup. The user approved the complete implementation plan.

#### 1. Host client input and output

**Goal.** Put UI and Programmatic input, execution work and output at their real owners; separate headless output.

**Work and deliverables.** Establish controller command/error/output contracts, move concrete output behavior and correlation, separate UI command receipt, and assign UI DTOs. Move shared agent events and introduce the Core state-query contract as needed to remove client-to-Core imports. Establish the selected-process-to-controller handoff without extra restart.

**Exit criteria.** All client modes retain acceptance/progress/terminal ordering, cancellation and output behavior. Every UI command reaches the actual controller. Unsolicited and operation output share one writer. Client Host packages no longer import Core or concrete output.

**Risks.** Splitting correlation or losing writer failure ownership can produce stale active-run state or duplicate terminal results. Retain [Programmatic lifecycle tests](../../../../../../host/internal/controller/programmatic), [UI runtime tests](../../../../../../host/internal/infra/plugins/ui/runtime), headless renderer tests, and cross-mode lifecycle integration tests.

#### 2. Run control, events and admission

**Goal.** Remove execution/settlement/gate callbacks without a reverse dependency on Core.

**Work and deliverables.** Add a failing regression for the approved cancellation-recovery behavior during an AgentStart observer before the first append. Move coordinator behavior to run control, report Core's actual settlement transition in its execution result, and add dispatcher, gate and real output assertions. Use the client package boundaries established in correction 1.

**Exit criteria.** Prepared cancellation releases once. Every finished Core run settles, including zero-history cancellation and first-append persistence failure. A failed begin does not settle another run. Core settlement, client settled delivery and observers finish before gate release. A later run is admitted on the open client connection. The actual import graph remains acyclic.

**Risks.** Reusing the history/error inference would preserve the stuck-run defect. Retain [Core persistence tests](../../../../../../host/internal/usecase/agent/run/persistence_test.go), [coordinator tests](../../../../../../host/internal/usecase/host/events/coordinator_test.go), UI prepared-operation tests and Programmatic state tests, and execute the new public observer-cancellation regression.

#### 3. Session queries, navigation and publication

**Goal.** Give state and navigation owners their direct consumers and preserve atomic commit-to-output ordering.

**Work and deliverables.** Remove sessioncontrol/sessionnavigation, assign client/commit contracts, bind entry publication at sessions, move history projection, and make storage version repository-owned. This depends on correction 1's real output owners.

**Exit criteria.** Navigation and summary commit together. Its snapshot enqueues before later appends. No wire wait holds commit protection. Publication failure returns committed state. Stale context cannot commit. Create/resume/fork/clone and replay retain version-2 storage behavior and visibility.

**Risks.** An output adapter must not reconstruct a committed snapshot from newer state. Retain [navigation/message integration](../../../../../../host/internal/app/extension_message_navigation_public_integration_test.go), [incarnation integration](../../../../../../host/internal/app/context_incarnation_public_integration_test.go), [recovery integration](../../../../../../host/internal/app/session_state_recovery_public_integration_test.go), [summary atomicity](../../../../../../host/internal/app/summary_control_atomicity_integration_test.go), and repository/replacement suites.

#### 4. Runtime boundaries, discovery and startup

**Goal.** Remove borrowed process aggregates and adapter/application policy overlap without changing startup or OAuth behavior.

**Work and deliverables.** Establish raw/filtered process payloads, move discovery acceptance to Host, replace app forwarding with direct owner bindings, move startup/warning output, and implement provider authorization at the real output owner. This follows corrections 1 through 3.

**Exit criteria.** Registration cannot supply trusted discovered identity. Process payloads preserve original/current state and exclude private data. Catalogue outcomes match the baseline. Warnings deliver once. Monitoring/authentication start only with initialized output. Storage and OAuth failure ordering remain intact.

**Risks.** Provider-neutral filtering must not become capability policy. Retain [handler composition](../../../../../../host/internal/app/handler_composition_integration_test.go), [lifecycle observer modes](../../../../../../host/internal/app/lifecycle_observer_modes_integration_test.go), [context catalogue modes](../../../../../../host/internal/app/context_catalogue_modes_integration_test.go), [configured requests](../../../../../../host/internal/app/configured_request_modes_integration_test.go), catalogue/selection/startup tests and Codex OAuth tests.

#### 5. Bundled tool and TUI ownership

**Goal.** Put bash execution timeout and TUI command/projection behavior at their owners.

**Work and deliverables.** Apply the TUI and bash designs above. Remove the empty presentation usecase. Add the scoped SDK/I/O assertions with their implementation changes. This slice uses baseline public contracts and can be developed independently of Host structural slices, but not in a concurrently modified shared checkout.

**Exit criteria.** Bash timeout/cancellation/output behavior and TUI initialization, command dispatch, projection, foreground cancellation and terminal cleanup remain unchanged. No domain Host-command DTO or TUI reverse factory import remains.

**Risks.** Moving the sender must retain the active SDK context. Moving the timer must retain fractional timeout behavior and complete output. Reuse the linked bash tests, TUI domain/controller tests and real terminal/plugin integration tests; do not test the absence of the forwarding service.

#### 6. Complete error text

**Goal.** Implement QST-01's approved error-text behavior across source, SDK and client boundaries to close FND-05.

**Work and deliverables.** Execute the RED/GREEN cases below, correct each loss point, and rerun the corresponding public error paths. Apply structural cleanup only after the failure-preservation assertions pass.

**Exit criteria.** Complete source text survives all listed outcomes; categories and ordinary-handler continuation remain intact. No invalid action or label operation changes session state.

**Risks.** A test can accidentally preserve the old truncation rule. Replace those expectations with intact diagnostic suffixes and cause checks, not a larger arbitrary truncation limit.

#### 7. Run-persistence cause and TUI cleanup

**Goal.** Implement QST-02's approved persistence distinction and remove diagnostic-text-dependent domain behavior.

**Work and deliverables.** Implement the approved run-failure mapping, carry the Core persistence cause through both client mappings, map TUI semantic causes, and remove the prefix rule. Record implementation and verification status in the cross-phase issue and governing error-contract documentation in the same slice.

**Exit criteria.** Both client contracts expose the approved persistence category for the same source failure. TUI cleanup depends on that cause, not text. Other run failures remain `INTERNAL`; no new retry/provider categories or connection event kinds exist.

**Risks.** A TUI-only change would diverge from Programmatic behavior or merely retain `INTERNAL`. The RED/GREEN tests must start at the source failure and cross both client boundaries.

#### 8. Remove residue and verify the whole product

**Goal.** Close every audit disposition without hidden forwarding, copied contracts or missing assertions.

**Work and deliverables.** Remove obsolete declarations, packages, generated mocks and bindings from the owning slices. Update architecture component/ownership sections, this phase's evidence, and roadmap/issue status to match implemented results. Run the project verification sequence, then obtain independent whole-scope boundary review.

**Exit criteria.** Every FND-01 through FND-09 disposition is implemented and reviewed. Generation is repeatable. Required checks pass. No unresolved architectural violation remains in the audited scope. Only then can PHS-07 resume.

**Risks.** Passing tests can preserve a wrong boundary. Final review must repeat the 72-package baseline coverage accounting against the resulting package set and inspect both imports and runtime paths. No unrelated cleanup is included.

Corrections 1 and 2 separate client contracts before Core acquires their assertion imports. Correction 3 removes session forwarding rather than introducing a temporary publisher adapter. Correction 4 then changes runtime payloads and startup binding on those established owners. Correction 5 is a separate plugin path. Correction 2's cancellation recovery and corrections 6 and 7 have approved behavior and require failing tests before implementation. This order does not require artificial compatibility wrappers or tests of package layout.

### Behavioral regression cases

Existing test cases must be updated before adding another fixture for the same path. Each RED run must fail on the expected assertion, not a compilation error or timeout.

| Case and purpose | Inputs and expected result | Edge cases and dependencies |
| --- | --- | --- |
| Cancellation settlement recovery | Hold an AgentStart observer before the first user-history append, then cancel the accepted run without closing the connection. Core and output settle, exactly one terminal outcome occurs, and a later run is admitted. | No user append or provider request; failed begin does not settle, and first-append persistence failure still settles. Use Core/coordinator tests and production Programmatic/UI integration. The RED run must fail on state/admission, not a timeout. |
| SDK full error preservation | Replace the three [audited truncation tests](audit.md#verification-evidence) with diagnostics above 65,536 bytes. Failed, Rejected, HandlerError, malformed-event context and gRPC failures retain the complete safe text and category/cause. | Unicode text, recognizable final suffix, both operation initiators; shared operation tracking and real SDK integration for the wire path. |
| Codex source cause | HTTP 401 with a safe source detail retains that detail and detectable sign-in-required classification. | Wrapped errors and configured-model/agent callers; existing Codex request/transport fixtures. No live provider request is required. |
| Navigation validation cause | Invalid replacement target or custom-focus shape yields the exact validation issue, retains preceding handler state, and permits the next ordinary handler. | Missing/invalid action payload, later valid handler, no rejected-state commit; sessiontree handler tests and public issue mapping. |
| Label failure cause | Unknown target returns both entry-not-found classification and domain cause without a repository mutation. | Wrapped error propagation to both client command mappings; session service mocks and client integration cases. |
| Public run persistence distinction | Inject a typed persistence failure in an admitted run. Both UI and Programmatic terminal failures use the approved category with full text. | First user append, model/tool-result append, joined source failures, unrelated failure remaining `INTERNAL`; Core/coordinator tests and production client integration. |
| TUI semantic cleanup | The approved persistence cause with different text clears provisional state identically. An unrelated cause with the old text prefix does not trigger persistence cleanup. | Operation and connection mapping, unknown categories, preserved diagnostic display; projection logic tests and TUI integration. No new connection-event kind is implied. |

### Verification and completion evidence

For structure-only changes, run affected behavioral tests uncached without an artificial RED phase. Keep unit tests isolated and mark tests combining production components or real filesystem/process/terminal/stream adapters as integration tests. Use mockgen, not custom mocks. Do not test prompts, logs, package names, or the absence of removed wrappers.

After the implementation slices:

1. Run `task fmt`.
2. Run `task fix_dry_run`, inspect its proposals, then apply justified fixes manually or with `task fix`.
3. Run `task lint`.
4. Run `task test` and `task itest`. Also run the affected behavioral cases with `-count=1` to record uncached results.
5. Run `task test-coverage` with the configured project threshold.
6. Run generation from changed source contracts/mocks and repeat it. The second run must produce no diff. No dependency installation or unrelated cache cleanup is part of this plan.
7. Rebuild the actual import graph, check each implementation-package assertion and its consumer's transitive imports, and independently review all resulting production boundaries.

The audit's cached baseline tests are not implementation evidence. Record exact commands, exit status, generation result, covered behavior and remaining failures when implementation is approved and performed.

### Implementation evidence

#### U1: Host client input and output

Correction 1 is implemented. The [architecture components](../../architecture.md#components) describe the resulting input and output owners. The old domain UI package and Host delivery implementations are removed. Core supplies the Programmatic activity predicate through its consumer interface. The following evidence sections track subsequent corrections. Independent whole-product review remains open.

All commands below exited 0 on the U1 implementation:

- `task fmt`; `task fix_dry_run` with no proposals; `task lint`; `task test`; `task itest`; `task test-coverage`; `task build`; `git diff --check`.
- `go test -race -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/app ./host/internal/infra/headless ./host/internal/infra/plugins/ui/runtime ./host/internal/infra/programmatic/output ./host/internal/usecase/host/operationgate`.
- Two `go generate ./...` runs produced identical SHA-256 snapshots for all 66 generated Go files.

After comment refinement, the project checks passed again. The last coverage result was 83.5%, above the 80.0% threshold. Main-agent uncached tests passed for UI and Programmatic controllers, Programmatic output, both client Host usecases, and `internal/operation`. The dependency-closure check found no Host usecase or infrastructure dependency in those input controllers and no Core or concrete output dependency in either client Host usecase. The changed handwritten source contains no workflow labels or temporary-work markers. Main-agent source verification covered prepared cleanup, output correlation, initialization/activation, selected-process reuse, and requested/failed connection closure.

#### U2: Run control, events and admission

Correction 2 is implemented and passed main-agent source verification. `host/internal/usecase/host/runcontrol` owns prepared reservations and Core invocation. Core directly implements its execution and settlement contract. The result contains only `Outcome` and `SettlementRequired`. Core sets settlement required when `finish` enters awaiting-settlement state; a failed `begin` returns no settlement requirement. Terminal diagnostics and added history remain in the shared `AgentEnd` event. No Core forwarding implementation or compatibility declaration was added.

The dispatcher consumes `ClientDelivery` and retains client-first delivery followed by observers. UI and Programmatic consume their own `Gate` interfaces. The real operation gate implements those interfaces and run control's gate interface. Sessioncontrol no longer forwards gate acquisition. Its session/navigation forwarding remains for correction 3.

The executable RED command was `go test -race -tags=integration -count=1 ./host/internal/app -run '^TestProgrammaticObserverCancellationReleasesRun$'`. It exited 1 before recovery implementation. The test failed on an assertion, not compilation or timeout: the active operation identifier was `blocked-observer` after cancellation instead of empty. The same command exited 0 after implementation.

The [Programmatic regression](../../../../../../host/internal/app/observer_cancellation_integration_test.go) and [UI regression](../../../../../../host/internal/app/observer_cancellation_ui_integration_test.go) use the production app, client input/output, Core, dispatcher, lifecycle, and a public extension process. They block the AgentStart observer's nested configured-model request before its history append. After targeted cancellation, each retained connection reports empty history and admits a later run. They check one terminal result for the canceled run. Programmatic also reports idle Core state and no active output association. The first provider call belongs to the canceled observer; the next two belong to the later observer and later Core run.

[Core settlement tests](../../../../../../host/internal/usecase/agent/run/settlement_test.go) and [persistence tests](../../../../../../host/internal/usecase/agent/run/persistence_test.go) cover zero-history cancellation, failed begin, and first-append persistence failure. [Run-control tests](../../../../../../host/internal/usecase/host/runcontrol/coordinator_test.go), [prepared reservation tests](../../../../../../host/internal/usecase/host/runcontrol/gate_test.go), and [gate integration](../../../../../../host/internal/usecase/host/operationgate/lifecycle_test.go) cover exactly-once preparation release and Core/client/observer settlement before admission release. Dispatcher tests retain both error identities and text after failed client delivery.

All final commands exited 0:

- `task fmt`; `task fix_dry_run`; `task lint`; `task test`; `task itest`; `task test-coverage`; `task build`; `git diff --check`.
- `go test -race -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -count=5 ./host/internal/app -run '^Test(Programmatic|UI)ObserverCancellationReleasesRun$'`.
- Two `go generate ./...` runs. SHA-256 snapshots of all 65 generated Go files outside the separate experiment modules matched before, between, and after both runs.

`task fix_dry_run` produced no proposals. Local compile/lint failures were corrected by updating constructor calls, removing stale result fields, removing unused test directives, and adding the required structural settlement assertions in real output implementations. No suppression, dependency change, or cache clearing was used. The changed restarted-history test combines sessions with Core and now runs under the integration tag. Final coverage is 83.7%, above the 80.0% threshold. Dependency-closure checks cover all new implementation-to-consumer imports. Run control and both client Host usecases have no Core, dispatcher, or infrastructure dependency.

Main-agent tests passed with `go test -race -count=1 ./host/internal/usecase/host/runcontrol ./host/internal/usecase/host/events ./host/internal/usecase/agent/run`. Both public cancellation regressions also passed twice with `-race -tags=integration -count=2`. Source verification covered owner-reported settlement, failed-begin isolation, prepared reservation transfer, client/observer errors, and release ordering. Subsequent evidence sections track the remaining corrections. Independent whole-product review remains open.

#### U3: Session queries, navigation and publication

Correction 3 is implemented and passed main-agent source inspection. `sessioncontrol` and `sessionnavigation` are removed. Host [UI](../../../../../../host/internal/usecase/host/ui/interfaces.go) and [Programmatic](../../../../../../host/internal/usecase/host/programmatic/interfaces.go) own active-session contracts directly implemented by sessions. Each Host client owns its [navigation intent and completion](../../../../../../host/internal/usecase/host/ui/navigation.go), rather than borrowing controller response aggregates. Sessiontree directly implements both navigation ports. Client packages have no concrete session, navigation, Core, event, runtime, or output dependency.

Sessions returns replacement information and cloned entries from one committed state. Fork also returns the selected next-input text. Metadata and statistics come from one lock acquisition. The domain `Replacement`, `Summary`, and `InformationSnapshot` aggregates are removed. [Stored-list queries](../../../../../../host/internal/usecase/host/sessions/list.go) select exact first-user text, count the complete tree, and preserve update-time and ID ordering. Each Host client normalizes the public preview when constructing its controller-owned list row. The list ports do not borrow those public row types.

[Navigation commit](../../../../../../host/internal/usecase/host/sessions/navigation.go) retains expected-leaf validation, combined navigation and summary persistence, active-state and history update, and committed-tree enqueue under the session lock. Progress projection uses that immutable tree. Wire acknowledgement and observers remain outside commit protection. An enqueue failure returns committed metadata with a delivery issue. Named client navigation-failure contracts preserve category and text without importing tree-owned sentinels. The provider catalogue also asserts the tree consumer's named selection-failure contract.

[Sessions publication](../../../../../../host/internal/usecase/host/sessions/publication.go) owns `EntryPublisher` and its binding. Headless, UI runtime, and Programmatic output directly implement it. Extension context no longer stores or passes a publisher. Runtime and session revalidation remain in effect. Append persists, updates history, and enqueues under the session lock and runtime guard, then releases both before acknowledgement. Output consumes the committed entry rather than rereading active state.

[General history projection](../../../../../../host/internal/usecase/host/sessions/entry_history.go) now belongs to sessions; summary-request serialization stays in sessiontree. The [repository](../../../../../../host/internal/infra/persistence/sessions/service.go) alone selects version 2. [Replay](../../../../../../host/internal/infra/persistence/sessions/recovery.go) retains version validation. Domain headers contain no codec selector.

This is a structure-only correction. Existing replacement, navigation, publication, incarnation, recovery, repository, history, accounting, and admission tests were retargeted to the actual owners and run uncached. No behavioral correction or artificial RED test was introduced. The Programmatic lifecycle-list case and a UI list-operation case check that exact stored text becomes the established normalized public preview while absent text remains absent.

All final commands exited 0:

- `task fmt`; `task fix_dry_run`; `task lint`; `task test`; `task itest`; `task test-coverage`; `task build`; `git diff --check`.
- `go test -race -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -count=1 -v ./host/internal/app -run '^(TestProgrammaticPreCommitAppendSurvivesNavigationCancellation|TestUINavigationPublishesSnapshotBeforeObserverAppend|TestProgrammaticNavigationPublishesSnapshotBeforeObserverAppend|TestPublicRetainedContextNeverReactivates|TestPublicExtensionRecoversHiddenStateAfterProcessRestart|TestSummaryControlPreCommitFailureKeepsStoredTree)$'`.
- Two `go generate ./...` runs produced identical SHA-256 snapshots for all 67 generated Go files outside the separate experiment modules.

The first `task fix_dry_run` proposed typed `errors.AsType` in the two navigation classifiers. Those changes were applied manually because they preserve wrapped-error classification without mutable target variables. The final dry run had no proposals. Lint findings were resolved without new suppressions, dependency changes, or cache clearing. Final coverage is 83.6%, above the 80.0% threshold. Actual import-closure checks cover sessions, sessiontree, all three entry publishers, and the provider selection-failure assertion. Every checked consumer closure excludes its implementation. The changed handwritten Go files contain no removed-owner references or workflow residue.

Main-agent inspection covered commit/append protection, immutable publication snapshots, direct client navigation/query contracts, and repository version ownership. Uncached race tests passed for sessions, sessiontree, UI, and Programmatic. Main-agent integration checks also passed for the six named public atomicity/recovery scenarios and repository replay/replacement tests. Subsequent evidence sections track the remaining corrections. Whole-product architectural acceptance remains open. The label/navigation source-error corrections and run-persistence failure category are not part of this implementation.

#### U4: Runtime boundaries, discovery and startup

Correction 4 is implemented. Main-agent source verification covered runtime filtering, trusted identity, discovery acceptance, direct provider binding, startup warning ownership, and initialized activation. Independent uncached race tests passed for the runtime/startup/UI usecases, UI output, both filesystem catalogues, Codex OAuth, and public handler/lifecycle/catalogue/configured-request paths. The [architecture components](../../architecture.md#components) describe the resulting owners. Corrections 5 through 8 and whole-product architectural acceptance remain open.

The [runtime process contracts](../../../../../../host/internal/usecase/host/extensionruntime/interfaces.go) no longer borrow startup, sessiontree, or lifecycle aggregates. Registration contains process declarations without discovered identity. Runtime management attaches the accepted executable ID/path, retains runtime accounting, and projects original/current navigation state and lifecycle facts into process-visible payloads. Those payloads have no provider-context or hidden-extension-data field. Raw actions retain presence and preserve/replace/clear distinctions; capability owners still decide acceptance and composition. Transport retains SDK correlation, response-variant validation, and encoding. All three mode outputs directly implement runtime-failure reporting.

Filesystem adapters return executable observations, entry failures, and directory-read causes. Extension runtime management applies default/explicit directory policy and excludes every duplicate member. UI selection rejects the complete catalog before starting a candidate if any observation fails acceptance. Explicit/configured selection and sequential automatic probing retain their startup/close/restart sequence.

App's pricing/model forwarding objects and the Host interactions service are removed. Sessions and sessiontree bind the actual provider catalogue after storage initialization and before activation. UI output implements startup reporting, constructs initialization from the authoritative final report, presents authorization URLs, and owns selection-warning fallback. Host UI supplies model/session facts and consumes the runtime-activation port. The controller still attaches the initialized writer and failure owner before activation and asynchronous authentication. Shutdown joins authentication before closing UI output and extension runtimes. Non-UI modes provide no interaction implementation; Codex preserves the unavailable-interaction cause at the presentation step after OAuth setup.

Existing behavioral tests were retargeted without artificial RED. Added boundary tests cover trusted registration identity, private-data exclusion, original/current state, raw action semantics, distinct catalog acceptance, authoritative startup issues, once-only warning transfer/fallback, complete writer failures, and OAuth setup/cleanup without interaction.

All final commands below exited 0:

- `task fmt`; `task fix_dry_run`; `task lint`; `task test`; `task itest`; `task test-coverage`; `task build`; `git diff --check`.
- `go test -race -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/... ./internal/operation`.
- `go test -race -tags=integration -count=1 -v ./host/internal/app -run '.*(Handler|Lifecycle|Catalogue|Configured|Startup).*'`.
- `go test -race -tags=integration -count=1 -v ./host/internal/infra/providers/openai/codex ./host/internal/infra/plugins/extension/catalog ./host/internal/infra/plugins/ui/catalog ./host/internal/infra/plugins/ui/runtime`.
- `go test -race -count=1 -v ./host/internal/usecase/host/startup ./host/internal/usecase/host/ui ./host/internal/usecase/host/extensionruntime`.
- Two `go generate ./...` runs produced identical SHA-256 maps for 68 generated Go files outside experiments. Generated bodies were not edited manually.

The final dry run had no proposals. Compilation and lint failures were resolved through complete contract/test updates, explicit struct fields, indexed iteration, and ordinary formatting/error-construction corrections. No new suppression or dependency change was used. Coverage is 83.8%, above the 80.0% threshold. Consumer dependency closures were checked before implementation assertions; no consumer closure contains its implementation.

One additional parallel Codex run exposed a pre-existing test-client connection-lifetime race. Temporary connection-state diagnostics showed the successful callback connection close, followed by an unused connection in `StateNew` until the five-second shutdown deadline. No handler remained active. The [OAuth success fixture](../../../../../../host/internal/infra/providers/openai/codex/auth_test.go) now uses a dedicated non-reusing transport only for its two callback requests. Token exchange and production shutdown are unchanged. Thirty parallel Codex suite repetitions passed after this test-only correction. Temporary diagnostics were removed.

Supplemental unit-tag lint was also run over extensionruntime, Host UI, UI output, and extension transport. The full command still exits 1 on 28 pre-existing findings: two duplication findings, eight incomplete test literals, ten long lines, six error-assertion findings, one redundant conversion, and one integration-only fixture constant unused under unit tags. All introduced findings were corrected. The same command with `--new-from-rev=HEAD` exited 0 against the U4 parent `a63248d`. The 28 findings are pre-existing relative to that parent, not evidence that they predate the whole architecture correction. These results do not claim that the supplemental full command passed; its baseline findings remain outside this unit's cleanup scope.

### Execution control

The user selected `final_only`. Execute corrections 1 through 8 in the stated order, with no concurrent source changes in the shared checkout. Each correction is one implementation unit, U1 through U8, and has one separate local commit after its checks pass. Do not push. Rework of a committed unit produces a corrective commit. Overall user review follows integrated verification.

The [execution order](#execution-order) defines each unit's inputs, affected components, expected result, dependencies, risks, tests, and exit criteria. U1 establishes client contracts used by U2 and U3; U4 uses U1 through U3. U5 is independent of the Host structural changes but runs sequentially to avoid checkout interference. U6 corrects error paths at the owners established by the structural units. U7 uses run control, client mappings, and TUI ownership from U1, U2, and U5. U8 closes the combined result. Sequential execution is not an additional architectural dependency.

For each unit, inspect workflow-artifact residue before verification and commit. Task identifiers and stage names belong in execution records, not production names or comments. Reuse behavioral tests for structure-only changes. U2, U6, and U7 require the [behavioral regressions](#behavioral-regression-cases). Run the project verification sequence before committing code changes, and record uncached targeted results. Repeat generation when source contracts or mocks change.

| Workflow stage | Atomic work and instance allocation | Inputs and output | Main-agent verification and role rationale |
| --- | --- | --- | --- |
| `plan_guard` | No subagent. Main agent checks approvals, commit baseline, dependencies, and modification scope. | Approved plan, clean worktree, baseline evidence; execution may start. | This is approval ownership, not a separate analysis task. |
| `unit_1` through `unit_5` | One fresh `SubAgentCoderComplex` instance per unit, labelled `implement-U1` through `implement-U5`. Run sequentially. | The corresponding correction and preceding source state; complete path changes, tests, and check evidence. | Inspect changed owners, references, imports, behavior, and raw check results. These changes cross component boundaries; a regular coder is insufficient for ownership decisions. |
| `unit_6` | One fresh `SubAgentCoderRegular` instance labelled `implement-U6`. | Approved complete-text cases and corrected owners; RED/GREEN error preservation and verification evidence. | Inspect source-to-client cause preservation and continuation behavior. The approved fixes are bounded and need no new architectural choice. |
| `unit_7` | One fresh `SubAgentCoderComplex` instance labelled `implement-U7`. | Shared history-persistence source, client contracts, and TUI mapping; RED/GREEN semantic classification and updated error-contract documents. | Trace both client paths and presentation cleanup. Cross-component classification requires more than a local mapper change. |
| `unit_8` | One fresh `SubAgentCoderComplex` instance labelled `implement-U8`. | U1 through U7 diffs and audit dispositions; removed residue, complete assertions, and architecture/status documents. | Check all nine finding groups against source and ensure no wrapper hides a dependency. A coder can correct remaining code as well as documents. |
| `integrated_verification` | First one fresh `SubAgentExtractor` instance labelled `verify-product`, then one fresh `SubAgentAnalystComplex` instance labelled `review-product-boundaries`. | Full corrected source and unit evidence; whole-project check report, then independent whole-scope architecture review. | Inspect command outputs and each review finding. Extraction is mechanical; boundary review requires cross-component judgment. Review uses the check report, so these instances are sequential. |
| `user_review` | No subagent. Main agent presents the complete result and remaining issues. | Integrated evidence; explicit overall user acceptance or requested corrections. | User interaction belongs to the main agent. No per-unit user review is required under `final_only`. |
| `report_completion` | No subagent. Main agent reports accepted results and local commits. | Accepted result and final repository state; concise completion report. | No independent extraction or analysis is needed to repeat established evidence. |

Each instance owns one atomic unit. Continuation of an instance is limited to corrections in that unit. Different units never share an instance. Main-agent verification and local commits occur after each implementation response. No parallel subagent batch is planned.

## Overengineering and overspecification considerations

- Three new Host packages contain moved behavior. Five redundant or misowned Host/TUI packages are removed. No generic capability bus, second output queue, provider execution subsystem, or new TUI application service is added.
- Boundary mapping must perform input validation, consumer-specific query projection, trusted runtime binding, private-data filtering, storage encoding, or output construction. A field-for-field copy used only to preserve an old caller is not a correction.
- Keep operation reporters, release functions, commit guards and local mutation callbacks where they represent one operation. Replace persistent service method-value wiring at its real consumer.
- Private helper names and file splits remain implementation choices. Ownership, removed dependency edges, approved behavior and commit/settlement order define the plan's scope.
- No backward compatibility, data migration, feature flags, rollout or rollback mechanism is needed. Structural storage changes retain the version-2 format.

## Approved behavior decisions

The user approved QST-01 through QST-03 under ticket NFQ-01. QST-01 and QST-02 remain unimplemented and require the specified failing regressions. QST-03 has executable RED/GREEN evidence under U2. The user also approved the complete correction plan and execution policy.

### QST-01: Complete error-text corrections

Approved scope is defined under [complete source text](#complete-source-text). Categories, ordinary-handler continuation and mutation rules remain unchanged.

### QST-02: Narrow run-persistence distinction

Approved scope is defined by [APC-20](../../../../issues/blocking-contract-operation-processing/solution.md#operation-inventory) and [source-backed persistence classification](#source-backed-persistence-classification). Broader PHS-06/PHS-12 failure semantics remain deferred.

### QST-03: Cancellation settlement recovery

Approved scope is defined under [Core invocation, events and state queries](#core-invocation-events-and-state-queries). Recovery follows Core's actual settlement transition and adds no public failure category.

## Open questions

No unresolved behavioral, factual ownership, or execution-policy questions remain for this plan.

## References

- [Audit](audit.md) contains source locations, runtime paths, proposed-cycle diagnostics, coverage and baseline checks.
- [Ticket](ticket.md) defines FRQ-01 through FRQ-10, behavior-preservation gates and final acceptance.
- [Architecture](../../architecture.md) defines Core logical independence and consumer-owned contracts.
- [Project rules](../../../../../../AGENTS.md) define assertions, error preservation, testing and verification.
- [PHS-04 solution](../04-persistent-linear-sessions/solution.md) defines storage and publication failure boundaries.
- [Blocking contract operation processing solution](../../../../issues/blocking-contract-operation-processing/solution.md) defines operation ordering and the closed public error sets.
- [Agent-run failure issue](../../../../issues/agent-run-failure-semantics/problem.md) records later failure-classification scope.
- [Delivery plan](../../delivery-plan.md) defines the product phase order and PHS-07 pause.
