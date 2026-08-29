# Ticket: PHS-12.1 - Standard TUI transcript rendering and layout

Render the semantic agent transcript and a stable interaction dock before adding transcript navigation.

## Key definitions and abbreviations

- DEF-01: Transcript block. One logical user message, model content block, reasoning block, tool execution, extension message, or Host notification rendered by the standard TUI.
- DEF-02: Fixed interaction dock. The status, queued-message area, editor, and footer kept visible below transcript content.

## Problem Statement

- PRB-01: The standard TUI renders flattened text lines and keeps only the newest lines that fit the terminal. It does not provide Markdown, semantic block presentation, collapsible reasoning and tool output, image fallback, or stable dock content required by [`standard-tui.md`](../../standard-tui.md).

## Target Picture

- SOL-01: The standard TUI renders ordered semantic transcript blocks, updates streaming blocks in place, and keeps DEF-02 visible with active agent state.

## Scenarios

### SCN-01: Stream a mixed semantic response

- Actor: standard TUI user.
- Pre-condition: DEP-01 is met.
- Trigger: Host emits Markdown model text, reasoning, one tool execution, an image, and a warning during one run.
- Required behavior: the standard TUI renders each item as its own semantic block, updates active blocks without duplication, and retains a visible editor and status dock.
- Example input and expected output: Input: stream a heading, fenced Go code, hidden reasoning, tool progress, a PNG, and a warning. Expected output: each item has distinct non-color-only presentation, the code preserves indentation, reasoning and tool output can be toggled, and the PNG renders inline or as a typed placeholder.

## Scope

In scope:
- ISP-01: Transcript layout and rendering requirements FRQ-01 through FRQ-10 in [`standard-tui.md`](../../standard-tui.md).

Out of scope:
- OSP-01: Transcript scrolling, transcript search, mouse selection, editor completion, clipboard paste, selectors, and extension-provided renderers.

## Dependencies and Preconditions

- DEP-01: [PHS-12](../12-extension-defined-providers/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Provide semantic transcript blocks and a fixed interaction dock without moving Agent Core behavior into the standard TUI.

### Functional Requirements

- FRQ-01: Store transcript source as ordered logical blocks rather than terminal-height-truncated lines.
- FRQ-02: Render user text, model text, reasoning, refusal, tool call, tool progress, tool result, extension message, information, warning, and error with distinct text labels or structure.
- FRQ-03: Render Markdown headings, lists, emphasis, links, inline code, fenced code, and tables while preserving original text for copying.
- FRQ-04: Render file diffs with preserved indentation and available file metadata.
- FRQ-05: Update an active streaming model or tool block without appending duplicate blocks for deltas.
- FRQ-06: Collapse and expand model reasoning and tool output without changing persisted session content.
- FRQ-07: Render images according to terminal capability and use the textual fallback defined by `standard-tui.md` when inline rendering is unavailable.
- FRQ-08: Keep DEF-02 visible and show available provider, model, reasoning, context, session, run, and queue state, including all available session accounting values.
- FRQ-09: Copy original message text without terminal styling or viewport decoration.

### Non-Functional Requirements

- NFQ-01: Focused rendering tests must demonstrate RED and GREEN, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must not import standard TUI types or rendering dependencies.
- NFQ-03: Rendered color must not be the only distinction between the semantic states listed in FRQ-02.

### Deliverables

- DLV-01: Logical transcript-block presentation in the standard TUI.
- DLV-02: Markdown, code, diff, image, reasoning, tool, notification, and dock presentation.
- DLV-03: Process-level fixture that streams every semantic content kind through the UI plugin contract.

### Acceptance Criteria

- ACC-01: The SCN-01 fixture produces one ordered block for each semantic input and no duplicate block for streaming deltas.
- ACC-02: Collapsing and expanding reasoning and tool output changes only presentation state.
- ACC-03: Markdown source copied from a rendered message equals the original source text byte-for-byte.
- ACC-04: Inline image success and textual image fallback are both covered by process-level tests.
- ACC-05: The editor line and one status line remain visible at every positive terminal height without a panic.

## Overengineering and Overspecification Considerations

This ticket adds only standard transcript rendering required before scrolling and TUI extension renderers. It does not select Pi components, Pi screen modes, or a universal renderer for other UI plugins.

## Constraints and Risks

- RSK-01: Streaming Markdown reflow can duplicate or reorder content. Preserve stable logical block identity while recalculating rendered rows.
- RSK-02: Inline-image placement can become invalid after later scrolling. Keep image source metadata in the logical block so PHS-12.2 can re-render or replace it.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No Markdown library, syntax highlighter, diff renderer, or terminal image protocol is selected by this ticket.

## References

- REF-01: [`standard-tui.md`](../../standard-tui.md) - owning standard TUI behavior specification.
- REF-02: [`prd.md`](../../prd.md) - target product requirements and UI ownership boundaries.
- REF-03: [`Model.View`](../../../../../../plugins/ui/tui/internal/controller/tui/model.go) - implemented alternate-screen layout.
- REF-04: [Pi usage](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/usage.md) - behavioral source for Markdown, images, reasoning, tool output, and fixed fullscreen dock.
