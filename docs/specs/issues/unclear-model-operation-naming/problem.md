# Problem Statement

## Context

The issue was found while tracing the model request used by branch summarization. The call crosses `sessiontree.ModelCompleter`, an application binding, the provider catalogue, and a provider driver.

## Problem Statement

Some model-operation identifiers do not state the operation in project terms. They use provider-history terminology and state qualifiers that make the behavior, result, and execution boundary difficult to understand from the call site.

## Who is affected

Glyph developers who implement, review, debug, or extend model execution paths are affected.

## Evidence

- `sessiontree.ModelCompleter.CompleteConfigured` accepts a `model.Selection`, system instructions, and history, then returns a terminal `model.Response`.
- `providers.Catalog.CompleteConfigured` validates the selected catalogue entry, starts `Provider.Stream`, and waits for its terminal event.
- The same method supports both Chat Completions and Responses APIs. `Complete` therefore depends on historical provider terminology rather than one operation shared by those APIs.
- `ValidateConfigured` and `CompleteConfigured` repeat `Configured`, although both methods already accept the exact `model.Selection` that identifies the configured provider and model.
- User review could not determine the meaning of `CompleteConfigured` from its name and required tracing the full call chain.
- No repository-wide naming audit has established whether this problem is limited to these identifiers.

## Impact

Developers must inspect implementations to learn whether a method validates, starts a request, streams events, waits for a terminal response, or mutates active selection. This increases review and debugging time and makes incorrect assumptions about execution behavior more likely.

## Reproduction Steps

1. Open `host/internal/usecase/host/sessiontree/interfaces.go`.
2. Inspect `ModelCompleter.CompleteConfigured` without reading its implementation.
3. Try to determine the operation, whether it blocks, what `Configured` distinguishes, and which result it returns.
4. Trace the method through `host/internal/app/sessions.go` and `host/internal/usecase/host/providers/completion.go` to obtain those facts.

## Current State

Model execution vocabulary is defined locally by interfaces and methods. The codebase has no verified inventory that distinguishes clear domain names from provider-specific or mechanically composed names.

## Desired Outcome

Model-operation identifiers state their action, input boundary, result, and ownership in consistent Glyph terminology. A developer can understand a call without knowing historical provider API terms or reading every implementation in the chain.

## Success Metrics

- A model-operation naming audit covers Host interfaces, application bindings, provider catalogue methods, and provider-driver entry points.
- Every renamed identifier has one documented meaning that matches its observable behavior.
- Model request call sites do not require provider-specific knowledge to distinguish validation, generation, streaming, and terminal response handling.
- No compatibility alias or forwarding method preserves an unclear identifier.

## Scope

- Internal model request and response operation names.
- Related interface, binding, catalogue, mock, test, and documentation identifiers.
- Identification of other unclear names in the same execution boundary.

## Out of Scope / Non-Goals

- Renaming provider SDK types or wire fields.
- A repository-wide style rewrite unrelated to model execution.
- Changing model execution behavior.
- Adding compatibility aliases for replaced names.

## Constraints

- Project glossary terms remain the terminology source of truth.
- Provider-specific terms remain valid inside adapters when they name real provider API concepts.
- The project requires direct replacement and does not preserve old internal names.
- Code comments and documentation remain in English.

## Assumptions

The observed naming problem may extend beyond `CompleteConfigured`. A bounded inventory of related model-operation symbols must verify its actual extent before renaming begins.

## Open Questions

- Which model-operation identifiers besides `ModelCompleter` and `CompleteConfigured` fail to express their behavior?
- Which project term should name generation of one terminal model response across Chat Completions and Responses APIs?
- Should synchronous waiting and streaming event production use distinct operation names at their respective boundaries?
