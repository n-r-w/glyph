# Technical Solution: PHS-03 Providers, Models, and Runtime Selection

## Problem Statement

- PRB-01: Glyph constructs one OpenAI Codex provider, one model descriptor, and one reasoning level at startup. The agent core uses that fixed selection for every model request.
- PRB-02: Settings cannot describe multiple provider instances or explicit model catalogues. Glyph has no OpenAI-compatible adapter for Chat Completions or Responses.
- PRB-03: Programmatic Control and the standard TUI cannot list configured models or change the active provider instance, model, or reasoning level.
- PRB-04: Runtime selection must preserve conversation history and keep the agent core independent of settings, credentials, protobuf, gRPC, provider SDKs, and TUI packages.

## Proposed Solution

### Solution overview

- SOL-01: Replace the one-entry startup catalogue with a concurrency-safe Host provider catalogue built from strict settings.
- SOL-02: Configure one OpenAI Codex provider instance and any number of provider instances whose type is `openai-compatible`.
- SOL-03: Add provider-neutral reasoning capabilities and model selection to the model domain. The agent core reads one catalogue snapshot immediately before each model request.
- SOL-04: Add an OpenAI-compatible adapter with Chat Completions and Responses request paths selected by provider configuration and optional model overrides.
- SOL-05: Expose catalogue queries, model selection, and reasoning selection through Programmatic Control and the standard TUI.
- SOL-06: Preserve conversation history when selection changes. Provider adapters include opaque provider context only when its provider instance identifier matches the request model.

### Terms and ownership

- ENT-01: A provider type defines shared protocol and authentication behavior. PHS-03 has `openai-codex` and `openai-compatible` provider types.
- ENT-02: A provider instance is one configured provider with a unique identifier, models, endpoint, and authentication configuration.
- ENT-03: A model selection contains a provider instance identifier, model identifier, and reasoning level.
- CMP-01: `host/internal/infra/persistence/settings` parses and validates the settings file shape. It does not construct provider SDK clients.
- CMP-02: `host/internal/usecase/host/providers` owns the configured provider catalogue, active selection, selection rules, and provider lookup.
- CMP-03: `host/internal/usecase/agent/run` owns the consumer-side runtime interface used to obtain the selection for a model request.
- CMP-04: `host/internal/infra/providers/openai/codex` remains the OpenAI Codex adapter.
- CMP-05: `host/internal/infra/providers/openai/compatible` owns OpenAI-compatible Chat Completions and Responses serialization, streaming, and provider-neutral mapping.
- CMP-06: `host/internal/app` maps validated settings into provider configurations and wires concrete implementations. It contains no selection or credential-resolution rules.

### Settings contract

- APC-01: The settings file requires `defaultProvider`, `defaultModel`, and `defaultReasoningLevel`; retains optional `activeUI`; replaces `defaultThinkingLevel`; and adds a required `providers` map keyed by provider instance identifier.
- APC-02: Provider instance identifiers and model identifiers must be nonempty after trimming. Provider instance identifiers must be unique by map construction, and model identifiers must be unique within one instance.
- APC-03: `type` is required for every provider instance. Its PHS-03 values are `openai-codex` and `openai-compatible`.
- APC-04: Exactly one provider instance has type `openai-codex`, and its identifier is `openai-codex`. It has an explicit nonempty model list.
- APC-05: Each `openai-compatible` instance requires `baseURL`, `api`, and a nonempty model list. `api` is `chat-completions` or `responses`.
- APC-06: A model entry requires `id` and a nonempty `reasoningLevels` list. It can override its provider instance `api` with `chat-completions` or `responses`.
- APC-07: Reasoning levels use the closed set `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`. Duplicate configured levels are invalid.
- APC-08: `defaultProvider` and `defaultModel` must identify a configured model. `defaultReasoningLevel` must be listed by that model. Invalid defaults fail settings loading.
- APC-09: An `openai-compatible` instance can omit `apiKey`. When present, `apiKey` is a mapping with exactly one nonempty field from `literal`, `environment`, or `credential`.
- APC-10: Unknown provider types, unknown APIs, unknown fields, empty API-key values, unsupported reasoning levels, invalid absolute HTTP or HTTPS base URLs, and provider-specific fields on the wrong provider type fail settings loading.

Example:

```yaml
defaultProvider: openai-codex
defaultModel: gpt-5.6-luna
defaultReasoningLevel: high
activeUI: standard-tui

providers:
  openai-codex:
    type: openai-codex
    models:
      - id: gpt-5.6-luna
        reasoningLevels: [none, low, medium, high, xhigh]

  openrouter:
    type: openai-compatible
    baseURL: https://openrouter.ai/api/v1
    api: chat-completions
    apiKey:
      environment: OPENROUTER_API_KEY
    models:
      - id: anthropic/claude-sonnet-4
        reasoningLevels: [none, low, medium, high]
      - id: openai/gpt-5
        api: responses
        reasoningLevels: [none, low, medium, high]

  ollama:
    type: openai-compatible
    baseURL: http://localhost:11434/v1
    api: chat-completions
    models:
      - id: qwen3-coder
        reasoningLevels: [none]
```

### API-key resolution

- DEC-01: `apiKey.literal` is used as the API key. Literal values are permitted in settings by explicit product decision.
- DEC-02: `apiKey.environment` names one process environment variable. A missing or empty variable is a resolution error.
- DEC-03: `apiKey.credential` names one entry in the local credential file. The entry payload is `{"type":"api_key","key":"..."}`. A missing entry, another credential type, or an empty key is a resolution error.
- DEC-04: An omitted `apiKey` resolves to no key. The adapter sends no `Authorization` header and keeps the provider instance available for selection.
- DEC-05: API-key resolution never executes a command. Values beginning with `!` have no special behavior when stored in `literal`.
- DEC-06: A referenced API key is resolved before model selection commits and again before each provider request. A selection-time failure preserves the complete active selection. A request-time failure starts no HTTP request and leaves the active selection unchanged.
- CNS-01: Settings parse errors, logs, Programmatic Control responses, UI frames, and provider diagnostics must not contain literal keys, resolved environment values, credential-file key values, OAuth tokens, or request authorization headers.

### Provider and model catalogue

- ENT-04: `model.Descriptor` continues to carry provider instance ID, model ID, and tool capabilities. It adds the model's supported reasoning levels.
- ENT-05: `model.ReasoningLevel` owns the closed reasoning-level values. Persistence and transport layers map their strings and enums to this domain type.
- ENT-06: The catalogue stores immutable configured entries plus one mutex-protected active selection. Catalogue query results are defensive copies ordered by provider instance identifier and then by model configuration order.
- APC-11: `Catalog.Models` returns every configured model with supported reasoning levels. `Catalog.Selection` returns the active selection.
- APC-12: `Catalog.SelectModel` accepts provider instance ID and model ID. It validates the target and resolves a referenced OpenAI-compatible API key before committing the new selection.
- APC-13: Model selection preserves the active reasoning level when the target model supports it. Otherwise, the catalogue chooses the greatest supported level below the active level. When no supported level is below it, the catalogue chooses the target model's lowest supported level.
- APC-14: `Catalog.SelectReasoningLevel` requires the requested level to be supported by the active model. Unsupported direct selection returns an error and preserves the active selection.
- DEC-07: Model and reasoning selection can commit while an agent run is active. A provider request that already obtained its catalogue snapshot continues with that snapshot. The next provider request whose snapshot starts after the commit uses the new selection.
- DEC-08: Selection does not clear, rewrite, or partition agent history. Each provider adapter excludes opaque provider context whose provider instance identifier differs from the selected provider instance.

### Agent core boundary

- APC-15: `host/internal/usecase/agent/run` replaces its constructor parameters for one descriptor and one provider with a consumer-owned `ModelRuntime` interface.
- APC-16: `ModelRuntime.Current` returns one immutable runtime selection containing `model.Descriptor`, `model.ReasoningLevel`, and the `ModelProvider` required for that request.
- APC-17: `ModelRequest` adds `ReasoningLevel`. Immediately before every `ModelProvider.Stream` call, the agent core reads `ModelRuntime.Current` and copies its descriptor and reasoning level into the request.
- CNS-02: The agent core does not list models, resolve credentials, parse settings, select an OpenAI API, or import Host, protobuf, gRPC, provider SDK, persistence, or TUI packages.
- CNS-03: Selection changes cannot mutate the in-progress stream handler or partial response. They affect only later model requests as defined by DEC-07.

### OpenAI Codex adapter

- CMP-07: Codex request construction reads model and reasoning level from `ModelRequest` instead of constructor configuration. Static Codex endpoint, OAuth, streaming, and response mapping behavior remain provider-owned.
- APC-18: Codex rejects a request whose provider instance ID is not `openai-codex` or whose model is absent from the configured Codex catalogue.
- APC-19: Codex selection does not start OAuth or require stored credentials. The next Codex request uses the existing provider-owned credential classification, refresh, and standard TUI authentication-retry flow. Programmatic Control receives the existing terminal authentication failure when interaction is unavailable.

### OpenAI-compatible adapter

- CMP-08: One `compatible.Service` instance is constructed for each configured `openai-compatible` provider instance. Its immutable configuration contains provider instance ID, base URL, provider API, per-model API overrides, API-key configuration, and credential resolver.
- APC-20: `Service.Stream` rejects provider or model mismatches, resolves the API key, selects the configured API, maps provider-neutral history and tools, and emits the existing `run.StreamEvent` sequence.
- APC-21: The Chat Completions path maps user text and images, assistant text and tool calls, tool results, tool definitions, streaming text, refusal, tool-call deltas, usage, terminal outcomes, and safe errors.
- APC-22: The Responses path maps the same provider-neutral behavior through the Responses API and preserves provider-context items only for the exact provider instance ID.
- APC-23: For non-`none` reasoning, both paths send the selected reasoning level through the API's reasoning-effort field. For `none`, they omit that field.
- APC-24: The adapter adds `Authorization: Bearer <key>` only when API-key resolution returns a key. It does not read an SDK default key or process environment variable that is not named by configuration.
- DEC-09: PHS-03 uses the existing `github.com/openai/openai-go/v3` dependency. It adds no provider framework, compatibility registry, model download, catalogue refresh, or provider middleware.

### Programmatic Control contract

- APC-25: `OpenRequest.command` adds `GetModels get_models = 6`, `SelectModel select_model = 7`, and `SelectReasoningLevel select_reasoning_level = 8`.
- APC-26: `SelectModel` contains `provider_id = 1` and `model_id = 2`. `SelectReasoningLevel` contains `ReasoningLevel level = 1`.
- APC-27: `CommandResponse.result` adds `ModelsResult models = 6` and `ModelSelectionResult model_selection = 7`.
- APC-28: `ModelsResult` contains every configured `ConfiguredModel` and the active `ModelSelection`. `ConfiguredModel` contains provider ID, model ID, and supported reasoning levels. `ModelSelectionResult` contains the committed selection.
- APC-29: `CommandType` appends `GET_MODELS = 5`, `SELECT_MODEL = 6`, and `SELECT_REASONING_LEVEL = 7`. `ReasoningLevel` uses an unspecified zero value followed by the seven domain values.
- APC-30: `RejectionCode` appends `NOT_FOUND = 6`, `REASONING_UNSUPPORTED = 7`, and `CREDENTIAL_UNAVAILABLE = 8`. Empty identifiers and unspecified reasoning use `INVALID_ARGUMENT`.
- DEC-10: `GetModels` is allowed in every run state. Selection commands are also allowed during an active run and follow DEC-07. Each command produces one correlated response and no agent event.
- DEC-11: Programmatic Control keeps its own protobuf DTOs and mappings. It does not reuse UI plugin protobuf messages.

### Standard TUI contract and behavior

- APC-31: UI `Initialization` appends configured models and the active model selection. Host-to-UI frames add `ModelSelectionChanged`, which contains the committed selection.
- APC-32: UI-to-Host commands add `SelectModelCommand` with provider and model IDs and `SelectReasoningLevelCommand` with a reasoning enum.
- APC-33: `host/internal/domain/ui` adds transport-independent configured-model and selection values. `host/internal/usecase/host/ui.Session` maps UI commands to the shared catalogue and sends a changed frame only after a successful commit.
- FLR-01: A failed TUI selection sends a safe error frame and no changed frame. The TUI therefore keeps displaying the Host-confirmed selection.
- EVC-01: `/model` and Ctrl+L open the standard TUI model selector. Ctrl+P and Shift+Ctrl+P select the next or previous configured model. Shift+Tab selects the next supported reasoning level.
- EVC-02: The selector lists provider instance ID and model ID. It supports selection, confirmation, and cancellation required by PHS-03 without implementing the later transcript, search, mouse, or extension selector scope.
- EVC-03: The status area displays the Host-confirmed provider instance, model, and reasoning level. It updates only from initialization or `ModelSelectionChanged`.
- DEC-12: Selection commands remain available while a run is active. The input editor and conversation transcript are not cleared when a selection succeeds or fails.

### Application composition

- STP-01: Load and strictly validate settings.
- STP-02: Create the generic credential-file reader and environment resolver.
- STP-03: Construct the configured Codex instance and every configured OpenAI-compatible instance.
- STP-04: Construct the provider catalogue with the validated default selection.
- STP-05: Pass the catalogue through the agent-core `ModelRuntime` interface and through client-specific minimal interfaces owned by Programmatic Control and UI consumers.
- STP-06: Keep one-shot headless mode on the configured default selection. It gains the new catalogue and adapters but no runtime selection command.

### Failure behavior

- FLR-02: Invalid settings fail application startup before a UI process, Programmatic Control socket, provider request, or agent run starts.
- FLR-03: Unknown provider-model pairs return `NOT_FOUND` through Programmatic Control and a safe UI error.
- FLR-04: Unsupported direct reasoning selection returns `REASONING_UNSUPPORTED` and preserves the active selection.
- FLR-05: A missing or invalid referenced API key returns `CREDENTIAL_UNAVAILABLE`, contains only the source name and safe reason, and preserves the active selection.
- FLR-06: An OpenAI-compatible instance with omitted `apiKey` is not a credential error. Provider HTTP authentication failures follow normal provider error mapping.
- FLR-07: API-key resolution failure immediately before a request produces a terminal provider failure without opening an HTTP request or changing history beyond the already accepted user message under existing agent-run rules.

### Test strategy

Implementation follows RED, GREEN, REFACTOR, and VERIFY. Generated protobuf code is compile setup, not behavioral GREEN.

| ID | Purpose | Inputs and expected outputs | Edge cases | Dependencies |
|---|---|---|---|---|
| TSK-01 | Prove strict settings and catalogue construction | Load settings with Codex and multiple compatible instances; expect deterministic models and the configured active selection | Duplicate models, bad default, unknown type or API, invalid URL, malformed API-key union, unsupported default reasoning | Settings fixtures only |
| TSK-02 | Prove API-key resolution and secret safety | Resolve literal, named environment, and credential-file keys; expect exact internal key or a safe typed error | Omitted key, missing or empty environment, missing entry, wrong credential type, empty file key, no secret in errors | Temporary settings and credential files |
| TSK-03 | Prove OpenAI-compatible protocol behavior | Send one request to `httptest.Server` through Chat Completions and Responses; expect mapped history, tools, reasoning, stream events, response, and usage | Model API override, tool-call deltas, refusal, cancellation, HTTP failure, absent key with no `Authorization`, present key with bearer authorization | Existing OpenAI SDK and provider-neutral fixtures |
| TSK-04 | Prove runtime switching without history loss | Start a run, switch selection while its request is active, complete a tool call, and expect the next model request to use the new provider, model, and reasoning with preceding history | Failed credential preflight, unsupported reasoning, provider-context filtering, selection race around request snapshot | Generated mocks for `ModelRuntime` and providers |
| TSK-05 | Prove Programmatic Control commands | Query models, select model, select reasoning, and submit correlated user requests; expect one response per command and model events from the selected runtime | Invalid fields, unknown model, unresolved credential, selection during active run, repeated correlations | Generated protobuf and Programmatic Control fixture |
| TSK-06 | Prove standard TUI selection | Initialize with models, invoke `/model`, Ctrl+L, model cycling, and reasoning cycling; expect Host commands and Host-confirmed status updates | Cancel selector, failed selection, active run, one model, one reasoning level | UI protobuf generation and existing TUI harness |
| TSK-07 | Prove application composition | Start programmatic and UI compositions with Codex, authenticated compatible, and keyless Ollama-style settings; expect the selected provider request and unchanged history | Startup validation failure, multiple compatible instances, Codex OAuth selection, UI-free programmatic startup | Real application composition with local test servers |
| TSK-08 | Verify repository health | Run generation twice and the repository checks; expect no second-generation diff and all commands to pass | Generated mocks and protobuf files are committed and deterministic | All GREEN and REFACTOR slices |

- DEC-13: Each behavioral slice starts with a focused test that compiles and fails on its expected assertion. Missing generated symbols are resolved only through the smallest protobuf or mock generation setup before RED.
- DEC-14: After every GREEN slice, run its package tests without cache. Final verification runs `task generate` twice, `go mod tidy -diff`, `task lint`, `task test`, `task build`, `task test-coverage`, and `git diff --check`.

## Overengineering and Overspecification Considerations

- TRD-01: The provider catalogue supports multiple configured instances because the approved requirements include OpenRouter, OpenCode, ZAI, Ollama, and similar endpoints. It does not add extension provider registration, which remains PHS-12 scope.
- TRD-02: One OpenAI-compatible adapter contains two required API paths. It does not add a generic wire-protocol plugin layer or compatibility option catalogue.
- TRD-03: API-key configuration supports the three approved forms and omits `!command`, caching, stale-key reuse, secret-manager SDKs, and background refresh.
- TRD-04: The standard TUI adds only the model selector, cycling, reasoning cycling, and status needed by PHS-03. Later standard TUI interaction tickets retain their scope.
- TRD-05: Runtime selection uses one catalogue snapshot per model request. It adds no session partitioning, provider-specific history store, or migration layer.

## Open Questions

None.

## References

- REF-01: `docs/specs/features/initial/delivery-plan/03-providers-models-runtime-selection.md` - owning ticket and acceptance criteria.
- REF-02: `docs/specs/features/initial/prd.md` - product behavior and component boundaries.
- REF-03: `docs/terms.md` - domain terminology.
- REF-04: `docs/specs/features/initial/phs-02-programmatic-control_solution.md` - existing Programmatic Control ownership and transport boundaries.
- REF-05: `api/programmatic/v1/programmatic.proto` - current Programmatic Control wire contract.
- REF-06: `api/plugins/ui/v1/ui.proto` - current UI plugin wire contract.
- REF-07: `host/internal/usecase/host/providers/catalog.go` - current one-entry provider catalogue.
- REF-08: `host/internal/usecase/agent/run/service.go` - current fixed model runtime and history ownership.
- REF-09: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/models.md` - Pi feature comparison for configured providers, APIs, credentials, and reasoning capabilities.
