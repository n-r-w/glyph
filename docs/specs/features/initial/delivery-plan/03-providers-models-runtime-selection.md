# Ticket: PHS-03 - Providers, models, and runtime selection

Support the required built-in providers and model selection without ending the session.

## Key definitions and abbreviations

- DEF-01: Provider catalogue. The Host-owned set of configured provider implementations and their model descriptors.

## Problem Statement

- PRB-01: The provider catalogue is fixed at startup to one Codex model. Glyph cannot use the required OpenAI-compatible provider or change model and reasoning selection during a session.

## Target Picture

- SOL-01: Support the required built-in providers and model selection without ending the session.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user selects another configured model and sends a request.
- Required behavior: the next request uses the selected provider, model, and reasoning level without clearing conversation history.
- Example input and expected output: Input: select a configured OpenAI-compatible model and reasoning level, then submit a user message. Expected output: the next provider request uses that selection and preceding conversation entries remain present.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No extension-defined providers or provider middleware.

## Dependencies and Preconditions

- DEP-01: [PHS-02](02-programmatic-control-foundation.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Support the required built-in providers and model selection without ending the session.

### Functional Requirements

- FRQ-01: Add the user-configured OpenAI-compatible provider with Chat Completions and Responses support.
- FRQ-02: Replace the immutable startup model catalogue with configured provider and model catalogues.
- FRQ-03: Add model and reasoning selection to Programmatic Control and the standard TUI.
- FRQ-04: Resolve API keys through credential sources and keep secret values out of settings.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: OpenAI-compatible provider and configuration contract.
- DLV-02: Runtime model and reasoning selection through both client kinds.

### Acceptance Criteria

- ACC-01: A user selects a different configured model and the next request uses it without clearing conversation history.
- ACC-02: Failed credential resolution preserves the active model and returns an error before model execution.
- ACC-03: OpenAI-compatible operation without a credential source sends no authorization.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Provider-specific fields could leak into Agent Core. Provider adapters must continue mapping through the provider-neutral model contract.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype provider catalogue](../../../../../host/internal/usecase/host/providers/catalog.go) - prototype provider catalogue.
