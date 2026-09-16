# Problem Statement

## Context

PHS-07 is partially implemented, and [PHS-07.1](../07.1-architecture-audit-and-correction/ticket.md) is complete. Glyph Host owns the active session, configured model catalogue, active model selection, session persistence, and Agent Core event delivery.

External extensions use the public Extension Contract. Their behavior must remain independent of headless operation, the standard TUI, or another Glyph client.

## Observed Problem

An extension cannot change the active model or reasoning choice, participate in approval or transformation of that selection, or observe selection changes through the public Extension Contract. Extension authors therefore lack the selection-related part of session-bound behavior, although other PHS-07 capabilities are already exposed.

## Affected Audience

The problem affects extension authors and users who expect the same extension behavior through headless operation, the standard TUI, Programmatic Control, and future Glyph clients.

## Evidence

- `ExtensionRequest` in `api/plugins/extension/v1/extension.proto` exposes catalogue queries, configured-model requests, extension-entry appends, state recovery, and cancellation. It contains no model-selection or reasoning-selection request.
- `HandlerKind` in the same file exposes session-tree handlers and agent, turn, message, and tool-execution observers. It contains no selection handler or selection observer.
- `ExtensionContext` in `sdk/plugins/extension/v1/context.go` exposes asynchronous methods for the implemented context operations but no active-selection method.
- `providers.Catalog.SelectModel` and `providers.Catalog.SelectReasoningChoice` in `host/internal/usecase/host/providers/catalog.go` implement selection changes used by Glyph clients. Their presence does not provide extension access or selection-handler composition.

## Impact

An extension can use an explicitly selected model for its own work but cannot apply its selection rules to the conversation's active model. It also cannot react to a user's model or reasoning change through selection events. These limitations prevent selection-aware extension behavior independent of the connected Glyph client.

## Current State

The implemented context operations cover session binding, configured-model access, persisted extension entries and messages, and active-branch recovery. Lifecycle observers cover agent, turn, message, and tool execution. These capabilities form the partial PHS-07 baseline, not evidence that every phase acceptance criterion passes.

Glyph clients receive Host events through their own client contracts. Each client decides how to process or present those events.

## Desired State

Extension authors can implement session-aware, model-aware, and lifecycle-aware behavior once, independent of headless operation or the connected Glyph client. Glyph clients continue to receive results and events through the client-neutral Host event model and retain ownership of presentation.

## Problem Boundary

The problem concerns session-bound extension behavior across headless operation, the standard TUI, and Programmatic Control. Active model-selection participation and observation are the missing public capabilities identified here. The other PHS-07 capabilities remain part of the phase rather than becoming a separate feature.

The problem does not include how a Glyph client renders or otherwise presents events. It also does not include prompt, context, input, provider, tool, or TUI transformations.

## Assumptions

None.

## Open Questions

None.
