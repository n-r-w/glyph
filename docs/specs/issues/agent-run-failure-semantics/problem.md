# Problem Statement

## Context

The [Blocking contract operation processing Technical Solution](../blocking-contract-operation-processing/solution.md) introduces `Failed.code` for an accepted Programmatic `UserRequest` and UI `SubmitCommand`. Its current `RUN-F` definition contains only `INTERNAL`. The roadmap assigns logical model-execution outcomes to PHS-06 and provider source classification to PHS-12 before `RUN-F` can expose additional categories.

## Problem Statement

Glyph has no agreed end-to-end definition of which terminal agent-run failure causes clients must distinguish beyond `INTERNAL`. The current runtime cannot derive stable client categories for these causes at the client boundary.

## Who is affected

- Programmatic controllers that receive `Failed.code` after an accepted agent run fails.
- UI plugins that must present the same semantic agent-run outcome.
- Glyph developers working on contract lifecycle, model execution, retry control, extensions, and provider migration.

## Evidence

- [APC-20](../blocking-contract-operation-processing/solution.md) defines the current `RUN-F` set as `INTERNAL` and assigns later category work to PHS-06 and PHS-12.
- `host/internal/usecase/host/programmatic/prepared.go` maps every unclassified accepted-run error to `INTERNAL` through `failureCode`.
- Provider errors reach Agent Core as Go errors without a provider-neutral terminal failure kind.
- Extension tool execution errors become model-visible error tool results and do not directly fail the parent agent run.
- The [target architecture](../../features/initial/architecture.md) assigns provider response classification to provider implementations, terminal model-call results to Host model execution, and terminal agent-run outcomes to Agent Core.

## Impact

- Clients can distinguish terminal agent-run failures only as `INTERNAL`.
- Programmatic Control and UI plugins cannot expose finer terminal distinctions until their source conditions and ownership are defined.
- PHS-06 and PHS-12 can define incompatible failure semantics unless the cross-phase issue is resolved.

## Reproduction Steps

1. Start a Programmatic `UserRequest` through the production Host path.
2. Make the selected provider fail after the operation reaches `Running`.
3. Observe that the operation reaches `Failed` with code `INTERNAL` regardless of the provider failure cause.
4. Make an extension tool execution fail and observe that Agent Core receives a model-visible error tool result rather than a terminal agent-run failure.

## Current State

APC-20 defines `INTERNAL` as the only current terminal agent-run failure code. The Programmatic and UI run boundaries expose that category. Other failure information is provider-specific, removed before those boundaries, or represented as nonterminal model-visible tool data. PHS-06 and PHS-12 own the later work needed to define and carry additional categories.

## Desired Outcome

Glyph has one explicit definition of terminal agent-run failure semantics across Programmatic Control and UI plugins. Every public failure distinction has a real terminal source and remains observable at the client boundary. The roadmap assigns each required behavior to the phase that owns its source information.

## Success Metrics

- Every public terminal agent-run failure code has one defined source condition and an observable path to each Glyph client.
- Programmatic Control and UI plugins expose the same semantic terminal agent-run outcomes.
- The Blocking contract operation processing scope and later roadmap phases state consistent ownership of agent-run failure behavior.
- Public contract tests exercise every supported terminal agent-run failure distinction through production boundaries.

## Scope

- Terminal failures of accepted Programmatic `UserRequest` and UI `SubmitCommand` operations.
- The relationship between terminal agent-run failure, model-execution failure, provider failure, and extension tool failure.
- Contract ownership and delivery position in the current roadmap.

## Out of Scope / Non-Goals

- Failure codes for operations unrelated to an agent run.
- Detailed retry policy and provider-specific response mappings.
- A technical solution or implementation changes.

## Constraints

- Agent Core remains provider-neutral and independent of plugin transport.
- Provider-specific response classification remains owned by the provider implementation.
- Host owns model-execution and retry coordination outside Agent Core.
- Glyph clients receive the same semantic outcomes through Host use cases.

## Assumptions

None.

## Open Questions

- Which terminal agent-run failure causes must Glyph clients distinguish?
- Which component owns each client-visible distinction?
- Which distinctions belong to Blocking contract operation processing, PHS-06, or PHS-12?
