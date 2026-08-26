# Ticket: PHS-04 - Persistent linear sessions

Persist conversations and resume them after process restart.

## Key definitions and abbreviations

- DEF-01: Persistent session. A stored sequence of terminal user, model, tool, provider-context, and extension entries.

## Problem Statement

- PRB-01: Agent history exists only in memory. Process exit loses conversation, tool, and provider context and prevents session resume.

## Target Picture

- SOL-01: Persist conversations and resume them after process restart.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user resumes a stored session after restarting Host.
- Required behavior: the provider receives the prior terminal conversation and tool history and the user continues the conversation.
- Example input and expected output: Input: create a session, complete one model and tool turn, restart Host, and resume by session ID. Expected output: the resumed provider request contains the stored terminal conversation and tool history.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No branching, compaction, or environment reload.

## Dependencies and Preconditions

- DEP-01: [PHS-03](03-providers-models-runtime-selection.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Persist conversations and resume them after process restart.

### Functional Requirements

- FRQ-01: Add persistent session storage for user messages, model responses, tool calls, tool results, provider context, and extension entry envelopes.
- FRQ-02: Add session creation, resume, naming, information queries, entries queries, and statistics with message and tool counts, normalized token usage, persisted estimated cost, and provider-model cost breakdown.
- FRQ-03: Expose session operations through Programmatic Control and the standard TUI.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Versioned persistent session format and storage adapter.
- DLV-02: Session selection and naming through both client kinds.

### Acceptance Criteria

- ACC-01: A user exits Glyph, resumes the saved session, and continues with the prior model and tool history available to the provider.
- ACC-02: Session entries preserve text, images, provider context, tool results, and model outcomes without provider DTOs in the session domain.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Persisting streaming scratch state could make sessions unrecoverable. Persist only terminal entries and the terminal outcomes defined by Agent Core.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
