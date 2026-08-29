# Ticket: PHS-00 - Prototype baseline

Preserve an executable baseline before target behavior changes.

## Key definitions and abbreviations

- DEF-01: Prototype baseline. The Codex, extension tool, UI process contract, standard TUI consumer, and headless behavior implemented before target-product slices.
- DEF-02: Headless observation set. Final model text, tool execution start, tool execution end, tool name, tool status, and successful command completion exposed by the one-shot headless public output.
- DEF-03: Typed UI lifecycle observation set. The ordered public UI process lifecycle frames for one agent run: agent lifecycle, message completion, tool execution start and end, tool name, tool status and result, terminal run outcome, and Host settlement.
- DEF-04: Shared Host observation set. Final model text, tool execution start, tool execution end, tool name, tool status, and successful command completion.

## Problem Statement

- PRB-01: The implemented prototype spans several processes and contracts, but no linked regression fixtures protect the Host UI process contract and standard TUI consumer before target behavior changes.

## Target Picture

- SOL-01: Preserve an executable baseline before target behavior changes with linked Host process and standard TUI consumer fixtures.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph maintainer.
- Pre-condition: DEP-01 is met.
- Trigger: the baseline suite runs.
- Required behavior: The Host process fixture runs one fixed coding request twice with Codex streaming and the real bundled tools extension: once through the one-shot headless path and once through a semantic UI process client. The headless path records DEF-02. The UI process client records DEF-03. The fixture compares only DEF-04. The standard TUI consumer fixture feeds the typed UI lifecycle sequence through real standard-TUI controller logic. Together, the fixtures protect the public UI process contract and the standard TUI consumer without changing product behavior.
- Example input and expected output: Input: one fixed coding request. Expected output: the headless path records DEF-02, the UI process client records DEF-03, and both paths have matching DEF-04 fields. The standard TUI consumer fixture reaches the matching semantic state without asserting terminal presentation text.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No product behavior changes or new target contracts.

## Dependencies and Preconditions

- DEP-01: None. This is the first ticket.

## Requirements

### Goals

- GOL-01: Preserve an executable baseline before target behavior changes.

### Functional Requirements

- FRQ-01: Add two linked baseline fixtures. The Host process fixture records the headless and typed UI lifecycle observation sets. The standard TUI consumer fixture consumes the typed UI lifecycle sequence.
- FRQ-02: The Host process fixture shall run the same request with Codex streaming and the real bundled tools extension through the one-shot headless path and through a semantic UI process client. The headless path shall verify DEF-02 through its public output. The UI process client shall verify DEF-03 through the public UI process contract. The fixture shall compare only DEF-04.
- FRQ-03: The standard TUI consumer fixture shall consume the DEF-03 typed UI lifecycle sequence through real standard-TUI controller logic and verify semantic TUI state without terminal presentation text.
- FRQ-04: The fixtures shall not infer hidden lifecycle events, treat `error == nil` as proof of hidden lifecycle events, add production observation hooks, use proxy processes, assert PTY text, or add test-only composition hooks.
- FRQ-05: Record the prototype limitations that each later phase removes by referencing the [prototype Technical Solution](technical-solution.md).

### Non-Functional Requirements

- NFQ-01: Both baseline fixtures must pass when first added and after every later delivery phase because this ticket changes no product behavior.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Automated Host process fixture with the three observation sets in DEF-02 through DEF-04, plus an automated standard TUI consumer fixture that consumes the DEF-03 typed UI lifecycle sequence.

### Acceptance Criteria

- ACC-01: The Host process fixture runs the same request with the real bundled tools extension and Codex streaming through the one-shot headless path and through a semantic UI process client.
- ACC-02: The headless path verifies final model text, tool execution start, tool execution end, tool name, tool status, and successful command completion through its public output.
- ACC-03: The semantic UI process client verifies agent lifecycle, message completion, tool lifecycle and result, terminal run outcome, and Host settlement from typed UI lifecycle frames through the public UI process contract.
- ACC-04: The two Host paths compare only final model text, tool execution start, tool execution end, tool name, tool status, and successful command completion.
- ACC-05: The standard TUI consumer fixture uses real standard-TUI controller logic to consume the typed UI lifecycle sequence and verifies semantic TUI state without terminal presentation text.
- ACC-06: The fixtures do not infer hidden lifecycle events, treat `error == nil` as proof of hidden lifecycle events, add production observation hooks, use proxy processes, assert PTY text, or add test-only composition hooks.
- ACC-07: The fixtures change no product behavior.
- ACC-08: `git diff --check`, local-link validation, `task lint`, and `task test` pass.

## Overengineering and Overspecification Considerations

The ticket uses only the one-shot headless public output and the public UI process contract. It does not add observers, proxy processes, presentation assertions, or test-only composition hooks. OSP-01 remains outside the ticket.

## Constraints and Risks

- RSK-01: Treating successful command completion or `error == nil` as proof of hidden lifecycle events would create false baseline evidence. The headless path verifies only DEF-02.
- RSK-02: A test tied to presentation text would obstruct later UI work. The standard TUI consumer fixture verifies semantic state instead of mutable terminal layout.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No product technical design is selected by this ticket. Fixture placement and typed UI lifecycle-sequence representation remain implementation details.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [prototype requirements](baseline-prd.md) - prototype requirements.
- REF-04: [prototype architecture](technical-solution.md) - prototype architecture.
