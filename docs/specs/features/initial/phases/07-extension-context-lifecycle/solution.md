# Technical Solution: PHS-07 Extension Context and Lifecycle

## Problem Statement

The [Problem Statement](problem.md) describes the partial PHS-07 implementation and missing public selection capabilities. The [PRD](prd.md) defines the approved requirements. This design completes that phase without rebuilding its implemented capabilities.

## Proposed Solution

### Design and implementation status

The requirements and technical design are approved. The implementation is complete, and the Linux verification commands in [Implementation evidence](#implementation-evidence) pass. The phase is Completed with user-accepted [technical debt](technical-debt.md) for the remaining standard TUI PTY verification.

### Implemented capability

[PHS-07.1](../07.1-architecture-audit-and-correction/solution.md) establishes the ownership and error-preservation baseline. PHS-07 retains that baseline and adds the selection paths below.

| Requirement group | Implemented owner and public path | Implementation status |
| --- | --- | --- |
| FRQ-01 through FRQ-03 | `extensioncontext.Service`, `ExtensionRequest`, SDK `ExtensionContext`, and `modelexecution.Service.Request` provide binding, catalogues, and configured-model requests. | Retained behavior. Selection commit uses the same session/runtime binding protection. |
| FRQ-04 | `lifecycle.Service`, `events.Dispatcher`, and `LifecycleInvocation` provide Agent Core, model-selection, and reasoning-selection observation. | Retained Agent Core observation plus implemented selection observation. |
| FRQ-05 through FRQ-07 | `modelselection.Service` owns shared admission, ordered handlers, final validation coordination, atomic catalogue commit, client publication, and observer ordering for UI, Programmatic Control, and extension initiators. Programmatic Control executes selection only through `Service.Prepare`, `Service.prepareSelection`, `commandPrepared.Run`, `commandPrepared.runSelection`, and `commandPrepared.Release`. | Implemented selection behavior. Direct client mutation bypasses and the duplicate private Programmatic execution path are removed. |
| FRQ-08 through FRQ-11, including FRQ-08.1 and FRQ-08.2 | `sessions.Service`, public append/recovery operations, persistence, client projections, and navigation implement extension entries and messages. | Retained behavior with public regression evidence. |
| FRQ-12 | The shared operation runtime and PHS-07.1 preserve complete error causes. Selection mappers expose the closed categories and ordered diagnostic sources defined below. | Retained transport behavior plus implemented selection categories. |

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

### Implementation evidence

The table maps each [ticket acceptance criterion](ticket.md#acceptance-criteria) to executable behavior. “Retained” identifies behavior implemented before the selection work. “Selection” identifies the new shared selection capability.

| Criterion | Scope | Executable evidence | Evidence limit |
| --- | --- | --- | --- |
| ACC-01 | Retained plus selection | `TestPublicContextCataloguesAcrossApplicationModes`, `TestPublicConfiguredRequestAcrossApplicationModes`, `TestPublicExtensionMessagesAcrossApplicationModes`, and `TestPublicExtensionSelectionAcrossApplicationModes` run headless, UI application assembly, and Programmatic Control against the external fixture. | The real standard TUI PTY path remains [accepted technical debt](technical-debt.md). |
| ACC-02 | Retained plus selection | `TestPublicRetainedContextNeverReactivates`, `TestBindingsNeverReactivate`, and `TestProtectSelectionCommitRejectsSupersededContext` cover concrete session/runtime replacement and A-to-B-to-A identity. `TestExtensionSelectionRejectsStaleBindingAfterCredentialValidation` and `TestExtensionSelectionRejectsStaleBindingBeforeCommit` cover final protection rejection, complete stale text, and no selection commit. | None on Linux. |
| ACC-03 | Retained | `TestPublicContextCataloguesAcrossApplicationModes` and `TestCataloguesRevalidateBlockedReads` exercise the public descriptor, provider ordering, active selection, and stale-read paths. | None on Linux. |
| ACC-04 | Retained | `TestPublicConfiguredRequestAcrossApplicationModes`, `TestConfiguredRequestPassesExactInput`, and the `modelexecution` service tests cover ordered input, terminal content, diagnostics, no tools, and selection independence. | None on Linux. |
| ACC-05 | Retained | `TestLifecycleObserverAcrossApplicationModes`, `TestServiceObservesEveryLifecycleGroup`, `TestServiceContinuesAfterOrdinaryObserverError`, and the observer cancellation integration tests cover client-first delivery, registration order, continuation, and run release. | UI application assembly passes; standard TUI PTY evidence is tracked with ACC-01 in [technical debt](technical-debt.md). |
| ACC-06 | Selection | `TestPublicSelectionHandlersComposeOriginalAndCurrentTargets` and the `modelselection` handler tests cover immutable original, successive current values, preserve, replace, reject, invalid action, ordinary error, cancellation, and runtime loss. `TestMapHandleResponseReturnsOrdinaryHandlerError` and `TestRuntimeNormalizesEmptyHandlerErrorsWithoutStoppingRuntime` prove that only exactly empty ordinary error text becomes `extension handler returned an empty error message` while nonempty and whitespace-only text remain exact and the runtime remains active. | None on Linux. |
| ACC-07 | Selection | `TestCatalogResolvesValidatesAndCommitsCompleteSelection`, `TestCatalogCommitCancellationPreservesSelection`, `TestCatalogFinalValidationFailurePreservesSelection`, `TestServiceObservesChangedSelectionAfterDelivery`, `TestServiceObservesAfterNonCancellationDeliveryFailure`, `TestSelectionOwnerCancellationTerminatesObserverAndReleasesAdmission`, `TestServiceDoesNotPublishUnchangedSelection`, both stale-binding tests, `TestModelCommandsUseCatalogDuringActiveRun`, and `TestSelectionReadinessAndActiveRunIndependence` cover validation, atomic commit, active-run independence, no-op, reasoning-before-model observation, cancellation-linked post-commit observers, committed completion after cancellation, admission release, post-commit issues, and no pre-commit event. | None on Linux. |
| ACC-08 | Retained | `TestAppendUsesCurrentActiveLeafForEverySupportedEntry`, `TestHistoryProjectsBothExtensionMessageVisibilities`, and `TestPublicExtensionMessagesAcrossApplicationModes` cover implicit-root attachment, persistence fields, model visibility, and client visibility. | None on Linux. |
| ACC-08.1 | Retained | `TestPublicExtensionRecoversHiddenStateAfterProcessRestart` exercises the external process, restart, exact identities and payloads, parent links, and branch order through the public contract. | None on Linux. |
| ACC-08.2 | Retained | `TestPublicRetainedContextNeverReactivates`, `TestSessionRecoveryRejectsReplacementDuringRead`, `TestExtensionStateFiltersOneActiveBranch`, and `TestExtensionStateIsCoherentDuringNavigation` cover stale recovery and active-branch filtering. | None on Linux. |
| ACC-09 | Retained | `TestPublicExtensionMessagesAcrossApplicationModes`, `TestSessionEntryAddedRetainsHiddenTreeStateWithoutTranscriptRendering`, `TestRestoredTranscriptOmitsOnlyHiddenExtensionMessages`, and `TestMapSessionTreeRetainsExtensionMessageState` cover exact text and visibility in both client contracts and the TUI state. | None on Linux. |
| ACC-10 | Retained | `TestMessageAppendReturnsCommittedEntryWithDeliveryIssue`, `TestMessageAppendWithoutPublisherReportsCommittedDeliveryFailure`, and `TestExtensionMessageAppendCommitsBeforePublication` cover committed results and `DELIVERY_FAILED` without rollback. | None on Linux. |
| ACC-11 | Retained | `TestProgrammaticNavigationPublishesSnapshotBeforeObserverAppend`, `TestUINavigationPublishesSnapshotBeforeObserverAppend`, and session navigation unit tests cover exact next input, parent or implicit-root destination, summary modes, and no agent run. | None on Linux. |
| ACC-11.1 | Retained | The two public navigation snapshot tests above and `TestNavigationDeliveryProofSurvivesLateResult` cover progress-before-observer ordering and terminal completion without a replacement snapshot. | None on Linux. |
| ACC-12 | Retained plus selection | `TestSelectionFailureMappingsUsePublicCodes`, `TestSelectionFailureCodesMatchHostCategories`, `TestFailureCodeForCommandEnforcesClosedSets`, `TestSelectionErrorsPreservePublicCodesAndCauses`, `TestSelectionFailuresRemainRequestLocal`, `TestMapExtensionEventPreservesCompleteExternalErrorText`, `TestFailedTerminalSendPreservesCompletionCauses`, `TestSDKPersistenceCleanupUsesCause`, and `TestRendererRuntimeFailurePreservesSource` cover closed selection categories, request-local extension failures, and complete or long errors through Extension, Programmatic, UI, and headless boundaries. | None on Linux. |
| ACC-13 | Retained plus selection | `TestNestedCatalogueReadsKeepBothReceiveLoopsLive`, `TestSelectionFailuresRemainRequestLocal`, both public nested-selection tests, `TestHostDuplicateIDsAndInactiveCancellation`, `TestExternalExitCancelsAndJoinsHostReads`, and the stream-close integration tests cover nested work, later same-stream operations after selection failure, and shared lifecycle termination. | None on Linux. |
| ACC-14 | Retained plus selection | Compile-time assertions in `modelselection`, `extensioncontext`, `lifecycle`, catalogue, runtime, and output implementations plus `task lint` (`ifaceguard`) and `task test` enforce consumer-owned interfaces and the import graph. | None on Linux. |

The evidence is implemented in the linked ownership areas: [Host application integration tests](../../../../../../host/internal/app), [selection usecase tests](../../../../../../host/internal/usecase/host/modelselection), [extension SDK tests](../../../../../../sdk/plugins/extension/v1), and [standard TUI tests](../../../../../../plugins/ui/tui/internal).

#### Acceptance-closure coverage

The final Linux closure adds only missing regression coverage:

- `TestSelectionAdmissionIsSharedAcrossInitiators` proves that a reservation owned by any of UI, Programmatic Control, or an extension returns `BUSY` to all three initiator paths.
- `TestExtensionSelectionRejectsStaleBindingAfterCredentialValidation` blocks credential validation, then makes final binding protection return `STALE_CONTEXT` and proves that neither catalogue commit nor client publication occurs. Concrete session/runtime replacement remains covered by the `extensioncontext` tests listed for ACC-02.
- `TestFailureCodeForCommandEnforcesClosedSets` enumerates all five client-selection failure categories and rejects the agent-run-only `MODEL_FAILED` category.

These tests passed against the implementation without a production-code change. They add acceptance evidence, not new behavior, so no RED failure is claimed.

The later focused corrections add this evidence without changing the acceptance model:

- `TestSelectionFailuresRemainRequestLocal` proves `EXTENSION_REJECTED` and `EXTENSION_UNAVAILABLE` remain request-local for both extension selection operations, preserve complete text, and leave the stream available.
- `TestSelectionOwnerCancellationTerminatesObserverAndReleasesAdmission` proves targeted cancellation remains linked to post-commit observers while the terminal result remains Completed with the committed selection and diagnostics; cleanup releases selection admission.
- `TestMapHandleResponseReturnsOrdinaryHandlerError` and `TestRuntimeNormalizesEmptyHandlerErrorsWithoutStoppingRuntime` prove the exact-empty-only ordinary Host diagnostic and continued runtime availability.
- Programmatic selection tests use the `Service.Prepare`, `commandPrepared.Run`, and `commandPrepared.Release` lifecycle; `TestSelectionErrorsPreservePublicCodesAndCauses` covers preparation and execution failure projection.

#### TDD record

Two narrow historical exceptions are recorded:

1. The initial private selection-handler composition tests were added after the first composition implementation. A later public external-process composition scenario produced a valid behavioral RED before its fixture implementation, but that later RED does not rewrite the initial sequence.
2. Startup routing for model-selection and reasoning-selection observer kinds was implemented before its startup test produced a compiling RED. Removing the routing later proved test sensitivity, but that run was not pre-implementation RED.

All other recorded behavior corrections used compiling behavioral RED runs before production changes. The request-local failure, cancellation-linked observer, and exact-empty diagnostic corrections each had compiling behavioral RED evidence. Removing the duplicate Programmatic execution path was behavior-preserving cleanup and used the retained public prepared-operation tests without an artificial absence test. No additional historical exception was used.

#### Verification results

The final Linux closure produced these results:

| Command | Result |
| --- | --- |
| `task generate` twice | Passed; the second run produced no diff. |
| `task fmt` | Passed. |
| `task fix_dry_run` | Passed with no proposed fix. |
| `task lint` | Passed with zero issues, no `ifaceguard` errors, no vulnerabilities, and zero external-fixture issues. |
| `task test` | Passed. |
| `task itest` | Passed on Linux. `TestLifecycleObserverThroughStandardTUI` called `t.Skip` because the runtime was not Darwin arm64; this skip is not passing PTY evidence. |
| `task test-coverage` | Passed at 84.3%; the required minimum is 80.0%. |
| `task build` | Passed. |
| `git diff --check` | Passed. |

These command results do not replace the two approved historical TDD exceptions in the preceding section. No independent deep review is claimed by this implementation evidence. The [roadmap](../../../../../roadmap.md) marks the phase Completed with the remaining PTY verification tracked separately as [accepted technical debt](technical-debt.md). The skipped scenario is not reported as passing.

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
- `host/internal/usecase/host/modelselection`, `host/internal/usecase/host/providers/catalog.go`, `host/internal/usecase/host/ui/prepared_operations.go`, and `host/internal/usecase/host/programmatic/prepared.go` contain the implemented shared selection path.
- `host/internal/usecase/host/extensioncontext/service.go`, `host/internal/usecase/host/sessions/service.go`, and `host/internal/usecase/host/extensionruntime/context.go` contain binding and commit-protection mechanisms.
- `plugins/ui/tui/internal/controller/plugin/request_mapping.go` contains client completion and connection-event mapping.
