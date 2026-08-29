# Technical Solution: PHS-03 Providers, Models, and Runtime Selection

## Problem Statement

- PRB-01: `model.ReasoningLevel` represents model capability, user control, request mapping, and active selection as one effort list. It cannot represent toggle reasoning or fixed reasoning without ineffective choices.
- PRB-02: OpenAI-compatible Chat Completions sends `reasoning_effort` for every non-`none` value but does not map streamed reasoning fields into typed reasoning content.
- PRB-03: Provider reasoning context is scoped only by provider instance. Replay can therefore cross an API or model boundary that does not accept the source context.
- PRB-04: The standard TUI maps reasoning content to an unspecified presentation kind and cannot retain or display reasoning blocks.
- PRB-05: The solution must retain visible reasoning in model-visible history while keeping Agent Core independent of settings, credentials, protobuf, gRPC, provider SDKs, persistence adapters, and TUI packages.

## Proposed Solution

### Solution overview

- SOL-01: Replace `ReasoningLevel` with provider-neutral `ReasoningChoice` and `ReasoningCapabilities` throughout settings, the model domain, runtime selection, Programmatic Control, and the UI plugin contract.
- SOL-02: Require every configured model to declare its reasoning capability, effective choices, and explicit default. OpenAI-compatible Chat Completions reasoning also declares an adapter-private format.
- SOL-03: Keep reasoning request, stream, and replay mapping inside provider drivers. Agent Core receives only capabilities, active choice, typed visible reasoning, and opaque provider context.
- SOL-04: Retain visible reasoning in every later model request. A target provider driver uses its native reasoning representation or converts the reasoning to ordinary assistant text.
- SOL-05: Scope provider reasoning context by source provider instance, API, model, and optional compatibility key.
- SOL-06: Expose `Supported`, ordered `Choices`, `Default`, and the active reasoning choice to both Programmatic Control and the standard TUI.
- SOL-07: Store reasoning blocks in the TUI transcript, collapse all blocks by default, and control their display through one TUI-local state.

### Terms and ownership

- ENT-01: A provider type defines shared protocol and authentication behavior. PHS-03 has `openai-codex` and `openai-compatible` provider types.
- ENT-02: A provider instance is one configured provider with a unique identifier, models, endpoint, and authentication configuration.
- ENT-03: A provider driver implements one provider type's authentication, wire requests, streaming responses, and provider reasoning context replay.
- ENT-04: A model selection contains a provider instance identifier, model identifier, and reasoning choice.
- ENT-05: `ReasoningChoice` has the closed values `off`, `on`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`.
- ENT-06: `ReasoningCapabilities` contains `Supported`, ordered `Choices`, and `Default`.
- ENT-07: A reasoning compatibility key is an optional nonempty model setting that adds cross-model provider reasoning context compatibility within one provider instance and API.
- ENT-08: A reasoning format selects one private OpenAI-compatible Chat Completions implementation for request control, stream fields, and native history replay.
- CMP-01: `host/internal/infra/persistence/settings` parses the public settings shape and validates provider-neutral capability structure. It treats `reasoning.format` as opaque and does not validate provider values or map wire fields.
- CMP-02: `host/internal/usecase/host/providers` owns the provider catalogue, active selection, capability validation, fallback choice, and provider lookup.
- CMP-03: `host/internal/usecase/agent/run` owns the consumer-side `ModelRuntime` and `ModelProvider` interfaces.
- CMP-04: `host/internal/infra/providers/openai/codex` owns the Codex provider driver.
- CMP-05: `host/internal/infra/providers/openai/compatible` owns one OpenAI-compatible provider driver per configured provider instance.
- CMP-06: `host/internal/app` maps validated settings into model descriptors and private provider-driver configurations.

### Settings contract

- APC-01: The settings file requires `defaultProvider`, `defaultModel`, and a nonempty `providers` map. `defaultReasoningLevel` is removed because each model owns its reasoning default.
- APC-02: Every model entry requires `id` and a `reasoning` mapping with `supported`, `choices`, and `default`.
- APC-03: A model with `supported: false` requires `choices: [off]`, `default: off`, no `compatibilityKey`, and no `format`.
- APC-04: Fixed reasoning requires `supported: true`, `choices: [on]`, and `default: on`.
- APC-05: Toggle reasoning requires `supported: true`, exactly the choices `off` and `on`, and a default from those choices.
- APC-06: Effort reasoning requires `supported: true`, at least one effort choice, no `on`, an optional `off`, and a default from the configured choices.
- APC-07: OpenAI Codex and OpenAI-compatible Responses reasoning has no `format`. OpenAI-compatible Chat Completions reasoning requires `format: openai-chat` or `format: openrouter`.
- APC-08: For reasoning configuration, shared settings validation enforces APC-03 through APC-06 and APC-09. The OpenAI-compatible adapter validates a present format and its API compatibility during construction.
- APC-09: A present `compatibilityKey` must be nonempty after trimming. It is valid only when `supported` is true.
- APC-10: Duplicate choices, unknown choices, a default outside `choices`, and an invalid capability shape fail settings loading.
- APC-11: Provider instance identifiers and model identifiers must be nonempty after trimming. Model identifiers must be unique within one provider instance.
- APC-12: An `openai-compatible` instance requires `baseURL`, an API, and a nonempty model list. A nonempty model API overrides its provider instance API.
- APC-13: An `openai-compatible` instance can omit `apiKey`. A present `apiKey` contains exactly one nonempty `literal`, `environment`, or `credential` value.
- APC-14: Unknown fields, provider types, and APIs fail settings loading. Unknown or API-incompatible reasoning formats fail OpenAI-compatible adapter construction with provider and model context.

Example:

```yaml
defaultProvider: openai-codex
defaultModel: gpt-5.6-luna
activeUI: standard-tui

providers:
  openai-codex:
    type: openai-codex
    api: responses
    models:
      - id: gpt-5.6-luna
        reasoning:
          supported: true
          choices: [off, low, medium, high, xhigh]
          default: high

  ollama:
    type: openai-compatible
    baseURL: http://localhost:11434/v1
    api: chat-completions
    models:
      - id: smtek/ornith-1.5:35b
        reasoning:
          supported: true
          choices: [on]
          default: on
          compatibilityKey: ornith-1.5
          format: openai-chat
```

### API-key resolution

- DEC-01: `apiKey.literal` is the API key. Values beginning with `!` have no special behavior.
- DEC-02: `apiKey.environment` names one process environment variable. A missing or empty variable is a resolution error.
- DEC-03: `apiKey.credential` names one local credential-file entry whose payload is `{"type":"api_key","key":"..."}`. A missing entry, another credential type, or an empty key is a resolution error.
- DEC-04: An omitted `apiKey` resolves to no key. The provider driver sends no `Authorization` header.
- DEC-05: A referenced API key is resolved before model selection commits and again before each provider request. A failure starts no HTTP request and preserves the active selection.
- CNS-01: Settings errors, logs, Programmatic Control responses, UI frames, and provider diagnostics must not contain literal keys, resolved environment values, credential-file key values, OAuth tokens, or authorization headers.

### Model domain and provider catalogue

- ENT-09: `model.Descriptor` contains provider instance ID, model ID, `ReasoningCapabilities`, and tool capabilities. It does not contain a reasoning format.
- ENT-10: `model.Selection` contains provider instance ID, model ID, and active `ReasoningChoice`.
- ENT-11: A `ContentReasoning` value contains visible text and an optional `ProviderContext`. The separate `ContentProviderContext` kind is removed so one reasoning block owns its opaque replay data.
- ENT-12: `ProviderContext` contains a source snapshot with provider instance ID, API, model ID, compatibility key, and an opaque provider-driver payload.
- APC-15: Catalogue query results are defensive copies ordered by provider instance identifier and model configuration order.
- APC-16: `Catalog.SelectReasoningChoice` accepts only a choice listed by the active model. Rejection preserves the complete active selection.
- APC-17: Model selection first preserves an exact active choice.
- APC-18: When exact preservation is impossible, effort choices use the ordered ranks `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`. The catalogue chooses the minimum absolute rank distance and chooses the lower rank on a tie.
- APC-19: An effort maps to `on` for a toggle or fixed target. `on` maps to the explicit default of an effort target. `off` is preserved only when the target lists `off`. Every other case uses the target default.
- DEC-06: Selection can commit while an agent run is active. An in-progress provider request retains its snapshot, and the next request reads the committed selection.
- DEC-07: Selection does not clear, rewrite, or partition conversation history.

### Agent Core boundary

- APC-20: `ModelRuntime.Current` returns one immutable runtime selection with `model.Descriptor`, active `ReasoningChoice`, and the `ModelProvider` for that request.
- APC-21: `ModelRequest` carries the selected descriptor, active reasoning choice, conversation history, instructions, and tools.
- APC-22: Agent Core reads `ModelRuntime.Current` immediately before every `ModelProvider.Stream` call.
- APC-23: Agent Core stores visible reasoning and opaque provider context as model-domain content without interpreting provider context or choosing wire fields.
- CNS-02: Agent Core does not list models, resolve credentials, parse settings, select an OpenAI API, or import Host, protobuf, gRPC, provider SDK, persistence, or TUI packages.
- CNS-03: Selection changes cannot mutate an in-progress stream handler or partial response.

### Provider drivers

- DEC-08: Concrete `compatible.Service` and `codex.Service` types become `compatible.Driver` and `codex.Driver`. No rename is applied to `run.ModelProvider`, provider package names, or provider directory names.
- CMP-07: Each provider driver receives private per-model configuration containing API and reasoning compatibility key. The OpenAI-compatible driver also receives the raw `reasoning.format` value for reasoning models and parses it into its private closed type. Only `ReasoningCapabilities` enters the model descriptor.
- APC-24: A provider driver rejects a request whose provider instance or model does not match its configuration.
- APC-25: A provider driver owns reasoning request mapping, streamed reasoning parsing, final response mapping, native visible-reasoning replay, provider-context replay, and text fallback.
- APC-26: Visible reasoning always remains in model-visible history. A target provider driver uses native reasoning input when its provider format accepts the source block and otherwise converts visible reasoning to ordinary assistant text.
- APC-27: Opaque provider context is compatible only when source and target provider instance IDs and APIs match and either model IDs match or both models have the same nonempty reasoning compatibility key.
- APC-28: An exact model match remains compatible when the configured reasoning compatibility key changes. A key only adds cross-model compatibility.
- APC-29: Incompatible opaque context is omitted from the request. Its visible reasoning remains subject to APC-26.
- APC-30: A provider driver does not validate the semantic contents of encrypted provider data. A remote rejection returns a provider request error and leaves the active selection unchanged.

| ID | Owning API or format | Request mapping | Response and history mapping |
|---|---|---|---|
| WFM-01 | Responses API | `off` sends `reasoning.effort: "none"`; an effort sends its value; `on` omits effort and uses the provider default | Reasoning summary becomes visible reasoning. A stable reasoning ID and encrypted content become opaque provider context. Compatible context becomes a Responses reasoning input item |
| WFM-02 | `openai-chat` | `off` sends `reasoning_effort: "none"`; an effort sends its value; `on` omits `reasoning_effort` | Streamed `delta.reasoning` becomes visible reasoning. Native history uses the assistant `reasoning` field |
| WFM-03 | `openrouter` | `off` sends `reasoning: { effort: "none" }`; an effort sends `reasoning: { effort: <choice> }`; `on` sends `reasoning: { enabled: true }` | Streamed `delta.reasoning` becomes visible reasoning. Streamed `reasoning_details` is assembled in order, stored as opaque provider context, and replayed unchanged on a compatible assistant message |

- APC-31: The OpenAI-compatible Chat Completions driver reads `delta.reasoning` and `reasoning_details` through OpenAI SDK response extra fields. It writes assistant `reasoning` for visible native history or assistant `reasoning_details` when compatible opaque OpenRouter context exists.
- APC-32: The Responses drivers retain the stable response item ID, encrypted content, and summary values without interpreting encrypted content. Driver-owned serialization reconstructs the Responses reasoning input item, and its JSON key order and escaping are not part of provider reasoning context.
- APC-32.1: The OpenRouter format merges consecutive streamed `reasoning.text` and `reasoning.summary` fragments, retains encrypted and unknown detail fields without semantic interpretation, and preserves detail order.
- APC-33: Provider rejection of replayed reasoning produces the provider driver's terminal request failure and does not alter the active model selection.

### Programmatic Control contract

- APC-34: `ReasoningLevel` becomes `ReasoningChoice` with an unspecified zero value followed by `OFF`, `ON`, `MINIMAL`, `LOW`, `MEDIUM`, `HIGH`, `XHIGH`, and `MAX`.
- APC-35: `ConfiguredModel` contains provider ID, model ID, and one `ReasoningCapabilities` message with `supported`, ordered `choices`, and `default_choice`.
- APC-36: `ModelSelection` contains provider ID, model ID, and `reasoning_choice`.
- APC-37: `SelectReasoningLevel` becomes `SelectReasoningChoice`. Unsupported and unspecified choices return `REJECTION_CODE_REASONING_UNSUPPORTED` and preserve selection.
- APC-38: Programmatic Control exposes visible reasoning through typed model content and never serializes `ProviderContext`.
- DEC-09: Old reasoning-level protobuf fields, enums, commands, mappings, and translation paths are removed. PHS-03 adds no compatibility layer.

### Standard TUI contract and behavior

- APC-39: The UI plugin protobuf exposes `ReasoningChoice`, `ReasoningCapabilities.supported`, ordered `choices`, `default_choice`, and `ModelSelection.reasoning_choice` with the values defined by APC-34 through APC-36.
- APC-40: The standard TUI maps reasoning frames to a presentation reasoning kind and retains every reasoning block in transcript state.
- APC-41: One TUI-local boolean controls all reasoning blocks. Its initial value is collapsed.
- APC-42: The local display action changes only the boolean from APC-41. It sends no Host command and does not change active reasoning choice or provider requests.
- APC-43: Expanded reasoning uses the transcript's terminal-width-aware wrapping. Collapsed reasoning renders one block marker without reasoning text.
- APC-44: The TUI hides reasoning selection when the selected model has one effective choice. This covers non-reasoning and fixed-reasoning models.
- APC-45: Model and reasoning selectors update only from Host-confirmed initialization or selection-change frames.

### Application composition

- STP-01: Load and strictly validate settings.
- STP-02: Resolve API-key configuration through the generic credential and environment readers.
- STP-03: Construct one Codex provider driver and one OpenAI-compatible provider driver for each configured provider instance.
- STP-04: Build model descriptors from public capabilities and build separate private driver model configurations with API, raw OpenAI-compatible reasoning format, and compatibility key.
- STP-05: Construct the provider catalogue with the default model and that model's explicit default reasoning choice.
- STP-06: Pass the catalogue through Agent Core, Programmatic Control, and UI consumer-owned interfaces without sharing transport DTOs.

### Failure behavior

- FLR-01: Invalid settings fail startup before a UI process, Programmatic Control socket, provider request, or agent run starts.
- FLR-02: An unknown provider-model pair returns `NOT_FOUND` through Programmatic Control and sends the TUI an error frame without a selection-change frame.
- FLR-03: An unsupported reasoning choice returns the reasoning rejection category and preserves provider, model, and reasoning choice.
- FLR-04: A missing or invalid referenced API key returns `CREDENTIAL_UNAVAILABLE`, identifies the configured source name without a resolved credential value, and preserves selection.
- FLR-05: An omitted API key is not a credential error. Remote authentication failures follow provider error mapping.
- FLR-06: Incompatible provider reasoning context is omitted while visible reasoning remains in the request under APC-26.
- FLR-07: Remote rejection of provider reasoning context produces a terminal provider failure. Glyph does not retry the request without that context.

### Test strategy

Implementation follows RED, GREEN, REFACTOR, and VERIFY. Generated protobuf code is compile setup, not behavioral GREEN.

| ID | Purpose | Inputs and expected outputs | Edge cases | Dependencies |
|---|---|---|---|---|
| TSK-01 | Prove settings capability validation | Load fixed, toggle, effort, and non-reasoning models; expect exact capabilities, opaque formats, and per-model defaults | Duplicate choices, default outside choices, key or format on a non-reasoning model | Settings fixtures only |
| TSK-02 | Prove catalogue fallback and atomic selection | Switch between capability shapes; expect exact preservation or the APC-18 and APC-19 result | Equal-distance effort tie, target without `off`, unsupported direct choice, credential preflight failure | Provider catalogue fixture |
| TSK-03 | Prove OpenAI Responses reasoning behavior | Send each effective choice and replay history through an `httptest.Server`; expect exact reasoning request fields, visible summaries, and compatible encrypted context without a format setting | `off`, `on`, effort, same model, shared key, different API, different provider instance | OpenAI SDK and provider-neutral fixtures |
| TSK-04 | Prove OpenAI Chat Completions reasoning behavior | Stream `delta.reasoning`; expect typed visible reasoning and assistant `reasoning` in later history | Empty reasoning chunks, final text after reasoning, effort choice, fixed-on choice, native replay | OpenAI SDK response extra fields and request override |
| TSK-05 | Prove model-visible fallback | Send reasoning history to a driver that cannot use its native representation; expect ordinary assistant text and no opaque context | Empty visible text, incompatible encrypted context, multiple reasoning blocks | Driver history fixtures |
| TSK-06 | Prove client capability projection | Query and select through Programmatic Control and UI mappings; expect identical capabilities and active choice with no provider context | Fixed and non-reasoning selectors, unspecified choice, unsupported choice | Generated protobuf code |
| TSK-07 | Prove TUI reasoning display | Stream reasoning while collapsed, toggle display, and resize; expect retained hidden text, one global state, and wrapped expanded text | Multiple blocks, empty block, narrow terminal, selection change while expanded | Existing TUI harness |
| TSK-08 | Prove integrated runtime behavior | Switch models during a run and continue; expect the next request to use the new selection with all visible reasoning history | Provider error during context replay, keyless provider, selection race around request snapshot | Application composition and local test servers |
| TSK-09 | Prove OpenRouter reasoning continuity | Send `off`, effort, and `on`; stream visible reasoning plus fragmented text, summary, and encrypted details; then continue a tool call | Details before tool deltas, opaque vendor fields, compatible replay, incompatible context omission | OpenAI SDK extra fields, `ProviderContext`, and local test server |

- DEC-10: Each behavioral slice starts with a focused test that compiles and fails on its expected assertion. Missing generated symbols use only the smallest protobuf or mock generation setup before RED.
- DEC-11: New tests use `testify/suite`, `t.Context()`, and generated `go.uber.org/mock` mocks when an interface mock is required.
- DEC-12: Final verification runs `task generate` twice, `go mod tidy -diff`, `task lint`, `task test`, `task build`, `task test-coverage`, and `git diff --check`.

## Overengineering and Overspecification Considerations

- TRD-01: The solution implements two closed private Chat Completions reasoning formats and existing Responses reasoning behavior. It does not add a global format registry, runtime probing, or arbitrary request-field configuration.
- TRD-02: Reasoning compatibility uses one optional key and exact provider instance, API, and model metadata. It does not infer compatibility from model names or endpoints.
- TRD-03: Agent Core stores opaque provider context but does not import provider SDK types or interpret encrypted data.
- TRD-04: The standard TUI uses one display state for all reasoning blocks. It does not add per-block persistence or per-provider rendering policy.
- TRD-05: The project requires no backwards compatibility. The solution removes old settings and protobuf reasoning-level contracts instead of adding adapters or aliases.

## Open Questions

None.

## References

- REF-01: `docs/specs/features/initial/phases/03-providers-models-runtime-selection/ticket.md` - owning ticket and acceptance criteria.
- REF-02: `docs/specs/features/initial/prd.md` - product behavior and component boundaries.
- REF-03: `docs/terms.md` - domain terminology.
- REF-04: `host/internal/domain/model/model.go` - model selection, typed reasoning content, and provider context domain types.
- REF-05: `host/internal/usecase/host/providers/catalog.go` - catalogue selection and fallback behavior.
- REF-06: `host/internal/infra/persistence/settings/service.go` - strict settings schema and validation.
- REF-07: `host/internal/infra/providers/openai/compatible/chat.go` - Chat Completions request, history, and stream mapping.
- REF-08: `host/internal/infra/providers/openai/compatible/responses.go` - Responses reasoning and encrypted context mapping.
- REF-08.1: `host/internal/infra/providers/openai/compatible/reasoning.go` - private Chat Completions format parsing, request mapping, OpenRouter detail assembly, and replay.
- REF-09: `api/programmatic/v1/programmatic.proto` - Programmatic Control model-selection contract.
- REF-10: `api/plugins/ui/v1/ui.proto` - UI plugin model-selection and model-content contract.
- REF-11: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/api/transform-messages.js` - Pi same-model reasoning replay and cross-model visible-text fallback reference.
- REF-12: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/api/openai-responses.js` - Pi OpenAI Responses reasoning effort and encrypted-content reference.
- REF-13: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/api/openai-completions.js` - Pi OpenRouter request mapping and `reasoning_details` replay comparison.
- REF-14: `https://openrouter.ai/docs/guides/best-practices/reasoning-tokens` - OpenRouter reasoning controls, streamed detail shape, and replay contract.
