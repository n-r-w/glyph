# Problem Statement

## Context

PHS-05.1 established separate owners for extension runtime management, tool capability orchestration, and session-tree capability orchestration. Glyph Host already owns the active session, configured model catalogue, active model selection, session persistence, and Agent Core event delivery.

External extensions use the public Extension Contract. Their behavior must remain independent of headless operation, the standard TUI, or another Glyph client.

## Observed Problem

The public Extension Contract limits extensions to tool execution and session-tree handlers. An extension cannot identify the active session for its work, use a configured model for extension-owned behavior, add model-visible branch content with defined client visibility, observe the Agent Core lifecycle, or participate in active model-selection changes.

## Affected Audience

The problem affects extension authors and users who expect the same extension behavior through headless operation, the standard TUI, Programmatic Control, and future Glyph clients.

## Evidence

- `api/plugins/extension/v1/extension.proto` exposes registration, handler invocation, tool execution, cancellation, and operation lifecycle envelopes. It exposes no extension context, configured-model request, active-selection operation, or Agent Core lifecycle event.
- `host/internal/usecase/host/extensionruntime/service.go` tracks runtime availability and active operations but no runtime generation or active-session binding.
- `host/internal/usecase/host/providers/catalog.go` and `host/internal/usecase/host/providers/request.go` already own configured-model inspection, active selection, credential checks, and model requests without active-selection mutation. The Extension Contract cannot access these operations.
- `host/internal/usecase/agent/run/event.go` produces Agent Core lifecycle events. `host/internal/usecase/host/events/service.go` delivers them to the active client path but not to extension handlers.
- `host/internal/usecase/host/sessions/service.go` can persist branch-aware extension entries. `host/internal/usecase/host/sessiontree/history.go` excludes every extension entry from model context, and the Extension Contract exposes no entry-creation operation.
- UI Plugin Contract and Programmatic Control already expose client-neutral model, reasoning, agent-event, and session-tree information. The Extension Contract does not expose the corresponding extension capabilities.

## Impact

An external extension cannot implement session-aware, model-aware, or lifecycle-aware behavior through public contracts. Extension behavior that depends on these capabilities cannot work consistently across headless operation, the standard TUI, Programmatic Control, and future Glyph clients.

Any attempt to implement this behavior today would require unsupported access to Host internals or client-specific integration. A separately delivered extension cannot use either path because it imports only public Extension Contract and SDK packages.

## Current State

Extension processes register tools and session-tree handlers. Glyph Host manages the active session, configured models, active selection, persistence, and Agent Core events without exposing these Host-owned capabilities to extensions as one session-bound public context.

Glyph clients receive Host events through their own client contracts. Each client decides how to process or present those events.

## Desired State

Extension authors can implement session-aware, model-aware, and lifecycle-aware behavior once, independent of headless operation or the connected Glyph client. Glyph clients continue to receive results and events through the client-neutral Host event model and retain ownership of presentation.

## Problem Boundary

The problem covers missing public extension access to the active session, configured models, extension-owned model requests, branch-aware extension entries and their client visibility, Agent Core lifecycle activity, and active model-selection activity.

The problem does not include how a Glyph client renders or otherwise presents events. It also does not include prompt, context, input, provider, tool, or TUI transformations.

## Assumptions

None.

## Open Questions

None.
