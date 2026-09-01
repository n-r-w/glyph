# Ticket: PHS-10 - Commands, interaction, notifications, and extension events

Let extensions expose user actions and request Host or client behavior.

## Key definitions and abbreviations

- DEF-01: Interaction request. An extension request sent through Host to a Glyph client that expects a response.

## Problem Statement

- PRB-01: Extensions cannot register client commands, request user interaction, send notifications, or exchange transient events.

## Target Picture

- SOL-01: Let extensions expose user actions and request Host or client behavior.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 is met.
- Trigger: the user invokes an extension command that requests input and a model query.
- Required behavior: the active client returns the interaction result and the extension receives its configured-model result.
- Example input and expected output: Input: invoke extension command `review`, answer its interaction request with `strict`, and let it query a configured model. Expected output: the command receives `strict` and returns the model result through the invoking client.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No resource contributions, provider registration, or TUI component contribution.

## Dependencies and Preconditions

- DEP-01: [PHS-09](../09-tool-middleware-run-control/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Let extensions expose user actions and request Host or client behavior.

### Functional Requirements

- FRQ-01: Add command registration, discovery, invocation, and provenance through Glyph clients.
- FRQ-02: Add interface-neutral interaction requests and notifications with explicit unavailable-client and delivery-failure results.
- FRQ-02.1: An unavailable-client or delivery-failure result shall contain a closed Glyph category and complete error text. Equivalent failures shall expose the same category and information completeness through UI Plugin Contract and Programmatic Control.
- FRQ-03: Add non-persisted inter-extension events.
- FRQ-04: Allow extension commands to use the configured-model request contract delivered by PHS-07 without changing the active conversation model or reasoning choice.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Command registry and invocation contracts.
- DLV-02: Interaction, notification, and extension-event contracts.
- DLV-03: Reference command, interaction, notification, and model-query extensions.

### Acceptance Criteria

- ACC-01: The same extension command is discoverable and invokable through the standard TUI and Programmatic Control.
- ACC-02: An interaction request succeeds through either connected client kind and fails without a client with its defined Glyph category and complete error text.
- ACC-03: Notification success means Host transferred it to the client and does not require presentation or a user response. Delivery failure retains the same Glyph category and complete error text through UI Plugin Contract and Programmatic Control.
- ACC-04: An extension model request uses a configured model without changing the active conversation model.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: A client disconnect can leave an extension request pending. Host completes every pending interaction with a delivery error when the client connection closes.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [prototype Host interaction path](../../../../../../host/internal/usecase/host/interactions/service.go) - prototype Host interaction path.
