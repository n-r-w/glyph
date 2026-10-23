# PHS-06 acceptance corrections target picture

## Goal

Complete PHS-06 by correcting F23/F24 and supplying the missing evidence for F19/F20/F21 in the [phase acceptance report](../acceptance-report.md). Reassess all 40 acceptance clauses against the corrected implementation and update phase, roadmap, and linked issue statuses only when the evidence permits completion.

The existing [ticket](../ticket.md), [solution](../solution.md), [standard requirements](../phs-06.2_prd.md), and [prepared-history requirements](../../../../../issues/compaction-history-projection/prd.md) retain ownership of requirements. This supplement records delivery boundaries and acceptance scenarios without replacing those contracts.

## Acceptance scenarios

- A1: Ordinary and configured-model Codex requests receive a terminal provider refusal followed by a stream read failure. The public result preserves both causes, the refusal-based category, and the expected provider attempt count.
- A2: When retry is enabled, ordinary requests without a compaction recovery owner and configured-model requests dispatch context-overflow decisions to ordered retry handlers. Cancel, handler errors, and invalid actions terminate with their complete causes and stop later handlers. Preserve does not permit a repeat of unchanged overflowing input without recovery.
- A3: The bundled coordinator rejects invalid result updates and handler failures before installing a compaction marker. Tests assert last accepted state, no later handler, complete cause, and no mutation. A handler error wrapping context.Canceled remains distinct from explicit Cancel.
- A4: After repeated compaction and application-owner recreation, the next provider request contains the latest summary followed by the complete retained suffix in its original order. Expected suffix content comes from the saved branch and the second receipt's first-kept entry.
- A5: Cancellation occurs during an installed retry timer wait. Execution preserves the cancellation cause, makes no provider repeat, publishes terminal events in order, returns to idle, and permits another request through the retained client connection. Deterministic timer-wait evidence and public lifecycle evidence may be supplied by complementary tests.

## Decision authority and boundaries

Implementation, tests, and documentation necessary to these scenarios and phase completion are authorized. Implementation agents and independent reviewers may be used. Implementation choices within these boundaries are recorded in decisions.md.

Preserve the accepted architecture, public APIs, original journal format, and declared dependency versions. Do not introduce backward compatibility, fallback to an old implementation, or a mandatory timer-installation acknowledgement API. Do not accept new product limitations without a user decision.

F17 remains excluded from investigation, reproduction, design, and correction. F18 remains optional and is excluded from this delivery. Preserve previously accepted debt and evidence boundaries listed in the phase report, including the narrow GO-2026-6809 exception and original test-first exceptions. No scanner suppression or toolchain/dependency change is authorized.

Project rules require task fmt, task fix_dry_run with analysis of its proposals, task lint, task test, task itest, and task test-coverage. Record actual environment limits and distinguish skipped scenarios from executed scenarios. Reassess the complete acceptance inventory and independently review changes before reporting completion.

Local commits are authorized during implementation. Do not push or change remote refs. Preserve changes made by others.

## Verification policy

final_only. Intermediate research, design, planning, and implementation proceed without user confirmation within these boundaries. The user accepts the assembled result after all corrections and checks. Decisions outside these boundaries that block progress require a user answer.

## Task directory

This directory holds task-specific delivery records. Existing phase documents remain authoritative for their subjects and are referenced rather than duplicated.

## Open questions

No unresolved user decisions remain at alignment. Investigation questions about reproduction and implementation will be resolved in the research and design stages before coding.
