# Ticket: PHS-02 - Programmatic Control foundation

Provide a long-lived headless client contract independent of the standard TUI.

## Key definitions and abbreviations

- DEF-01: Programmatic Control. The transport-independent correlated command and event contract for a long-lived headless agent.
- DEF-02: Programmatic Control transport. The bidirectional gRPC stream over a Unix socket that exposes DEF-01 from the current `glyph` application's headless composition.

## Problem Statement

- PRB-01: The `glyph` application's headless execution is a one-shot CLI path. A controller cannot connect to its long-lived headless composition, correlate accepted commands with later events, query state, or abort and continue in the same process.

## Target Picture

- SOL-01: Extend the current `glyph` application's headless composition with bidirectional gRPC over a Unix socket, independent of the standard TUI and without a separate Host daemon.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: programmatic controller.
- Pre-condition: DEP-01 is met.
- Trigger: the controller submits a correlated user request.
- Required behavior: Host accepts or rejects it before completion, emits correlated events, supports abort, and accepts a later request.
- Example input and expected output: Input: submit correlation `c1`, abort its accepted run, query idle state, then submit correlation `c2`. Expected output: events for each operation carry only its correlation and the second request runs without restarting `glyph`.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No persistent sessions, model selection, or extension-defined commands.

## Dependencies and Preconditions

- DEP-01: [PHS-01](01-complete-standard-tools.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Extend the current `glyph` application's headless composition with bidirectional gRPC over a Unix socket, independent of the standard TUI and without a separate Host daemon.

### Functional Requirements

- FRQ-01: Define transport-independent correlated commands, acceptance or rejection responses, and asynchronous operation events.
- FRQ-02: Implement user request, abort, run-state query, message query, and programmatic shell execution.
- FRQ-03: Route commands through Host use cases rather than through UI-specific code.
- FRQ-04: Expose DEF-01 through DEF-02 from the current `glyph` application's headless composition. The `glyph` process shall host the gRPC service and shall not start or require a separate Host daemon.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Public Programmatic Control contract with bidirectional gRPC over a Unix socket as its supported transport.
- DLV-02: Programmatic controller process fixture or SDK client used by end-to-end tests.

### Acceptance Criteria

- ACC-01: A controller submits a request, receives acceptance before completion, correlates every resulting event, aborts an active run, and submits another request without restarting `glyph`.
- ACC-02: Starting Programmatic Control does not load a UI plugin or create a separate Host daemon.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: A transport-shaped internal API would couple later features to one protocol. Controllers must map transport DTOs to consumer-owned Host commands and events.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

This ticket selects bidirectional gRPC over a Unix socket as the supported Programmatic Control transport. Contract shapes, package placement, and transport-independent Host command and event types require a phase-specific technical solution before implementation.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [existing correlated UI stream patterns](../../../../../api/plugins/ui/v1/ui.proto) - existing correlated UI stream patterns.
