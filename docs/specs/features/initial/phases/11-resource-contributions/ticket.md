# Ticket: PHS-11 - Resource contributions

Collect extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.

## Key definitions and abbreviations

- DEF-01: Resource contribution. A typed skill, prompt template, or context file supplied by an active extension.

## Problem Statement

- PRB-01: Extensions cannot contribute skills, prompt templates, or context files, and Agent Core has no bundled resource-processing path that preserves its resource independence.

## Target Picture

- SOL-01: Collect extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension author.
- Pre-condition: DEP-01 is met.
- Trigger: Host collects resources at startup.
- Required behavior: the contributed skill, prompt template, and context file become available through their owning Host and client paths.
- Example input and expected output: Input: start an extension that contributes one skill, one prompt template, and one context file. Expected output: clients discover the prompt and Agent Core receives only resolved instructions and context.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No provider registration or TUI-specific resource rendering.

## Dependencies and Preconditions

- DEP-01: [PHS-10](../10-commands-interaction-notifications-events/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Collect extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.

### Functional Requirements

- FRQ-01: Add typed resource contribution collection at startup.
- FRQ-02: Implement the bundled resource extension through the ordinary extension runtime.
- FRQ-03: Convert skills and context files into resolved instructions and model context and expose prompt templates through Glyph clients.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Resource contribution contract and Host registry.
- DLV-02: Bundled resource extension.

### Acceptance Criteria

- ACC-01: An extension contributes one skill, prompt template, and context file that are available through both client kinds.
- ACC-02: Agent Core receives only resolved instructions and context and imports no resource types.
- ACC-03: Recollecting one extension's resources replaces that extension's preceding contribution set.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Resource paths can outlive the contributing runtime. Host snapshots or resolves contributions according to one ownership rule before publishing them.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
