# Problem statement

## Context

Glyph can run on a server over SSH while the user's browser runs on a different computer.

## Observed problem

The browser authorization flow redirects to `localhost` on the browser computer. That redirect cannot reach the callback listener on the remote Glyph computer without additional network setup.

## Affected audience

Users running Glyph over SSH who want to sign in to OpenAI Codex through their local browser.

## Evidence

The reported authorization URL used `http://localhost:1455/auth/callback`. The user identified the remote Glyph and local browser setup and requested both the browser flow and a code-based flow.

## Impact

A clickable authorization URL alone does not let the remote Glyph process receive the browser's authorization result.

## Desired state

Users can sign in from either a local terminal or an SSH session without replacing the local browser sign-in experience.

## Problem boundary

This feature concerns interactive OpenAI Codex sign-in in the standard TUI. It does not concern model execution, API-key configuration, or terminal URL wrapping.

## Open questions

None about the requested login scenarios.
