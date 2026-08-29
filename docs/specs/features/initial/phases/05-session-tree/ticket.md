# Ticket: PHS-05 - Session tree

Support branch-preserving session navigation.

## Key definitions and abbreviations

- DEF-01: Active leaf. The session-tree entry from which later entries continue.
- DEF-02: Branch summarization. Creation of a summary for entries on the branch that the user leaves during tree navigation.
- DEF-03: `BranchSummaryEntry`. The persisted result of branch summarization.
- DEF-04: `session_before_tree`. The transforming extension point before tree navigation and branch summarization.
- DEF-05: `session_tree`. The extension event emitted after tree navigation and any `BranchSummaryEntry` persistence commit.
- DEF-06: Navigation result. The client-neutral tree-navigation outcome containing the committed active leaf and optional next-input text.
- DEF-07: Navigation destination. The session-tree position selected before an optional `BranchSummaryEntry` becomes the active leaf.

## Problem Statement

- PRB-01: A linear session cannot preserve alternate continuations, navigate prior entries, label branches, or perform branch summarization.
- PRB-02: Extensions cannot compose with or replace branch summarization before tree navigation.

## Target Picture

- SOL-01: Support branch-preserving session navigation with Host-validated extension control over branch summarization.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 and DEP-02 are met.
- Trigger: the user continues from an earlier session entry.
- Required behavior: Glyph creates a new branch, preserves the preceding branch, and allows navigation between both branches.
- Example input and expected output: Input: navigate from leaf `e20` to earlier entry `e10` and submit new text. Expected output: a new child of `e10` becomes active while the branch ending at `e20` remains selectable.

### SCN-02: Extension-controlled branch summarization

- Actor: extension author.
- Pre-condition: DEP-01 and DEP-02 are met and two `session_before_tree` handlers are active.
- Trigger: the user navigates away from a branch with branch summarization enabled.
- Required behavior: each handler receives the immutable original request and current request and result from preceding handlers, can select another configured model for branch summarization, Host validates and commits the final `BranchSummaryEntry` with navigation, and `session_tree` reports the committed navigation.
- Example input and expected output: Input: handler A selects a cheaper configured model and changes the custom focus, handler B provides a branch summarization result, and a result handler refines it. Expected output: later handlers observe preceding current values, the built-in summarizer does not run, and the refined `BranchSummaryEntry` is persisted with the selected active leaf.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No context compaction or extension session context.
- OSP-02: No public operation or client projection for model-visible extension messages. [PHS-07](../07-extension-context-lifecycle/ticket.md) owns that behavior.

## Dependencies and Preconditions

- DEP-01: [PHS-04](../04-persistent-linear-sessions/ticket.md) must meet all acceptance criteria.
- DEP-02: [PHS-04.1](../04.1-model-execution-capabilities/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Support branch-preserving session navigation.

### Functional Requirements

- FRQ-01: Every session entry shall record its parent relation. A session shall store one tree and one active leaf.
- FRQ-01.1: The tree, active leaf, entry labels, and `BranchSummaryEntry` values shall survive application restart.
- FRQ-01.2: Continuing from an earlier entry shall create a new branch without modifying or deleting another branch.
- FRQ-01.3: Selecting a user message shall use its parent as the navigation destination and shall return its exact text as editable next input. Selecting a root user message shall use an empty conversation as the navigation destination. Glyph shall not submit the returned text automatically.
- FRQ-01.4: Selecting another entry shall use that entry as the navigation destination and shall return no user input.
- FRQ-01.5: Tree navigation during an active agent run shall return `busy` and shall not change the session.
- FRQ-01.6: Forking shall create a session from the source path through the parent of a selected user message and shall return that message's text. Cloning shall create a session from the complete active branch.
- FRQ-02: After target selection, the standard TUI shall offer `No summary`, `Summarize`, and `Summarize with custom prompt`. `No summary` shall be the default.
- FRQ-02.1: `No summary` shall create no `BranchSummaryEntry`.
- FRQ-02.2: Branch summarization shall cover the abandoned path from the first entry after the last common ancestor through the preceding active leaf.
- FRQ-02.3: The built-in summarizer shall use a snapshot of the active provider, model, and reasoning choice by default.
- FRQ-02.4: `session_before_tree` shall let an extension select another configured provider, model, and reasoning choice or provide a completed branch summarization result.
- FRQ-02.5: Host shall validate the final summary model selection and shall resolve provider credentials without exposing them to the extension.
- FRQ-02.6: Request and result handlers shall run in registration order with immutable original and current values. An ordinary error shall preserve the current value received by that handler. Cancellation shall stop the operation.
- FRQ-02.7: An operation-ending error, cancellation, inconsistent branch summarization mode and result, model failure, validation failure, or persistence failure shall preserve the preceding active leaf and shall create no `BranchSummaryEntry`.
- FRQ-02.8: When branch summarization produces a `BranchSummaryEntry`, Host shall attach it as a child of the navigation destination and make it the active leaf. Without a `BranchSummaryEntry`, the navigation destination shall become the active leaf. Host shall commit this state in one storage operation, then emit `session_tree`.
- FRQ-02.9: A `BranchSummaryEntry` shall store its branch boundary, provider, model, reasoning choice, and explicit optional states for normalized token usage and persisted estimated cost. Usage shall be absent when the provider does not report it. Estimated cost shall be absent when usage or configured pricing is unavailable.
- FRQ-03: The UI Plugin Contract and Programmatic Control contract shall expose typed tree commands and results without terminal-specific concepts.
- FRQ-03.1: The standard TUI shall implement the tree, search, filters, branch folding, and labels according to [the standard interaction baseline](../../tui-defaults.md).
- FRQ-03.2: PHS-05 shall preserve extension entries in their branches. PHS-07 shall add creation and client presentation of model-visible extension messages with the same resubmission behavior as user messages.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by `go fix`, `task lint`, and `task test`.
- NFQ-02: Agent Core must remain independent of the session tree, protobuf, gRPC, extensions, persistence, and UI packages.
- NFQ-03: Every built-in branch-summarization prompt shall be stored in a Markdown file and embedded into its owning Go binary with `//go:embed`. Go source shall contain no built-in prompt text.

### Deliverables

- DLV-01: Persistent session tree and `BranchSummaryEntry` model.
- DLV-01.1: Public `session_before_tree` and `session_tree` contracts with a reference extension that composes with another branch summarization extension.
- DLV-02: Standard TUI session tree interaction.

### Acceptance Criteria

- ACC-01: Continuing from an earlier entry creates a new branch without deleting the previous branch.
- ACC-02: Selecting a user message returns its exact text without starting an agent run and uses its parent as the navigation destination. Selecting another entry uses that entry as the destination and returns no next-input text. Without branch summarization, the destination becomes the active leaf. With branch summarization, the created `BranchSummaryEntry` becomes the active leaf. Both supported Glyph client kinds observe the same navigation result.
- ACC-03: Tree navigation during an active run returns `busy`, preserves the active leaf, and writes no entry.
- ACC-04: No-summary navigation writes no `BranchSummaryEntry`. Built-in and custom-focus navigation summarize only the entries after the last common ancestor through the preceding active leaf.
- ACC-05: Two request handlers receive the same original navigation request, while the second receives the current request and current result returned by the first. An extension can select another configured model or provide a result that prevents the built-in summarizer from running. Ordered result handlers can preserve or replace that result.
- ACC-05.1: Clearing an extension-provided result makes the built-in summarizer run with the final validated summary model selection when the final current request requires branch summarization.
- ACC-06: Cancellation, an inconsistent no-summary result, model failure, an invalid final result, or persistence failure preserves the active leaf and writes no `BranchSummaryEntry`. A successful commit emits one `session_tree` event.
- ACC-07: Forking copies only the path through the selected user message's parent and returns its text. Cloning copies only the complete active branch. Neither operation changes the source session.
- ACC-08: The active leaf, labels, parent-child relations, retained extension entries, and each `BranchSummaryEntry` branch boundary, provider, model, reasoning choice, usage state, and estimated-cost state survive restart.
- ACC-09: The standard TUI supports tree search, filters, branch folding, labels, and next-input editor placement. Programmatic Control exposes equivalent typed tree operations and the same client-neutral results.

## Overengineering and Overspecification Considerations

The ticket uses one `session_before_tree` operation for built-in and extension-provided branch summarization. Original and current values provide composition and replacement without a separate override API, separate summary-model setting, or load-order winner. OSP-01 and OSP-02 remain outside the ticket.

## Constraints and Risks

- RSK-01: Tree operations can leave partial state after storage failure. Apply each navigation, active-leaf change, and `BranchSummaryEntry` append atomically at the session-storage boundary.
- RSK-02: An extension can return a summary for the wrong source branch or target. Host validates the final branch boundary and target before persistence.
- RSK-03: An extension can select a missing model, unsupported reasoning choice, or unavailable credentials. Host validates the complete summary model selection before model execution and persistence.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [target architecture](../../architecture.md) - Host extension composition and session ownership.
- REF-04: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/extensions.md` - feature evidence for `session_before_tree` and `session_tree`.
