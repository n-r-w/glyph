# Technical Solution: PHS-07 Extension Context and Lifecycle

## Problem Statement

The [Problem Statement](problem.md) describes the partial PHS-07 implementation and missing public selection capabilities. The [PRD](prd.md) defines the approved requirements. This design completes that phase without rebuilding its implemented capabilities.

## Proposed Solution

### Design status

The requirements and technical design are approved. The implementation dry run is complete with no unresolved design blockers. The remaining selection slice is not implemented, and implementation verification is pending.

### Implemented baseline

[PHS-07.1](../07.1-architecture-audit-and-correction/solution.md) establishes the ownership and error-preservation baseline. Source presence and existing test scenarios do not establish completion of the PHS-07 acceptance criteria.

| Requirement group | Implemented owner and public path | Remaining design scope |
| --- | --- | --- |
| FRQ-01 through FRQ-03 | `extensioncontext.Service`, `ExtensionRequest`, SDK `ExtensionContext`, and `modelexecution.Service.Request` provide binding, catalogues, and configured-model requests. | Apply binding protection to selection commits. Keep configured requests independent of active selection. |
| FRQ-04 | `lifecycle.Service`, `events.Dispatcher`, and `LifecycleInvocation` provide Agent Core observation. | Add model-selection and reasoning-selection observation. |
| FRQ-05 through FRQ-07 | `providers.Catalog` stores selection. UI and Programmatic usecases call its selection methods directly. | Add shared admission, ordered selection handlers, final validation, and client-neutral publication. |
| FRQ-08 through FRQ-11, including FRQ-08.1 and FRQ-08.2 | `sessions.Service`, public append/recovery operations, persistence, client projections, and navigation implement extension entries and messages. | Retain these paths and verify them with the completed selection capability. |
| FRQ-12 | The shared operation runtime and PHS-07.1 preserve complete error causes. | Extend selection category mappings and result diagnostics across all callers. |

The code paths that determine the remaining work are:

- UI: `ui.Session.prepareSelection` performs in-memory validation and reserves a UI-local selection flag. Its execution calls `Catalog.SelectModel` or `Catalog.SelectReasoningChoice` and returns a selection frame.
- Programmatic: `programmatic.Service.Prepare` validates selection. `selectModel` and `selectReasoningChoice` later call the catalogue. This path has no reservation shared with UI or extension operations.
- Extension: `controller/extension.Service.Prepare` validates a context reference and accounts for the runtime operation. `ExtensionRequest` has no selection operation yet.
- Provider catalogue: `Catalog.SelectModel` checks credentials before locking, then computes reasoning fallback and changes selection. `SelectReasoningChoice` changes reasoning under the catalogue lock. Neither invokes extension selection handlers.
- TUI: `controller/plugin.DecodeCompleted` currently turns selection completion into a presentation-state update. This path must be reconciled with unsolicited extension-originated changes.

### Ownership and dependency direction

Add only `host/internal/usecase/host/modelselection` as a new capability package. It owns selection admission, handler registrations and composition, final-selection coordination, and commit-event ordering. It does not own catalogue state, extension processes, transport, or client presentation.

Retain these owners:

| Owner | Responsibility in this phase |
| --- | --- |
| `providers.Catalog` | Model descriptors, reasoning fallback, credential checks, and the authoritative active selection. |
| `extensioncontext.Service` | Issued context identity and validation of runtime/session bindings. |
| `sessions.Service` | Active-session incarnation and protection against session replacement during a bound commit. |
| `extensionruntime.Service` | Runtime availability, operation accounting, runtime commit protection, and process-facing handler calls. |
| `lifecycle.Service` | Observer registration order, invocation, and ordinary-observer-error handling. |
| Host UI and Programmatic usecases | Their client preparation/readiness rules, command results, and public error projections. |
| Extension controller | Protobuf validation, bound runtime identity, operation preparation, and result/error mapping. |
| Mode output adapters | Ordered selection and issue delivery through the connection's existing writer. |
| TUI presentation usecase | Applying Host-confirmed selection and displaying operation outcomes. |

Each consumer declares its own selection port and command/result types. `modelselection` implements the ports of Host UI, Host Programmatic, and the Extension controller. Their boundary methods project into one private selection operation; they do not duplicate composition policy.

The Extension controller calls the selection port directly. Do not route selection through an `extensioncontext` forwarding method: selection needs binding protection, so that arrangement would create opposite dependencies between the two capability packages.

`modelselection` declares the catalogue, runtime-handler, binding-protection, observer, and output ports it consumes. Their real owners implement them with implementation-package assertions. In particular, `extensioncontext`, `lifecycle`, `providers`, and `extensionruntime` may import the `modelselection` consumer contracts; `modelselection` imports none of those implementations. Binding types in this port belong to `modelselection`, not to the implementing context package.

The proposed import edges were checked against the transitive package graph from `go list`. The modeled graph is acyclic. This check does not replace compilation and interface-signature checks during implementation. No shared contract-only package, forwarding service, alias, or assertion exception is added.

### Public operations and handlers

Reuse `ExtensionService.Open` and its two initiator namespaces. Add these typed payloads to the owning protobuf sources and SDK:

- Extension-initiated model selection carries `ExtensionContextRef`, provider ID, and model ID. Reasoning selection carries the reference and one reasoning choice.
- `HostCompleted` gains selection completion containing the committed full selection and ordered issues. SDK context start methods return concrete operation handles; `Wait` and cancellation retain the implemented separation between operation cancellation and local wait cancellation.
- `HandlerKind` gains separate model-selection request and reasoning-selection request kinds, plus model-selection and reasoning-selection observer kinds.
- Selection handler invocation carries the issued extension context, immutable original target, and current target. Each target is a complete provider/model/reasoning selection.
- Selection action has exactly one variant: preserve, replace with a complete target, or reject with nonempty diagnostic text. An ordinary handler error remains a handler result, not a process failure.
- `LifecycleInvocation` gains typed model-selection and reasoning-selection events. Each carries the preceding and committed full selections from the same commit. These are Host facts, not Agent Core events; they carry no fabricated run ID.

Extend `model.proto` for selection operation and handler payloads, `lifecycle.proto` for observer payloads, and the existing handler envelope sources. Startup validates all handler IDs and kinds, partitions declarations by capability, and commits registrations only for accepted extensions. Preserve declared handler order within the accepted extension order.

Use edition 2023, enum zero sentinels, and `int64` for numeric positions or counters. No compatibility fields, aliases, reserved declarations, additional stream, or callback listener are added.

### Active model selection

#### Preparation and admission

- Keep preparation limited to request shape, in-memory readiness/catalogue checks, context validation, and reservation. Credential access and extension calls run only after acceptance.
- Preserve the client contracts' initial `NOT_FOUND` and `REASONING_UNSUPPORTED` rejections. This validates the requested starting selection, not a handler's later replacement. The extension selection operations use the same starting-selection rules.
- Move selection reservation from the UI-local flag to `modelselection`. Acquire it before reading active selection and normalizing the target; release it on preparation rejection. All three initiators share the reservation through handler execution, commit, publication, and observer completion, then release it through prepared-operation cleanup.
- A second selection request receives `BUSY`; it is not queued. This also applies to a request started inside a selection handler or observer, so a handler cannot wait on its own selection operation. Other extension operations remain available.
- Keep the UI's authentication-readiness check at its consuming usecase. Do not use the agent-run/session-mutation gate for selection. An active model request keeps its selection snapshot, and committed selection affects later requests.

#### Original and current target

A model request resolves its requested provider/model against the catalogue and computes the original reasoning choice using `providers.fallbackReasoningChoice`. Preserve its supported-choice, target-default, effort-to-`on`, nearest-effort, and lower-effort tie rules. A reasoning request combines its explicit choice with the active provider/model snapshot. Initialize current target from original target.

This initial resolution performs no credential I/O and changes no active selection. A handler can redirect a request away from a model with unavailable credentials; credentials are checked only for the final target.

#### Handler composition

1. Take the matching registered handlers in order, excluding runtimes already unavailable when the chain is selected.
2. Issue an invocation context and call each handler through runtime management. Every handler receives the same original target and the current target left by its predecessor.
3. Preserve leaves current target unchanged. Replace changes all three target fields. Reject stops the chain and fails the operation without a selection commit.
4. Validate action shape and required fields before changing current target. An invalid action or ordinary handler error records and reports an issue, preserves the received current target, and continues the chain without disabling the runtime.
5. A runtime that fails after inclusion in the chain causes `EXTENSION_UNAVAILABLE` before commit. Runtime management owns its unavailable transition and failure reporting.
6. After the chain, validate model existence, reasoning support, and credentials for the final target. A structurally complete replacement may be corrected by a later handler; catalogue validation of the final target occurs after composition.

Cancellation before commit leaves active selection unchanged. Calls to append entries or make configured-model requests from a handler remain separate operations. A selection failure does not roll back an entry already committed by such a call.

### Atomic commit and stale-context protection

Split catalogue selection mutation into non-mutating target resolution/validation and one full-selection commit. Replace the direct selection-mutating methods used by UI and Programmatic; retain no bypass around the new capability owner.

Credential I/O completes before commit protection is acquired. Under the catalogue mutex, recheck the final descriptor/choice and cancellation, update provider/model/reasoning and the active entry index together, and return the preceding and committed selections. The shared reservation prevents another selection writer from changing the baseline during composition. Catalogue reads and model requests remain available while a handler or credential check waits.

For an extension-initiated selection, protect the issued runtime and session incarnation through the commit:

- `extensioncontext` resolves the issued reference and coordinates protection through its consumed session/runtime ports.
- `sessions` validates and protects the expected incarnation at the session owner. Runtime protection reuses `extensionruntime.BeginContextCommit`.
- Acquire session protection before runtime protection, as in extension-message append. The protection interval covers final binding validation, catalogue mutation, and enqueue of the committed client update.
- Release session/runtime protection before waiting for transport acknowledgement or invoking observers. Never hold these guards across credential access, model execution, or extension calls.
- A preceding session A binding remains stale after A-to-B-to-A replacement. Reusing the durable session ID does not authorize a commit.

Do not hold the context-service mutex for a complete selection operation. Do not add a second selection store, cross-process lock, or generic transaction coordinator.

### Client publication and lifecycle observation

Every changed selection publishes one client-neutral full-selection connection event, regardless of whether UI, Programmatic Control, or an extension initiated the operation. Both client protobuf contracts gain this event. Mode outputs use their existing ordered writer; headless composition acknowledges the state publication without inventing unsolicited CLI selection text.

The operation sequence is:

1. Atomically commit the full selection.
2. Enqueue the committed full-selection client event before releasing bound commit protection.
3. Release commit protection, then attempt client-delivery acknowledgement.
4. Invoke reasoning-selection observers when reasoning changed, then model-selection observers when provider or model changed. Both groups receive the same detached preceding/committed values. Attempt observers even after a client-delivery failure.
5. Return the committed selection and ordered issues to the initiator.

A successful no-op returns its selection but emits neither a client selection-change event nor a selection lifecycle event.

Selection completion is an operation result, not a second authoritative state update. TUI applies initialization and selection connection events to active selection; terminal selection completion finishes the pending command and retains diagnostics without reapplying its selection. Programmatic clients receive the same distinction. This prevents a delayed completion for selection A from overwriting a later connection event for selection B. No revision counter or event queue is required.

Ordinary handler and observer errors use the existing `ExtensionIssue` connection event and remain available as ordered operation-result issues. Failure to publish an issue before commit fails selection with `INTERNAL` and retains both causes; this is a delivery failure, not an ordinary handler error. Standard TUI presents the connection issue once; completion diagnostics do not print that issue again. Completion reports delivery failures that prevented issue publication, retaining both the source and delivery text. A missing or failed connection cannot roll back a committed selection.

After commit, cancellation or delivery/observer failure returns a completed selection with diagnostics to a reachable initiator, following the implemented committed-append convention. It must not claim that no selection occurred. After connection loss, retain undelivered source errors through operation/runtime completion under the PHS-07.1 error rules.

Keep the Agent Core event dispatcher and its client-first observation path unchanged. Add selection observation at the Host capability boundary, not inside Agent Core.

### Error contract

Use the shared [Error Semantics](../../prd.md#error-semantics). Codes supplement complete causes; transport capacity does not authorize diagnostic truncation.

| Selection outcome | Public representation |
| --- | --- |
| Invalid request shape or missing required fields | Rejected `INVALID_ARGUMENT`. |
| Requested starting model missing or requested starting reasoning unsupported | Rejected `NOT_FOUND` or `REASONING_UNSUPPORTED`. |
| Another selection is reserved | Rejected `BUSY`. No selection queue. |
| Invalid extension binding at admission | Rejected `STALE_CONTEXT`. |
| Final transformed target is missing or has unsupported reasoning | Failed `MODEL_UNAVAILABLE`, with the exact final validation cause. |
| Final target credentials are unavailable | Failed `CREDENTIAL_UNAVAILABLE`. |
| Handler explicitly rejects | Failed `EXTENSION_REJECTED`, with its complete diagnostic text. |
| Selected handler runtime becomes unavailable before commit | Failed `EXTENSION_UNAVAILABLE`. |
| Extension binding is invalidated before commit | Failed `STALE_CONTEXT`. |
| Pure cancellation before commit | Canceled under the shared operation lifecycle. |
| Failure to deliver a handler issue before commit | Failed `INTERNAL`, retaining the handler and delivery causes; no selection commit. |
| Ordinary handler error or invalid action | Reported `HANDLER_ERROR` or `INVALID_HANDLER_ACTION`; preserve current target and continue. |
| Observer error after commit | Completed selection with `OBSERVER_ERROR`; continue later available observers. |
| Delivery error after commit | Completed selection with `DELIVERY_FAILED`; retain the committed selection. |
| Other failure before commit | Failed `INTERNAL` with complete causes. |

Selection rejection sets retain the shared operation-ID and readiness categories. Extension selection adds starting-selection validation to the context-operation rejection set. Client selection adds common `BUSY` admission. UI retains `NOT_READY` during authentication work. Cancellation requests retain `CANCEL-R`, including `TARGET_NOT_ACTIVE` for an already terminal target.

The closed failed set for client selection is `MODEL_UNAVAILABLE`, `CREDENTIAL_UNAVAILABLE`, `EXTENSION_REJECTED`, `EXTENSION_UNAVAILABLE`, and `INTERNAL`. Extension-initiated selection additionally admits `STALE_CONTEXT`. A completed result can contain `HANDLER_ERROR`, `INVALID_HANDLER_ACTION`, `OBSERVER_ERROR`, and `DELIVERY_FAILED` issues.

Update both client usecase mappers, the Programmatic controller's command-specific failure allowlist, and SDK validation together. Amend the owning selection rows and `MODEL-F` definition in the [shared operation inventory](../../../../issues/blocking-contract-operation-processing/solution.md#operation-inventory); do not change accepted agent-run failure categories.

Use the existing mixed-error rule: only pure cancellation becomes a canceled terminal state. Independent acquired failures retain their source text and the owning failure category. Successfully reported nonfatal handler issues remain diagnostics, not invented terminal failures. Selection completions retain diagnostic sources through `CompletedWithSource`, as navigation completions already do.

### Preserved context, persistence, and navigation paths

No new persistence or recovery design is needed. Preserve these implemented boundaries:

- `ExtensionContext` start methods return without waiting for Host acceptance. Nested handler operations run outside the stream receive loops. Local `Wait` cancellation does not cancel the remote operation.
- `modelexecution.Service.Request` owns configured requests, uses an explicit selection, supplies no tools, and reduces the terminal response. `providers.Catalog` supplies model bindings and credential checks, not a second execution path. PHS-06 adds retries inside model execution; PHS-12 replaces provider attempts.
- `sessions.Service` persists hidden entries and visible-model messages, protects runtime/session binding, and publishes committed messages. Delivery failure retains the entry and reports `DELIVERY_FAILED`.
- Recovery returns a single active-branch snapshot filtered to the calling extension. It preserves exact stored bytes, text, identity, parent links, and order without parsing extension state or replaying it automatically.
- Navigation to an extension message uses its parent, including the implicit root, and returns exact next input. Committed progress precedes observer appends; terminal metadata does not replace a later transcript state.
- The TUI presentation usecase remains the sole owner of private client state. SDK and terminal infrastructure retain their PHS-07.1 responsibilities.

### Composition and affected boundaries

Application assembly constructs one `modelselection` instance per Host. It binds the real catalogue, context protection, runtime, lifecycle, and mode output through their consumer-owned interfaces. Bind the Extension controller's selection dependency before extension operations become available. Extend startup registration validation before accepting selection handlers; bind model dependencies before runtime activation. Assembly contains no selection policy.

The implementation changes the Extension protobuf/SDK, extension controller/runtime, startup registration, provider selection methods, both client usecases and output mappings, client protobuf/SDK validation, TUI notification/presentation handling, and the external extension fixture. Agent Core, provider drivers, configured-model execution, and persistence formats gain no new selection responsibility.

### Verification

Reuse the existing behavioral tests before adding new fixtures. New behavior follows RED, GREEN, and REFACTOR. Tests below assert logic and public outcomes, not document text, prompts, or logs.

| Test group and purpose | Inputs and expected outcomes | Boundary cases and dependencies |
| --- | --- | --- |
| Catalogue and selection composition | Reuse `providers/catalog_test.go`, `ui/session_selection_test.go`, and `programmatic/selection_test.go`. Two handlers see one original target and successive current values. Only the final target commits. | Preserve reasoning fallback and catalogue reads during blocked credentials; cover preserve, replace, reject, invalid action, ordinary error, final invalid target, and unchanged selection on failure. Unit tests use generated mocks of production consumer ports. |
| Shared admission and binding | Overlap requests from different initiators; the second receives `BUSY`. Replace runtime/session during a blocked handler or credential check; a stale extension operation commits nothing. | Include nested selection from a handler and observer, A-to-B-to-A, cancellation before commit, and release after rejection/connection close. Extend context binding and commit-protection tests. |
| Public Extension Contract | Extend the separate-module fixture to select a model and reasoning choice, register two composing handlers, and observe committed selections. SDK start returns before Host acceptance. | Exercise both operation namespaces, nested catalogue/model/append operations, runtime failure, exact error text, and cancellation. Use the real stream/process integration paths already used by `context_integration_test.go`. |
| Client state and events | UI and Programmatic receive one committed full selection before observer effects. Both changed fields produce reasoning observation before model observation. No-op produces no selection event. | Delay one completion until after a later commit event; client state retains the later selection. Cover extension initiation, headless operation, active agent requests, and ordinary observer errors. Extend TUI `selection_lifecycle_test.go` and real public-contract tests. |
| Commit and error delivery | Pre-commit failure preserves state. Post-commit cancellation, observer error, or writer failure retains committed selection and complete diagnostics. | Verify error-category allowlists, long error text, joined source/delivery errors, terminal source retention, and no duplicate TUI issue presentation. Use controlled writers in unit tests and existing real-stream integration fixtures. |
| Full PHS-07 regression | Retain catalogue, lifecycle, append/recovery, and navigation scenarios across headless, UI, and Programmatic compositions. | Reuse `context_catalogue_modes_integration_test.go`, `lifecycle_observer_modes_integration_test.go`, `session_state_recovery_public_integration_test.go`, and `extension_message_navigation_public_integration_test.go`. Include exact payloads, active branches, hidden transcript filtering, implicit-root navigation, and observer appends. |

Run `task generate` twice and require no second-run diff. Run `task fmt`, review `task fix_dry_run`, apply accepted fixes, then run `task lint`, `task test`, `task itest`, and `task test-coverage`. Report platform-gated integration scenarios separately; a skipped test is not passing evidence for that scenario.

Keep the [roadmap](../../../../../roadmap.md) phase status at Planned until implementation and the [ticket acceptance criteria](ticket.md#acceptance-criteria) pass. The implementation handoff must distinguish preserved baseline behavior from the new selection slice, not remove either from phase acceptance.

## Overengineering and Overspecification Considerations

- One capability owner is necessary because all three initiators must share handler composition and admission. `extensioncontext` and `lifecycle` already exist and are extended rather than recreated.
- A selection reservation returns `BUSY` instead of adding a queue, scheduling policy, or recursive-selection protocol. It does not block model execution or unrelated context operations.
- Session/runtime commit protection addresses replacement during a real asynchronous handler or credential request. It reuses state owners rather than adding a distributed lock or generic transaction subsystem.
- One client state-publication path avoids late-completion overwrite without revision counters or duplicate authoritative state.
- The design adds no callback service, generic capability bus, compatibility layer, event-replay store, new persistence schema, speculative provider API, or later-phase middleware, retry, command, and UI-extension capabilities.
- Private helper names, file splits, and mock layouts remain implementation choices. Public outcomes, dependency direction, and commit/publication order are the design commitments.

## Open Questions

None.

## References

- [Problem Statement](problem.md) and [PRD](prd.md) define the approved problem and requirements.
- [Ticket](ticket.md) defines phase acceptance and required verification.
- [Domain Glossary](../../../../../terms.md) defines shared terms.
- [Target architecture](../../architecture.md) defines ownership and import rules.
- [PHS-07.1 solution](../07.1-architecture-audit-and-correction/solution.md) defines the implemented architecture and complete-error baseline.
- [Shared operation contract](../../../../issues/blocking-contract-operation-processing/solution.md) defines asynchronous lifecycle and category inventories.
- `api/plugins/extension/v1`, `api/plugins/ui/v1`, and `api/programmatic/v1` contain the public source contracts.
- `host/internal/usecase/host/providers/catalog.go`, `host/internal/usecase/host/ui/prepared_operations.go`, and `host/internal/usecase/host/programmatic/prepared.go` contain the selection baseline.
- `host/internal/usecase/host/extensioncontext/service.go`, `host/internal/usecase/host/sessions/service.go`, and `host/internal/usecase/host/extensionruntime/context.go` contain binding and commit-protection mechanisms.
- `plugins/ui/tui/internal/controller/plugin/request_mapping.go` contains client completion and connection-event mapping.
