# Requirements: Compaction of prepared conversation history

Status: FRQ-01 through FRQ-03 are approved for PHS-06 completion. The [technical solution](solution.md) records the approved design, implementation evidence, and remaining acceptance conditions.

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

These requirements apply to standard manual, automatic, and overflow compaction, including repeated compaction, restoration, and navigation of the selected branch. The [parent ticket](../../features/initial/phases/06-context-compaction-retry-control/ticket.md) owns the complete PHS-06 requirements, including custom composition, cancellation, committed failures, retry control, and terminal failure preservation. Its FRQ-04.8.1 covers a terminal provider failure followed by stream truncation; that correction is separate from history-source selection.

The approved [standard-generator source limits](../../features/initial/phases/06-context-compaction-retry-control/phs-06.2_prd.md#functional-requirements), FRQ-08, remain unchanged. Explicit Cancel actions retain the parent ticket's exclusive terminal semantics. A participant error containing `context.Canceled` alone does not establish a separate cancellation defect. Changes to the summary algorithm, token-estimation formula, and retention setting are outside scope.

## Requirements

### Functional Requirements

- [x] FRQ-01: When standard compaction selects conversation content for summary generation, Glyph shall select from the prepared conversation history. Responses excluded from that history shall remain excluded from summary source. On repeated compaction, Glyph shall use the preceding summary and the newly selected older portion of the remaining conversation history, subject to the approved generator source limits.
  - Origin: source. Parent ticket DEF-01, FRQ-01.2, and FRQ-01.12; standard requirements FRQ-06 and FRQ-08; the approved problem statement.
  - Goal: Keep summaries based on the context being compacted.
  - Goal achievement: Full. Summary source follows the conversation context under the approved generator limits.
  - Feasibility: Confirmed. Glyph already prepares conversation history and retains the preceding summary.
- [x] FRQ-02: When standard compaction selects the recent history to preserve, Glyph shall count only content present in the prepared conversation history toward the configured retained-history target. Existing complete-message and tool-call/result integrity rules shall remain in effect.
  - Origin: source. Parent ticket FRQ-01.11 and ACC-01.7; standard requirements FRQ-05; the approved problem statement.
  - Goal: Preserve recent conversation detail without assigning retention weight to excluded records.
  - Goal achievement: Full. Excluded responses contribute no retention weight. The existing explicit failure when no older portion is reducible remains applicable.
  - Feasibility: Confirmed. Prepared content, original records, and tool-call/result associations are available.
- [x] FRQ-03: After compaction, Glyph shall retain the original session records, including records excluded from model context. The next conversation context shall contain the accepted summary followed by the preserved history. Restoration and navigation shall preserve these properties for the selected branch under the parent ticket's session projection rules.
  - Origin: source. Parent ticket FRQ-01.13, ACC-01, ACC-01.8, ACC-01.10, and ACC-03; the approved desired state.
  - Goal: Preserve the journal and continue the conversation with the intended compacted context.
  - Goal achievement: Full. Users retain access to originals and continue with the compacted context, including after restoration and navigation.
  - Feasibility: Confirmed. Original-record persistence and compacted-context restoration are implemented and have behavioral tests.

### Non-Functional Requirements

Parent-ticket [NRQ-01](../../features/initial/phases/06-context-compaction-retry-control/ticket.md#non-functional-requirements) and the [PHS-06.4 exit criteria](../../features/initial/phases/06-context-compaction-retry-control/migration-plan.md#phase-phs-064---independent-acceptance) own the phase-wide evidence, independent acceptance, mandatory checks, and reporting of skips and evidence limits.

## Behavior Examples

- A failed response has partial assistant text. The text remains in the session journal. When Glyph excludes that response from prepared history, its text contributes neither summary source nor retained-history token estimates.
- After a second compaction, the next model request contains the updated summary and preserved suffix. It contains no additional copy of the preceding summary or superseded original prefix.

## Open Questions

None at the requirements level. The [technical solution](solution.md) records implementation and acceptance status.

## References

- [Problem statement](problem.md): approved basis.
- [PHS-06 ticket](../../features/initial/phases/06-context-compaction-retry-control/ticket.md): shared compaction and session requirements.
- [PHS-06.2 requirements](../../features/initial/phases/06-context-compaction-retry-control/phs-06.2_prd.md): standard compaction and approved generator limitations.
