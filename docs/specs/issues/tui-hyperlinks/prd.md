# Idea: Usable TUI hyperlinks

## Definitions

See [terms](terms.md).

## Context and problem

See [problem statement](problem.md).

## Goal

Open web links from the standard TUI without copying and joining wrapped URL fragments.

## Scenarios

- Open a long OAuth URL displayed across multiple terminal rows.
- Open links in model responses, tool output, and session previews.
- Open a visible URL fragment after wrapping or viewport clipping has hidden another fragment.

## Scope and non-scope

Read-only TUI output uses one hyperlink behavior. Ordinary HTTP and HTTPS URLs remain visible as addresses. Inline Markdown links display their label.

Editable request and tree input retain their original text and cursor positions. This issue does not change OAuth callback routing, provider authentication, or the full Markdown rendering work in PHS-12.1.

## Requirements

### Functional requirements

- [X] FRQ-01: Every displayed fragment of a recognized web link shall address the complete target after wrapping or clipping.
  - Origin: formulated from the reported partial-click failure and approved whole-TUI scope.
  - Goal: Remove manual address reconstruction.
  - Goal achievement: Full. Layout changes do not shorten the address opened by the terminal.
- [X] FRQ-02: Read-only TUI output shall apply this behavior to authorization, model responses, tool output, messages, and session previews rather than special-case OAuth.
  - Origin: formulated from the user's rejection of an OAuth-only correction and approval of option O3-1.
  - Goal: Give displayed web links consistent behavior.
  - Goal achievement: Full. The correction covers each source of displayed links.

## Open questions

None for implementation. The user's terminal must support OSC 8 to activate the explicit hyperlink targets.

## References

- [Technical solution and verification](solution.md)
