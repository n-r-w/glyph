# Ticket: PHS-03 - Providers, models, and runtime selection

Support the required built-in providers and model selection without ending the session.

## Key definitions and abbreviations

- DEF-01: Provider catalogue. The Glyph Host-owned set of configured provider instances and their model descriptors.
- DEF-02: Reasoning capability. A model's ability to produce reasoning content separately from its final answer.
- DEF-03: Reasoning choice. One available value of the single user-facing reasoning control: `off`, `on`, or a supported reasoning effort.
- DEF-04: Fixed reasoning. Model reasoning behavior that the user cannot change.
- DEF-05: Visible reasoning content. Provider-returned reasoning text intended for typed history and client presentation.
- DEF-06: Provider reasoning context. Opaque or encrypted provider-owned reasoning data retained for compatible request replay but not exposed to clients.
- DEF-07: Reasoning compatibility key. An optional nonempty model identifier that explicitly permits provider reasoning context replay between models with the same provider instance, API, and key.

## Problem Statement

- PRB-01: The provider catalogue is fixed at startup to one Codex model. Glyph cannot use the required OpenAI-compatible provider or change model and reasoning choice during a session.
- PRB-02: One reasoning-level list conflates reasoning capability, user control, provider wire behavior, and response content. It cannot represent on/off, fixed reasoning, or reasoning without effort control.
- PRB-03: Chat Completions reasoning content and provider-owned encrypted reasoning context can be lost, misclassified as final text, or exposed to clients.

## Target Picture

- SOL-01: Support the required built-in providers and model selection without ending the session.
- SOL-02: Expose one adaptive reasoning control, preserve visible reasoning content, and replay opaque reasoning context only to explicitly compatible requests.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: Glyph user.
- Pre-condition: DEP-01 is met.
- Trigger: the user selects another configured model and sends a request.
- Required behavior: the next request uses the selected provider, model, and reasoning choice without clearing conversation history.
- Example input and expected output: Input: select a configured OpenAI-compatible model and reasoning choice, then submit a user message. Expected output: the next provider request uses that selection and preceding conversation entries remain present.

### SCN-02: Reasoning capability projection

- Actor: Glyph user or Programmatic Control client.
- Pre-condition: The provider catalogue contains models with effort, on/off, fixed, and no reasoning capabilities.
- Trigger: the actor selects or inspects one model.
- Required behavior: Glyph exposes only the model's effective reasoning choices and its explicit default without presenting ineffective levels.

### SCN-03: Reasoning content continuity

- Actor: Glyph user.
- Pre-condition: a model response contains visible reasoning content and provider reasoning context.
- Trigger: Glyph renders the response and sends a later compatible request.
- Required behavior: visible reasoning content remains available as collapsed typed history, while provider reasoning context remains hidden and is replayed without interpretation only to a compatible request.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No extension-defined providers or provider middleware.
- OSP-02: No `!command` API-key resolution.
- OSP-03: No Together, DeepSeek, Qwen, chat-template reasoning controls, or thinking token budgets.
- OSP-04: No runtime probing of model reasoning capabilities.

## Dependencies and Preconditions

- DEP-01: [PHS-02](../02-programmatic-control/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Support the required built-in providers and model selection without ending the session.

### Functional Requirements

- FRQ-01: Support multiple user-configured provider instances of the OpenAI-compatible provider type. Each instance shall have a unique identifier and support Chat Completions, Responses, or both APIs.
- FRQ-02: Replace the immutable startup model catalogue with configured provider and model catalogues.
- FRQ-03: Add model and adaptive reasoning control to Programmatic Control and the standard TUI.
- FRQ-04: An OpenAI-compatible provider instance shall accept an optional API key as a literal value, an environment-variable reference, or a local credential-file entry reference. `!command` resolution is not supported.
- FRQ-05: Settings shall explicitly declare each model's reasoning capability, effective reasoning choices, and default reasoning choice and shall reject incomplete or contradictory combinations.
- FRQ-06: The Host shall expose one adaptive reasoning control to both client kinds: unavailable for non-reasoning models, `off` and `on` for toggle models, configured efforts with optional `off` for effort models, and no selectable alternative for fixed reasoning.
- FRQ-07: Selecting an unsupported reasoning choice shall preserve the active selection and return an error.
- FRQ-08: Model switching shall preserve a semantically compatible reasoning choice and otherwise use the target model's explicit default.
- FRQ-09: Visible reasoning content shall remain typed conversation content, remain available to clients, and remain in model-visible history for later requests. A provider driver shall replay it through a native reasoning field when the target provider format supports it and shall otherwise convert it to ordinary assistant text. The standard TUI shall collapse every reasoning block by default and use one display action to expand or collapse all blocks without changing provider behavior.
- FRQ-10: Provider reasoning context shall remain hidden from clients. A provider driver may parse and serialize its API item structure, but provider-owned opaque values shall remain unchanged. Replay shall require the same provider instance and API plus either the same model identifier or the same nonempty reasoning compatibility key.
- FRQ-11: A reasoning compatibility key shall add cross-model compatibility and shall not disable replay to the same model identifier. An absent key shall never create cross-model compatibility.
- FRQ-12: OpenAI Codex and OpenAI-compatible Responses shall own their Responses reasoning behavior without a `format` setting. OpenAI-compatible Chat Completions reasoning shall require the adapter-private `openai-chat` or `openrouter` format.
- FRQ-13: `openai-chat` shall use top-level `reasoning_effort`, streamed `delta.reasoning`, and assistant `reasoning`. `openrouter` shall use nested `reasoning`, map streamed `delta.reasoning` to visible reasoning, preserve streamed `reasoning_details` as opaque provider context, and replay compatible details on the assistant message.
- FRQ-14: Shared settings shall treat `reasoning.format` as an opaque string. The owning provider adapter shall validate accepted values and API compatibility during construction before UI startup, RPC socket creation, provider requests, or agent execution.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: OpenAI-compatible provider and configuration contract.
- DLV-02: Runtime model selection and adaptive reasoning control through both client kinds.
- DLV-03: Typed visible reasoning content, opaque provider reasoning context replay, and adaptive reasoning capability projection.

### Acceptance Criteria

- ACC-01: A user selects a different configured model and the next request uses it without clearing conversation history.
- ACC-02: Failure to resolve a referenced API key preserves the active provider, model, and reasoning choice and returns an error before applying the requested selection.
- ACC-03: An OpenAI-compatible provider instance without an API key remains selectable, and its requests contain no `Authorization` header.
- ACC-04: A Chat Completions model with `choices: [on]` and `default: on` sends no `reasoning_effort`, and its `delta.reasoning` field becomes visible reasoning content rather than final answer text.
- ACC-05: Effort, on/off, fixed, and non-reasoning models expose only their effective reasoning choices through TUI and Programmatic Control.
- ACC-06: An unsupported reasoning choice returns an error and preserves provider, model, and reasoning choice.
- ACC-07: Visible reasoning content remains available after turn completion, is collapsed by default in the standard TUI, and expands or collapses through one session-wide display action.
- ACC-08: Provider reasoning context never appears in TUI or Programmatic Control output. A later request under FRQ-10 or FRQ-11 contains the same provider-owned opaque values, while JSON serialization details can differ.
- ACC-09: Switching to a model outside the source reasoning compatibility boundary omits the source provider reasoning context from the next request.
- ACC-10: An OpenRouter Chat Completions request sends nested reasoning control. Streamed visible reasoning remains typed content, and the next compatible tool continuation replays the assembled `reasoning_details` array without exposing it to clients.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Provider-specific fields could leak into Agent Core. Provider adapters must continue mapping through the provider-neutral model contract.
- RSK-02: A wrong reasoning compatibility key can replay opaque context to an incompatible model. Cross-model replay therefore requires an explicit nonempty key and matching provider instance and API.
- RSK-03: A provider can add or change a reasoning format independently. The owning provider adapter must reject unsupported values during construction rather than falling back silently.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [prototype provider catalogue](../../../../../../host/internal/usecase/host/providers/catalog.go) - prototype provider catalogue.
- REF-04: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/models.md` - feature comparison for reasoning capabilities, level maps, and provider compatibility.
