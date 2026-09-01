# Ticket: PHS-12 - Bundled and extension-defined providers

Run every concrete model provider through one extension contract and move the bundled providers out of Host.

## Key definitions and abbreviations

- DEF-01: Provider implementation. Provider-owned authentication, model catalogue, request serialization, streaming behavior, failure classification, and provider reasoning context replay.
- DEF-02: Bundled provider extension. A provider extension distributed and enabled by default with Glyph while retaining the ordinary extension runtime and lifecycle.

## Problem Statement

- PRB-01: Adding a provider requires rebuilding Host because extensions cannot register a complete provider implementation.
- PRB-02: OpenAI Codex and OpenAI-compatible implementations run as Host infrastructure while tools and future providers run through extensions. Two provider execution paths can diverge in registration, execution, cancellation, failure handling, and lifecycle behavior.

## Target Picture

- SOL-01: Every concrete provider implementation, including the implementations distributed with Glyph, runs as an extension through one provider contract. Each provider registration becomes the runtime source for its complete provider-neutral model descriptors. Host owns provider-neutral catalogue, selection, validation, credential storage, middleware coordination, retry coordination, and dispatch.

## Scenarios

### SCN-01: Provider execution through the ordinary extension runtime

- Actor: Glyph user.
- Pre-condition: DEP-01 and DEP-02 are met.
- Trigger: Glyph starts with its bundled provider extensions and another discovered provider extension.
- Required behavior: every provider registers through the same extension contract, publishes models into the Host catalogue, authenticates through Host-provided operations, and streams responses without a provider-specific Host execution path.
- Example input and expected output: Input: authenticate with the bundled OpenAI Codex provider, select one of its models, then select a model published by another provider extension and submit text to each. Expected output: both responses stream through Agent Core, credentials and provider reasoning context remain provider-scoped, and neither provider requires a Host rebuild.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No standard-TUI-specific presentation or interaction contract.
- OSP-02: No extension package installation or persistent enablement operations. PHS-15 owns those operations.

## Dependencies and Preconditions

- DEP-01: [PHS-11](../11-resource-contributions/ticket.md) must meet all acceptance criteria.
- DEP-02: [PHS-04.1](../04.1-model-execution-capabilities/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Use one extension-owned implementation path for bundled and separately delivered providers.

### Functional Requirements

- FRQ-01: Add provider registration and removal with authentication, model catalogue, request serialization, streamed responses, failure classification, and provider reasoning context replay. Every registered model shall publish the complete provider-neutral descriptor defined by PHS-04.1, including `input`, `contextWindow`, `maxTokens`, and `toolCapabilities`. After successful registration, that descriptor shall be the runtime source for the Host catalogue, budgeting, and input validation. Programmatic Control shall expose only its defined `input`, `contextWindow`, `maxTokens`, and reasoning projection.
- FRQ-01.1: Host shall reject a complete provider registration when any published model violates the PHS-04.1 descriptor rules and shall register none of that provider's models.
- FRQ-01.2: Provider reasoning context emitted by a provider extension shall remain unchanged in persisted session data and shall return only to the owning compatible provider implementation after session resume.
- FRQ-01.3: A provider execution failure shall cross the Extension Contract with complete bounded error text, its original cause as represented by that text, and the provider-neutral failure classification required by Host retry coordination.
- FRQ-01.4: Provider transport status, response codes, and provider-specific error types shall not become public Glyph categories. The provider implementation shall map them to the provider-neutral contract while preserving their bounded text.
- FRQ-02: Move the OpenAI Codex and OpenAI-compatible provider implementations from Host infrastructure into bundled provider extensions.
- FRQ-02.1: The bundled provider extensions shall preserve the PHS-03 behavior for OpenAI Codex OAuth, OpenAI-compatible API-key resolution, Chat Completions, Responses, streamed output, usage, failure classification, reasoning formats, and provider reasoning context replay.
- FRQ-02.2: After migration, Host shall contain no provider-specific authentication, request serialization, response decoding, stream assembly, usage mapping, failure classification, or provider reasoning context replay implementation.
- FRQ-02.3: Bundled and separately delivered provider extensions shall use the same registration, execution, cancellation, failure, process runtime, and shutdown contracts.
- FRQ-02.3.1: Loss of a provider extension shall preserve the extension runtime error and shall produce the same provider-neutral failure information for bundled and separately delivered providers.
- FRQ-02.4: Provider extension registration shall replace settings-defined built-in model metadata as the runtime source for bundled model descriptors. Each bundled provider extension shall register complete descriptors from declarative extension-owned model catalogue data. Host shall not construct a bundled provider's descriptor from a provider type, model identifier, or Host-owned provider-specific defaults.
- FRQ-02.5: Executable bundled-provider code shall contain no model-identifier capability branch and no model-specific capability table. The declarative catalogue file format remains a PHS-12 technical-solution decision.
- FRQ-03: Host shall provide provider implementations with generic credential storage and client interaction.
- FRQ-03.1: Credential operations shall use the registered provider identity as their namespace to prevent accidental cross-provider reads and writes. This namespace shall not claim to isolate credentials from trusted extensions running with the user's operating-system permissions.
- FRQ-04: Integrate every registered provider with the Host model catalogue, model selection, extension model requests, provider middleware, retry coordination, and Agent Core model execution.
- FRQ-05: A provider identifier shall belong to at most one active provider registration. When two or more active extensions register the same provider identifier, Host shall reject every registration in that duplicate group and load order shall select no winner.

### Non-Functional Requirements

- NFQ-01: Implementation shall follow RED, GREEN, and REFACTOR for each behavioral slice, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, provider SDKs, provider settings, credentials, and TUI packages.

### Deliverables

- DLV-01: Public extension-provider contract and SDK support.
- DLV-02: Bundled OpenAI Codex and OpenAI-compatible provider extensions.
- DLV-03: Host provider execution that uses only provider registrations from extension runtimes.

### Acceptance Criteria

- ACC-01: The bundled OpenAI Codex and OpenAI-compatible providers start through the ordinary extension runtime and retain the provider, model, authentication, streaming, usage, reasoning, and model-selection behavior required by PHS-03 and PHS-04.1.
- ACC-02: Starting a separately delivered provider extension adds its models without changing or rebuilding Host, and each added model exposes exact `input`, `contextWindow`, and `maxTokens` values through the Host catalogue and Programmatic Control.
- ACC-03: Authentication stores provider-owned opaque credentials through Host storage for bundled and separately delivered provider extensions.
- ACC-04: Removing or losing a provider registration preserves the active model when selection of a replacement model fails.
- ACC-05: Invalid execution-capability metadata returns an explicit registration error and adds none of that provider's models. A duplicate provider identifier group registers no provider from that group.
- ACC-06: A provider extension emits opaque provider reasoning context, Glyph persists and restores it with the session, and the next compatible request returns the exact payload only to that provider implementation.
- ACC-07: Host packages contain no OpenAI Codex or OpenAI-compatible authentication, wire request, response decoding, stream assembly, usage mapping, failure classification, provider reasoning context replay, or built-in model-descriptor construction.
- ACC-08: A bundled provider extension registers one complete model descriptor with explicit `input`, `contextWindow`, `maxTokens`, and `toolCapabilities`. The Host catalogue preserves the exact complete descriptor, while Programmatic Control exposes exact `input`, `contextWindow`, `maxTokens`, and reasoning values without exposing `toolCapabilities`. Host neither reconstructs nor overwrites registered descriptor values from settings-defined built-in model metadata.
- ACC-09: Bundled provider extensions load descriptor values from declarative extension-owned model catalogue data. Executable provider code contains no model-identifier capability branch or model-specific capability table.
- ACC-09.1: Equivalent failures from a bundled provider and a separately delivered provider retain their bounded source text and produce the same provider-neutral classification at the Host boundary.

## Overengineering and Overspecification Considerations

The ticket replaces two provider execution paths with one extension contract. It adds no separate override or restoration mechanism for bundled providers. It does not select the declarative model catalogue file format before the PHS-12 technical solution. PHS-15 applies the ordinary extension package lifecycle to bundled provider extensions.

## Constraints and Risks

- RSK-01: A provider implementation can accidentally address another provider's credential record. Host namespaces credential operations by the registered provider identity. This is an API ownership rule, not a sandbox or malicious-extension security boundary.
- RSK-02: Load-order conflict resolution can replace a provider without user intent. Host rejects every provider registration in a duplicate identifier group instead of selecting one.
- RSK-03: Failure of a bundled provider extension can leave Glyph without an available model. Host shall report that provider as unavailable and preserve other extension runtimes.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes, provider configuration ownership, and package placement require a phase-specific technical solution before implementation.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [model execution capabilities](../04.1-model-execution-capabilities/ticket.md) - provider-neutral model descriptor requirements.
- REF-04: [PHS-03 technical solution](../03-providers-models-runtime-selection/solution.md) - provider behavior that the bundled provider extensions must retain.
