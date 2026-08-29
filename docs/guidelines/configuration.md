# Configuration

Glyph reads Host settings from `~/.glyph/settings.yaml`. The file must contain one YAML document. Unknown fields, duplicate fields, and additional YAML documents cause startup to fail.

The settings loader in `host/internal/infra/persistence/settings/service.go` is the executable contract for this format.

Keep `~/.glyph/` at mode `0700` and `settings.yaml` at mode `0600`, especially when using `apiKey.literal`.

## Complete example

```yaml
defaultProvider: openrouter
defaultModel: z-ai/glm-5.3-flash
activeUI: glyph-tui

providers:
  openai-codex:
    type: openai-codex
    models:
      - id: gpt-5.6-luna
        input: [text, image]
        contextWindow: 272000
        maxTokens: 128000
        toolCapabilities:
          strictJSONSchema: true
          grammar:
            lark: true
            regex: true
        reasoning:
          supported: true
          choices: [off, minimal, low, medium, high, xhigh, max]
          default: low

  openrouter:
    type: openai-compatible
    baseURL: https://openrouter.ai/api/v1
    api: chat-completions
    apiKey:
      environment: OPENROUTER_API_KEY
    models:
      - id: z-ai/glm-5.3-flash
        input: [text, image]
        contextWindow: 1048576
        maxTokens: 131072
        toolCapabilities: {}
        reasoning:
          supported: true
          choices: [max, high, low]
          default: max
          format: openrouter
```

Model limits and capabilities are configuration data. Check them against the provider before using this example with another model.

## Top-level fields

| Field | Required | Description |
|---|---|---|
| `defaultProvider` | Yes | Provider ID selected at startup. It must identify an entry in `providers`. |
| `defaultModel` | Yes | Model ID selected at startup. It must belong to `defaultProvider`. |
| `providers` | Yes | Nonempty map of provider IDs to provider configurations. |
| `activeUI` | No | Persisted UI plugin ID. Glyph converts it to lowercase, normalizes runs of whitespace, `_`, and `-` to one `-`, and rejects an empty result. |

`defaultProvider`, `defaultModel`, provider IDs, model IDs, API-key references, and compatibility keys must be nonempty and must not contain surrounding whitespace.

## Extensions and tools

`settings.yaml` does not select extensions or individual tools. Glyph treats every regular executable file in the extension catalog directory as an extension candidate. Each started extension reports its own tools.

The default extension catalog directory is `~/.glyph/plugins/extension/`. Use `--extension-dir` to replace the default directory for one invocation:

```bash
glyph --extension-dir ./plugins/extension
glyph run --extension-dir ./plugins/extension "Summarize README.md"
glyph rpc --extension-dir ./plugins/extension
```

`--extension-dir` replaces the complete catalog. It does not add another directory. Glyph has no extension or tool allowlist in `settings.yaml`.

Model `toolCapabilities` describes how a provider model accepts tool definitions. It does not enable, disable, or select extension tools.

## Providers

Every installation must configure exactly one `openai-codex` provider with the provider ID `openai-codex`. Additional providers use unique map keys.

### OpenAI Codex

```yaml
openai-codex:
  type: openai-codex
  models:
    - id: gpt-5.6-luna
      input: [text, image]
      contextWindow: 272000
      maxTokens: 128000
      toolCapabilities: {}
      reasoning:
        supported: true
        choices: [off, low, high]
        default: high
```

An `openai-codex` provider must not set `baseURL`, `api`, or `apiKey`. Authentication uses the Codex credential flow.

### OpenAI-compatible provider

```yaml
openrouter:
  type: openai-compatible
  baseURL: https://openrouter.ai/api/v1
  api: chat-completions
  apiKey:
    environment: OPENROUTER_API_KEY
  models:
    - id: example-model
      input: [text]
      contextWindow: 32768
      maxTokens: 4096
      toolCapabilities: {}
      reasoning:
        supported: false
        choices: [off]
        default: off
```

| Field | Required | Description |
|---|---|---|
| `type` | Yes | `openai-codex` or `openai-compatible`. |
| `baseURL` | For `openai-compatible` | Absolute `http` or `https` URL with a host. |
| `api` | For `openai-compatible` | Default API for the provider. Allowed values are `chat-completions` and `responses`. |
| `apiKey` | No | One API-key source. Omit it for providers that accept unauthenticated requests. |
| `models` | Yes | Nonempty ordered list of model configurations. Model IDs must be unique within the provider. |

### API-key sources

`apiKey` must contain exactly one source:

```yaml
apiKey:
  environment: OPENROUTER_API_KEY
```

```yaml
apiKey:
  credential: openrouter
```

```yaml
apiKey:
  literal: secret-value
```

- `environment` names an environment variable.
- `credential` names an entry in Glyph's local credential store.
- `literal` stores the secret directly in `settings.yaml` and must not be empty. Prefer `environment` or `credential` so the settings file does not contain the secret.

## Models

| Field | Required | Description |
|---|---|---|
| `id` | Yes | Provider model ID. |
| `api` | No | Per-model API override. It is allowed only under `openai-compatible` and accepts `chat-completions` or `responses`. |
| `input` | Yes | Ordered list of accepted input modalities. Allowed values are `text` and `image`. The list must contain `text` and must not contain duplicates. |
| `contextWindow` | Yes | Positive context-window token limit. |
| `maxTokens` | Yes | Positive maximum output token count. It must not exceed `contextWindow`. |
| `toolCapabilities` | Yes | Constrained tool capability mapping. An empty mapping sets every capability to `false`. |
| `reasoning` | Yes | Reasoning capability and provider format. |
| `pricing` | No | USD rates per one million tokens. |

### Tool capabilities

```yaml
toolCapabilities:
  strictJSONSchema: true
  grammar:
    lark: true
    regex: false
```

All three booleans default to `false` when omitted inside the required `toolCapabilities` mapping.

- `strictJSONSchema` enables strict JSON Schema for function tools.
- `grammar.lark` enables Lark grammar tools.
- `grammar.regex` enables regular-expression grammar tools.

## Reasoning

Allowed reasoning choices are `off`, `on`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`. Choices must be unique, and `default` must appear in `choices`.

The fixed, toggle, and effort examples below use Chat Completions to show `format`. Remove `format` when the effective API is Responses.

### No reasoning

```yaml
reasoning:
  supported: false
  choices: [off]
  default: off
```

A non-reasoning model must not set `compatibilityKey` or `format`.

### Fixed reasoning

```yaml
reasoning:
  supported: true
  choices: [on]
  default: on
  format: openai-chat
```

### Toggle reasoning

```yaml
reasoning:
  supported: true
  choices: [off, on]
  default: on
  format: openai-chat
```

### Effort reasoning

```yaml
reasoning:
  supported: true
  choices: [off, low, medium, high]
  default: medium
  format: openrouter
```

An effort configuration must contain at least one effort value. It can contain `off`, but it must not contain `on`.

### Reasoning format

`format` is private to the provider adapter:

| Effective API | `format` |
|---|---|
| OpenAI Codex Responses | Omit the field. |
| OpenAI-compatible Responses | Omit the field. |
| OpenAI-compatible Chat Completions with OpenAI fields | `openai-chat` |
| OpenAI-compatible Chat Completions with OpenRouter fields | `openrouter` |

The OpenAI-compatible adapter rejects an unknown format, a format on Responses, and a reasoning-enabled Chat Completions model without a format during startup.

`compatibilityKey` is optional. Matching nonempty keys permit opaque reasoning-context replay between different models on the same provider instance and API. Exact model replay does not require a key.

## Pricing

Pricing values are USD rates per one million tokens:

```yaml
pricing:
  input: 1.25
  output: 10
  cacheRead: 0.125
  cacheWrite: 1.25
  tiers:
    - inputTokensAbove: 272000
      input: 2.50
      output: 15
      cacheRead: 0.25
      cacheWrite: 2.50
```

When `pricing` is present, `input`, `output`, `cacheRead`, and `cacheWrite` are all required. Every rate must be finite and nonnegative.

Each tier must contain `inputTokensAbove` and all four rates. Thresholds must be positive and strictly increasing. Glyph selects the last tier whose threshold is strictly lower than the request's uncached, cached, and cache-write input token total. The selected tier replaces all four base rates. Tiers do not inherit individual rates from the base mapping or an earlier tier.

## Related specifications

- `docs/specs/features/initial/phases/03-providers-models-runtime-selection/solution.md`
- `docs/specs/features/initial/phases/04.1-model-execution-capabilities/solution.md`
