# Ticket: PHS-00 - Prototype baseline

Preserve an executable baseline before target behavior changes.

## Key definitions and abbreviations

- DEF-01: Prototype baseline. The Codex, extension tool, UI process contract, standard TUI consumer, and headless behavior implemented before target-product slices.
- DEF-02: Typed lifecycle sequence. The ordered Host lifecycle events for one agent run, including run start and terminal outcome, message completion, tool execution start and end, tool name and terminal result, and Host settlement.

## Problem Statement

- PRB-01: The implemented prototype spans several processes and contracts, but no linked regression fixtures protect the Host UI process contract and standard TUI consumer before target behavior changes.

## Target Picture

- SOL-01: Preserve an executable baseline before target behavior changes with linked Host process and standard TUI consumer fixtures.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph maintainer.
- Pre-condition: DEP-01 is met.
- Trigger: the baseline suite runs.
- Required behavior: The Host process fixture runs one fixed coding request twice with Codex streaming and the real bundled tools extension: once through the one-shot headless path and once through a semantic UI process client. It compares the observed semantic outcomes and records the typed lifecycle sequence delivered through the public UI process contract. The standard TUI consumer fixture feeds that sequence through real standard-TUI controller logic. Together, the fixtures protect the public UI process contract and the standard TUI consumer without changing product behavior.
- Example input and expected output: Input: one fixed coding request. Expected output: both Host paths observe the same agent run start and terminal outcome, message completion, tool execution start and end, tool name and terminal result, and Host settlement. The standard TUI consumer fixture reaches the matching semantic state without asserting terminal presentation text.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No product behavior changes or new target contracts.

## Dependencies and Preconditions

- DEP-01: None. This is the first ticket.

## Requirements

### Goals

- GOL-01: Preserve an executable baseline before target behavior changes.

### Functional Requirements

- FRQ-01: Add two linked baseline fixtures that use the same typed lifecycle sequence.
- FRQ-02: The Host process fixture shall run the same request with Codex streaming and the real bundled tools extension through the current one-shot headless path and through a semantic UI process client. It shall compare the observed semantic run, message, tool, terminal, and Host settlement outcomes and record the typed lifecycle sequence delivered to the semantic UI process client.
- FRQ-03: The standard TUI consumer fixture shall feed that typed lifecycle sequence through real standard-TUI controller logic and verify semantic TUI state without asserting terminal presentation text.
- FRQ-04: Record the prototype limitations that each later phase removes by referencing [`prototype-technical-solution.md`](../prototype-technical-solution.md).

### Non-Functional Requirements

- NFQ-01: Both baseline fixtures must pass before and after this ticket because the ticket changes no product behavior.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Automated Host process fixture using the existing extension and UI process contracts, plus an automated standard TUI consumer fixture linked by the same typed lifecycle sequence.

### Acceptance Criteria

- ACC-01: The Host process fixture runs the same request with the real bundled tools extension and Codex streaming through the current one-shot headless path and through a semantic UI process client.
- ACC-02: The Host process fixture observes agent run start and terminal outcome, message completion, tool execution start and end, tool name and terminal result, and Host settlement.
- ACC-03: The standard TUI consumer fixture uses real standard-TUI controller logic to consume the same typed lifecycle sequence and verifies semantic TUI state without terminal presentation text.
- ACC-04: The two fixtures protect the existing public UI process contract and standard TUI consumer without a production observation hook, gRPC proxy, PTY text assertion, or product behavior change.
- ACC-05: `task lint` and `task test` pass.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: A test tied to presentation text would obstruct later UI work. The standard TUI consumer fixture verifies semantic state instead of mutable terminal layout.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No product technical design is selected by this ticket. Fixture placement and shared lifecycle-sequence representation remain implementation details.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype requirements](../prototype-prd.md) - prototype requirements.
- REF-04: [prototype architecture](../prototype-technical-solution.md) - prototype architecture.
