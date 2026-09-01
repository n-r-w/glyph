# Ticket: PHS-09 - Tool middleware and run control

Allow extensions to control tool policy and agent-run continuation.

## Key definitions and abbreviations

- DEF-01: Active tool set. The registered tools included in the next model request.

## Problem Statement

- PRB-01: Extensions cannot enforce tool policy, transform tool results, replace registered tools, change the active set, terminate a tool batch, or queue steering and follow-up messages.

## Target Picture

- SOL-01: Allow extensions to control tool policy and agent-run continuation.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension author.
- Pre-condition: DEP-01 is met.
- Trigger: the model requests a governed tool and queued messages exist.
- Required behavior: registered middleware applies tool policy and Agent Core delivers queued messages and termination according to their contracts.
- Example input and expected output: Input: let a policy extension reject one protected `edit` call while a `steer` message is queued. Expected output: the model receives one tool error, the extension remains active, and the steering message is delivered before the next model request.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No command, interaction, resource, provider-registration, or TUI contribution contracts.

## Dependencies and Preconditions

- DEP-01: [PHS-08](../08-prompt-context-input-provider-middleware/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Allow extensions to control tool policy and agent-run continuation.

### Functional Requirements

- FRQ-01: Add pre-execution tool handlers for allow, reject, input modification, and handler-error blocking. Each modifying handler shall receive the immutable original tool call and the current tool call returned by preceding handlers.
- FRQ-01.1: A rejected tool call or pre-execution handler error shall become a model-visible error result with a closed Glyph category and complete error text. It shall not fail the parent agent run automatically.
- FRQ-02: Add sequential tool-result transformation in which each handler receives the immutable original result and the current result returned by preceding handlers, plus deterministic tool replacement, registered and active tool inspection, and active-set changes for subsequent model requests.
- FRQ-02.1: Tool-result transformations shall preserve the error category and complete error text unless a handler explicitly replaces the complete current tool result.
- FRQ-03: Add parallel tool batches and the batch-wide `terminate` rule.
- FRQ-04: Add `steer`, `followUp`, `nextTurn`, abort, and queue modes `all` and `one-at-a-time`.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Tool middleware and active-tool contracts.
- DLV-02: Queued-message and termination behavior in Agent Core.
- DLV-03: Reference policy, tool-override, and terminating-output extensions.

### Acceptance Criteria

- ACC-01: A policy extension rejects one dangerous call, leaves the extension active, and lets the agent continue from a model-visible error result that contains its Glyph category and complete error text.
- ACC-02: An extension replaces one registered tool without disabling unrelated extensions.
- ACC-02.1: A later tool-result handler can inspect the immutable original result and either preserve or discard changes made by an earlier handler.
- ACC-03: Active-tool changes affect the next model request and do not mutate a request already in progress.
- ACC-04: Agent Core skips the next automatic model request only when every completed result in the active batch has `terminate`.
- ACC-05: Queued messages follow their delivery points and selected queue mode.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Concurrent tool completion can reorder persisted results. Execute concurrently while storing final tool-result messages in source-call order.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [Agent Core tool loop](../../../../../../host/internal/usecase/agent/run/service.go) - Agent Core tool loop.
