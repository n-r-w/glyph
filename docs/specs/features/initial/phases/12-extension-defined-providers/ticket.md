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
- Pre-condition: DEP-01 and DEP-02 are met.
- Trigger: an installed extension registers a provider.
- Required behavior: the provider authenticates through Host, publishes models, and streams a model response without a Host rebuild.
- Example input and expected output: Input: start a provider extension, complete its authentication interaction, select one published model, and submit text. Expected output: credentials remain provider-scoped and the response streams through Agent Core.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No standard-TUI-specific presentation or interaction contract.

## Dependencies and Preconditions

- DEP-01: [PHS-11](../11-resource-contributions/ticket.md) must meet all acceptance criteria.
- DEP-02: [PHS-04.1](../04.1-model-execution-capabilities/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Allow an installed extension to add and remove complete model provider implementations.

### Functional Requirements

- FRQ-01: Add provider registration and removal with authentication, model catalogue, request serialization, streamed responses, and provider reasoning context replay. Every registered model shall publish the provider-neutral `input`, `contextWindow`, and `maxTokens` descriptor fields defined by PHS-04.1.
- FRQ-01.1: Host shall reject the complete provider registration when any published model violates the PHS-04.1 descriptor rules and shall register none of that provider's models.
- FRQ-01.2: Provider reasoning context emitted by an extension-defined provider shall remain unchanged in persisted session data and shall return only to the owning compatible provider implementation after session resume.
- FRQ-02: Expose generic credential storage and client interaction to provider implementations.
- FRQ-02.1: Credential operations shall use the registered provider identity as their namespace to prevent accidental cross-provider reads and writes. This namespace shall not claim to isolate credentials from trusted extensions running with the user's operating-system permissions.
- FRQ-03: Integrate registered providers with model selection, extension model requests, and provider middleware.
- FRQ-04: An extension-defined provider shall use an identifier not owned by another configured or extension-defined provider. A conflict with a settings-defined provider shall reject the extension registration and preserve the settings-defined provider. A duplicate group of extension-defined providers shall reject every extension registration in that group. Load order shall select no winner.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Public extension-provider contract and SDK support.
- DLV-02: Reference provider extension with interactive authentication and streaming.

### Acceptance Criteria

- ACC-01: Installing the reference extension adds its models without changing or rebuilding Host, and each added model exposes exact `input`, `contextWindow`, and `maxTokens` values through the Host catalogue and Programmatic Control.
- ACC-02: Authentication stores provider-owned opaque credentials through Host storage.
- ACC-03: Removing the provider preserves the active model when selection of a replacement model fails.
- ACC-04: Invalid execution-capability metadata returns an explicit registration error and adds none of that provider's models. A settings-defined identifier conflict preserves the settings-defined provider, and a duplicate extension-defined identifier group registers no provider from that group.
- ACC-05: The reference provider emits opaque provider reasoning context, Glyph persists and restores it with the session, and the next compatible request returns the exact payload only to that provider implementation.

## Overengineering and Overspecification Considerations

The ticket adds providers only under distinct identifiers and the provider-neutral model descriptor. It adds no built-in-provider override, provider restoration, or load-order winner behavior. OSP-01 remains outside the ticket.

## Constraints and Risks

- RSK-01: A provider implementation can accidentally address another provider's credential record. Host namespaces credential operations by the registered provider identity. This is an API ownership rule, not a sandbox or malicious-extension security boundary.
- RSK-02: Load-order conflict resolution can replace a provider without user intent. Host rejects every extension-defined provider in a duplicate identifier group instead of selecting one.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [model execution capabilities](../04.1-model-execution-capabilities/ticket.md) - provider-neutral model descriptor requirements.
