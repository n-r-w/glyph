# Requirements: Compaction of prepared conversation history

Status: FRQ-01 through FRQ-03 and the [technical solution](solution.md) approved. Design dry run is complete; the correction is ready for implementation.

## Definitions

Use the existing definitions in the [domain glossary](../../../terms.md).

## Context and Problem

The approved [problem statement](problem.md) describes the difference between conversation context and the history used for compaction.

## Goal

Keep summary source selection and recent-history retention consistent with the context of the conversation while preserving the original session journal.

## Scenarios

- A user continues after a failed or aborted model response and later compacts the conversation.
- A growing conversation triggers compaction automatically or after a provider context-overflow response.
- A conversation with tool calls or an earlier compaction summary is compacted again.

## Scope and Non-Scope

These requirements apply to standard manual, automatic, and overflow compaction, including repeated compaction. The [parent ticket](../../features/initial/phases/06-context-compaction-retry-control/ticket.md) retains its requirements for custom composition and session restoration.

The approved [standard-generator source limits](../../features/initial/phases/06-context-compaction-retry-control/phs-06.2_prd.md#functional-requirements), FRQ-08, remain unchanged. The separate participant-cancellation defect remains outside this correction. Changes to the summary algorithm, token-estimation formula, and retention setting are outside scope.

## Requirements

### Functional Requirements

- [x] FRQ-01: When standard compaction selects conversation content for summary generation, Glyph shall select from the prepared conversation history. Responses excluded from that history shall remain excluded from summary source. On repeated compaction, Glyph shall use the preceding summary and the newly selected older portion of the remaining conversation history, subject to the approved generator source limits.
  - Origin: `source`, parent ticket DEF-01, FRQ-01.2, and FRQ-01.12; standard requirements FRQ-06 and FRQ-08; the approved problem statement.
  - Goal: Keep summaries based on the context being compacted.
  - Goal achievement: Partial. Glyph excludes discarded responses and previously summarized original prefixes from source selection.
- [x] FRQ-02: When standard compaction selects the recent history to preserve, Glyph shall count only content present in the prepared conversation history toward the configured retained-history target. Existing complete-message and tool-call/result integrity rules shall remain in effect.
  - Origin: `source`, parent ticket FRQ-01.11 and ACC-01.7; standard requirements FRQ-05; the approved problem statement.
  - Goal: Preserve recent conversation detail without assigning retention weight to excluded records.
  - Goal achievement: Partial. Glyph evaluates the retained-history target using usable conversation context. The existing explicit failure when no older portion is reducible remains applicable.
- [x] FRQ-03: After compaction, Glyph shall retain the original session records, including records excluded from model context. The next conversation context shall contain the accepted summary followed by the preserved history under the existing session projection rules.
  - Origin: `source`, parent ticket FRQ-01.13, ACC-01, and ACC-01.8; the approved desired state.
  - Goal: Preserve the journal and continue the conversation with the intended compacted context.
  - Goal achievement: Partial. Users retain their original records while subsequent requests use the summary and preserved context.

## Behavior Examples

- A failed response has partial assistant text. The text remains in the session journal. When Glyph excludes that response from prepared history, its text contributes neither summary source nor retained-history token estimates.
- After a second compaction, the next model request contains the updated summary and preserved suffix. It contains no additional copy of the preceding summary or superseded original prefix.

## Open Questions

None at the requirements level. The representation of the relation between prepared history and original records belongs to technical design.

## References

- [Problem statement](problem.md): approved basis.
- [PHS-06 ticket](../../features/initial/phases/06-context-compaction-retry-control/ticket.md): shared compaction and session requirements.
- [PHS-06.2 requirements](../../features/initial/phases/06-context-compaction-retry-control/phs-06.2_prd.md): standard compaction and approved generator limitations.
