# Ticket: PHS-15 - Extension installation and state management

Manage compatible extensions without rebuilding Glyph.

## Key definitions and abbreviations

- DEF-01: Configured extension state. Persisted installation and enablement state, separate from a running extension process.

## Problem Statement

- PRB-01: Extension executables can be discovered manually, but users cannot install, enable, disable, update, or replace compatible extensions through an owned product lifecycle.

## Target Picture

- SOL-01: Manage compatible extensions without rebuilding Glyph.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user changes installed extension state.
- Required behavior: Host records install, enable, disable, update, replacement, or removal without rebuilding Glyph.
- Example input and expected output: Input: install a compatible extension package, enable it, update it, disable it, and remove it. Expected output: configured state records each operation and no Host rebuild occurs.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No environment reload or package registry requirement beyond the local lifecycle in the PRD.

## Dependencies and Preconditions

- DEP-01: [PHS-14](14-interactive-tui-extensions.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Manage compatible extensions without rebuilding Glyph.

### Functional Requirements

- FRQ-01: Add installation, enablement, disablement, and update operations for compatible extension packages.
- FRQ-02: Store configured extension state separately from discovered runtime state.
- FRQ-03: Move bundled tools and bundled resource processing under the same lifecycle rules as other extensions.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Extension package lifecycle and persistent enablement state.
- DLV-02: Bundled extensions represented through the ordinary extension catalogue.

### Acceptance Criteria

- ACC-01: A user installs, enables, disables, updates, and removes an extension without rebuilding Host.
- ACC-02: The bundled tools and resource extensions can be disabled, updated, and replaced through the same operations.
- ACC-03: An incompatible package never starts and does not affect compatible extensions.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Updating a running executable can produce mixed runtime state. Package mutation changes configured state only; PHS-16 applies runtime changes through environment reload.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
