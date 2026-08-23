# Ticket: PHS-08 - Prompt, context, input, and provider middleware

Allow extensions to change model-facing input through ordered generic extension points.

## Key definitions and abbreviations

- DEF-01: Extension point. A documented operation boundary with observe, block, modify, replace, or handle semantics.

## Problem Statement

- PRB-01: Extensions cannot change system instructions, outbound context, user input, finalized messages, or provider requests through ordered generic extension points.

## Target Picture

- SOL-01: Allow extensions to change model-facing input through ordered generic extension points.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension author.
- Pre-condition: DEP-01 is met.
- Trigger: the agent prepares a model request.
- Required behavior: registered handlers transform the prompt or request-local context in order without mutating persisted history.
- Example input and expected output: Input: register two context transformations that append markers `A` and `B`, then run one model request. Expected output: the outbound context contains `A` before `B` and persisted session entries contain neither marker.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No tool middleware, extension commands, or TUI-specific contribution.

## Dependencies and Preconditions

- DEP-01: [PHS-07](07-extension-context-lifecycle.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Allow extensions to change model-facing input through ordered generic extension points.

### Functional Requirements

- FRQ-01: Add system-prompt changes, per-request context transformations, user text and image handling, finalized-message replacement, provider-header changes, serialized-request replacement, and provider-response observation.
- FRQ-02: Apply transformations sequentially and pass each transformed value to the next handler.
- FRQ-03: Continue later handlers and the core operation after an ordinary handler error while reporting that error.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Public middleware contracts with declared observe, modify, replace, and handle semantics.
- DLV-02: Reference extensions for project instructions, context projection, and provider-request changes.

### Acceptance Criteria

- ACC-01: A project-instructions extension changes the effective system prompt in headless and TUI runs.
- ACC-02: A context transformation changes one outbound provider request without changing persisted session entries.
- ACC-03: An input handler can transform or fully handle text and image input.
- ACC-04: Two transforming handlers observe registration order, including when an earlier ordinary handler reports an error.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Persisting transformed provider context would violate the request-local contract. Tests must compare persisted entries with the outbound provider request.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype internal hook types](../../../../../host/internal/hooks/hooks.go) - prototype internal hook types.
