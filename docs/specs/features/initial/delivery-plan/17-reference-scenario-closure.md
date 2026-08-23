# Ticket: PHS-17 - Reference scenario closure

Demonstrate that generic Glyph contracts cover all 20 reference entry points listed in [`prd.md`](../prd.md).

## Key definitions and abbreviations

- DEF-01: Reference scenario. Generic observable behavior used to exercise one or more extension contracts without Pi API compatibility.

## Problem Statement

- PRB-01: The generic contracts are not exercised together against every capability combination in the 20-row Reference Scenario Coverage matrix.

## Target Picture

- SOL-01: Demonstrate that generic Glyph contracts cover all 20 reference entry points listed in [`prd.md`](../prd.md).

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph maintainer.
- Pre-condition: DEP-01 is met.
- Trigger: the reference scenario suite runs.
- Required behavior: each of the 20 PRD coverage rows completes through generic public Glyph contracts.
- Example input and expected output: Input: execute the automated matrix for all 20 rows under Reference Scenario Coverage. Expected output: every row completes through generic public Glyph contracts and no fixture imports suite-specific Agent Core behavior.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No port of pi-agent-suite and no suite-specific Agent Core concept.

## Dependencies and Preconditions

- DEP-01: [PHS-16](16-environment-reload.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Demonstrate that generic Glyph contracts cover all 20 reference entry points listed in [`prd.md`](../prd.md).

### Functional Requirements

- FRQ-01: Create one generic reference extension fixture for each distinct capability combination needed by the Reference Scenario Coverage matrix.
- FRQ-02: Run every non-terminal scenario through Programmatic Control and every terminal scenario through the standard TUI.
- FRQ-03: Confirm that no fixture requires a suite-specific Agent Core concept or imports another project's internal package.

### Non-Functional Requirements

- NFQ-01: Every reference scenario must pass against behavior delivered by its owning product ticket. A failure reopens that owning ticket.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Automated reference-scenario suite linked to the 20-entry coverage matrix.

### Acceptance Criteria

- ACC-01: Every Reference Scenario Coverage row has at least one passing process-level scenario through public contracts.
- ACC-02: Headless-capable fixtures run without loading the standard TUI.
- ACC-03: TUI-only calls fail explicitly when no standard TUI capability is active.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Fixtures can accidentally become ports of `pi-agent-suite`. Keep fixtures minimal and assert generic observable behavior rather than package-specific output.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [researched extension capability surface](../../../../artefacts/pi-extension-surface.md) - researched extension capability surface.
- REF-04: [reference extension scenarios](https://github.com/n-r-w/pi-agent-suite) - reference extension scenarios.
