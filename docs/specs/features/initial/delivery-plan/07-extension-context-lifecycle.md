# Ticket: PHS-07 - Extension context and lifecycle

Give extension processes session-bound access and lifecycle events without terminal dependencies.

## Key definitions and abbreviations

- DEF-01: Extension context. Host access bound to one extension runtime generation and one active session.

## Problem Statement

- PRB-01: Extension processes receive tool calls but no session-bound context or Agent Core lifecycle events. Stateful headless extensions cannot observe runs or persist branch-aware entries.

## Target Picture

- SOL-01: Give extension processes session-bound access and lifecycle events without terminal dependencies.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension author.
- Pre-condition: DEP-01 is met.
- Trigger: the extension handles a lifecycle event in an active session.
- Required behavior: the handler receives a session-bound context and can persist an entry on the active branch.
- Example input and expected output: Input: deliver `agent_start` to an extension in session `s1` and let it append a model-hidden entry. Expected output: the entry is stored on the active branch of `s1` and a context from a replaced session is rejected.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No prompt, context, input, provider, tool, or TUI transformations.

## Dependencies and Preconditions

- DEP-01: [PHS-06](06-context-compaction-retry-control.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Give extension processes session-bound access and lifecycle events without terminal dependencies.

### Functional Requirements

- FRQ-01: Add extension context identity, runtime generation, active session identity, cancellation, cwd, model catalogue inspection, and provider catalogue inspection.
- FRQ-02: Deliver agent, turn, message, tool-execution, model-selection, and reasoning-selection events to registered extension handlers.
- FRQ-03: Add model-hidden and model-visible branch-aware session entry operations.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Public extension context and lifecycle contract.
- DLV-02: Reference extension that records lifecycle-derived branch state.

### Acceptance Criteria

- ACC-01: The reference extension works headlessly and through the standard TUI without changing its core behavior.
- ACC-02: Session replacement creates a new context and every operation through the preceding context fails.
- ACC-03: Extension entries attach to the active branch and survive restart.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Long-lived requests could use a context after session replacement. Host validates runtime generation and session identity for every context operation.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype extension process boundary](../../../../../api/plugins/extension/v1/tool.proto) - prototype extension process boundary.
