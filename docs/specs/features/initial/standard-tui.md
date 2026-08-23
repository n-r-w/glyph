# Specification: Standard TUI Interaction Baseline

## Key definitions and abbreviations

- DEF-01: Transcript. The ordered rendered conversation containing user messages, model content, reasoning, tool calls, tool progress, tool results, extension messages, and Host notifications.
- DEF-02: Transcript viewport. The terminal region that displays a bounded visible portion of DEF-01 while retaining access to the complete transcript.
- DEF-03: Fixed interaction dock. The status, queued-message area, editor, and footer that remain visible while DEF-02 moves through the transcript.
- DEF-04: Follow mode. Viewport behavior that keeps the newest transcript content visible as streaming updates arrive.
- DEF-05: Prompt marker. The start position of a rendered user message in DEF-01.
- DEF-06: Selector. A focused list or search interface for commands, models, sessions, themes, or another closed set of choices.
- DEF-07: Supported input image. A JPEG, static PNG, GIF, WebP, or BMP file identified from its content rather than its filename.

## Context and Problem

Current state: The standard TUI uses an alternate screen and keeps only the newest transcript lines that fit above the editor. [`Model.visibleBodyLines`](../../../../plugins/ui/tui/internal/controller/tui/model.go) discards older lines from the visible result and stores no viewport position.

Problems:
- PRB-01: The user cannot scroll to earlier conversation content after it leaves the terminal height.
- PRB-02: Streaming output always replaces older visible content, so the user cannot inspect an earlier section while a run continues.
- PRB-03: [`prd.md`](prd.md) requires a configurable terminal agent but does not define transcript navigation, transcript search, editor behavior, clipboard and attachment behavior, selector interaction, or terminal restoration.
- PRB-04: [`tui-defaults.md`](tui-defaults.md) records several Pi key defaults without owning requirements for the actions behind those keys.

Why now:
- TRG-01: The delivery plan must include a usable standard terminal agent before standard-TUI-specific extension presentation and interaction are added.

## Goal (Outcome)

Goal: Define the observable standard TUI behavior required to read, navigate, search, compose, submit, and copy agent interactions without adopting Pi runtime structure or API contracts.

Success metrics:
- MET-01: Every transcript entry remains reachable after it leaves the visible terminal height.
- MET-02: Every mouse-driven transcript operation has a keyboard-driven equivalent except free-form text selection.
- MET-03: Terminal resize, suspension, normal exit, UI process failure, and Host termination leave the terminal in an interactive state with application-owned modes disabled.
- MET-04: Every action named by this specification has a configurable key binding or a discoverable command.

Non-goals:
- NGL-01: Pi API, component, event, setting, file-layout, or TUI-mode compatibility.
- NGL-02: A choice between Pi regular and fullscreen modes. Glyph owns one standard TUI behavior.
- NGL-03: Standard-TUI extension presentation and interaction, which remain owned by the `TUI Extension Capabilities` section of [`prd.md`](prd.md).
- NGL-04: Windows behavior, remote UI plugins, browser UI, and graphical desktop UI.
- NGL-05: Terminal-specific workaround requirements for individual terminal products.

## Scenarios

Actors:
- ATR-01: A user running the standard TUI in an interactive terminal.
- ATR-02: Glyph Host sending ordered lifecycle, model, tool, notification, and interaction events to the standard TUI.

Top scenarios:
- SCN-01: While model output streams, the user scrolls to an earlier tool result. New output continues without moving the viewport. The user invokes bottom navigation and resumes DEF-04.
- SCN-02: The user searches the complete rendered transcript, moves between matches, copies the underlying assistant message, and closes search without changing session state.
- SCN-03: The user composes multiline Unicode input, completes a path or command, attaches an image, edits the text in an external editor, and submits one user message.
- SCN-04: During an active run, the user queues steering and follow-up messages, restores a queued message to the editor, and aborts the run without losing unsent editor text.
- SCN-05: The terminal changes size, suspends, resumes, and then loses the UI process. Glyph restores the terminal and the active session remains recoverable.

Operational and UX constraints:
- CNS-01: The standard TUI owns terminal input dispatch, rendering, focus, viewport state, editor state, and selection state. Agent Core owns none of these concerns. The standard TUI sends Host commands and renders Host events or results. It does not execute agent or shell behavior.
- CNS-02: TUI presentation never changes persisted model, tool, or session content.
- CNS-03: Keyboard operation remains available when mouse reporting, clipboard integration, inline images, or hyperlink activation are unavailable.

## Scope of Change

In scope:
- ISP-01: Transcript layout, semantic rendering, viewport movement, follow mode, prompt navigation, search, selection, copying, and link activation.
- ISP-02: Multiline editor behavior, prompt history, command and path completion, file and image attachment, clipboard integration, external editor integration, direct shell actions, and queued-message editing.
- ISP-03: Selector interaction, configurable key bindings, hotkey discovery, terminal resize, suspension, resume, and restoration.

Out of scope:
- OSP-01: Agent-run, session, provider, tool, command, queue, compaction, and extension business rules already owned by [`prd.md`](prd.md).
- OSP-02: Extension-provided widgets, overlays, renderers, editors, themes, and shortcuts.
- OSP-03: Persisting viewport, search, editor undo, selector, or text-selection state across Host restart.

System and domain boundaries:
- CNS-04: The standard TUI implements this specification through its UI plugin process and Host-owned client contracts.
- CNS-05: Programmatic Control is not required to emulate viewport, editor, clipboard, selector, mouse, or terminal-lifecycle behavior.

## High-Level Requirements

Functional:

### Transcript layout and rendering

- FRQ-01: The standard TUI shall retain the complete DEF-01 content received for the active application lifecycle and expose it through DEF-02.
- FRQ-02: DEF-03 shall remain visible while DEF-02 moves, streams, or searches.
- FRQ-03: The transcript shall distinguish user text, model text, reasoning, refusal, tool call, tool progress, tool result, extension message, information, warning, and error without relying on color alone.
- FRQ-04: Model text and extension-provided Markdown text shall render headings, lists, emphasis, links, inline code, fenced code, and tables while preserving the original text for copying and persistence.
- FRQ-05: Fenced code and file diffs shall preserve indentation and indicate their language or file path when that metadata is available.
- FRQ-06: Model reasoning and tool output shall support collapsed and expanded presentation. Toggling presentation shall not change session content.
- FRQ-07: An image shall render inline when the terminal reports a compatible image capability. Otherwise, the transcript shall show a textual placeholder containing image media type and dimensions when dimensions are available.
- FRQ-08: Streaming updates shall modify the active model or tool block instead of appending a duplicate block for each delta.
- FRQ-09: DEF-03 shall show active provider, model, reasoning level, context usage, session identity or name, run state, and queued-message count for every value Host supplies. It shall omit a value Host marks unavailable.
- FRQ-10: Copying one message shall copy its original text rather than terminal styling, truncation markers, or viewport decorations.

### Transcript viewport

- FRQ-11: DEF-04 shall be active when DEF-02 is at the transcript bottom.
- FRQ-12: Moving DEF-02 away from the bottom shall disable DEF-04 and shall not block incoming transcript updates.
- FRQ-13: Moving to the transcript bottom shall enable DEF-04 and display the newest complete visible region.
- FRQ-14: The user shall be able to move DEF-02 by one line, half a page, one page, transcript start, and transcript end.
- FRQ-15: The user shall be able to move to the preceding and following DEF-05.
- FRQ-16: Transcript search shall examine the complete rendered transcript, highlight the active match, move to the next or preceding match, and close without changing DEF-01 or the session.
- FRQ-17: Mouse wheel and trackpad scroll events shall move the transcript region when the pointer is not over another scrollable TUI region.
- FRQ-18: The standard TUI shall display a scroll-position indicator whenever DEF-02 does not cover the complete transcript.
- FRQ-19: Activating a rendered hyperlink shall send its URL to the operating-system handler only after an explicit user click or key action.
- FRQ-20: Primary-button drag shall select visible transcript text for clipboard copy. Dragging at the top or bottom viewport edge shall continue selection by scrolling in that direction.

### Editor, completion, and queued input

- FRQ-21: The editor shall accept multiline Unicode text and preserve newline boundaries in the submitted user message.
- FRQ-22: The user shall be able to move by character, word, visual line, logical line boundary, and editor page.
- FRQ-23: The user shall be able to delete one character, one word, from cursor to line start, and from cursor to line end.
- FRQ-24: The editor shall support undo and restoration of recently deleted text within the active editor lifecycle.
- FRQ-25: Prompt history shall move to older and newer submitted user text without changing session entries until the user submits.
- FRQ-26: Submit and insert-newline shall be separate configurable actions.
- FRQ-27: Command completion shall use commands discoverable through Host and shall insert the selected command into the editor.
- FRQ-28: Path completion shall complete filesystem paths, and file-reference completion shall fuzzy-search project files and attach the selected file to the pending user input.
- FRQ-29: Clipboard paste shall preserve multiline text. Pasted text shall enter the editor and shall not submit automatically.
- FRQ-30: Clipboard paste and terminal file-drop input shall attach DEF-07 images as typed user content instead of inserting binary data into editor text.
- FRQ-31: External-editor invocation shall open the complete editor text and replace editor text with the saved result. Cancellation or editor failure shall preserve the preceding editor text and report an error.
- FRQ-32: The user shall be able to copy the active editor selection and the selected transcript message.
- FRQ-33: While a run is active, the user shall be able to submit steering and follow-up messages and restore queued messages to the editor before delivery.
- FRQ-34: The standard TUI shall send a Host direct-shell command that requests either model-visible output in the next user context or model-hidden output outside model context, and shall render the Host result or error.
- FRQ-35: Clear, abort, and exit shall be distinct actions. Exit shall require an empty editor and no active focused dialog. Abort shall target the active run and shall not discard unsent editor text.

### Selectors and dialogs

- FRQ-36: Every DEF-06 shall support preceding item, following item, page up, page down, confirm, and cancel actions.
- FRQ-37: A selector with more entries than its visible height shall keep the selected item visible and shall expose text filtering when its item source is searchable.
- FRQ-38: Cancelling a selector or dialog shall restore the preceding editor text, viewport position, and focus.

### Terminal lifecycle and discoverability

- FRQ-39: Every standard TUI key binding shall be configurable, and user configuration shall replace the defaults owned by [`tui-defaults.md`](tui-defaults.md).
- FRQ-40: The user shall be able to display active commands and key bindings from the standard TUI.
- FRQ-41: Terminal resize shall recompute wrapping, DEF-02 height, DEF-03 placement, selection geometry, and scroll indicator without dropping DEF-01 content.
- FRQ-42: Suspending and resuming Glyph shall restore application input modes, visible content, viewport position, editor text, and focus.
- FRQ-43: Normal exit, UI process failure, and Host termination shall disable alternate screen, mouse reporting, bracketed paste, focus reporting, and application keyboard modes enabled by the standard TUI.

Non-functional:
- NFQ-01: At every positive terminal width and height, rendering shall not panic. When the complete dock does not fit, the editor line and one status line take precedence over transcript content.
- NFQ-02: Rendering, search, copy, completion, and selector filtering shall not modify persisted session entries.
- NFQ-03: Clipboard, external-editor, hyperlink, shell, and attachment errors shall remain user-visible and shall leave the standard TUI usable.
- NFQ-04: No TUI state or event shall contain provider credentials, authorization headers, OAuth verifier values, or credential-file contents.

## Overengineering and Overspecification Considerations

This specification extracts observable terminal-agent behavior from Pi documentation and keeps Glyph ownership boundaries from [`prd.md`](prd.md). It does not require Pi screen modes, action identifiers, TypeScript components, settings format, event types, or terminal-specific workarounds. Mouse behavior supplements keyboard behavior and does not create a second control path in Agent Core.

## Risks

- RSK-01: Reflow after resize can move the user's reading position. Preserve the logical top transcript block and intra-block offset rather than a raw terminal row number.
- RSK-02: Streaming Markdown can produce unstable wrapping. Keep one logical block identity and recompute only its rendered rows and later viewport geometry.
- RSK-03: Mouse reporting can interfere with terminal-native selection. Make keyboard copying complete and disable every application mouse mode during suspension and termination.
- RSK-04: Large transcripts can make reflow and search expensive. Store logical blocks separately from rendered rows and invalidate rendered data only when source content or width changes.

## Assumptions

- ASM-01: The standard TUI continues to use an application-owned alternate screen. Justification: [`Model.View`](../../../../plugins/ui/tui/internal/controller/tui/model.go) enables alternate screen today. Verification: the phase-specific TUI technical solution shall preserve or replace this decision before implementation.
- ASM-02: Host provides the semantic model, tool, session, queue, notification, and interaction events required to render DEF-01 and DEF-03. Justification: these events are owned by [`prd.md`](prd.md). Verification: PHS-12.1 shall map each required field to a Host event before implementation.

## Open Questions

None.

## Decisions

- DEC-01: Glyph shall provide one application-owned transcript viewport. Pi regular and fullscreen modes are not copied.
- DEC-02: Pi action names remain source references in [`tui-defaults.md`](tui-defaults.md) and do not become Glyph API identifiers.
- DEC-03: Every mouse navigation operation has a keyboard equivalent. Free-form drag selection is the only mouse-only operation.
- DEC-04: Inline image rendering is capability-dependent, but the textual fallback in FRQ-07 is always required.

## Standards Deviations

None.

## Technical Supplement

No terminal widget library, viewport data structure, Markdown renderer, image protocol, clipboard library, or external-editor process contract is selected by this specification.

## References

- REF-01: [`prd.md`](prd.md) - Glyph ownership boundaries and target standard TUI behavior.
- REF-02: [`tui-defaults.md`](tui-defaults.md) - Glyph command names and configurable default keys.
- REF-03: [`Model.visibleBodyLines`](../../../../plugins/ui/tui/internal/controller/tui/model.go) - implemented newest-lines-only presentation.
- REF-04: [Pi key bindings](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/keybindings.md) - transcript viewport, editor, selector, clipboard, session, model, display, and queue actions.
- REF-05: [Pi usage](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/usage.md) - editor completion, attachments, external editor, message queue, and terminal viewport behavior.
- REF-06: [Pi terminal setup](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/terminal-setup.md) - mouse, trackpad, image, link, and terminal restoration constraints.
