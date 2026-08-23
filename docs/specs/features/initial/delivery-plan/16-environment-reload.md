# Ticket: PHS-16 - Environment reload

Apply environment changes without ending the active session.

## Key definitions and abbreviations

- DEF-01: Environment reload. Replacement of the Glyph environment without replacing the active session or selected UI plugin.

## Problem Statement

- PRB-01: Applying changed settings, providers, extensions, resources, themes, and key bindings requires ending the active session and restarting Glyph.

## Target Picture

- SOL-01: Apply environment changes without ending the active session.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user requests reload while Glyph is idle.
- Required behavior: Host replaces the reloadable environment, preserves the session, and rejects every preceding extension context.
- Example input and expected output: Input: change extension and resource configuration while Glyph is idle and request reload. Expected output: the active session ID and history remain, new runtime contributions are active, and calls through the preceding context fail.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No replacement of the active UI plugin and no rollback to the preceding environment after reload failure.

## Dependencies and Preconditions

- DEP-01: [PHS-15](15-extension-installation-state.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Apply environment changes without ending the active session.

### Functional Requirements

- FRQ-01: Add a quiescence check that rejects reload during an agent run or compaction.
- FRQ-02: Reload Host settings except active UI selection, provider registrations, extension runtimes, and resource contributions.
- FRQ-03: Reload standard TUI themes and key bindings while retaining the selected UI plugin.
- FRQ-04: Invalidate preceding extension contexts and bind later events and commands to the replacement runtimes and active session.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Environment reload use case exposed through the standard TUI and Programmatic Control.

### Acceptance Criteria

- ACC-01: Reload preserves the active session and history while applying changed settings, providers, extensions, and resources.
- ACC-02: Reload while a run or compaction is active is rejected with a warning and changes no environment state.
- ACC-03: Failed reinitialization preserves the session, reports the error, requires restart, and does not restore the preceding environment.
- ACC-04: Every operation through a context created before reload fails.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Partial replacement can combine old and new environment state. Build the replacement environment separately, switch ownership once, and follow the PRD failure rule instead of restoring the old environment.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
