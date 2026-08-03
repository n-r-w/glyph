# Technical Solution: Glyph Minimal Prototype

## Problem Statement

- PRB-01: The prototype must prove the complete coding-agent path through the standard TUI and headless operation without making a Glyph client own agent behavior.
- PRB-02: The Agent Core must use OpenAI Codex and tools from a separately built Go extension while remaining independent of provider authentication, plugin processes, and terminal rendering.
- PRB-03: The extension boundary must carry tool discovery, streamed progress, results, errors, and cancellation without allowing an extension failure to terminate the Glyph Host.
- PRB-04: The UI plugin boundary must carry user commands and Agent Core events while making any UI plugin termination cancel the active run and terminate the Glyph Host.
- CNS-01: `docs/specs/features/initial/prototype-prd.md` is the requirements source for the prototype.
- CNS-02: Component ownership and dependency directions must remain suitable for the target product. Internal Go APIs are excluded from target compatibility guarantees.
- CNS-03: The prototype is validated on macOS/arm64 and uses Go 1.26.5.

## Proposed Solution

### Status

- SOL-01: This document defines the approved implementation-ready technical solution for the prototype.

### Process Topology

```text
glyph (Glyph Host)
├── Agent Core
│   ├── provider contract ── OpenAI Codex provider
│   └── tool contract ────── Host tool gateway
│                                  │
│                                  │ go-plugin + extension gRPC v1
│                                  ▼
│                             extension processes
│                             └── glyph-tools
│                                 ├── read
│                                 ├── edit
│                                 └── bash
├── headless controller
└── UI host controller
       │
       │ go-plugin + UI gRPC v1
       │ one bidirectional stream
       ▼
    glyph-tui ── Bubble Tea ── controlling terminal
```

- DGM-01: The headless controller and UI host controller use the same Agent Core behavior. The Glyph Host supplies the configured provider and extension-backed tools; Agent Core owns the run.
- DGM-02: Extension and UI plugin processes use separate catalogs, protobuf contracts, protocol handshakes, and lifecycle adapters even though both use `go-plugin` with gRPC.

### Components

- CMP-01: One repository and one Go module build three executables: `glyph`, `glyph-tui`, and `glyph-tools`.
- CMP-02: Agent Core owns the in-memory history, agent-run state, model/tool loop, sequential tool execution, ordered events, and cancellation.
- CMP-03: Glyph Host owns configured providers, extension and UI catalogs, plugin discovery, plugin processes, UI selection, the cached tool catalog, and plugin availability state.
- CMP-04: `glyph-tui` is the standard TUI UI plugin. It maps Host events to Bubble Tea messages, maps terminal input to Host commands, and contains no model or tool orchestration.
- CMP-05: The OpenAI Codex provider owns OAuth, token refresh, request authorization, request serialization, transport selection, and streamed response decoding. Agent Core does not depend on provider SDK types.
- CMP-06: The Host tool gateway merges validated catalogs from available extension runtimes, maps each tool name to one owning runtime, routes Agent Core calls through the public extension SDK, and maps streamed protobuf events back to Agent Core events.
- CMP-07: `glyph-tools` is the standard tools extension executable. It registers and executes `read`, `edit`, and `bash`; other compatible extension executables use the same SDK and runtime contract.
- CMP-08: `pkg/plugins/extension/v1` and `pkg/plugins/ui/v1` contain the generated public protobuf and gRPC wire contracts.
- CMP-09: The Host UI transport adapter owns UI discovery, selection, availability, and product lifecycle policy while using the public UI SDK for process bootstrap and connection.
- CMP-10: `sdk/plugins/extension/v1` and `sdk/plugins/ui/v1` contain the handwritten public Go SDK for protocol bootstrap, connection, server startup, and contract testing.
- CMP-11: `host`, `plugins/extension/tools`, and `plugins/ui/tui` are separate project roots. Each project owns its command entry point, nested `internal` implementation, and composition root.

### Contract Map

```text
UI plugin ◀── UI Plugin Contract v1 ──▶ Glyph Host
                                            │
Headless controller ────────────────────────┤ Agent Run Contract
                                            ▼
                                        Agent Core
                                         │      │
                         Model Provider  │      │ Tool Runtime
                              Contract   │      │ Contract
                                         ▼      ▼
                               Codex adapter   Host tool gateway
                                                   │
                                      Extension Contract v1
                                                   ▼
                                              glyph-tools

Agent Core ── Agent Event Contract ──▶ Glyph Host ──▶ Glyph clients
Codex adapter ── Credentials and Interaction Contracts ──▶ Glyph Host
```

- DGM-03: The UI plugin connection is bidirectional. The remaining arrows identify the primary consumer-to-provider call direction. Glyph Host is the only component that connects Glyph clients, Agent Core, providers, and plugin processes.

#### Public Process Contracts

- APC-01: UI Plugin Contract v1 is owned by Glyph Host and implemented by the selected UI plugin. It covers version negotiation, fixed startup capabilities including whether the plugin controls a terminal, one bidirectional connection, initial UI state, user commands, agent and tool events, interaction requests, notifications, errors, and UI termination.
- APC-02: Extension Contract v1 is owned by Glyph Host and implemented by an extension process. It covers version negotiation, tool catalog discovery, JSON Schemas, execution, progress, terminal results, cancellation, and protocol failures.
- APC-03: Programmatic Control Contract is owned by Glyph Host and used by a programmatic controller for correlated commands, acceptance results, asynchronous execution events, agent and session control, interaction requests, and notifications. The prototype does not implement this target-only public contract; `glyph run` remains an internal one-shot controller.

#### Internal Application Contracts

- APC-04: Agent Run Contract connects Host controllers to Agent Core. Agent Core owns run behavior; each controller owns its consumed Go interface. The contract covers one active run, user input, `run_id`, concurrent-run rejection, cancellation, history effects, terminal outcome, and settlement.
- APC-05: Model Provider Contract is owned by Agent Core as consumer and implemented by provider adapters. It covers model requests, streaming output, terminal response outcomes, opaque provider context, tools exposed to the model, and cancellation without exposing provider SDK types.
- APC-06: Tool Runtime Contract is owned by Agent Core as consumer and implemented by the Host tool gateway. It covers the available tool catalog, tool execution, progress, terminal results, error results, and cancellation without exposing extension protobuf types.
- APC-07: Agent Event Contract is defined by Agent Core and delivered by Glyph Host. It covers ordered agent, turn, message, and tool-execution lifecycle events, `run_id`, terminal payloads, delivery failure, `agent_end`, and Host-owned `agent_settled`.

#### Host Infrastructure Ports

- APC-08: Credentials Contract is owned by each provider adapter as consumer and implemented by Host persistence. It covers provider-owned opaque payload loading, atomic saving, deletion, and owner-only file access without exposing the credential-file format to providers.
- APC-09: Glyph Client Interaction behavior is provided by Glyph Host. Each provider or extension consumer owns its internal Go interface. The contract covers interaction requests, responses, notifications, explicit failure without a Glyph client, and OAuth URL presentation without depending on a specific UI plugin.
- APC-10: Plugin Catalog and Lifecycle Contracts are owned by Host use cases. Filesystem catalog adapters implement discovery; SDK-backed runtime adapters implement compatibility validation, startup, shutdown, availability state, process failure, and startup-only UI selection.

#### Dependency Rules

- SOL-02: Agent Core does not depend on protobuf, gRPC, `go-plugin`, Bubble Tea, files, provider SDKs, UI plugins, or extensions.
- SOL-03: A UI plugin communicates only with Glyph Host; an extension receives no Agent Core history or provider access; provider adapters do not define shared Agent Core models.
- SOL-04: Each internal Go interface belongs to its consumer package. Every concrete implementation has a production compile-time assertion against that consumer-owned Go interface.
- SOL-05: Exact Go function, RPC, type, field, and parameter names are implementation choices. They must preserve APC-01 through APC-10 and must not move responsibilities across component boundaries.
- SOL-06: Host and concrete plugin projects share code only through `pkg/plugins/...` and `sdk/plugins/...`. No project imports another project's nested `internal` packages.
- SOL-07: Standard TUI `domain/presentation` is a derived, non-authoritative projection of Host events. Agent Core remains the only owner of run history and agent state.

### Source and Package Architecture

```text
api/plugins/
├── extension/v1/
└── ui/v1/

pkg/plugins/
├── extension/v1/
└── ui/v1/

sdk/plugins/
├── extension/v1/
└── ui/v1/

host/
├── cmd/glyph/
└── internal/
    ├── domain/
    │   ├── agent/
    │   └── tool/
    ├── usecase/
    │   ├── agent/run/
    │   └── host/
    │       ├── startup/
    │       ├── events/
    │       ├── interactions/
    │       └── tools/
    ├── controller/
    │   ├── cli/headless/
    │   └── ui/
    ├── infra/
    │   ├── providers/openai/codex/
    │   ├── plugins/
    │   │   ├── extension/
    │   │   │   ├── catalog/
    │   │   │   └── runtime/
    │   │   └── ui/
    │   │       ├── catalog/
    │   │       └── runtime/
    │   ├── persistence/
    │   │   ├── credentials/
    │   │   └── settings/
    │   ├── schemas/jsonschema/
    │   └── terminal/
    └── app/

plugins/extension/tools/
├── cmd/glyph-tools/
└── internal/
    ├── usecase/tools/
    │   ├── read/
    │   ├── edit/
    │   └── bash/
    ├── controller/extension/
    ├── infra/
    │   ├── filesystem/project/
    │   └── process/bash/
    └── app/

plugins/ui/tui/
├── cmd/glyph-tui/
└── internal/
    ├── domain/presentation/
    ├── usecase/presentation/
    ├── controller/
    │   ├── plugin/
    │   └── tui/
    ├── infra/terminal/
    └── app/
```

- DGM-04: Top-level project roots and nested `internal` directories enforce source ownership. Intermediate grouping directories do not become Go packages unless implementation adds source files with an approved responsibility.
- CMP-12: The Host project contains Agent Core, Host use cases, headless and UI command controllers, provider and plugin infrastructure, terminal recovery, persistence, schema validation, and the Host composition root.
- CMP-13: The standard tools extension project contains independent tool use cases, one extension contract controller, shared project-filesystem and bash-process adapters, and its own composition root.
- CMP-14: The standard TUI project contains a non-authoritative presentation projection, presentation use cases, separate plugin-contract and Bubble Tea controllers, terminal infrastructure, and its own composition root.

### Approved Decisions

#### Application and Ownership

- DEC-01: Build three separate executables. `glyph` is the Glyph Host, `glyph-tui` is the standard TUI UI plugin, and `glyph-tools` is the standard tools extension. `glyph run` performs one headless request without starting a UI plugin.
- DEC-02: Agent Core owns run state, history, the model/tool loop, ordered events, and cancellation. Glyph Host owns providers and plugin processes and supplies them through contracts. UI plugin and headless code only handle input and events.
- DEC-46: Keep `glyph`, `glyph-tui`, and `glyph-tools` in one repository and one Go module. Process and contract boundaries remain separate even though builds and contract changes are tested atomically.
- DEC-60: Organize internal code under the approved `domain`, `usecase`, `controller`, `infra`, and `app` layers. Group technical variants under stable owners such as `infra/providers/<provider-family>/<provider>` and `infra/plugins/<plugin-kind>`; DGM-04 defines the approved package tree.
- DEC-63: APC-01 through APC-10 define the complete architectural contract map. Exact function and parameter shapes are deferred to implementation and cannot change contract ownership, purpose, or functional scope.
- DEC-64: Publish handwritten SDK packages at `sdk/plugins/extension/v1` and `sdk/plugins/ui/v1`, separate from generated contracts under `pkg/plugins` and concrete projects under `plugins`.
- DEC-65: Keep the prototype SDK thin: include handshake and protocol versioning, Host-side process startup and connection, plugin-side gRPC server startup, fixed UI capability retrieval, access to generated gRPC contracts, and contract-test helpers. Add no second public model layer over protobuf and no Host product policy.
- DEC-66: Use `host`, `plugins/extension/tools`, and `plugins/ui/tui` as project roots. Each project owns its `cmd` entry point and nested `internal/app` composition root; nested `internal` visibility enforces project isolation.
- DEC-67: Use the Host package tree in DGM-04, including `controller/cli/headless`, separate extension and UI `catalog` and `runtime` packages, `infra/schemas/jsonschema`, and `infra/terminal`.
- DEC-68: Use shared standard-tools project layers with separate `read`, `edit`, and `bash` use cases, one extension controller, and shared filesystem and process infrastructure adapters.
- DEC-69: Give the standard TUI its own `domain/presentation` state, `usecase/presentation` behavior, plugin-contract controller, Bubble Tea controller, terminal adapter, and composition root.
- DEC-70: Preserve grouping directories when approved target behavior provides plausible sibling responsibilities. Do not create empty Go packages or speculative Go interfaces solely to materialize the documented hierarchy.
- DEC-71: Use `~/.glyph/plugins/extension/` and `~/.glyph/plugins/ui/` as default catalog roots. `--extension-dir <path>` replaces the extension directory for one UI or headless invocation. `--ui-dir <path>` replaces the UI directory for one UI invocation; combining it with `glyph run` fails startup. Overrides do not change settings. Source builds place plugin executables under `bin/plugins/{extension,ui}` and run `bin/glyph` with these overrides; no staging task copies files outside the repository.
- DEC-72: Derive a plugin ID from its executable filename by converting it to lowercase, replacing runs of whitespace and `_` with one `-`, collapsing repeated `-`, and trimming leading or trailing `-`. Normalize catalog entries, `--ui`, and `activeUI` before comparison. In the extension catalog, exclude and report each empty-ID candidate and every candidate in a duplicate-ID group while retaining unaffected candidates. In the UI catalog, an empty normalized ID or duplicate normalized IDs make the catalog invalid.
- DEC-73: Use `--ui <plugin-id>` for explicit UI selection and optional `activeUI` in `~/.glyph/settings.yaml` for persisted selection. Combining `--ui` with `glyph run` fails startup.
- DEC-74: Before sole-candidate UI selection, validate each effective candidate by starting it through the UI SDK, completing the `go-plugin` handshake, retrieving its fixed startup capabilities, opening no UI stream or terminal, and stopping the probe process. Exclude unavailable or incompatible candidates with warnings. A candidate selected through `--ui` or `activeUI` starts once and reuses its validated connection.
- DEC-75: Treat an empty extension catalog as a normal empty tool catalog. Otherwise, start every discovered extension independently, validate and cache its complete catalog, keep one runtime per normalized plugin ID, and stop every started runtime during shutdown. When a runtime becomes unavailable, immediately remove every tool owned by that runtime from the global registry. A later call from an already completed model response to a removed tool produces a terminal `ToolResult` with `is_error=true`; Agent Core returns it to the model and continues.
- DEC-76: Require tool names to be globally unique across successfully started extensions. When a name conflicts, mark every extension that registers it unavailable as a complete extension, remove all of their tools, stop every conflicting extension process, report the name and plugin IDs, and keep non-conflicting extensions available.
- DEC-77: After startup, emit one informational summary identifying the selected UI plugin or headless mode, every successfully loaded extension, and the tools registered by each extension. Send the summary to the active UI plugin as informational content or to stderr in headless mode. Represent empty extension and tool lists as normal information; report extension failures separately.
- DEC-78: Treat a missing default extension directory as an empty catalog without a warning or error. When the default extension directory is unreadable, report the error and continue with an empty tool catalog. A missing or unreadable explicit `--extension-dir` fails startup. In UI mode, a missing or unreadable effective UI directory fails startup. Headless mode does not inspect the default UI directory.
- DEC-79: UI Contract v1 exposes an immutable startup capability that states whether the UI plugin controls a terminal. Glyph Host retrieves it after protocol negotiation and before opening the UI stream. A non-terminal UI causes no terminal access by Glyph Host.

#### Codex Provider

- DEC-33: Send prototype model requests to the ChatGPT Codex Responses endpoint with `store=false`, the coding-agent prompt in `instructions`, `reasoning.encrypted_content` included for local history replay, and optional `defaultThinkingLevel` mapped to reasoning effort. Resolve a fresh OAuth access token and account ID for every request, set `chatgpt-account-id`, `OpenAI-Beta: responses=experimental`, `originator: glyph`, and `User-Agent: glyph`, and disable SDK retries.
- DEC-34: Use `github.com/openai/openai-go/v3` as the internal implementation of the prototype Codex SSE transport. Keep its request, response, and event types inside that transport and emit provider-neutral events to Agent Core. A future custom WebSocket transport, connection-scoped continuation cache, and automatic transport fallback remain internal Codex-provider changes.
- DEC-35: Use `golang.org/x/oauth2` for PKCE generation, authorization URL construction, and authorization-code exchange. Keep the loopback callback server, `state` validation, Codex JSON refresh request, and provider-owned credential mapping inside the Codex provider.
- DEC-36: Support browser OAuth only when Glyph and the browser run on the same computer. Attempt registered callback address `127.0.0.1:1455` first and `127.0.0.1:1457` second. Always display the authorization URL in the TUI and treat automatic browser launch as best-effort.
- DEC-37: Refresh an OAuth access token before a model request when no more than five minutes remain before expiry and persist the rotated credential through DEC-29. When the backend returns `401`, fail the run with a sign-in-required error; do not refresh and replay the model request.
- DEC-42: Serialize every ChatGPT Codex request input as an ordered list of Responses input items, including the first user message. Do not use the SDK string-input shorthand.
- DEC-43: Preserve provider output items in their original order. Treat a reasoning item as opaque provider context consisting of its `id`, `encrypted_content`, and `summary`, and replay it immediately before the output item it produced. Do not log or interpret encrypted reasoning.
- DEC-44: When a reasoning item was returned without `encrypted_content`, fail stateless continuation instead of dropping that item. When the provider returned no reasoning item, continue without synthesizing one.
- DEC-45: Wrap the SDK HTTP client with a Codex-owned error transport that reads only bounded `4xx` and `5xx` response bodies, restores the body for the SDK, and extracts `detail` or `error.message`. Do not retain request or response headers, and limit the user-visible provider detail to 4000 characters.

#### Agent Core History and Events

- DEC-50: Store Agent Core history as an ordered list of user messages, model responses, and tool results. Each model response contains its own ordered text, opaque provider-context, and tool-call items.
- DEC-51: Represent provider context as an opaque item containing a provider ID and bytes. Agent Core preserves item order without decoding the payload; each provider adapter consumes only its own items and ignores items owned by other providers.
- DEC-52: During streaming, expose the partial response as current run state and events. On cancellation or provider failure, finalize and store a model response with outcome `aborted` or `failed`, retain finalized items and a safe error message, remove streaming scratch data, execute none of its tool calls, and exclude the complete response from later provider requests.
- DEC-53: When cancellation stops an active tool, store its cancellation as an error result. Do not persist results for later unstarted calls. When constructing a later provider request, temporarily supply each missing result as an error with text `Tool call skipped because the agent run was cancelled.` without adding it to history.
- DEC-54: Model-response outcomes are `stop`, `tool_use`, `length`, `aborted`, and `failed`. A `length` response without tool calls ends the run. A `length` response with tool calls executes none of them, stores one error result per call, and continues with another model request.
- DEC-55: A provider-neutral tool call contains a call ID, tool name, and JSON-compatible argument map supporting strings, numbers, booleans, null, arrays, and nested maps. Provider adapters map SDK values into this model; Host schema validation precedes extension transport serialization.
- DEC-56: Emit lifecycle events at agent, turn, message, and tool-execution levels. One run starts with `agent_start`; each turn emits `turn_start`, ordered message events, ordered tool-execution and tool-result message events, and `turn_end`; another model request begins another turn; the final event from Agent Core is `agent_end`.
- DEC-57: Deliver Agent Core events to the Glyph Host one at a time in creation order. Add no application-level queue between Agent Core and Host; gRPC flow control and the Bubble Tea event loop retain their own concurrency boundaries.
- DEC-58: Agent Core emits `agent_end`. Glyph Host emits `agent_settled` only after every `agent_end` recipient completes; the agent becomes idle after settlement.
- DEC-59: `message_end` contains the complete terminal message, `turn_end` contains the model response and tool results for that turn, and `agent_end` contains the run outcome and ordered history entries added by the run.
- DEC-61: `message_update` contains only the new text fragment and its content-item position. Consumers accumulate updates in order; `message_end` supplies the complete terminal message and finalizes the accumulated state.
- DEC-62: Glyph Host creates `run_id` before invoking Agent Core and passes it with the run request. Agent Core includes `run_id` in every lifecycle event. Host owns client `correlation_id` values and adds them only when sending external events to a Glyph client.

#### Extension Process and Transport

- DEC-03: Use `github.com/hashicorp/go-plugin` with gRPC for extension process lifecycle and IPC.
- DEC-04: Use `go-plugin.VersionedPlugins` with exactly protocol version `1`. A version mismatch makes the extension unavailable. The prototype has no compatibility path for older protocol versions.
- DEC-05: Use the Unix domain socket selected by `go-plugin` on macOS. Rely on owner access and the library-required `MagicCookie`; do not add TLS, `AutoMTLS`, checksum verification, or reattach support.
- DEC-06: Keep Host catalog, selection, availability, and lifecycle policy inside Host infrastructure and use cases. Place shared `go-plugin` handshake, connection, and server bootstrap in the public plugin SDK defined by DEC-64 and DEC-65.
- DEC-07: Start every extension process with the working directory and environment inherited from `glyph`. OAuth credentials remain in the credential file and are not injected into extension environments.

#### UI Plugin Process and Transport

- DEC-47: Use a separate UI plugin handshake and protocol version `1` with `go-plugin` and gRPC. The UI and extension protocol versions are independent even when both equal `1`.
- DEC-48: Open one persistent bidirectional gRPC stream from Glyph Host to the selected UI plugin. Host-to-UI messages carry initialization and lifecycle events; UI-to-Host messages carry commands. Stream completion or failure is the authoritative UI termination signal that cancels the active run and terminates Glyph Host.
- DEC-49: `glyph-tui` opens the controlling terminal through `tea.OpenTTY()` and passes the returned files through `tea.WithInput` and `tea.WithOutput`. `go-plugin` stdout and stderr remain reserved for handshake and process logs. For a terminal UI, Glyph Host snapshots the controlling-terminal state through `github.com/charmbracelet/x/term` after selection and before opening the UI stream; snapshot failure fails UI startup. The UI plugin exclusively owns terminal input, rendering, and raw mode while active. After UI stream completion or failure, Host stops the UI process, resets synchronized output, keyboard enhancements, mouse tracking, focus reporting, bracketed paste, Unicode mode, alternate-screen mode, and cursor visibility through `github.com/charmbracelet/x/ansi`, then restores the saved terminal state.

#### Protobuf Contract

- DEC-08: Store plugin contract sources at `api/plugins/extension/v1/tool.proto` and `api/plugins/ui/v1/ui.proto`. Store generated Go code at `pkg/plugins/extension/v1` and `pkg/plugins/ui/v1`, and handwritten SDK code at `sdk/plugins/extension/v1` and `sdk/plugins/ui/v1`; group future plugin kinds as sibling directories under each public root.
- DEC-09: Pin Easyp, `mockgen`, `protoc-gen-go`, and `protoc-gen-go-grpc` with standard `tool` directives in the main `go.mod`. Invoke them through `go tool`, commit generated Go files, and exclude Easyp package management, remote plugins, and breaking checks from the prototype workflow.
- DEC-09.1: Override Easyp's retracted transitive `github.com/klauspost/compress v1.18.1` with `v1.19.1` in the main `go.mod`.
- DEC-09.2: Use `google.golang.org/grpc v1.82.1` when production plugin contracts first import gRPC. Do not retain `v1.81.0`, whose HTTP/2 transport reaches GO-2026-6061 through the selected streaming and server paths.
- DEC-10: Expose one unary `ListTools` RPC and one server-streaming `Execute` RPC.
- DEC-11: `ListTools` returns each tool's name, description, and one provider-neutral JSON Schema. The Host calls it once for each started extension, validates each complete extension catalog independently, and caches valid catalogs until their processes stop.
- DEC-12: Reject one extension's complete catalog, stop that extension process, and mark only that extension unavailable when a descriptor has an empty or duplicate name, an empty description, a schema that fails Draft 2020-12 compilation, or a schema outside the prototype profile in DEC-38. Do not partially register that extension's tools.
- DEC-13: Encode JSON Schema and tool arguments as UTF-8 JSON in protobuf `bytes` fields named `input_schema_json` and `arguments_json`. A model-argument validation failure produces a terminal `ToolResult` with `is_error=true`; it does not make the extension unavailable.
- DEC-38: Keep `input_schema_json` general enough to carry one provider-neutral JSON Schema, but accept only the prototype profile: a root object with `properties`, every property defined by exactly `type: string` and a nonempty `description`, every property name listed exactly once in `required`, and `additionalProperties: false`. Reject every other schema keyword in the prototype.
- DEC-39: Use `github.com/santhosh-tekuri/jsonschema/v6` v6.0.2 with Draft 2020-12. Glyph Host compiles and caches each schema during catalog validation. The extension compiles its registered schemas once and validates each `arguments_json` instance before typed decoding.
- DEC-40: Configure the Codex function tools with `strict=true`. This setting belongs to the Codex adapter and is not part of the public extension contract.
- DEC-41: Define the prototype inputs as `read(path)`, `edit(path, oldText, newText)`, and `bash(command)`. Every listed argument is a required string with a nonempty description.

#### Tool Execution Stream

- DEC-14: `Execute` emits zero or more progress events followed by one terminal result and then closes the stream. `ExecuteEvent` uses `oneof progress | result`; progress channels are `STATUS`, `STDOUT`, and `STDERR`.
- DEC-15: Return tool-operation failures in a terminal `ToolResult` with `is_error=true`, so Agent Core can return them to the model and continue the loop. Reserve gRPC status errors for cancellation, protocol violations, and extension-process unavailability.
- DEC-16: Propagate cancellation by cancelling the `Execute` context. Do not add an execution identifier or a separate `Cancel` RPC.
- DEC-17: Treat clean stream completion without a result, a second result, or an event after the terminal result as a protocol violation. Fail the active tool call, stop the extension process, and mark the extension unavailable. A context-driven `codes.Canceled` result is not a protocol violation.
- DEC-18: A nonzero `bash` exit produces a terminal `ToolResult` with `is_error=true` containing `stdout`, `stderr`, and the exit code. Agent Core returns it to the model and continues the loop.

#### Tool Implementation and Interfaces

- DEC-19: Resolve `bash` through `exec.LookPath` and execute `bash -c <command>` in the working directory without loading login profiles.
- DEC-20: Discover extension and UI plugin executables from their effective catalog directories after applying DEC-71 command-line overrides. The standard source build produces `glyph-tools` and `glyph-tui` in separate build-output catalog directories.
- DEC-21: Implement `glyph-tui` with Bubble Tea v2 and one TUI state model. Translate UI protocol events into Bubble Tea messages and terminal input into UI protocol commands; keep input and rendering state inside the UI plugin process.
- DEC-22: Store one English coding-agent prompt in a text file owned by the prototype coding-agent configuration, embed it into `glyph`, and inject its content into Agent Core at construction.
- DEC-23: Use TDD with Agent Core unit tests against generated consumer-owned mocks; Host tests for default and overridden catalog-directory failures, catalog overrides in UI and headless modes, rejected headless UI overrides, empty and multiple extension catalogs, isolated invalid and duplicate extension IDs, failure isolation, immediate tool removal and late calls to unavailable runtimes, tool-name conflicts and process shutdown, UI selection, plugin ID normalization, startup summaries, terminal capability handling and recovery after normal and abnormal UI termination, owner-only Glyph directory permissions, history projection, and cancellation; extension and UI SDK contract tests; standard TUI presentation-state tests; tool tests in temporary directories; and Codex adapter tests against `httptest` streams. Do not test documentation or source layout.
- DEC-24: Accept the headless request as the remaining `glyph run` command-line argument, write model text to stdout, write tool lifecycle and progress with short textual prefixes, and write terminal errors to stderr.
- DEC-25: Store prototype settings at `~/.glyph/settings.yaml`. Use `~/.glyph/plugins/extension/` and `~/.glyph/plugins/ui/` as default catalog directories, subject to the per-invocation replacements in DEC-71.
- DEC-26: Create one application context with `signal.NotifyContext`, derive one context per agent run, cancel the active run before killing all extension and UI plugin clients, use `log/slog` for application logs, and keep `go-plugin`'s `hclog` use inside SDK and transport adapters.
- DEC-27: Start `bash` in its own process group. On cancellation, send immediate `SIGKILL` to the group and fall back to the direct child PID when group termination fails.
- DEC-29: Create and enforce `~/.glyph` with mode `0700`. Store OAuth credentials at `~/.glyph/credentials.json` with mode `0600`, use a versioned map from provider ID to provider-owned opaque JSON payload, and replace the file atomically through a temporary file inside `~/.glyph`.
- DEC-30: Define `~/.glyph/settings.yaml` with required `defaultProvider` and `defaultModel` fields plus optional `defaultThinkingLevel` and `activeUI`. Reject provider values other than `openai-codex`, use the model's default reasoning level when `defaultThinkingLevel` is absent, and normalize `activeUI` through DEC-72 before lookup.
- DEC-31: Parse settings through `go.yaml.in/yaml/v3` v3.0.5 with `Decoder.KnownFields(true)`.
- DEC-32: Append TUI-mode structured logs to `~/.glyph/logs/glyph.log`, create `~/.glyph/logs/` with mode `0700`, and write headless operational logs to stderr. Do not rotate prototype logs.
- DEC-28: `edit` shall validate that the exact source fragment occurs once before writing, then replace the file content directly through `os.WriteFile`. Do not create a temporary file or promise atomic recovery from a write failure.

### Data and Control Flow

- STP-01: `glyph` loads settings and persisted OAuth credentials and resolves the effective extension directory through DEC-78. It discovers and normalizes extension candidates, accepts an empty catalog, starts every valid candidate independently, validates each complete tool catalog, removes extensions involved in tool-name conflicts, and builds the global tool-to-runtime registry.
- STP-01.1: Headless mode does not read or probe the UI catalog. UI mode resolves the effective UI directory through DEC-78, discovers and normalizes UI candidates, resolves `--ui` or `activeUI`, or probes candidate startup capabilities for sole-candidate selection.
- STP-02: UI mode starts the selected UI plugin, negotiates UI protocol version `1`, and retrieves its fixed startup capabilities. For a terminal UI, Host captures the controlling-terminal state before Host opens the bidirectional stream. After the stream opens, a terminal UI opens its controlling terminal.
- STP-02.1: Before accepting the first UI task, the Codex provider resolves or refreshes credentials and performs browser OAuth through Glyph Client Interaction when required. Headless startup with unusable credentials fails without starting OAuth.
- STP-02.2: Glyph emits the DEC-77 informational startup summary after extension and UI startup resolution and before accepting a UI task or writing headless model output.
- STP-03: The active controller submits one user message. Agent Core appends it to in-memory history, emits ordered lifecycle events, and starts the first model turn.
- STP-04: Before each model request, Agent Core projects history into provider context: it excludes `aborted` and `failed` responses and temporarily adds error results for tool calls skipped by cancellation.
- STP-05: The Codex adapter converts projected history and the global tool registry into an ordered Responses input-item list. It maps provider deltas to provider-neutral updates and returns one terminal model response containing ordered text, provider context, and tool calls.
- STP-06: Agent Core stores the terminal model response and applies its outcome. It ends on `stop`, executes finalized `tool_use` calls sequentially, handles `length` through DEC-54, and records `aborted` or `failed` responses through DEC-52.
- STP-07: For each executable tool call, the Host tool gateway resolves the owning extension runtime, sends `Execute`, maps progress to tool-execution updates, and returns the terminal result to history and the next model request.
- STP-08: Agent Core emits each event to Glyph Host in order. Glyph Host maps the event to headless output or the active UI stream and emits `agent_settled` after all `agent_end` recipients complete.
- STP-09: A standard TUI stop command or headless `Ctrl+C` cancels the run context. The same context reaches the provider request and active extension `Execute` stream.
- STP-10: UI stream completion or failure cancels the active run. Glyph Host then stops every extension runtime and the UI plugin process before terminating.

### Failure Modes

- FLR-01: UI mode fails startup when the effective UI directory is missing or unreadable, when the UI catalog contains an empty or duplicate normalized ID, or when a selected terminal UI cannot snapshot the controlling-terminal state. During sole-candidate selection, unavailable or incompatible candidates are excluded with warnings; zero or multiple eligible candidates fail startup. A `--ui` or `activeUI` selection that is unavailable, incompatible, or cannot start fails without fallback. Headless mode does not inspect the UI catalog.
- FLR-02: Active UI stream completion or failure cancels the active agent run and stops the selected UI process. For a terminal UI, Host then resets terminal modes and restores the saved state. Host stops every remaining plugin process, reports any terminal-recovery error, and terminates without selecting another UI plugin.
- FLR-03: A missing default extension directory or an empty extension catalog is normal. An unreadable default extension directory reports an error and yields an empty tool catalog; a missing or unreadable explicit extension directory fails startup. An extension candidate with an empty normalized ID is excluded; every candidate in a duplicate-ID group is excluded; unaffected candidates continue loading. Extension incompatibility, startup failure, process failure, invalid tool catalog, or protocol violation makes only that extension unavailable; its active tool call fails, every tool owned by it is removed immediately, other extensions remain available, and the prototype does not restart it. A later call to a removed tool returns a model-visible unavailable-tool error result.
- FLR-04: A duplicate tool name makes every extension registering that name unavailable as a complete extension. Glyph reports the conflicting name and plugin IDs, removes all tools owned by those extensions, stops their processes, and preserves every non-conflicting extension.
- FLR-05: Provider cancellation or failure finalizes history through DEC-52, reports the error, and performs no automatic retry. An unexpected Codex `401` reports sign-in-required and does not replay the failed model request.
- FLR-06: Agent event delivery failure ends the active run. A UI delivery failure follows FLR-02; a headless controller reports the terminal error and exits unsuccessfully.

### Operational Properties

- NFQ-01: Reliability comes from one active run, sequential tool execution, ordered event delivery, context cancellation, startup-only catalogs, terminal recovery after UI process failure, and no automatic process or provider retries.
- NFQ-02: Security relies on trusted local plugins with user permissions, owner-only credential and log directories, `go-plugin` cookies and Unix sockets, no credentials in plugin environments, and no secret data in logs or model-visible tool inputs.
- NFQ-03: Observability uses context-aware structured `log/slog` records. TUI-mode logs go to `~/.glyph/logs/glyph.log`; headless operational logs go to stderr; plugin-local `hclog` remains inside SDK and transport boundaries.
- NFQ-04: Prototype capacity is one Host process, one active UI plugin, one extension process per successfully loaded extension, one active run, one global cached tool registry, and one in-memory linear history. Numerical performance and load targets remain outside prototype scope.

## Overengineering and Overspecification Considerations

- TRD-01: The public SDK is limited to versioned protocol bootstrap and generated-contract access needed by independent plugin projects. It does not add a second model layer or expose Host internals, which keeps the public surface small while avoiding duplicated `go-plugin` wiring.
- TRD-02: The prototype supports one extension protocol version, one UI protocol version, any number of independently isolated extension processes, one active UI plugin, and three standard tools. Backward compatibility, runtime catalog changes, and automatic process restart are excluded.
- TRD-03: The gRPC contracts carry text progress and text results only. Rich content blocks and UI-specific rendering remain outside prototype scope.
- TRD-04: Owner-only local Unix sockets are sufficient for trusted local plugin processes. Additional transport security would not protect against a plugin that already has the user's operating-system permissions.
- TRD-05: Generated protobuf code is committed so normal source builds do not depend on Easyp or protobuf generators.
- TRD-06: Direct `edit` writes avoid transient project-directory files and editor or file-watcher churn. The accepted trade-off is that an I/O failure during `os.WriteFile` can leave an empty or partial file.
- TRD-07: Agent Core adds no event queue because gRPC and Bubble Tea already own the required concurrency boundaries. This avoids queue sizing, overflow, drain, and shutdown rules without blocking UI input or rendering.
- TRD-08: `tea.OpenTTY()` avoids a custom file-descriptor protocol while preserving terminal size, resize handling, and raw input inside the UI plugin process. The Host recovery guard duplicates only terminal cleanup needed after process death; it does not read input or render UI content.

### Accepted Technical Debt

- LIM-01: `~/.glyph/logs/glyph.log` grows without rotation or retention limits in the prototype.
- NXT-01: The full-product logging design must define rotation, retention, and maximum disk usage before replacing the prototype logging implementation.
- LIM-02: The prototype Codex provider has only an SSE transport. It does not implement WebSocket transport, connection-scoped continuation caching, automatic fallback from WebSocket to SSE, zstd request compression, device-code login, or provider-level automatic retries.
- NXT-02: Reassess the LIM-02 capabilities after the prototype proves browser OAuth, token refresh, SSE Responses, encrypted reasoning replay, and tool calls against the ChatGPT Codex backend.
- LIM-03: Browser OAuth does not work when Glyph runs through SSH on a remote computer or VPS because the browser's `localhost` callback reaches the user's local computer. The prototype does not accept a manually copied authorization code or redirect URL.
- NXT-03: Before claiming remote-terminal OAuth support, the full product must select and implement either manual redirect transfer, device-code login, or another provider-supported remote login flow.
- LIM-04: The prototype host accepts only the DEC-38 schema profile even though the protobuf field can carry a broader provider-neutral JSON Schema.
- NXT-04: Define the full product's supported provider-neutral schema profile and provider-specific transformations from evidence for each added provider. Preserve one schema per tool and keep provider adaptation outside Agent Core, following Pi's architectural direction without treating Pi behavior as an implicit requirement.
- LIM-05: The prototype Codex adapter sets `strict=true` for every tool because every DEC-41 schema satisfies the Codex strict-schema rules. It has no provider or schema capability selection.
- NXT-05: Each full-product provider adapter must select strictness from the provider, model, and schema capabilities while retaining local argument validation. Pi's capability-aware conversion is the reference direction.
- LIM-06: The prototype tool schemas exclude Pi's optional `read.offset`, `read.limit`, and `bash.timeout` arguments and its multi-replacement `edit.edits` structure.
- NXT-06: Define complete input schemas for all target bundled tools in their owning feature decisions. Use Pi schemas as evidence, not as requirements, and do not add fields until their behavior is approved.

## Open Questions

None.

## References

- REF-01: `docs/specs/features/initial/prototype-prd.md` — approved prototype requirements.
- REF-02: `docs/specs/features/initial/prd.md` — target product requirements and ownership boundaries.
- REF-03: `docs/terms.md` — domain terminology source.
- REF-04: `docs/artefacts/go-extension-feasibility.md` — evaluated Go extension mechanisms.
- REF-05: `https://github.com/hashicorp/go-plugin` — selected process and IPC library.
- REF-06: `https://github.com/easyp-tech/easyp` — selected protobuf generation and lint tool.
- REF-07: `https://github.com/openai/openai-go` — selected prototype Codex SSE client.
- REF-08: `https://github.com/charmbracelet/bubbletea` — selected TUI framework.
- REF-09: `go.mod` — runtime dependencies and pinned project tools.
- REF-10: `@earendil-works/pi-coding-agent/dist/core/tools/bash.js` and `dist/utils/shell.js` — Pi shell execution and cancellation behavior.
- REF-11: `@earendil-works/pi-coding-agent/dist/core/tools/edit.js` — Pi edit mutation and write behavior.
- REF-12: `@earendil-works/pi-ai/dist/auth/oauth/openai-codex.js` and `dist/api/openai-codex-responses.js` — Pi Codex OAuth and provider implementation.
- REF-13: `@earendil-works/pi-ai/dist/api/openai-responses-shared.js`, `anthropic-messages.js`, and `google-shared.js` — Pi provider-specific tool-schema conversion.
- REF-14: `https://github.com/openai/openai-go/tree/v3.49.0` — official Go Responses client evaluated for the Codex adapter.
- REF-15: `https://github.com/yaml/go-yaml/tree/v3.0.5` — selected YAML parser.
- REF-16: `https://github.com/santhosh-tekuri/jsonschema/tree/v6.0.2` — selected Draft 2020-12 schema compiler and validator.
- REF-17: `https://developers.openai.com/api/docs/guides/function-calling` — OpenAI function-tool and strict-schema requirements.
- REF-18: `https://github.com/anthropics/anthropic-sdk-go/blob/0303a8539676836e0cb351f3489fc2d347bbacde/message.go` — Anthropic Draft 2020-12 tool schema and strict-tool contract.
- REF-19: `experiments/codex-oauth-spike` — persisted `gpt-5.6-luna` live verification for PKCE, credential refresh, SSE, strict tool calling, error details, and encrypted reasoning replay.
- REF-20: Installed Pi `pi-agent-core/dist/agent-loop.js`, `agent.js`, and `harness/agent-harness.js` plus `pi-ai/dist/api/transform-messages.js` and `types.d.ts` — history shape, lifecycle-event ordering, aborted-response persistence, provider-context exclusion, and orphaned tool-result projection.
- REF-21: `go-plugin v1.8.0` `client.go` and `server.go`, Bubble Tea v2.0.8 `tea.go` and `tty.go`, and Ultraviolet `tty_unix.go` — plugin stdio behavior, lack of a public process completion channel, and direct controlling-terminal access.
- REF-22: `experiments/plugin-runtime-spike` — real-process and controlling-terminal evidence for versioned extension and UI protocols, multiple extensions, streamed execution, cancellation, crash isolation, collision cleanup, UI capability probing, bidirectional UI streaming, resize delivery, and Host terminal recovery after `os.Exit(23)`.
- REF-23: `https://pkg.go.dev/vuln/GO-2026-6061` — reachable gRPC HTTP/2 transport vulnerability fixed by DEC-09.2.
