# Ticket: PHS-05 - Session tree

Support branch-preserving session navigation.

## Key definitions and abbreviations

- DEF-01: Active leaf. The session-tree entry from which later entries continue.
- DEF-02: Branch summarization. Creation of a summary for entries on the branch that the user leaves during tree navigation.
- DEF-03: `BranchSummaryEntry`. The persisted result of branch summarization.
- DEF-04: `session_before_tree`. The transforming extension point before tree navigation and branch summarization.
- DEF-05: `session_tree`. The extension event emitted after tree navigation and any `BranchSummaryEntry` persistence commit.

## Problem Statement

- PRB-01: A linear session cannot preserve alternate continuations, navigate prior entries, label branches, or perform branch summarization.
- PRB-02: Extensions cannot compose with or replace branch summarization before tree navigation.

## Target Picture

- SOL-01: Support branch-preserving session navigation with Host-validated extension control over branch summarization.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user continues from an earlier session entry.
- Required behavior: Glyph creates a new branch, preserves the preceding branch, and allows navigation between both branches.
- Example input and expected output: Input: navigate from leaf `e20` to earlier entry `e10` and submit new text. Expected output: a new child of `e10` becomes active while the branch ending at `e20` remains selectable.

### SCN-02: Extension-controlled branch summarization

- Actor: extension author.
- Pre-condition: DEP-01 is met and two `session_before_tree` handlers are active.
- Trigger: the user navigates away from a branch with branch summarization enabled.
- Required behavior: each handler receives the immutable original request and current request and result from preceding handlers, Host validates and commits the final `BranchSummaryEntry` with navigation, and `session_tree` reports the committed navigation.
- Example input and expected output: Input: handler A changes the custom focus, handler B provides a branch summarization result, and a result handler refines it. Expected output: later handlers observe preceding current values, the built-in summarizer does not run, and the refined `BranchSummaryEntry` is persisted with the selected active leaf.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No context compaction or extension session context.

## Dependencies and Preconditions

- DEP-01: [PHS-04](04-persistent-linear-sessions.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Support branch-preserving session navigation.

### Functional Requirements

- FRQ-01: Add parent-child entry relations, active leaf, fork, clone, switch, tree navigation, labels, and session information.
- FRQ-02: Add tree navigation with no branch summarization, built-in branch summarization, and branch summarization with custom focus, including usage accounting owned by each `BranchSummaryEntry`.
- FRQ-02.1: Before tree navigation, Host shall create an immutable original request and an equal current request containing the source branch, target entry, branch summarization choice, custom focus, and cancellation capability.
- FRQ-02.2: `session_before_tree` request handlers shall run in registration order. Each handler shall receive the original request, current request, and current result when one exists.
- FRQ-02.3: A request handler shall be able to preserve or replace the current request, preserve, set, replace, or clear the current result, or cancel navigation. A handler can derive its returned state from the original request, current state, or both. Cancellation shall be terminal and shall stop later branch summarization handlers.
- FRQ-02.4: When request handlers end without a result and the final current request selects built-in branch summarization or custom focus, Host shall run the built-in summarizer. No-summary navigation shall create no `BranchSummaryEntry`.
- FRQ-02.5: After a result exists, result handlers shall run in registration order with the original request, final current request, immutable original result, and current result returned by preceding handlers. A result handler shall preserve, replace, or cancel the result. Cancellation shall be terminal and shall stop later branch summarization handlers.
- FRQ-02.6: An invalid handler action or ordinary handler error shall be reported, shall preserve the state received by that handler, and shall not stop later handlers or deactivate the extension.
- FRQ-02.7: Host shall validate the final tree target, summary content, branch boundary, and usage shape before it atomically commits the active-leaf change and any `BranchSummaryEntry`.
- FRQ-02.8: Host shall emit `session_tree` only after the navigation and any `BranchSummaryEntry` commit. Cancellation, final validation failure, or persistence failure shall emit no `session_tree` event and shall change neither the active leaf nor persisted entries.
- FRQ-03: Add complete tree presentation and navigation to the standard TUI and equivalent Programmatic Control commands.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Persistent session tree and `BranchSummaryEntry` model.
- DLV-01.1: Public `session_before_tree` and `session_tree` contracts with a reference extension that composes with another branch summarization extension.
- DLV-02: Standard TUI session tree interaction.

### Acceptance Criteria

- ACC-01: Continuing from an earlier entry creates a new branch without deleting the previous branch.
- ACC-01.1: Two request handlers receive the same original navigation request, while the second receives the current request and current result returned by the first.
- ACC-01.2: An extension-provided result prevents the built-in summarizer from running, and ordered result handlers can preserve or replace that result.
- ACC-01.3: Clearing an extension-provided result makes the built-in summarizer run when the final current request requires branch summarization.
- ACC-01.4: Cancellation, an invalid final result, or persistence failure preserves the active leaf and writes no `BranchSummaryEntry`. A successful commit emits one `session_tree` event.
- ACC-02: Selecting user or model-visible extension content places the expected text in the editor and moves the active leaf to the required parent.
- ACC-03: Labels and `BranchSummaryEntry` values survive restart.

## Overengineering and Overspecification Considerations

The ticket uses one `session_before_tree` operation for built-in and extension-provided branch summarization. Original and current values provide composition and replacement without a separate override API or load-order winner. OSP-01 remains outside the ticket.

## Constraints and Risks

- RSK-01: Tree operations can leave partial state after storage failure. Apply each navigation, active-leaf change, and `BranchSummaryEntry` append atomically at the session-storage boundary.
- RSK-02: An extension can return a summary for the wrong source branch or target. Host validates the final branch boundary and target before persistence.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [target architecture](../architecture.md) - Host extension composition and session ownership.
- REF-04: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/extensions.md` - feature evidence for `session_before_tree` and `session_tree`.
