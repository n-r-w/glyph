# Problem Statement: Compaction source differs from conversation context

Status: Problem statement, [requirements](prd.md), and [technical solution](solution.md) approved. Design dry run is complete; implementation has not started.

## Context

Glyph keeps original session records while using prepared history for model requests. After a provider failure, the session can contain a partial assistant response that Glyph excludes from subsequent conversation context. Later, the user can request compaction, or the standard extension can start compaction as the conversation grows.

Terminology follows the [domain glossary](../../../terms.md), particularly `context compaction`, `prepared history`, and `session`.

## Observed Problem

The standard compaction extension selects summary source and recent history from original session-entry projections. These projections contain partial failed responses without their terminal outcome. Consequently, the extension can treat a response excluded from conversation context as an ordinary assistant message.

The extension can therefore send discarded response text to the summary generator and count that text toward the recent-history retention target. Compaction then operates on content different from the context of the conversation being compacted.

## Affected Audience

Users who continue a conversation after a failed model request can receive a summary generated from discarded assistant output. They can also retain less recent unsummarized conversation content than their configured target calls for.

## Evidence

During PHS-06.4 review at revision `7e11ef6`, public Programmatic Control probes reproduced both effects with the bundled compaction executable. The probes exercised production application, SDK, runtime, session persistence, and provider-adapter wiring. Only external provider HTTP transport was mocked.

- After a failed model response, an ordinary continuation request excluded its partial text. A subsequent standard manual compaction sent that text to the summary generator as assistant source.
- In automatic compaction, a failed response containing 3,000 ASCII characters contributed to selection of the retained suffix. With a 4,500-token retention target, the committed result retained a usable recent message estimated at 4,006 tokens. Glyph excluded the failed response from the actual outgoing context.

These probes establish which content reached generation and retention selection. They do not establish what a live model would write in the resulting summary.

Source locations explain the observed path:

- `host/internal/usecase/agent/run/history.go`, `ProjectHistory`, excludes failed and aborted model responses from prepared history.
- `host/internal/usecase/host/sessions/manual_compaction.go`, `Service.CaptureCompaction`, captures original entries and prepared history separately.
- `host/internal/usecase/host/extensionruntime/handler_projection.go`, `ProjectTreeEntry`, projects original model content without its terminal outcome.
- `plugins/extension/compaction/internal/domain/compaction/retention.go`, `SelectRequest`, selects summary source and retained entries from original-entry projections.

## Impact

The summary generator receives text that Glyph excluded from the conversation context. That text can influence the summary, although the probes do not measure this effect on a live provider.

When discarded text contributes to the retained-history estimate, the extension can reach the configured target before retaining enough usable conversation content. As a result, older useful messages can enter the summary earlier than the target calls for.

Original session records remain stored. The observed defect concerns summary source and retained context rather than loss of the journal.

## Current State

Original-entry projections retain session-entry identifiers. Prepared history contains the model-facing content but has no original-entry identifiers. The standard extension uses the original-entry projections to select the boundary that Host later applies to the session.

Prepared history also includes existing transformations for tool results and earlier summaries. Those transformations matter to the relation between original records and conversation context. The failed-response probes do not establish a separate defect in every such transformation.

## Desired State

Users receive compaction summaries based on their conversation context and retain recent details without excluded responses affecting that selection. The original session journal remains available to them.

## Problem Boundary

The problem concerns agreement between prepared conversation history and the history used for standard manual, automatic, and overflow compaction, including repeated compaction. The failed partial response is a reproduced example of that disagreement.

The separate participant-cancellation defect found during PHS-06.4 is outside this problem. The approved standard-generator limits in [PHS-06.2 FRQ-08](../../features/initial/phases/06-context-compaction-retry-control/phs-06.2_prd.md#functional-requirements), including image omission and tool-result text truncation, remain unchanged.

## Open Questions

None at the problem-definition level. See the [technical solution](solution.md#design-dry-run-evidence) for design-validation evidence.
