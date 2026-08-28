# Ticket: PHS-08 - Prompt, context, input, and provider middleware

Allow extensions to change model-facing input through ordered generic extension points.

## Key definitions and abbreviations

- DEF-01: Extension point. A documented operation boundary with observe, block, modify, replace, or handle semantics.

## Problem Statement

- PRB-01: Extensions cannot change system instructions, outbound context, user input, finalized messages, or provider requests through ordered generic extension points.

## Target Picture

- SOL-01: Allow extensions to change model-facing input through Host-owned ordered generic extension points while Agent Core consumes only provider-neutral final values.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension author.
- Pre-condition: DEP-01 and DEP-02 are met.
- Trigger: the agent prepares a model request.
- Required behavior: registered handlers receive the immutable original input and the current value from preceding handlers and transform model-facing input without mutating persisted history.
- Example input and expected output: Input: let handler A append marker `A` to the current context and handler B replace that context with a value derived from the original context plus marker `B`. Expected output: B sees both context versions, the outbound context contains `B` without `A`, and persisted session entries contain neither marker.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No tool middleware, extension commands, or TUI-specific contribution.

## Dependencies and Preconditions

- DEP-01: [PHS-06](06-context-compaction-retry-control.md) must meet all acceptance criteria.
- DEP-02: [PHS-04.1](04.1-model-execution-capabilities.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Allow extensions to change model-facing input through ordered generic extension points.

### Functional Requirements

- FRQ-01: Add system-prompt changes, per-request context transformations, user text and image handling, finalized-message replacement, provider-header changes, serialized-request replacement, and provider-response observation.
- FRQ-01.1: Host shall own extension dispatch, deterministic handler order, plugin transport mapping, ordinary handler failure mapping, and final-value validation outside Agent Core.
- FRQ-01.2: After input handlers finish, Host shall validate every final text and image content kind against the selected model descriptor's `input` modalities before Agent Core or a provider driver receives the input.
- FRQ-01.3: A final image for a text-only model shall fail input processing without starting an agent run or provider request.
- FRQ-01.4: Agent Core shall declare a minimal consumer-owned interface in `host/internal/usecase/agent/run` for obtaining effective provider-neutral model context. A Host adapter shall implement extension dispatch through that interface without exposing handler count, handler order, plugin processes, protobuf, gRPC, or extension failure mapping to Agent Core.
- FRQ-01.5: Agent Core shall not import `host/internal/hooks`. Host shall own any adapter from Agent Core's effective-context interface to extension middleware.
- FRQ-02: Apply transformations sequentially. Each handler shall receive the immutable original input and the current value returned by preceding handlers and shall be able to preserve the current value or replace it with a value derived from either input.
- FRQ-03: Continue later handlers and the core operation after an ordinary handler error while reporting that error. The next handler shall receive the same current value that the failed handler received.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Public middleware contracts with declared observe, modify, replace, and handle semantics.
- DLV-01.1: Agent Core consumer-owned effective-context interface and Host middleware adapter.
- DLV-02: Reference extensions for project instructions, context projection, and provider-request changes.

### Acceptance Criteria

- ACC-01: A project-instructions extension changes the effective system prompt in headless and TUI runs.
- ACC-02: A context transformation changes one outbound provider request without changing persisted session entries.
- ACC-03: An input handler can transform or fully handle text and image input.
- ACC-03.1: A text-and-image model accepts final text and image input, while a text-only model rejects final image input before Agent Core or a provider request starts.
- ACC-04: Two transforming handlers observe registration order, and the second can inspect the immutable original input while preserving or discarding the first handler's current value.
- ACC-05: After an earlier ordinary handler error, the next handler receives the unchanged original input and the same current value that the failed handler received.
- ACC-06: `host/internal/usecase/agent/run` obtains effective context through its own minimal interface, imports no `host/internal/hooks` package, and receives no plugin or transport type.

## Overengineering and Overspecification Considerations

The ticket uses one Host middleware path for every model-facing transformation and one consumer-owned Agent Core interface for effective context. It adds no Agent Core plugin dispatcher, duplicate image-capability source, or persisted transformed context. OSP-01 remains outside the ticket.

## Constraints and Risks

- RSK-01: Persisting transformed provider context would violate the request-local contract. Tests must compare persisted entries with the outbound provider request.
- RSK-02: Validating modalities before input handlers finish can reject an image that an extension would remove or fully handle. Host validates only the final handler result.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype internal hook types](../../../../../host/internal/hooks/hooks.go) - prototype-only hook types that Agent Core must stop importing.
- REF-04: [model execution capabilities](04.1-model-execution-capabilities.md) - input modality ownership.
- REF-05: [target architecture](../architecture.md) - Host dispatch and Agent Core consumer-owned interfaces.
