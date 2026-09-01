# Ticket: PHS-17 - Glyph public-behavior traceability

Demonstrate that every Glyph-owned public behavior group in [`prd.md`](../../prd.md) has traceable public-contract evidence.

## Key definitions and abbreviations

- DEF-01: Glyph public-behavior traceability. The mapping of each Glyph-owned public behavior group to its owning PRD section, owner ticket, and public-contract scenario.

## Problem Statement

- PRB-01: Glyph-owned public behavior groups lack one traceability pass that links each group to its owning requirement, owner ticket, and public-contract evidence.

## Target Picture

- SOL-01: Demonstrate that every Glyph-owned public behavior group in [`prd.md`](../../prd.md) has traceable public-contract evidence.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph maintainer.
- Pre-condition: DEP-01 is met.
- Trigger: the public-behavior traceability suite runs.
- Required behavior: every public behavior group in the Glyph public-behavior traceability matrix has passing evidence through public Glyph contracts.
- Example input and expected output: Input: execute the automated matrix for all rows under Glyph public-behavior traceability. Expected output: every row links to passing public-contract evidence and no fixture imports another project's internal package.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No Pi compatibility requirement, external entry-point coverage requirement, or another project's internal package.

## Dependencies and Preconditions

- DEP-01: [PHS-16](../16-environment-reload/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Demonstrate that every Glyph-owned public behavior group in [`prd.md`](../../prd.md) has traceable public-contract evidence.

### Functional Requirements

- FRQ-01: Create and maintain the Glyph public-behavior traceability matrix. Each row shall identify one Glyph-owned public behavior group, its owning PRD section, owner ticket, public-contract scenario, and passing evidence.
- FRQ-02: Run every headless scenario through Programmatic Control and every terminal scenario through the standard TUI.
- FRQ-03: Confirm that no fixture requires Pi compatibility, an external entry point, or another project's internal package.
- FRQ-04: For every error behavior available through more than one Glyph client interface, the public-contract scenario suite shall trigger the same source condition through each interface and compare the resulting category and complete error text.

### Non-Functional Requirements

- NFQ-01: Every public-contract scenario must pass against behavior delivered by its owner ticket. A failure reopens that owner ticket.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Glyph public-behavior traceability matrix and automated public-contract scenario suite.

### Acceptance Criteria

- ACC-01: Every Glyph public-behavior traceability row has at least one passing process-level scenario through public contracts.
- ACC-02: Headless-capable fixtures run without loading the standard TUI.
- ACC-03: TUI-only calls fail explicitly when no standard TUI capability is active.
- ACC-04: Every multi-interface error scenario produces the same Glyph category and information completeness through all applicable interfaces.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Fixtures can accidentally create an external compatibility requirement. Keep fixtures minimal and assert Glyph-owned observable behavior.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [Glyph public-behavior traceability](../../prd.md#glyph-public-behavior-traceability) - owning behavior matrix.
