# Ticket: PHS-12 - Extension-defined providers

Allow an installed extension to add and remove complete model provider implementations.

## Key definitions and abbreviations

- DEF-01: Provider implementation. Provider-owned authentication, model catalogue, serialization, and streaming behavior.

## Problem Statement

- PRB-01: Adding a provider requires rebuilding Host because extensions cannot register authentication, model catalogue, request serialization, and streaming behavior.

## Target Picture

- SOL-01: Allow an installed extension to add and remove complete model provider implementations.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 is met.
- Trigger: an installed extension registers a provider.
- Required behavior: the provider authenticates through Host, publishes models, and streams a model response without a Host rebuild.
- Example input and expected output: Input: start a provider extension, complete its authentication interaction, select one published model, and submit text. Expected output: credentials remain provider-scoped and the response streams through Agent Core.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No standard-TUI-specific presentation or interaction contract.

## Dependencies and Preconditions

- DEP-01: [PHS-11](11-resource-contributions.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Allow an installed extension to add and remove complete model provider implementations.

### Functional Requirements

- FRQ-01: Add provider registration and removal with authentication, model catalogue, request serialization, and streamed responses.
- FRQ-02: Expose generic credential storage and client interaction to provider implementations.
- FRQ-03: Integrate registered providers with model selection, extension model requests, and provider middleware.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Public extension-provider contract and SDK support.
- DLV-02: Reference provider extension with interactive authentication and streaming.

### Acceptance Criteria

- ACC-01: Installing the reference extension adds its models without changing or rebuilding Host.
- ACC-02: Authentication stores provider-owned opaque credentials through Host storage.
- ACC-03: Removing the provider preserves the active model when selection of a replacement model fails.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Provider callbacks can expose credential contents to unrelated extensions. Credential operations remain scoped to the registered provider identity.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
