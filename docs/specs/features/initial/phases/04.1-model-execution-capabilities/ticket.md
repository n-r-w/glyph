# Ticket: PHS-04.1 - Model execution capabilities

Expose provider-neutral model input and token limits before context and input behavior consumes them.

## Key definitions and abbreviations

- DEF-01: Input modality. One content kind accepted by a model. PHS-04.1 defines `text` and `image`.
- DEF-02: Context window. The maximum combined model input and generated-output token capacity declared for one model.
- DEF-03: Maximum output tokens. The maximum generated-output token count declared for one model.
- DEF-04: Tool capabilities. Provider-neutral strict JSON Schema, Lark grammar, and Regex grammar support declared for one model.

## Problem Statement

- PRB-01: Glyph model settings and the provider-neutral model descriptor contain reasoning, tool, and pricing capabilities but no input modalities, context window, or maximum output tokens.
- PRB-02: PHS-06 cannot calculate context and response budgets from the selected model, and PHS-08 cannot validate text and image input against model capabilities.
- PRB-03: Programmatic Control clients cannot inspect the execution limits that later Host behavior applies.

## Target Picture

- SOL-01: Every configured model has strict provider-neutral `input`, `contextWindow`, `maxTokens`, and `toolCapabilities` metadata in the Host model catalogue. Programmatic Control exposes `input`, `contextWindow`, `maxTokens`, and reasoning through its defined model projection.

## Scenarios

### SCN-01: Inspect model execution capabilities

- Actor: Programmatic Control client.
- Pre-condition: DEP-01 is met and settings contain one text-only model and one text-and-image model.
- Trigger: the client requests the model catalogue.
- Required behavior: Glyph returns each model's ordered input modalities, context window, and maximum output tokens exactly as declared in settings.
- Example input and expected output: Input: configure model `vision` with `input: [text, image]`, `contextWindow: 131072`, and `maxTokens: 16384`. Expected output: the Programmatic Control descriptor for `vision` contains both modalities in that order and both integer values.

## Scope

In scope:

- ISP-01: Strict settings fields `input`, `contextWindow`, `maxTokens`, and `toolCapabilities` for every configured model.
- ISP-02: Provider-neutral model descriptor ownership and Host model catalogue projection.
- ISP-03: Programmatic Control projection of `input`, `contextWindow`, `maxTokens`, and reasoning, with public-contract evidence.

Out of scope:

- OSP-01: No context budgeting, compaction, retry policy, or overflow recovery.
- OSP-02: No image acquisition, decoding, resizing, presentation, or model-input rejection.
- OSP-03: No provider probing, provider fallback, model selection behavior, or extension-defined provider registration.

## Dependencies and Preconditions

- DEP-01: [PHS-04](../04-persistent-linear-sessions/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Define one provider-neutral model descriptor contract shared by settings, built-in providers, later extension-defined providers, and the Host catalogue. Programmatic Control exposes only the projection defined by FRQ-06.

### Functional Requirements

- FRQ-01: Every configured model shall require a nonempty ordered `input` list whose values come from the closed set `text` and `image`.
- FRQ-02: Settings loading shall reject an unknown input modality, a duplicate modality, an empty modality list, and a model whose list does not contain `text`.
- FRQ-03: Every configured model shall require integer `contextWindow` and `maxTokens` values greater than zero. Settings loading shall reject `maxTokens` greater than `contextWindow`.
- FRQ-04: The provider-neutral model descriptor shall contain the ordered input modalities, context window, and maximum output tokens without provider wire or settings serialization types.
- FRQ-04.1: Settings shall be the persisted configuration source for settings-defined models, including `toolCapabilities`, until PHS-12. PHS-12 extension provider registration shall be the source for extension-defined models. After validation, the provider-neutral descriptor shall be the runtime source for the Host catalogue, PHS-06 budgeting, and PHS-08 input validation. Programmatic Control shall read the descriptor but expose only the projection defined by FRQ-06.
- FRQ-04.2: Every settings-defined model shall require a top-level `toolCapabilities` mapping. The mapping shall preserve `strictJSONSchema`, `grammar.lark`, and `grammar.regex` exactly. An omitted boolean member inside the mapping shall mean false.
- FRQ-05: The Host model catalogue shall return defensive copies that preserve the complete provider-neutral descriptor, including all settings-defined tool capabilities.
- FRQ-06: Programmatic Control model catalogue results shall expose the ordered input modalities, context window, maximum output tokens, and reasoning for every model. Programmatic Control shall not expose `toolCapabilities` in PHS-04.1.
- FRQ-07: Built-in provider entries shall use the same provider-neutral descriptor contract required for extension-defined providers in PHS-12.
- FRQ-08: Invalid execution-capability settings shall fail Glyph startup before a UI plugin, Programmatic Control socket, provider request, or agent run starts.

### Non-Functional Requirements

- NFQ-01: Focused settings, catalogue, and Programmatic Control work shall follow RED, GREEN, and REFACTOR, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of settings DTOs, protobuf, gRPC, plugin SDKs, persistence adapters, provider SDKs, and TUI packages.

### Deliverables

- DLV-01: Strict settings and provider-neutral model execution-capability model.
- DLV-02: Complete Host catalogue descriptors and the defined Programmatic Control projection.

### Acceptance Criteria

- ACC-01: A model configured with `input: [text, image]`, `contextWindow: 131072`, `maxTokens: 16384`, and explicit `toolCapabilities` loads with those exact descriptor values.
- ACC-01.1: A known Codex model configured with all tool capabilities false and an arbitrary Codex model configured with all tool capabilities true expose those configured values. Provider type and model identifier do not change them.
- ACC-02: Table-driven settings tests reject an empty input list, missing `text`, duplicate values, an unknown value, zero or negative limits, and `maxTokens` greater than `contextWindow`.
- ACC-03: A Programmatic Control model query returns exact `input`, `contextWindow`, `maxTokens`, and reasoning values for text-only and text-and-image models without exposing `toolCapabilities`, settings DTOs, or provider wire DTOs.
- ACC-04: Failure under ACC-02 starts no UI plugin, Programmatic Control socket, provider request, or agent run.

## Overengineering and Overspecification Considerations

The ticket adds required execution and tool-capability fields through settings and the Host catalogue. Programmatic Control receives only `input`, `contextWindow`, and `maxTokens` in addition to its existing reasoning projection. The ticket does not implement PHS-06 and PHS-08 consumers, infer capabilities from provider or model names, or add modalities beyond text and image.

## Constraints and Risks

- RSK-01: A settings, domain, or Programmatic Control projection can omit one field that it owns and create inconsistent behavior. ACC-01 compares the complete settings-to-catalogue descriptor, while ACC-03 compares every Programmatic Control field.
- RSK-02: Treating an absent capability as an inferred default can hide incomplete configuration. All three fields are required and have no fallback.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No package, Go type, protobuf field number, or implementation sequence is selected by this ticket. Those decisions require a phase-specific Technical Solution before implementation.

## References

- REF-01: [target product requirements](../../prd.md) - target model-provider and Programmatic Control behavior.
- REF-02: [target architecture](../../architecture.md) - model descriptor ownership and component boundaries.
- REF-03: [ticket order and ownership](../../delivery-plan.md) - delivery order and dependencies.
- REF-04: [PHS-03](../03-providers-models-runtime-selection/ticket.md) - existing provider-neutral model catalogue and selection behavior.
