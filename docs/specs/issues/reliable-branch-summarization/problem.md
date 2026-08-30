# Problem Statement

## Context

PHS-05 added branch summarization so navigation can preserve useful context from the conversation path that is no longer active. The generated `BranchSummaryEntry` becomes model-visible context for continued work.

## Problem Statement

Built-in branch summarization does not produce sufficiently reliable summaries for continued work because its request does not clearly separate summarization instructions from source conversation data and does not define a stable summary structure.

## Who is affected

Glyph users who continue work after navigation creates a `BranchSummaryEntry` are affected.

## Evidence

- User evaluation found that generated branch summaries do not preserve and organize enough information for reliable continuation.
- `host/internal/usecase/host/sessiontree/history.go` converts source entries into ordinary role-bearing model history.
- `host/internal/usecase/host/sessiontree/prompts/branch_summary.md` uses a free-form branch-specific instruction without a stable output structure.
- APC-06 in `docs/specs/features/initial/phases/05-session-tree/solution.md` requires one serialized user input, but the current request uses multiple history entries.

## Impact

An incomplete or poorly organized `BranchSummaryEntry` can omit goals, constraints, decisions, progress, or exact technical context. The next model request can then repeat work, use an obsolete assumption, or require the user to restore context manually.

## Reproduction Steps

1. Create a session path that contains goals, constraints, completed work, decisions, and exact technical details.
2. Navigate to another path with branch summarization enabled.
3. Inspect the generated `BranchSummaryEntry` and the next model-visible context.

## Current State

The built-in strategy sends source entries as ordinary model history and asks for a free-form summary of an abandoned conversation branch. Prior branch summaries use branch-specific framing in model-visible context.

## Desired Outcome

Built-in branch summarization produces grounded, structured context that preserves the information needed to continue work. The request distinguishes source data from instructions and does not expose session-tree mechanics to the summarization task.

## Success Metrics

- Request-construction tests show one explicitly delimited serialized source input instead of source entries represented as model history.
- Representative summaries contain only applicable approved sections and preserve exact technical details needed for continuation.
- Stored branch summaries are presented to the model as explicitly delimited summary data without branch-specific framing.

## Scope

The problem covers built-in branch-summary instructions, source-conversation representation, generated summary structure, additional focus, and stored-summary context presentation.

## Out of Scope / Non-Goals

The problem does not cover tool-result summarization, context compaction, a universal summarizer, shared prompts for different summarization tasks, token budgeting, file tracking, or new extension APIs.

## Constraints

- Each summarization task owns its prompts, model selection, validation, and orchestration.
- Existing extension result replacement must continue to bypass the built-in branch-summary strategy.
- Tests check behavior rather than mutable prompt content.
- Authored instructions use ASD-STE100 English.

## Assumptions

Clear source boundaries and a fixed set of applicable output sections are expected to improve summary reliability. Request-construction tests and evaluation with representative conversations will verify this assumption.

## Open Questions

None.
