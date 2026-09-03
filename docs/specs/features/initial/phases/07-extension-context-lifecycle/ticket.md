# Ticket: PHS-07 - Extension context and lifecycle

Give extension processes session-bound access, configured-model requests, active-selection control, and lifecycle events without terminal dependencies.

## Key definitions and abbreviations

- DEF-01: Extension context. Host access bound to one extension runtime generation and one active session.
- DEF-02: Original target selection. The immutable provider, model, and reasoning selection requested before selection handlers run.
- DEF-03: Current target selection. The selection returned by preceding selection handlers.
- DEF-04: Model-visible extension message. An extension-created session message associated with one session-tree branch and included in model context.

## Problem Statement

- PRB-01: Extension processes receive tool calls but no session-bound context, configured-model request capability, or Agent Core lifecycle events. Stateful headless extensions cannot observe runs, use a configured model for extension-owned behavior, or persist branch-aware entries.
- PRB-02: Selection lifecycle events alone would let extensions observe model and reasoning changes but would provide no operation for requesting or transforming the active conversation selection.

## Target Picture

- SOL-01: Give extension processes session-bound access, configured-model requests, active-selection operations, and lifecycle events without terminal dependencies.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension author.
- Pre-condition: DEP-01, DEP-02, and DEP-03 are met.
- Trigger: the extension handles a lifecycle event in an active session.
- Required behavior: the handler receives a session-bound context, can make a configured-model request without changing the active conversation model, and can persist an entry on the active branch.
- Example input and expected output: Input: deliver `agent_start` to an extension in session `s1`, let it query one configured model, and append the result as a model-hidden entry. Expected output: the entry is stored on the active branch of `s1`, the active conversation model remains unchanged, and a context from a replaced session is rejected.

### SCN-02: Extension-controlled active selection

- Actor: extension author.
- Pre-condition: DEP-01, DEP-02, and DEP-03 are met and two selection handlers are active.
- Trigger: a Glyph client or extension requests another active model or reasoning choice.
- Required behavior: handlers compose one target selection in registration order, Host validates the complete provider, model, reasoning, and authentication state, commits it atomically, and emits an event after commit.
- Example input and expected output: Input: request model `m2`, let handler A select reasoning `low`, and let handler B preserve A's current selection. Expected output: both handlers receive the same immutable original selection, B receives A's current selection, and the next model request uses `m2` with `low` after one committed selection event.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No prompt, context, input, provider, tool, or TUI transformations.

## Dependencies and Preconditions

- DEP-01: [PHS-05](../05-session-tree/ticket.md) must meet all acceptance criteria.
- DEP-02: [PHS-04.1](../04.1-model-execution-capabilities/ticket.md) must meet all acceptance criteria.
- DEP-03: [PHS-05.1](../05.1-extension-boundary-cleanup/ticket.md) must meet all acceptance criteria before this phase adds Extension Contract capabilities.

## Requirements

### Goals

- GOL-01: Give extension processes session-bound access, configured-model requests, active-selection control, and lifecycle events without terminal dependencies.

### Functional Requirements

- FRQ-01: Add extension context identity, runtime generation, active session identity, cancellation, cwd, model catalogue inspection, provider catalogue inspection, and configured-model requests for extension-owned behavior.
- FRQ-01.1: A configured-model request shall use an explicitly selected configured model, shall not change the active conversation model or reasoning choice, and shall expose no provider credential or provider reasoning context to the extension.
- FRQ-01.2: The Host use case that consumes a configured-model request shall declare its smallest provider-neutral model-request interface at the consumption site. It shall import no concrete provider package. Application composition shall bind the implemented in-process provider catalogue through that interface until PHS-06 replaces the implementation behind it.
- FRQ-02: Deliver agent, turn, message, tool-execution, model-selection, and reasoning-selection events to registered extension handlers.
- FRQ-02.1: Add extension context operations that request an active conversation model change or active reasoning-choice change through Host without exposing provider credentials.
- FRQ-02.2: Every client- or extension-initiated model or reasoning change shall create an immutable original target selection and an equal current target selection before commit.
- FRQ-02.3: Selection handlers shall run in registration order. Each handler shall receive the original and current target selections and shall preserve, replace, or reject the current selection. Rejection shall be terminal and shall stop later selection handlers.
- FRQ-02.4: An invalid selection action or ordinary handler error shall be reported, shall preserve the selection received by that handler, and shall not stop later handlers or deactivate the extension.
- FRQ-02.5: Host shall validate model existence, reasoning capability, and authentication against the final current selection before it atomically commits provider, model, and reasoning choice. The selected catalogue entry shall retain the `input`, `contextWindow`, and `maxTokens` descriptor values delivered by PHS-04.1.
- FRQ-02.6: Rejection or validation failure shall preserve the active selection. Host shall emit model-selection or reasoning-selection events only after a successful commit.
- FRQ-02.7: A selection rejection, handler error, or final selection-validation failure shall preserve the active selection and return its closed Glyph category and complete error text to the operation initiator.
- FRQ-03: Add model-hidden and model-visible branch-aware session entry operations.
- FRQ-03.1: Glyph clients shall present model-visible extension messages in the session tree. Selecting one shall use its parent as the navigation destination and shall return its exact text as editable next input without submitting that text automatically. The PHS-05 branch-summarization rules shall determine the committed active leaf.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, concrete provider packages, and TUI packages. The extension-context use case shall also import no concrete provider package.

### Deliverables

- DLV-01: Public extension context, configured-model request, active-selection, and lifecycle contract.
- DLV-02: Reference extension that records configured-model results as model-hidden and model-visible lifecycle-derived branch state and composes one active-selection change with another extension.

### Acceptance Criteria

- ACC-01: The reference extension works headlessly and through the standard TUI without changing its core behavior.
- ACC-02: Session replacement creates a new context and every operation through the preceding context fails.
- ACC-03: Extension entries attach to the active branch and survive restart.
- ACC-04: An extension uses an explicitly selected configured model without changing the active conversation model or reasoning choice and without receiving provider credentials or provider reasoning context.
- ACC-04.1: The extension-context model-request use case depends only on its consumer-owned provider-neutral interface. Its production implementation has a compile-time interface assertion, and the use case imports no OpenAI Codex, OpenAI-compatible, provider SDK, plugin transport, or provider credential package.
- ACC-05: An extension changes the active conversation model and reasoning choice, and the next model request uses the committed selection without clearing session history.
- ACC-06: Two selection handlers receive the same original target selection, while the second receives the current selection returned by the first.
- ACC-07: Handler rejection, an invalid replacement, unavailable credentials, or an unsupported reasoning choice preserves the active provider, model, and reasoning choice, emits no selection event, and returns the same Glyph category and complete error text through UI Plugin Contract and Programmatic Control.
- ACC-08: A model-visible extension message survives restart, appears through both Glyph client contracts, and returns its exact text and parent navigation destination when selected without starting an agent run. When branch summarization is enabled, the created `BranchSummaryEntry` becomes the active leaf.

## Overengineering and Overspecification Considerations

The ticket uses one Host-owned selection path for Glyph clients and extensions. Ordered original-and-current handlers avoid a second extension-only selection model and undocumented last-call winner behavior. OSP-01 remains outside the ticket.

## Constraints and Risks

- RSK-01: Long-lived requests could use a context after session replacement. Host validates runtime generation and session identity for every context operation.
- RSK-02: A model request could expose provider-owned authentication or replay data to an extension. Host resolves credentials and provider reasoning context without returning either to the extension.
- RSK-03: Selection can change while a model request streams. Each in-progress request retains its immutable selection snapshot, and the committed change affects the next model request.
- RSK-04: Binding configured-model requests directly to the concrete provider catalogue would force PHS-06 and PHS-12 to change the extension-context use case. FRQ-01.2 keeps that replacement behind a consumer-owned interface.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [extension process boundary](../../../../../../api/plugins/extension/v1) - public Extension Contract sources after PHS-05.1.
- REF-04: [model execution capabilities](../04.1-model-execution-capabilities/ticket.md) - provider-neutral descriptor metadata used during selection validation.
- REF-05: [target architecture](../../architecture.md) - Host selection ownership and extension composition.
- REF-06: [extension boundary cleanup](../05.1-extension-boundary-cleanup/ticket.md) - required runtime and capability ownership baseline.
