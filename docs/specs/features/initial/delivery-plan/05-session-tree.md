# Ticket: PHS-05 - Session tree

Support branch-preserving session navigation.

## Key definitions and abbreviations

- DEF-01: Active leaf. The session-tree entry from which later entries continue.

## Problem Statement

- PRB-01: A linear session cannot preserve alternate continuations, navigate prior entries, label branches, or produce branch summaries.

## Target Picture

- SOL-01: Support branch-preserving session navigation.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user continues from an earlier session entry.
- Required behavior: Glyph creates a new branch, preserves the preceding branch, and allows navigation between both branches.
- Example input and expected output: Input: navigate from leaf `e20` to earlier entry `e10` and submit new text. Expected output: a new child of `e10` becomes active while the branch ending at `e20` remains selectable.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No context compaction or extension session context.

## Dependencies and Preconditions

- DEP-01: [PHS-04](04-persistent-linear-sessions.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Support branch-preserving session navigation.

### Functional Requirements

- FRQ-01: Add parent-child entry relations, active leaf, fork, clone, switch, tree navigation, labels, and session information.
- FRQ-02: Add branch summaries with no summary, default summary, and custom-focus summary choices, including branch-summary usage accounting.
- FRQ-03: Add complete tree presentation and navigation to the standard TUI and equivalent Programmatic Control commands.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Persistent session tree and branch-summary model.
- DLV-02: Standard TUI session tree interaction.

### Acceptance Criteria

- ACC-01: Continuing from an earlier entry creates a new branch without deleting the previous branch.
- ACC-02: Selecting user or model-visible extension content places the expected text in the editor and moves the active leaf to the required parent.
- ACC-03: Labels and branch summaries survive restart.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Tree operations can leave partial state after storage failure. Apply each navigation or branch operation atomically at the session-storage boundary.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
