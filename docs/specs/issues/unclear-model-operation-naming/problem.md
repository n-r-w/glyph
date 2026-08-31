# Problem Statement

## Context

The issue was found while tracing the model request used by branch summarization. The call crosses a session-tree interface, an application binding, the provider catalogue, and a provider driver.

## Problem Statement

Some model-operation identifiers do not state their action or result in project terms. They use provider-history terminology, omit the object of an action, or retain names from replaced domain concepts. A developer cannot determine the execution boundary from the call site.

## Who is affected

Glyph developers who implement, review, debug, or extend model execution paths are affected.

## Evidence

- The branch summarizer calls a synchronous catalogue operation that accepts an exact `model.Selection`, system instructions, and history, then waits for and returns a terminal `model.Response`.
- The synchronous catalogue operation starts `ModelProvider.Stream` but does not expose intermediate stream events to its caller.
- The session-tree interface also exposes the active selection and an availability check, although its name describes only terminal response production.
- The availability check can resolve API-key sources or refresh provider-owned OAuth credentials, although its prior name suggested a local configuration validation.
- The provider catalogue exposes an identifier-only active selection and a request snapshot containing a descriptor and provider driver. Their prior accessor names did not express this difference.
- Variables of type `ReasoningChoice` retain the old `level` name after PHS-03 replaced the reasoning-level domain concept.

## Impact

Developers must inspect implementations to learn whether an operation checks availability, starts a request, streams events, waits for a terminal response, or changes the active selection. This increases review and debugging time and permits incorrect assumptions about execution behavior.

## Reproduction Steps

1. Follow branch summarization from `host/internal/usecase/host/sessiontree/summarizer.go` to the provider catalogue.
2. Compare the synchronous terminal-response operation with `ModelProvider.Stream`.
3. Compare the catalogue accessors for the active `model.Selection` and the Agent Core request snapshot.
4. Trace credential checks from model selection and model requests into API-key and OAuth implementations.

## Current State

Model request, selection, availability, request snapshot, credential, and reasoning-choice names are defined across several consumer interfaces and provider implementations. Inconsistent names obscure distinctions between these operations.

## Desired Outcome

Model-operation identifiers state their action, result, and ownership in consistent Glyph terminology. A developer can distinguish availability checks, synchronous model requests, request snapshots, and provider streams without reading every implementation in the chain.

## Success Metrics

- The naming audit covers Host interfaces, application bindings, provider catalogue methods, provider-driver entry points, credentials, and reasoning-choice mappings.
- Each identifier has one meaning that matches its observable behavior.
- Synchronous model requests and streaming event production use distinct operation names.
- Variables of type `ReasoningChoice` use choice terminology.
- No compatibility alias or forwarding method preserves a replaced identifier.

## Scope

- Internal model request and response operation names.
- Active-selection and request-snapshot names.
- Credential-check names used by selection and request execution.
- Reasoning-choice variables and comments that retain reasoning-level terminology.
- Related bindings, mocks, tests, errors, filenames, and documentation.

## Out of Scope / Non-Goals

- Renaming provider SDK types or wire fields.
- Renaming Chat Completions or Responses API concepts inside provider adapters.
- A repository-wide style rewrite unrelated to model execution.
- Changing model execution behavior.
- Adding compatibility aliases for replaced names.

## Constraints

- Project glossary terms remain the terminology source of truth.
- Provider-specific terms remain valid inside adapters when they name real provider API concepts.
- The project requires direct replacement and does not preserve old internal names.
- Code comments and documentation remain in English.

## Assumptions

The audit is bounded to the model execution path and related selection, credentials, and reasoning-choice contracts.

## Open Questions

None.
