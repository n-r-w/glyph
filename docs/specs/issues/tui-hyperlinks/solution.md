# Technical solution: TUI hyperlinks

## Problem statement

See [problem statement](problem.md) and [requirements](prd.md).

## Proposed solution

The terminal adapter attaches OSC 8 metadata before wrapping or truncating text. Every retained visual row carries its own complete hyperlink target and reset sequence. Viewport clipping therefore cannot discard the target required by a visible fragment.

[`hyperlinks.go`](../../../../plugins/ui/tui/internal/infra/terminal/hyperlinks.go) owns recognition and row-local metadata. It recognizes HTTP and HTTPS URLs, excludes surrounding prose delimiters, and preserves balanced parentheses in paths. Inline `[label](URL)` links display their label. Existing OSC 8 links retain their targets and parameters.

The common body renderer covers authorization, startup messages, user and model transcript entries, streaming output, tool output, and branch summaries. Selector truncation and tree previews attach targets before removing visible text. Read-only status text uses the same recognizer. Editors do not transform their input.

### Terminal behavior

The local terminal application owns hyperlink activation, including any required modifier key. Glyph emits the complete target over its terminal output, including over SSH. No remote browser command is used for link activation.

A terminal without OSC 8 support still receives the visible text but cannot use the attached target. Actual activation in the user's terminal has not been exercised by automated tests. OAuth callback routing remains outside this correction.

### Verification

- [`hyperlinks_test.go`](../../../../plugins/ui/tui/internal/infra/terminal/hyperlinks_test.go) interprets rendered OSC 8 targets independently. It checks wrapping, clipping, inline labels, punctuation, existing links, multiple output sources, selector truncation, and editable-text preservation. The initial tests failed on missing targets before the implementation.
- [`hyperlinks_integration_test.go`](../../../../plugins/ui/tui/internal/infra/terminal/hyperlinks_integration_test.go) runs the real Bubble Tea renderer. It checks that output contains the complete target even when only the end of the visible URL remains in the viewport. Its output recorder uses an explicit terminal profile because an ordinary pipe otherwise suppresses hyperlinks.
- Final Linux checks passed: `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build`. Combined coverage is 84.4%, above the 80.0% threshold. The fix dry run is empty after applying its `strings.SplitSeq` proposal. Targeted unit-tag lint also passed.

### Related finding outside this correction

The existing `ansi.Wrap` behavior produces empty rows for the tested hyphenated OAuth-shaped URL at a terminal width of one column. The same failure occurred before hyperlink changes. This correction does not change the wrapping algorithm.

## Overengineering and overspecification considerations

The implementation uses the already-declared `github.com/charmbracelet/x/ansi` dependency. It adds no dependencies, provider changes, plugin contracts, browser launcher, or general Markdown renderer.

## Open questions

None for the code change. Manual activation remains a user-terminal check.
