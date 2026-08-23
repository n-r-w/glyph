# Ticket: PHS-00 - Prototype baseline

Preserve an executable baseline before target behavior changes.

## Key definitions and abbreviations

- DEF-01: Prototype baseline. The Codex, extension tool, UI plugin, and headless behavior implemented before target-product slices.

## Problem Statement

- PRB-01: The implemented prototype spans several processes and contracts, but no single regression scenario protects that complete path before target behavior changes.

## Target Picture

- SOL-01: Preserve an executable baseline before target behavior changes.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph maintainer.
- Pre-condition: DEP-01 is met.
- Trigger: the baseline suite runs.
- Required behavior: Host starts Codex, an extension process, and the selected UI or headless controller and completes the prototype task flow.
- Example input and expected output: Input: run one fixed coding request through the standard TUI and through the one-shot headless command. Expected output: both paths emit the same semantic run, message, and tool lifecycle and finish with the same terminal outcome.

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

- FRQ-01: Add one process-level regression scenario covering Codex streaming, one extension tool call, standard TUI event delivery, and one-shot headless execution.
- FRQ-02: Record the prototype limitations that each later phase removes by referencing [`prototype-technical-solution.md`](../prototype-technical-solution.md).

### Non-Functional Requirements

- NFQ-01: The baseline process scenario must pass before and after this ticket because the ticket changes no product behavior.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Automated baseline scenario using the existing extension and UI process contracts.

### Acceptance Criteria

- ACC-01: The baseline scenario passes through the standard TUI path and the headless path without changing product behavior.
- ACC-02: `task lint` and `task test` pass.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: A test tied to presentation text would obstruct later UI work. Verify semantic events and terminal outcomes instead of mutable text layout.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype requirements](../prototype-prd.md) - prototype requirements.
- REF-04: [prototype architecture](../prototype-technical-solution.md) - prototype architecture.
