# Idea: PHS-06 acceptance corrections

## Definitions

Use the [domain glossary](../../../../../../terms.md) and [phase definitions](../ticket.md#key-definitions-and-abbreviations). Requirement identifiers in this supplement are local to this delivery; parent identifiers are qualified as ticket or standard requirements.

## Context and Problem

The [problem statement](problem.md) describes the two caller-visible correction subjects and three evidence subjects.

## Goal

Complete the phase with complete caller-visible failure causes, effective enabled retry handlers, and executable observations of the required composition, restored history, and cancellation behavior.

## Scenarios

The [confirmed acceptance scenarios](target-picture.md#acceptance-scenarios), A1 through A5, define the caller actions and expected outcomes. Existing phase conditions remain in the [ticket](../ticket.md#acceptance-criteria), [standard requirements](../phs-06.2_prd.md#requirements), and [prepared-history requirements](../../../../../issues/compaction-history-projection/prd.md#requirements).

## Scope and Non-Scope

Correct F23/F24 and close evidence findings F19/F20/F21. Preserve the confirmed architecture, APIs, journal format, dependency versions, generator limits, and accepted debt. F17, optional F18 strengthening, new performance guarantees, and new client/provider Cartesian matrices remain excluded. Prior limits are neither silently removed nor expanded.

## Requirements

Checkboxes record separate user confirmation of these requirements. They do not record implementation verification. Intermediate stages proceeded autonomously under [final_only](target-picture.md#verification-policy), so these boxes remain unchecked. The [UNT-06 requirement assessment](unt-06-evidence.md#correction-requirement-assessment) records implementation evidence and the approved independent acceptance conclusion. Final text review is approved and the UNT-06 documentation is committed in `e7dd665cfc2dd50ab00182ad5be188dbe5619d27`. Final user acceptance remains separate and pending.

### Functional Requirements

- [ ] FRQ-01: When an ordinary or configured-model Codex request receives a terminal provider refusal followed by a stream read failure, the caller shall receive the complete acquired refusal and read causes. Classification and retry behavior shall remain based on the refusal under the existing policy and handler actions.
  - Origin: source. Target A1 and ticket FRQ-04.8.1, FRQ-04.9, ACC-04.5.
  - Goal: Preserve complete failure explanations without transport-based reclassification of an acquired refusal.
  - Goal achievement: Full. Both independent causes remain available while the received refusal retains its meaning.
  - Feasibility: Confirmed. The provider sends the refusal before the later read fails, and logical failure reporting already carries category separately from diagnostic text.
- [ ] FRQ-02: When retry is enabled and context overflow has no recovery owner, ordinary and configured-model requests shall pass the failure through the ordered retry handlers. Terminal cancellation, handler failure, or an invalid action shall stop later handlers and preserve acquired causes. Handler participation shall not permit repeating unchanged overflowing input without recovery.
  - Origin: source. Target A2 and ticket FRQ-04.1 through FRQ-04.5, ACC-04.1 through ACC-04.3.
  - Goal: Preserve handler authority while retaining the recovery constraint.
  - Goal achievement: Full. Each enabled handler has the required opportunity to act before repeat admission.
  - Feasibility: Confirmed. Ordered handler composition and recovery admission already exist as distinct parts of logical execution.
- [ ] FRQ-03: When a bundled compaction result handler fails or returns a rejected update, compaction shall preserve the last accepted state, report the complete cause, invoke no later participant, and attempt no durable replacement. Executable evidence shall distinguish a live-context participant error containing cancellation from explicit Cancel.
  - Origin: source. Target A3 and ticket FRQ-01.7.1, FRQ-01.8, ACC-01.5, ACC-02; standard FRQ-07 and FRQ-10.
  - Goal: Establish the bundled coordinator's failure semantics directly.
  - Goal achievement: Full. Callers can observe terminal failure and accepted diagnostic state without partial mutation.
  - Feasibility: Confirmed. Participant outcomes, acquired state, and persistence outcomes are observable through existing operation and session contracts.
- [ ] FRQ-04: After repeated compaction and application-owner recreation, the next model input shall contain the latest summary followed by the entire selected retained suffix in order. Executable evidence shall compare that suffix positively against the saved branch and latest receipt.
  - Origin: source. Target A4, ticket ACC-01.10 and ACC-03, and prepared-history FRQ-03.
  - Goal: Establish completeness and order of restored conversation context.
  - Goal achievement: Full. Omitted or reordered retained messages are observable in the next request.
  - Feasibility: Confirmed. Saved entries, the selected retained boundary, and outgoing model input are available in existing acceptance scenarios.
- [ ] FRQ-05: When an owning caller cancels during an installed retry-delay wait, logical execution shall preserve the cancellation and provider causes and make no repeat. Evidence shall establish installed-wait cancellation and retain public terminal ordering, idle state, and recovery through the retained connection.
  - Origin: source. Target A5 and ticket FRQ-05, ACC-06.
  - Goal: Establish cancellation while delay is actually pending.
  - Goal achievement: Full. Delay interruption and public reuse remain directly observable, with their evidence levels distinguished.
  - Feasibility: Confirmed. The retry mechanism supports context interruption during delay, and public cancellation and recovery observations already exist.

### Non-Functional Requirements

- [ ] NRQ-01: Corrections shall have compiling failure-before-fix and passing-after-fix observations for changed behavior, independent review, and all mandatory whole-project checks. The final report shall reassess all 40 phase conditions, identify actual skips and evidence limits, preserve accepted exceptions, and update closure statuses only when justified.
  - Origin: source. Target verification policy, ticket NFQ-01 and NRQ-01, and project rules.
  - Goal: Establish phase completion from the corrected revision rather than historical results.
  - Goal achievement: Full. The phase conclusion rests on observable regressions and complete verification records.
  - Feasibility: Confirmed. Project checks, independent review roles, public fixtures, and isolated execution are available. Environment failures remain explicit until resolved.

## Open Questions

No unresolved product requirement remains. The original [fact-collection questions](facts.md#open-questions) are resolved by the linked unit observations in the [UNT-06 requirement assessment](unt-06-evidence.md#correction-requirement-assessment). Independent acceptance-conclusion review approves all six correction requirements and all 40 phase clauses within the recorded boundaries. Final text review is approved and the UNT-06 documentation commit exists. Final user acceptance remains pending.

## Technical Supplement

No additional implementation constraint is introduced. The [confirmed boundaries](target-picture.md#decision-authority-and-boundaries) own mandatory constraints.

## References

- [Problem statement](problem.md): affected callers and evidence subjects.
- [Target picture](target-picture.md): confirmed scenarios, authority, and verification policy.
- [Phase report](../acceptance-report.md): findings and retained evidence boundaries.
