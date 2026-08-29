# Ticket: PHS-18 - Cleanup

Remove prototype-only restrictions and implementation residue superseded by target behavior.

## Key definitions and abbreviations

- DEF-01: Implementation residue. Reachable code, contracts, fixtures, or documentation superseded by target-product behavior.

## Problem Statement

- PRB-01: After target slices land, prototype-only restrictions, adapters, contract versions, and fixtures can remain reachable and obscure the owning product path.

## Target Picture

- SOL-01: Remove prototype-only restrictions and implementation residue superseded by target behavior.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph maintainer.
- Pre-condition: DEP-01 is met.
- Trigger: all target product slices have completed.
- Required behavior: superseded prototype paths are removed and public documentation describes only owned product paths.
- Example input and expected output: Input: search for every prototype-only symbol and contract path listed by the cleanup work. Expected output: no production reference remains and the full test suite still passes.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No new product behavior or requirement expansion.

## Dependencies and Preconditions

- DEP-01: [PHS-17](../17-reference-scenario-closure/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Remove prototype-only restrictions and implementation residue superseded by target behavior.

### Functional Requirements

- FRQ-01: Remove unused prototype schema restrictions, startup-only catalog paths, one-shot-only control paths, dead contract versions, temporary adapters, debugging artifacts, and obsolete test fixtures.
- FRQ-02: Align public documentation and SDK examples with the contracts exercised by PHS-17.
- FRQ-03: Confirm that no Agent Core package depends on protobuf, gRPC, plugin SDKs, persistence adapters, or TUI packages.

### Non-Functional Requirements

- NFQ-01: Cleanup must not change product behavior. Focused regression tests, `task lint`, and `task test` must remain green throughout the ticket.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Product code and documentation without superseded prototype paths.

### Acceptance Criteria

- ACC-01: Repository search finds no referenced prototype-only implementation path that was replaced by PHS-01 through PHS-17.
- ACC-02: Public SDK examples compile against the final public contracts.
- ACC-03: `task lint` and `task test` pass after cleanup.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Cleanup can remove a still-used compatibility path. Remove code only after symbolic reference search and focused regression tests show no remaining owner.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
