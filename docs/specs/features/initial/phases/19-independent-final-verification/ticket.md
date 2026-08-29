# Ticket: PHS-19 - Independent final verification

Verify the complete PRD through public behavior after cleanup.

## Key definitions and abbreviations

- DEF-01: Requirement evidence. A passing test or observable packaging or licensing artifact linked to one normative PRD requirement.

## Problem Statement

- PRB-01: The complete product requires one independent requirement-to-evidence pass on native macOS and clean Linux Docker after cleanup.

## Target Picture

- SOL-01: Verify the complete PRD through public behavior after cleanup.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: independent verifier.
- Pre-condition: DEP-01 is met.
- Trigger: the final product suite runs on native macOS and in a clean Linux Docker environment.
- Required behavior: every normative PRD requirement has passing evidence and both environments complete required process scenarios.
- Example input and expected output: Input: run the final requirement-to-evidence suite from a native macOS checkout and a clean Linux Docker environment. Expected output: every PRD requirement links to passing evidence and both environments complete all required process scenarios.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No new requirements. A failed criterion reopens its owning ticket.

## Dependencies and Preconditions

- DEP-01: [PHS-18](../18-cleanup/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Verify the complete PRD through public behavior after cleanup.

### Functional Requirements

- FRQ-01: Run the full unit, integration, contract, and process-level suites on native macOS and in a clean Linux Docker environment.
- FRQ-02: Execute the standard coding-agent scenario through the standard TUI and Programmatic Control.
- FRQ-03: Audit every normative requirement in [`prd.md`](../../prd.md) against a passing test or an observable packaging or licensing artifact.

### Non-Functional Requirements

- NFQ-01: This ticket changes no product behavior. It runs the final evidence suite, `task lint`, and `task test` from a native macOS checkout and a clean Linux Docker environment.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Final requirement-to-evidence matrix and platform test results.

### Acceptance Criteria

- ACC-01: Every in-scope functional requirement has passing evidence through its owning public or internal contract.
- ACC-02: Native macOS and clean Linux Docker builds execute the standard coding-agent, extension, provider, session, reload, TUI, and Programmatic Control scenarios.
- ACC-03: `task lint` and `task test` pass from a clean repository checkout.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Platform-specific process or terminal behavior can fail only in one required environment. Run process and TUI lifecycle tests on native macOS and clean Linux Docker rather than relying on cross-compilation.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
