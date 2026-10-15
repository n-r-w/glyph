# Problem Statement: PHS-06.2 standard context compaction

Status: Approved. The [requirements](phs-06.2_prd.md) and [technical solution](phs-06.2_solution.md) describe the subsequent design levels.

## Context

A developer works on a coding task in one standard Glyph session. User messages, model responses, and tool results accumulate during that work. The selected model has a finite context capacity.

The [domain glossary](../../../../../terms.md) defines session and context compaction. This phase introduces no new domain terms.

## Observed Problem

The standard agent does not yet reduce growing context automatically. When the history and other request content exceed the selected model's capacity, the agent cannot continue work with that context.

## Affected Audience

Developers using the standard coding agent encounter the context limit while their coding task is still unfinished.

## Evidence

- The [migration plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records the standard compaction implementation as pending.
- In `host/internal/usecase/host/extensioncontext/conversation_observation.go`, `Service.CaptureConversation` returns no policy owner when none is registered. In `host/internal/usecase/host/modelexecution/service.go`, `Service.prepareProviderContext` then preserves the request without automatic compaction.
- The design investigation inspected those paths. It did not reproduce a particular provider rejection or measure how often developers encounter the limit.

## Impact

The developer reaches a limit on continued work in one session before the task is complete. This interrupts work that depends on the accumulated conversation.

## Current State

[PHS-06.1](system-integration-evidence.md) supplies the system support for compaction. Standard compaction remains unimplemented.

## Desired State

The developer can continue long-running work with essential information about earlier actions and access to the original history.

## Problem Boundary

The problem concerns context capacity during long sessions of the standard coding agent. Provider connectivity and availability failures are outside this problem.

## Open Questions

None at the problem-definition level.
