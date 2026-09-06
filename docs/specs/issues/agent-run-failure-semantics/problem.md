# Problem Statement

## Context

The [Blocking contract operation processing Technical Solution](../blocking-contract-operation-processing/solution.md#operation-inventory) defines `Failed.code` for an accepted Programmatic `UserRequest` and UI `SubmitCommand`. The approved [PHS-07.1 correction](../../features/initial/phases/07.1-architecture-audit-and-correction/solution.md#source-backed-persistence-classification) adds a source-backed history-persistence distinction to `RUN-F`. That behavior is not implemented. Logical model-execution outcomes beyond this distinction remain in PHS-06, and provider source classification remains in PHS-12.

## Problem Statement

Beyond the approved history-persistence distinction, Glyph has no agreed end-to-end definition of terminal agent-run failure categories. The runtime still exposes only `INTERNAL` for accepted-run failures; the approved distinction and broader model/provider classifications are not implemented at the client boundary.

## Who is affected

- Programmatic controllers that receive `Failed.code` after an accepted agent run fails.
- UI plugins that must present the same semantic agent-run outcome.
- Glyph developers working on contract lifecycle, model execution, retry control, extensions, and provider migration.

## Evidence

- [APC-20](../blocking-contract-operation-processing/solution.md#operation-inventory) defines the approved `RUN-F` mapping and assigns the history-persistence correction to PHS-07.1. Broader category work remains in PHS-06 and PHS-12.
- `host/internal/usecase/agent/run/interfaces.go` exposes the history-persistence failure identity, which the baseline client mappings do not distinguish.
- `host/internal/usecase/host/programmatic/prepared.go` maps every unclassified accepted-run error to `INTERNAL` through `failureCode`.
- Provider errors reach Agent Core as Go errors without a provider-neutral terminal failure kind.
- Extension tool execution errors become model-visible error tool results and do not directly fail the parent agent run.
- The [target architecture](../../features/initial/architecture.md) assigns provider response classification to provider implementations, terminal model-call results to Host model execution, and terminal agent-run outcomes to Agent Core.

## Impact

- Clients can distinguish terminal agent-run failures only as `INTERNAL`.
- The approved history-persistence distinction is not yet exposed to either client. Source conditions and ownership for broader terminal distinctions remain undefined.
- PHS-06 and PHS-12 can define incompatible failure semantics unless the cross-phase issue is resolved.

## Reproduction Steps

1. Start a Programmatic `UserRequest` through the production Host path.
2. Make the selected provider fail after the operation reaches `Running`.
3. Observe that the operation reaches `Failed` with code `INTERNAL` regardless of the provider failure cause.
4. Make an extension tool execution fail and observe that Agent Core receives a model-visible error tool result rather than a terminal agent-run failure.

## Current State

The Programmatic and UI run boundaries expose only `INTERNAL` for accepted-run failures. The source-backed persistence behavior specified by APC-20 is approved but awaits PHS-07.1 implementation and verification. Other failure information remains provider-specific, is removed before client boundaries, or is represented as nonterminal model-visible tool data. PHS-06 and PHS-12 retain the broader classification work. This issue remains planned, not completed.

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

- Which additional terminal agent-run failure causes must Glyph clients distinguish beyond the approved history-persistence case?
- Which component owns each additional client-visible distinction?
- Which remaining distinctions belong to PHS-06 or PHS-12?
