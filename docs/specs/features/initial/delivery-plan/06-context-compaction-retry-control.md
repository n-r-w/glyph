# Ticket: PHS-06 - Context compaction and retry control

Keep long sessions usable within model context limits.

## Key definitions and abbreviations

- DEF-01: Context compaction. Replacement of an older context prefix with a summary while preserving the remaining suffix.

## Problem Statement

- PRB-01: Long sessions have no context-budget compaction. They eventually exceed the selected model context and cannot continue through the required automatic or manual flow.

## Target Picture

- SOL-01: Keep long sessions usable within model context limits.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the active context reaches the compaction threshold.
- Required behavior: Glyph creates and persists a summary, preserves the context suffix, and continues the session.
- Example input and expected output: Input: submit a request whose projected context exceeds the response-budget threshold. Expected output: an older prefix is replaced by one persisted summary and the unchanged suffix is sent with the next provider request.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No extension replacement of compaction behavior.

## Dependencies and Preconditions

- DEP-01: [PHS-05](05-session-tree.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Keep long sessions usable within model context limits.

### Functional Requirements

- FRQ-01: Add response-budget accounting and automatic compaction.
- FRQ-02: Add manual compaction with user instructions and default summary behavior.
- FRQ-03: Add configurable retry control through Programmatic Control and the standard TUI.
- FRQ-04: Provider adapters shall classify provider responses and errors as retryable or non-retryable. Agent Core shall own retry attempts and delay scheduling. The Glyph host shall own retry-policy configuration. Glyph clients shall receive retry events and choose how to present them.
- FRQ-05: General abort shall cancel an in-progress provider request or pending retry delay and transition the agent to idle.
- FRQ-06: A retry shall repeat only the failed model request and shall not repeat any completed tool execution. Failed intermediate attempts shall produce operation events and shall not create session messages or enter model context. After retry finishes, Glyph shall persist only the terminal model outcome.
- FRQ-07: Enable retry by default with three retries after the initial request and delays of 1, 2, and 4 seconds. Cap a provider-supplied `Retry-After` delay at 30 seconds. The built-in retryable HTTP statuses shall be 408, 429, 500, 502, 503, and 504. Transport timeouts, connection resets, and unexpected connection closure before a terminal provider response shall also be retryable.
- FRQ-08: Make the enabled state, maximum retry count, ordered retry delays, `Retry-After` cap, and built-in retryable HTTP statuses configurable through the Glyph host.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Compaction use case and persisted summary entries.
- DLV-02: Manual compaction, retry-policy configuration, and retry client operations.

### Acceptance Criteria

- ACC-01: Automatic compaction replaces an older context prefix, preserves the remaining suffix, and allows the run to continue.
- ACC-02: Manual compaction applies user instructions and persists the resulting summary.
- ACC-03: A compacted session resumes after restart with the same active branch.
- ACC-04: A retryable provider failure produces client retry events and follows the configured policy. The default policy makes three retries after the initial request with delays of 1, 2, and 4 seconds.
- ACC-05: Retrying a failed model request repeats no completed tool, adds no intermediate attempt to session messages or model context, and persists only the terminal model outcome.
- ACC-06: Abort cancels an in-progress provider request or pending retry delay and leaves the agent idle.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Provider token accounting may be absent or approximate. Define one Agent Core budget calculation based on the selected model descriptor and available usage data.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
