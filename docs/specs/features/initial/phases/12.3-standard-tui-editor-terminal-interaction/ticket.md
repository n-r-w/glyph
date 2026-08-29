# Ticket: PHS-12.3 - Standard TUI editor and terminal interaction

Provide the standard editor, completion, clipboard, selector, queued-input, and terminal-lifecycle behavior required for daily terminal-agent use.

## Key definitions and abbreviations

- DEF-01: Pending input. Editor text and typed file or image attachments not yet submitted as a user message.
- DEF-02: Prompt history. Submitted user text available for temporary recall into the editor without changing session entries.
- DEF-03: Selector. A focused list or search interface for commands, models, sessions, themes, or another closed set of choices.
- DEF-04: Supported input image. A JPEG, static PNG, GIF, WebP, or BMP file identified from its content rather than its filename.

## Problem Statement

- PRB-01: The standard TUI editor currently accepts a single flattened line and a small fixed key set. It has no multiline editing, history, completion, attachments, clipboard semantics, external editor, reusable selector behavior, hotkey discovery, suspension, or complete terminal restoration contract.

## Target Picture

- SOL-01: The user can compose and revise rich pending input, operate every built-in selector, manage queued messages, discover configured keys, and recover a usable terminal after every lifecycle outcome.

## Scenarios

### SCN-01: Compose and revise rich input

- Actor: standard TUI user.
- Pre-condition: DEP-01 is met.
- Trigger: the user composes a multiline prompt with a project file and pasted image, opens an external editor, and submits the result while another run is active.
- Required behavior: the standard TUI preserves text and attachments, exposes completion and history, queues the selected message kind, restores terminal modes after suspension, and disables them before normal exit.
- Example input and expected output: Input: type two Unicode lines, complete a project path, attach one PNG, cancel one external-editor attempt, save a second attempt, queue as follow-up, restore it to the editor, then submit. Expected output: no text or attachment is lost, cancellation preserves the prior editor value, and the queued message is delivered only at its selected delivery point.

## Scope

In scope:
- ISP-01: Editor, completion, queued-input, selector, keybinding, hotkey, lifecycle, and error requirements FRQ-21 through FRQ-42 in [`standard-tui.md`](../../standard-tui.md).
- ISP-02: Removal of the public `controls_terminal` capability and its complete Host, SDK, and standard TUI mapping path.

Out of scope:
- OSP-01: Agent queue business rules, provider and session selectors' domain rules, extension-replaced editors, extension autocomplete, extension shortcuts, and future UI plugins.
- OSP-02: Any replacement Host terminal-ownership capability, terminal-state inspection, snapshot, reset, restoration, or automatic TUI restart.

## Dependencies and Preconditions

- DEP-01: [PHS-12.2](../12.2-standard-tui-viewport-navigation/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Deliver a configurable multiline terminal editor and reusable terminal interaction behavior without moving queue, session, provider, or Agent Core rules into the standard TUI.

### Functional Requirements

- FRQ-01: Support multiline Unicode editing, character, word, line, and page movement, character and word deletion, deletion to line boundaries, undo, and restoration of recently deleted text.
- FRQ-02: Recall older and newer DEF-02 values without changing session entries until submit.
- FRQ-03: Keep submit and insert-newline as separate configurable actions.
- FRQ-04: Complete Host-discoverable commands, filesystem paths, and fuzzy project-file references into DEF-01.
- FRQ-05: Paste multiline text without automatic submission and attach pasted or dropped DEF-04 images as typed user content.
- FRQ-06: Round-trip complete editor text through an external editor and preserve preceding text on cancellation or failure.
- FRQ-07: Copy active editor selection and selected transcript messages as source text.
- FRQ-08: Expose steering, follow-up, and restore-to-editor actions for queued messages while preserving Agent Core delivery ownership.
- FRQ-09: Keep clear, abort, and exit as distinct actions and preserve unsent DEF-01 when aborting a run.
- FRQ-10: Make every DEF-03 support item movement, page movement, confirm, cancel, selected-item visibility, and filtering for searchable sources.
- FRQ-11: Restore editor text, viewport position, and focus after selector or dialog cancellation.
- FRQ-12: Apply configurable keys from [`tui-defaults.md`](../../tui-defaults.md) and expose active command and key help.
- FRQ-13: Recompute editor wrapping, selector geometry, and focused-region placement after terminal resize.
- FRQ-14: Restore application input modes, viewport, editor, and focus after suspension and resume.
- FRQ-15: Before normal exit, the standard TUI shall disable alternate screen, mouse reporting, bracketed paste, focus reporting, and application keyboard modes that it enabled. Host shall provide no fallback terminal cleanup after unexpected TUI termination.
- FRQ-16: Remove `controls_terminal` from `api/plugins/ui/v1/ui.proto` and regenerate the public Go contract. Because it is the only startup capability, also remove the `GetCapabilities` RPC and messages, SDK capability retrieval and caching, `ui.Capabilities`, runtime capability mapping, selection capability state and logging, standard TUI capability responses, and their obsolete test setup. Add no replacement capability or compatibility path.

### Non-Functional Requirements

- NFQ-01: Focused editor and terminal-lifecycle tests must demonstrate RED and GREEN, followed by passing `task lint` and `task test`.
- NFQ-02: Clipboard, external-editor, completion, and attachment errors must remain visible and leave the editor usable.
- NFQ-03: At every positive terminal size, rendering must not panic. The editor line and one status line take precedence when the complete layout does not fit.
- NFQ-04: TUI state and errors must not expose provider credentials, authorization headers, OAuth verifier values, or credential-file contents.

### Deliverables

- DLV-01: Multiline editor, history, completion, attachments, clipboard, external-editor, and queued-input presentation.
- DLV-02: Reusable selector and dialog interaction behavior.
- DLV-03: Configurable action dispatch, hotkey help, suspension, resume, resize, and TUI-owned terminal cleanup. Existing Host terminal snapshot and recovery behavior is removed.
- DLV-04: UI Plugin Contract, SDK, Host selection, and standard TUI cleanup with no `controls_terminal`, `GetCapabilities`, or internal terminal-capability mapping.

### Acceptance Criteria

- ACC-01: Multiline Unicode input survives cursor movement, undo, history recall, external-editor cancellation, and successful external-editor replacement.
- ACC-02: Command, path, and project-file completion each insert the selected value without submitting.
- ACC-03: Multiline paste preserves newlines and image paste or drop creates typed attachment content.
- ACC-04: Steering and follow-up messages use their Agent Core delivery points, and restore-to-editor removes the queued item without losing its text or attachments.
- ACC-05: Every built-in selector passes movement, paging, filtering, confirmation, cancellation, and focus-restoration tests.
- ACC-06: User key configuration replaces defaults and hotkey help shows the effective bindings.
- ACC-07: Suspension, resume, and normal TUI exit leave all TUI-owned terminal modes in the required state.
- ACC-07.1: Host contains no terminal-state inspection, snapshot, reset, restoration, or automatic TUI restart behavior after this ticket.
- ACC-07.2: Generated UI Plugin Contract code exposes no `controls_terminal` field or `GetCapabilities` RPC. UI runtime and selection contain no capability state, and starting a compatible UI requires only successful plugin protocol startup.
- ACC-07.3: Explicit, configured, and sole-candidate UI selection retain their current success and failure behavior without a capability request.
- ACC-08: Terminal sizes down to one row and one column do not panic.

## Overengineering and Overspecification Considerations

This ticket extracts user-visible terminal behavior and leaves Agent Core queue semantics, Host command discovery, session operations, and provider selection in their owning use cases. The standard TUI owns terminal lifecycle without Host recovery or restart behavior. Removing the only startup capability also removes its RPC and mapping instead of retaining an empty public abstraction. The ticket does not copy Pi editor classes, settings files, or action dispatch architecture.

## Constraints and Risks

- RSK-01: Editor and application bindings can conflict by focus. Focused editor and selector actions take precedence only in their active region; unhandled input returns to application actions.
- RSK-02: An external editor child process can leave terminal modes active. Suspend application modes before external-editor execution and restore them after child exit.
- RSK-03: Bracketed paste and terminal drop sequences can resemble typed input. Parse them as explicit terminal events and never submit from a paste event.
- RSK-04: Unexpected TUI termination can occur before TUI cleanup runs. Host does not repair terminal state; the user restarts the Glyph client.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No editor widget, clipboard library, fuzzy matcher, external-editor command resolution, or terminal keyboard protocol is selected by this ticket.

## References

- REF-01: [`standard-tui.md`](../../standard-tui.md) - owning editor, selector, and terminal-lifecycle requirements.
- REF-02: [`tui-defaults.md`](../../tui-defaults.md) - standard command names and default keys.
- REF-03: [`Model.Update`](../../../../../../plugins/ui/tui/internal/controller/tui/model.go) - implemented fixed key and single-line editor path.
- REF-04: [Pi key bindings](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/keybindings.md) - behavioral source for editor, selector, clipboard, application, and queue actions.
- REF-05: [Pi usage](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/usage.md) - behavioral source for completion, attachments, external editor, and queued messages.
- REF-06: [`ui.proto`](../../../../../../api/plugins/ui/v1/ui.proto) - current public `controls_terminal` and `GetCapabilities` contract to remove.
- REF-07: [`ui.Capabilities`](../../../../../../host/internal/domain/ui/model.go) - current internal capability model to remove with its runtime and selection mappings.
- REF-08: [target architecture](../../architecture.md) - TUI terminal ownership and UI Plugin Contract boundary.
