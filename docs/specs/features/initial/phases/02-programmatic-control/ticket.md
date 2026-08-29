# Ticket: PHS-02 - Programmatic Control foundation

Provide a long-lived headless client contract independent of the standard TUI.

## Key definitions and abbreviations

- DEF-01: Programmatic Control. The transport-independent correlated command and event contract through which one controller owns a headless agent process and submits multiple operations over one connection.
- DEF-02: Programmatic Control transport. The bidirectional gRPC stream over a Unix socket that exposes DEF-01 from the current `glyph` application's headless composition.

## Problem Statement

- PRB-01: The `glyph` application's headless execution is a one-shot CLI path. A controller cannot connect to its long-lived headless composition, correlate accepted commands with later events, query state, or abort and continue in the same process.

## Target Picture

- SOL-01: Extend the current `glyph` application's headless composition with controller-owned bidirectional gRPC over a Unix socket, independent of the standard TUI and without a separate Host daemon.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: programmatic controller.
- Pre-condition: DEP-01 is met.
- Trigger: the controller submits a correlated user request.
- Required behavior: Host accepts or rejects it before completion, emits correlated events, supports abort, accepts a later request, and exits after the controller closes the connection.
- Example input and expected output: Input: submit correlation `c1`, abort its accepted run, query idle state, submit correlation `c2`, then close the connection. Expected output: events for each operation carry only its correlation, the second request runs without restarting `glyph`, and `glyph` exits after connection closure.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No persistent sessions, model selection, or extension-defined commands.
- OSP-02: No Glyph-client direct shell action. Shell execution remains available when the model invokes the bundled `bash` tool.

## Dependencies and Preconditions

- DEP-01: [PHS-01](../01-complete-standard-tools/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Extend the current `glyph` application's headless composition with bidirectional gRPC over a Unix socket, independent of the standard TUI and without a separate Host daemon.

### Functional Requirements

- FRQ-01: Define transport-independent correlated commands, acceptance or rejection responses, and asynchronous operation events.
- FRQ-02: Implement user request, abort, run-state query, and message query.
- FRQ-03: A message query shall return an ordered snapshot of user messages, finalized model responses, and tool results.
- FRQ-04: Route commands through Host use cases rather than through UI-specific code.
- FRQ-05: Expose DEF-01 through DEF-02 from the current `glyph` application's headless composition. The `glyph` process shall host the gRPC service and shall not start or require a separate Host daemon.
- FRQ-06: One controller connection shall accept multiple sequential user requests. Closing the connection shall cancel and settle an active agent run, close the Host composition, and terminate the `glyph` process.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.
- NFQ-03: The implementation must use platform-independent Go facilities when they provide the required behavior and must not reject Windows by operating-system identity. Windows behavior is not tested or guaranteed by this ticket.

### Deliverables

- DLV-01: Public Programmatic Control contract with bidirectional gRPC over a Unix socket as its supported transport.
- DLV-02: Programmatic controller process fixture or SDK client used by end-to-end tests.

### Acceptance Criteria

- ACC-01: A controller submits a request, receives acceptance before completion, correlates every resulting event, aborts an active run, queries idle state, and submits another request without restarting `glyph`.
- ACC-02: A message query returns the ordered user messages, finalized model responses, and tool results held by the controlled headless agent.
- ACC-03: Closing the controller connection cancels and settles an active run, closes the Host composition, removes the Unix socket, and terminates `glyph`.
- ACC-04: Starting Programmatic Control does not load a UI plugin or create a separate Host daemon.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: A transport-shaped internal API would couple later features to one protocol. Controllers must map transport DTOs to consumer-owned Host commands and events.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

This ticket selects bidirectional gRPC over a Unix socket as the supported Programmatic Control transport. Contract shapes, package placement, and transport-independent Host command and event types are defined in the [PHS-02 technical solution](technical-solution.md).

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [existing correlated UI stream patterns](../../../../../../api/plugins/ui/v1/ui.proto) - existing correlated UI stream patterns.
- REF-04: [PHS-02 technical solution](technical-solution.md) - approved Programmatic Control contract, package placement, lifecycle, and verification design.
