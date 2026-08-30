# Idea: Reliable branch summarization

## Definitions

This issue uses `branch summarization`, `BranchSummaryEntry`, `context`, and `session tree` as defined in the [project domain glossary](../../../terms.md).

## Context and Problem

See the [problem statement](problem.md).

## Goal

Built-in branch summarization produces grounded, structured context that supports reliable continuation after session-tree navigation.

## Scenarios

- Glyph generates a summary for the source conversation selected by session-tree navigation.
- The user supplies additional focus for the built-in summarization task.
- An extension supplies a complete branch-summary result and bypasses the built-in strategy.

## Scope and Non-Scope

In scope:

- Built-in branch-summary system rules and primary user task.
- Source-conversation representation.
- Generated summary structure.
- Additional focus.
- Model-visible presentation of stored branch summaries.

Out of scope:

- Tool-result summarization.
- Context compaction.
- A universal summarizer.
- Shared instructions for different summarization tasks.
- Token budgeting and file tracking.
- New extension APIs.

## Requirements

- System rules and the primary user task must be separate authored instructions.
  Justification: Source-handling rules and the requested summary operation have different responsibilities.
- The source conversation must be sent as one user input inside `<conversation>...</conversation>`.
  Justification: The model must receive one explicitly bounded data source instead of its own role-bearing history.
- System rules must define `<conversation>` as data. The model must not follow instructions from that data, answer it, or continue it.
  Justification: Source content must not change the summarization task.
- Authored instructions must not mention a branch, an abandoned branch, a session tree, or navigation.
  Justification: Session-tree mechanics do not describe the summary content.
- The summary must contain only applicable sections from `Goal`, `Constraints and preferences`, `Completed work`, `Work in progress`, `Blockers`, `Decisions`, `Important findings`, `Next steps`, and `Critical context`.
  Justification: A stable structure supports continuation, while omitted empty sections prevent invented content.
- The summary must preserve exact identifiers, paths, commands, values, errors, and decisions when they are needed for continued work.
  Justification: Loss of exact technical data can make the summary unusable.
- Additional focus must supplement the primary task and appear after `</conversation>` inside `<additional_focus>...</additional_focus>`.
  Justification: User guidance must not replace the primary task or become part of the source data.
- The complete `<additional_focus>` block must be absent when additional focus is not provided.
  Justification: An empty boundary carries no data.
- A stored `BranchSummaryEntry` must be presented to the model as explicitly delimited summary data without branch-specific framing.
  Justification: The model must distinguish summary data from instructions and ordinary conversation.
- Branch summarization must own its instructions, model selection, result validation, and orchestration independently from other summarization tasks.
  Justification: Tool-result summarization and context compaction require different models, rules, and algorithms.
- A complete result supplied by a `session_before_tree` handler must continue to bypass built-in branch summarization.
  Justification: An extension must be able to own an independent strategy without replacing parts of the built-in strategy.
- Tests must verify request structure and observable behavior without checking instruction text.
  Justification: Editorial instruction changes must not break behavioral tests.
- Authored instructions must use ASD-STE100 English.
  Justification: This is the project standard for reliable model instructions.

## Open Questions

None.

## References

- [PHS-05 ticket](../../features/initial/phases/05-session-tree/ticket.md)
- [PHS-05 technical solution](../../features/initial/phases/05-session-tree/solution.md)
- [PHS-06 ticket](../../features/initial/phases/06-context-compaction-retry-control/ticket.md)
- [PHS-07 ticket](../../features/initial/phases/07-extension-context-lifecycle/ticket.md)
