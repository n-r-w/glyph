# Idea: Codex sign-in choice in the TUI

## Definitions

See [terms](terms.md).

## Context and problem

See [problem statement](problem.md).

## Goal

Support both local browser sign-in and code-based sign-in from an SSH session.

## Scenarios

- A local user chooses browser sign-in and completes the existing callback-based flow.
- An SSH user chooses device-code sign-in, opens the provider page on the client computer, and enters the displayed code.
- A user cancels a sign-in attempt and starts another attempt without restarting Glyph.

## Scope and non-scope

The standard TUI offers the two methods when the user requests authentication. Host and UI contracts carry the chosen method and the provider's URL and optional code. OAuth remains owned by the Codex provider adapter.

This feature adds no CLI flag, SSH tunnel, copied callback URL, generalized interaction framework, or authentication method for another provider.

## Requirements

### Functional requirements

- [X] FRQ-01: The TUI shall let the user choose browser sign-in or device-code sign-in before starting authentication.
  - Origin: formulated in the user's approved option O5-1.
  - Goal: Keep both methods available without editing configuration or restarting Glyph.
  - Goal achievement: Full. The user chooses the method at the point of sign-in.
- [X] FRQ-02: Device-code sign-in shall display the provider page and one-time code and complete without a callback to the Glyph computer.
  - Origin: source, the user's SSH scenario and request for a code-based second method.
  - Goal: Authenticate Glyph while the browser runs on another computer.
  - Goal achievement: Full. The browser confirms the code with the provider rather than contacting the remote terminal.
- [X] FRQ-03: Browser sign-in shall retain the local callback flow.
  - Origin: source, the user requested the existing method alongside the new method.
  - Goal: Preserve local sign-in.
  - Goal achievement: Full. Local users retain browser authorization and credential reuse.

### Non-functional requirements

- [X] NRQ-01: Authentication shall remain asynchronous, keep the TUI responsive, and support cancellation through the existing operation lifecycle.
  - Origin: source, project asynchronous-operation and responsive-UI rules applied to authentication.
  - Goal: Keep the terminal usable while awaiting browser approval.
  - Goal achievement: Full. Waiting runs outside the UI event loop and can be canceled.

## Open questions

None for implementation.

## References

- [Technical solution and verification](solution.md)
- [Sign-in instructions](../../../guidelines/authentication.md)
