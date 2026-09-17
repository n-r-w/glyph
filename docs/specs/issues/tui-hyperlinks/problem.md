# Problem statement

## Context

A user runs Glyph over SSH and opens authorization links in a local browser.

## Observed problem

The TUI displays a long OAuth URL across several rows. The user's terminal recognizes only part of the address as clickable. Opening that fragment cannot complete the intended navigation.

## Affected audience

Users opening web links from the standard TUI, including links in model responses and tool output.

## Evidence

The reported authorization screen showed one URL across three rows. Before this correction, `wrappedBodyLines` wrapped plain text without attaching a complete click target. The regression tests in [the solution](solution.md#verification) failed because visible URL characters had no hyperlink target.

## Impact

The user must copy the address and remove line breaks manually. Correcting only the authorization screen would leave other displayed URLs with the same problem.

## Desired state

Users can follow displayed web links without reconstructing addresses after terminal layout changes.

## Problem boundary

This issue concerns displayed links. OAuth callback routing between the browser and a remote Glyph process is a separate concern.

## Open questions

None about the reported display problem.
