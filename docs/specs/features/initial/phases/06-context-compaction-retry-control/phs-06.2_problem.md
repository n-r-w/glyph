# Problem Statement: PHS-06.2 standard context compaction

Status: Approved. The [requirements](phs-06.2_prd.md) and [technical solution](phs-06.2_solution.md) describe the subsequent design levels.

## Context

A developer works on a coding task in one standard Glyph session. User messages, model responses, and tool results accumulate during that work. The selected model has a finite context capacity.

The [domain glossary](../../../../../terms.md) defines session and context compaction. This phase introduces no new domain terms.

## Observed Problem

Before PHS-06.2, the standard agent did not reduce growing context automatically. When the history and other request content exceeded the selected model's capacity, the agent could not continue work with that context.

## Affected Audience

Developers using the standard coding agent encounter the context limit while their coding task is still unfinished.

## Evidence

- At design approval, the standard compaction extension was pending. The [migration plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records its subsequent implementation.
- In `host/internal/usecase/host/extensioncontext/conversation_observation.go`, `Service.CaptureConversation` returns no policy owner when none is registered. In `host/internal/usecase/host/modelexecution/service.go`, `Service.prepareProviderContext` then preserves the request without automatic compaction.
- The design investigation inspected those paths. It did not reproduce a particular provider rejection or measure how often developers encounter the limit.

## Impact

Before PHS-06.2, the developer reached the context limit in one session before the task was complete. This interrupted work that depended on the accumulated conversation.

## Current State

The standard compaction extension is implemented on the [PHS-06.1 system foundation](system-integration-evidence.md). The [migration plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records delivered behavior, verification limits, completed PHS-06.3 residue closure, and pending PHS-06.4 independent acceptance.

## Desired State

The developer can continue long-running work with essential information about earlier actions and access to the original history.

## Problem Boundary

The problem concerns context capacity during long sessions of the standard coding agent. Provider connectivity and availability failures are outside this problem.

## Open Questions

None at the problem-definition level.
