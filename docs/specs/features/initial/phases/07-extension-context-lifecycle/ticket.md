# Ticket: PHS-07 Extension Context and Lifecycle

Implement one vertical slice that gives isolated extension processes UI-neutral access to session-bound Host capabilities through protobuf contracts.

## Key definitions and abbreviations

The [phase terminology](terms.md) identifies the terms used by this ticket. The [Domain Glossary](../../../../../terms.md) defines their meanings.

## Problem Statement

The [Problem Statement](problem.md) defines the missing public extension access to session-bound Host capabilities and its effect on extension authors and Glyph clients.

## Target Picture

An extension can observe agent and selection lifecycles, use configured models, change active model selection, and persist branch-aware state through one session-bound context. The behavior is independent of standard TUI, Programmatic Control, and future UI plugins.

## Scenarios

### SCN-01: Session-bound extension behavior

- Actor: Extension author.
- Pre-condition: One extension runtime and active session are available.
- Trigger: The extension receives `agent_start`.
- Required behavior: The handler receives an extension context, reads model and provider catalogues, makes a configured-model request, and appends the result to the active session without changing active model selection.
- Example input and expected output: The extension sends ordered text messages to configured model `m1`, receives final text and visible reasoning content, and appends a model-visible extension message. The next agent model request includes that message, and the active provider, model, and reasoning choice remain unchanged.

### SCN-02: Composed active selection

- Actor: Glyph client or extension.
- Pre-condition: Two selection handlers are registered.
- Trigger: The actor requests another model or reasoning choice.
- Required behavior: Both handlers receive one original target selection. The second handler receives the current target selection returned by the first. Host validates and atomically commits the final target.
- Example input and expected output: Handler A replaces reasoning with `low`, handler B preserves that target, and the next agent model request uses the committed provider, model, and `low` reasoning choice.

### SCN-03: UI-neutral extension message

- Actor: Glyph client.
- Pre-condition: An extension has appended visible and hidden client-visibility messages.
- Trigger: Host publishes `SessionEntryAdded`, or the client reads session state.
- Required behavior: The client receives exact message text and client visibility through its protobuf contract. The ordinary transcript excludes the hidden message, while the complete session tree retains both messages.
- Example input and expected output: Selecting either extension message returns its exact text as next input and uses its parent as the navigation destination without starting an agent run.

## Scope

In scope:

- Extension context and stale-context rejection.
- Model and provider catalogue inspection.
- Configured-model requests with ordered text history.
- Agent, turn, message, tool-execution, model-selection, and reasoning-selection lifecycle events.
- Composed active-selection handlers and atomic selection commit.
- Model-hidden extension entries and model-visible extension messages.
- Client visibility, client connection events, persistence, restart, and session-tree navigation.
- Extension SDK, external process fixture, standard TUI, and Programmatic Control behavior required by the acceptance criteria.

Out of scope:

- Prompt, context, input, provider, and tool middleware.
- Context compaction and retry control.
- Extension commands, interactions, notifications, and provider implementations.
- Extension-defined UI rendering.
- Tools, images, and provider-specific options in configured-model requests.

## Dependencies and Preconditions

- DEP-01: [PHS-04.1](../04.1-model-execution-capabilities/ticket.md) is complete and supplies provider-neutral model descriptors.
- DEP-02: [PHS-05](../05-session-tree/ticket.md) is complete and supplies session-tree persistence, navigation, and branch summarization.
- DEP-03: [PHS-05.1](../05.1-extension-boundary-cleanup/ticket.md) is complete and supplies separate extension runtime, tool, and session-tree capability owners.
- DEP-04: [Blocking contract operation processing](../../../../issues/blocking-contract-operation-processing/solution.md) is complete and supplies the asynchronous operation lifecycle and public error transport.

## Requirements

### Goals

- Implement every behavior in [PRD](prd.md) FRQ-01 through FRQ-12 as one extension-to-Host-to-client vertical slice.

### Functional Requirements

- The [PRD](prd.md) FRQ-01 through FRQ-12 are the normative functional requirements for this ticket.

### Non-Functional Requirements

- NFQ-01: Focused behavior shall follow RED, GREEN, and REFACTOR. Implementation completion requires passing every verification command listed in this ticket.
- NFQ-02: Agent Core shall remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, concrete provider packages, extension packages, UI packages, and TUI packages.
- NFQ-03: Each Go interface shall belong to its consuming package. Every production implementation shall include a compile-time interface assertion after the import graph is checked for cycles.
- NFQ-04: Extension-originated external error text shall be limited to 65,536 UTF-8 bytes at Extension Contract ingress. Only secrets shall be redacted, and later layers shall preserve the bounded text and every added cause.

## Deliverables

- DLV-01: Public Extension Contract and SDK for extension context, extension-initiated Host operations, selection handlers, and lifecycle observers.
- DLV-02: Host extension-context, lifecycle, and model-selection capability use cases with the ownership boundaries defined by the [Technical Solution](solution.md).
- DLV-03: Persisted model-hidden extension entries and model-visible extension messages with client visibility and session-tree navigation.
- DLV-04: UI Plugin Contract, Programmatic Control, standard TUI, and headless delivery for committed extension messages, selection changes, lifecycle issues, and complete errors.
- DLV-05: An external reference extension that exercises configured-model requests, both extension entry types, client visibility, lifecycle observation, selection composition, and stale-context rejection.

## Acceptance Criteria

- ACC-01: The reference extension runs through headless composition, standard TUI, and Programmatic Control without changing extension behavior or importing a client package.
- ACC-02: Active-session or extension-runtime replacement makes every operation through the preceding extension context fail with `STALE_CONTEXT` and complete error text. No stale operation commits session or selection state.
- ACC-03: Model catalogue results contain every provider-neutral descriptor field defined by PHS-04.1 and active model selection. Provider catalogue results contain provider IDs and ordered model IDs. Neither result contains credential values, credential sources, endpoints, provider API configuration, or provider reasoning context.
- ACC-04: A configured-model request accepts instructions and ordered `user` and `assistant` text messages, returns ordered terminal text, refusal, tool-call, and visible reasoning content with outcome, usage, and diagnostics, supplies no tools to the provider, and leaves active model selection unchanged.
- ACC-05: Glyph client delivery precedes extension observer delivery for each Agent Core lifecycle event. Observers run in registration order before Agent Core processes the next event. An ordinary observer error reaches the Glyph client as `ExtensionIssue`, does not stop later observers, and does not deactivate the extension.
- ACC-06: Two selection handlers receive the same original target selection, while the second receives the current target selection returned by the first. Preserve, replace, reject, invalid-action, ordinary-error, cancellation, and unavailable-runtime paths produce the outcomes defined by the PRD and Technical Solution.
- ACC-07: Host validates provider-model existence, reasoning support, and credentials before one atomic selection commit. Failure preserves active model selection and emits no selection event. A successful commit emits events only for changed values, with reasoning selection before model selection when both change.
- ACC-08: Both extension entry types attach to the active leaf, survive restart, and preserve exact extension ID, entry type, parent, timestamp, and payload. Only model-visible extension messages enter model context.
- ACC-09: UI Plugin Contract and Programmatic Control receive exact model-visible extension message text and `visible` or `hidden` client visibility through session state and `SessionEntryAdded`. Standard TUI excludes hidden messages from its ordinary transcript but retains them in session-tree state.
- ACC-10: A client delivery failure after extension-message persistence returns the committed entry and `DELIVERY_FAILED` issue to the extension. The committed session state is not rolled back.
- ACC-11: Selecting a model-visible extension message with either client visibility uses its parent as the navigation destination and returns exact text as next input without starting Agent Core. Without a branch summary, the destination becomes the active leaf. With a branch summary, the new `BranchSummaryEntry` becomes the active leaf.
- ACC-12: Extension Contract, UI Plugin Contract, Programmatic Control, and headless output preserve the operation's closed Glyph error category and complete error text. A machine-readable code never replaces error text.
- ACC-13: One handler can start and await an extension-initiated Host operation over `ExtensionService.Open` without deadlock. Cancellation, duplicate operation IDs in one initiator namespace, stream close, and runtime exit terminate every owned operation through the shared operation lifecycle.
- ACC-14: `extensioncontext`, `lifecycle`, and `modelselection` depend only on consumer-owned interfaces. Agent Core and these use cases import no concrete provider, plugin transport, client, or TUI package prohibited by NFQ-02 and NFQ-03.

## Verification

- RED: Add one focused behavioral test before each behavior change and observe its expected failure.
- GREEN: Implement the minimum production change that makes the focused test pass.
- REFACTOR: Improve the implementation without changing behavior and keep the focused tests passing.
- `task generate` runs twice, and the second run produces no diff.
- `task fmt` passes.
- `task fix_dry_run` is reviewed, and accepted fixes are applied through `task fix` or manual changes.
- `task lint` passes.
- `task test` passes.
- `task itest` passes.
- `task test-coverage` passes.

## Overengineering and Overspecification Considerations

The ticket reuses one bidirectional extension stream, the shared operation lifecycle, the implemented provider catalogue, session persistence, runtime manager, startup coordinator, and event dispatcher. It adds no callback service, second socket, generic capability bus, asynchronous lifecycle queue, compatibility layer, or later-phase middleware.

## Constraints and Risks

- A slow lifecycle observer delays later Agent Core events. The connected Glyph client receives the current event before observer execution, and general cancellation cancels observer work.
- Nested operations can deadlock when a stream receive loop executes handlers directly. Both SDK peers must route operation execution away from their receive loops.
- Session or selection commit can succeed before client delivery fails. The committed result and `DELIVERY_FAILED` issue expose both outcomes without rollback.
- Three public protobuf contracts can drift in content, visibility, selection, or error mapping. Contract tests must compare their shared semantic values.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

The [Technical Solution](solution.md) defines protobuf source ownership, Host package ownership, operation and event order, persistence records, closed errors, and verification design.

## References

- [Problem Statement](problem.md) - observed problem and boundary.
- [Phase terminology](terms.md) - terminology index.
- [PRD](prd.md) - normative behavior and goals.
- [Technical Solution](solution.md) - implementation design.
- [Domain Glossary](../../../../../terms.md) - shared Glyph terminology.
- [Target architecture](../../architecture.md) - process, state, and dependency ownership.
- [Delivery plan](../../delivery-plan.md) - phase order and dependencies.
