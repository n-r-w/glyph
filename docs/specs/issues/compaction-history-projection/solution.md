# Technical Solution: Compaction of prepared conversation history

Status: D1 shared projection and D2 prepared-history retention are approved for PHS-06 completion, together with FRQ-01 through FRQ-03. Completion-plan A4 implements the history-source migration with original source-kind metadata. Its [implementation evidence](../../features/initial/phases/06-context-compaction-retry-control/a4-prepared-history-evidence.md) records verification and remaining acceptance conditions. A4 is accepted after code-quality, text-quality, and independent unit review under the recorded exceptions. The [whole-phase review](../../features/initial/phases/06-context-compaction-retry-control/acceptance-report.md) retains restored-suffix evidence finding F20; full PHS-06 acceptance remains blocked. The [parent solution](../../features/initial/phases/06-context-compaction-retry-control/solution.md#phs-064-completion-corrections) owns D3 terminal-failure preservation and D4 phase-wide acceptance.

## Problem Statement

See the approved [problem statement](problem.md) and [requirements](prd.md). The following approach addresses their summary-source, retention, and journal-preservation requirements.

## Proposed Solution

### D1: One shared history projection with source correspondence

Extend the existing shared model-history projection to retain the correspondence between each output message and the caller input that produced it. Ordinary model execution and compaction use the same filtering, ordering, and skipped-result synthesis. The projection expresses correspondence in terms of its input positions, so Agent Core needs no session-entry identifiers or persistence types.

The session owner retains the association between its projection inputs and original branch entries. Host combines that association with projection output to supply prepared content with its original session reference and original branch order. This association is constructed during conversion and projection; Host does not reconstruct it by comparing message text.

This changes the output contract of the shared projection rather than adding a compaction-specific visibility flag. A failed or aborted response produces no prepared message, so the extension has no discarded response content to select or count.

In `host/internal/usecase/agent/run/history.go`, `ProjectHistory` owns response exclusion, result ordering, and skipped-result synthesis. In `host/internal/usecase/host/sessions/entry_history.go`, `historyFromEntries` retains original entry identity, position, and kind during conversion. Host binds the shared projection's output positions to those captured sources. Together, these operations address FRQ-01 and FRQ-02.

### Compaction receives prepared content with original-session references

Replace the public compaction invocation's parallel raw-entry and content-only history representations with one prepared-history representation carrying source correspondence. Retain the existing preceding-compaction information used for repeated compaction and accounting, and identify its rendered contribution within prepared history unambiguously.

Migrate compaction request prefixes and suffixes to that same prepared-history representation. Otherwise the initial policy input would be corrected while handlers and generators still exchanged the old raw-entry representation. Custom request handlers continue to transform current requests and supply complete results; original request and result values remain immutable under the existing composition semantics.

Session-tree and ordinary lifecycle-event contracts retain their purposes. This correction does not add a flag to a general event stream. Host retains original entries internally for session validation and persistence. The correction changes what the compaction source represents; it does not delete original records or add an alternative source-selection fallback.

The bundled extension and the public reference extensions migrate together with their compaction contract consumers. The project permits coordinated breaking changes and requires removal of obsolete contract representations. No compatibility adapter is proposed.

### D2: Source correspondence and retained-history boundaries

An ordinary projected message refers to its original stored entry. A synthesized skipped-tool result refers to the model response whose tool call caused it; the synthetic result has no invented stored entry identifier. A rendered previous compaction summary refers to the compaction marker that supplied it. Several prepared messages can therefore share one source reference. The reference identifies their origin, not the identity of each prepared message.

The correspondence includes the original entry kind. Prepared user-role content can originate from an original user, a model-visible extension message, a branch-summary entry, or a compaction marker. Reference policies use that captured kind to preserve latest-original-user boundary selection.

The standard extension chooses a retained boundary only where the prepared-history split corresponds to a split of the original branch. It keeps response/result groups intact and includes intervening stored entries when necessary to preserve that correspondence. Original order is needed because the shared projector can reorder results ahead of intervening messages.

For example:

```text
Original branch: response A, message B, tool result C, recent message D
Prepared history: response A, tool result C, message B, recent message D
```

Summarizing A and C while preserving B would not correspond to a stored-branch prefix: B lies between A and C. The extension therefore treats A, B, and C as an indivisible span for this boundary decision. It can preserve that span or summarize it and retain D, subject to the existing retention target. Host continues to validate the selected stored-entry boundary before mutation.

The shared projector supplies content and source correspondence. The extension derives legal source spans and selects among them using its retention policy. Core does not gain compaction budgets or a boundary-selection algorithm.

### Repeated compaction and estimates

The extension evaluates the complete prepared input for the existing automatic threshold. The rendered preceding summary contributes once to that estimate. For recent-history retention, the extension selects from the remaining unsummarized history and excludes the preceding summary from the recent-history target.

For generation, the extension supplies the preceding summary once and the newly selected prepared prefix. The rendered summary is not also copied into that prefix. Superseded original prefixes remain in the journal but do not return to summary source.

For example, after an earlier compaction, prepared history contains summary S and messages A, B, C. A subsequent compaction that preserves C supplies S together with A and B to generation. The next conversation context contains the new summary and C.

The approved content estimator, generator source limits, usage-observation rules, and pricing remain unchanged. Source references, original kinds, and branch positions are control metadata and do not add model tokens or enter summary prompt text. Usage continuation comparisons remain comparisons of the actual prepared model input.

### Session capture and durable replacement

Construct the prepared view and its source correspondence from the same detached branch capture under the existing session synchronization. This keeps content, original order, active leaf, and preceding summary consistent. The source association belongs to that capture and is rebuilt after restart or navigation; it requires no new persistent session format.

The extension continues to select a stored first-kept entry and request candidate projection and replacement through existing operation-bound services. Host retains structural validation, concurrent-mutation rejection, one-commit authority, per-request accounting, and receipt publication. Candidate and post-commit projections use the same shared history rules as ordinary model execution.

Thus FRQ-03 retains the journal and durable replacement mechanics. The correction changes the source used for selection and generation, together with the correspondence needed to select a valid existing boundary.

### Alternatives and trade-offs

| Approach | Benefit | Cost or limitation | Assessment |
| --- | --- | --- | --- |
| Shared prepared projection with caller-relative source correspondence, then Host binding to original records | Content selection and ordinary model input use one algorithm; synthetic and reordered messages retain their origin | Changes the shared projection result and the public compaction representation; all compaction consumers need coordinated migration | Selected; addresses the source mismatch and valid boundary selection together |
| Add response outcome or visibility metadata to raw compaction entries | Smaller correction for the reproduced failed-response case | Leaves the extension reconstructing model-visible history; filtering, ordering, and skipped-result handling can continue to differ | Not selected; addresses the observed exclusion case without making prepared history the compaction source |
| Match prepared messages to original entries by content inside the extension | Avoids changing the public contract | Repeated message text and generated results make correspondence ambiguous | Not selected; source identity cannot be inferred reliably from content |

No separate generic provenance framework, durable metadata store, Host token estimator, or new retention configuration is proposed. The source correspondence is the output of the existing history projection and is consumed by the existing session/compaction boundary.

## Overengineering and Overspecification Considerations

The solution adds only the source information needed to translate prepared-history selection into an existing session boundary. It reuses the current projector and keeps the original storage and commit mechanism. It does not move compaction policy into Host or add storage knowledge to Agent Core.

Exact types, field names, and code organization belong to implementation design. Acceptance covers excluded responses, interleaved tool results, synthesized skipped results, repeated compaction, restoration, and navigation because those transformations affect source correspondence. The [parent solution's D4 acceptance method](../../features/initial/phases/06-context-compaction-retry-control/solution.md#d4-public-boundary-acceptance) owns the complete phase verification scope. Explicit Cancel actions retain the approved terminal behavior; the observed participant error containing `context.Canceled` establishes no separate violation of that behavior.

## Open Questions

None at the requirements or technical-solution level.

## References

- [Problem statement](problem.md): approved problem.
- [Requirements](prd.md): approved FRQ-01 through FRQ-03.
- `host/internal/usecase/agent/run/history.go`, `ProjectHistory`: shared history rules.
- `host/internal/usecase/host/sessions/entry_history.go`, `historyFromEntries` and `compactedHistoryFromEntries`: session-to-history conversion and previous-summary projection.
- `host/internal/usecase/host/sessions/manual_compaction.go`, `Service.CaptureCompaction`: captured branch, history, and preceding compaction.
- `host/internal/controller/extension/compaction_history.go`, `MapCompactionHistory`: content-only public prepared history.
- `api/plugins/extension/v1/compaction.proto`: policy invocation and composed-request representations.
- `plugins/extension/compaction/internal/domain/compaction/retention.go`, `SelectRequest`: standard retention selection.
- `plugins/extension/compaction/internal/domain/compaction/entry.go`, `findGroupStarts`: response/result grouping and legal original-order spans.
