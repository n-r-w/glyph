# Technical Solution: Glyph Minimal Prototype

## Problem Statement

- PRB-01: The prototype must prove the complete coding-agent path through a standard TUI and headless operation without making either interface own agent behavior.
- PRB-02: The Agent Core must use OpenAI Codex and tools from a separately built Go extension while remaining independent of provider authentication, extension processes, and terminal rendering.
- PRB-03: The extension boundary must carry tool discovery, streamed progress, results, errors, and cancellation without allowing an extension failure to terminate the Glyph Host.
- CNS-01: `docs/specs/features/initial/prototype-prd.md` is the requirements source for the prototype.
- CNS-02: Component ownership and dependency directions must remain suitable for the target product, while internal Go APIs may change after the prototype.
- CNS-03: The prototype is validated on macOS/arm64 and uses Go 1.26.5.

## Proposed Solution

### Status

- SOL-01: This document is a living technical solution. The decisions below are approved; the Open Questions section contains unresolved design choices and current recommendations.

### Process Topology

```text
glyph
├── standard TUI controller ─┐
└── headless run controller ─┴── Agent Core
                                  ├── model contract ── OpenAI Codex provider
                                  └── tool contract ─── Glyph Host extension client
                                                            │
                                                            │ go-plugin + gRPC
                                                            │ Unix domain socket
                                                            ▼
                                                       glyph-tools
                                                       ├── read
                                                       ├── edit
                                                       └── bash
```

- DGM-01: Both interface controllers invoke the same Agent Core contracts. The Glyph Host supplies the configured provider and extension-backed tools; Agent Core owns the run.

### Components

- CMP-01: `glyph` is the only application executable. Running `glyph` starts the standard TUI; `glyph run` starts one headless request.
- CMP-02: Agent Core owns the in-memory history, agent-run state, model/tool loop, sequential tool execution, and cancellation.
- CMP-03: Glyph Host owns configured providers, extension discovery, the `glyph-tools` process, the cached tool catalog, and extension availability state.
- CMP-04: The standard TUI and headless controllers convert user input into Agent Core calls and convert Agent Core events into terminal output. They contain no model/tool orchestration.
- CMP-05: The OpenAI Codex provider owns OAuth, token refresh, request authorization, request serialization, transport selection, and streamed response decoding. Agent Core does not depend on provider SDK types.
- CMP-06: The internal extension IPC adapter maps Agent Core tool calls to the public protobuf contract and maps streamed protobuf events back to Agent Core events.
- CMP-07: `glyph-tools` is a separately built executable that registers and executes `read`, `edit`, and `bash`.
- CMP-08: `pkg/extension/v1` is the only public Go package in the prototype extension boundary. It contains generated protobuf and gRPC code.

### Approved Decisions

#### Application and Ownership

- DEC-01: Build one `glyph` executable and one separate `glyph-tools` executable. `glyph` starts the TUI by default; `glyph run` performs one headless request.
- DEC-02: Agent Core owns run state, history, the model/tool loop, and cancellation. Glyph Host owns providers and extension processes and supplies them through contracts. TUI and headless code only handle input and events.

#### Codex Provider

- DEC-33: Send prototype model requests to the ChatGPT Codex Responses endpoint with `store=false`, the coding-agent prompt in `instructions`, `reasoning.encrypted_content` included for local history replay, and optional `defaultThinkingLevel` mapped to reasoning effort. Resolve a fresh OAuth access token and account ID for every request, set `chatgpt-account-id`, `OpenAI-Beta: responses=experimental`, `originator: glyph`, and `User-Agent: glyph`, and disable SDK retries.
- DEC-34: Use `github.com/openai/openai-go/v3` as the internal implementation of the prototype Codex SSE transport. Keep its request, response, and event types inside that transport and emit provider-neutral events to Agent Core. A future custom WebSocket transport, connection-scoped continuation cache, and automatic transport fallback remain internal Codex-provider changes.
- DEC-35: Use `golang.org/x/oauth2` for PKCE generation, authorization URL construction, and authorization-code exchange. Keep the loopback callback server, `state` validation, Codex JSON refresh request, and provider-owned credential mapping inside the Codex provider.
- DEC-36: Support browser OAuth only when Glyph and the browser run on the same computer. Attempt registered callback address `127.0.0.1:1455` first and `127.0.0.1:1457` second. Always display the authorization URL in the TUI and treat automatic browser launch as best-effort.
- DEC-37: Refresh an OAuth access token before a model request when no more than five minutes remain before expiry and persist the rotated credential through DEC-29. When the backend returns `401`, fail the run with a sign-in-required error; do not refresh and replay the model request.

#### Extension Process and Transport

- DEC-03: Use `github.com/hashicorp/go-plugin` with gRPC for extension process lifecycle and IPC.
- DEC-04: Use `go-plugin.VersionedPlugins` with exactly protocol version `1`. A version mismatch makes the extension unavailable. The prototype has no compatibility path for older protocol versions.
- DEC-05: Use the Unix domain socket selected by `go-plugin` on macOS. Rely on owner access and the library-required `MagicCookie`; do not add TLS, `AutoMTLS`, checksum verification, or reattach support.
- DEC-06: Keep the go-plugin handshake and client/server adapters in `internal/extensionipc`. Do not publish a prototype extension SDK.
- DEC-07: Start `glyph-tools` with the working directory and environment inherited from `glyph`. OAuth credentials remain in the credential file and are not injected into the environment.

#### Protobuf Contract

- DEC-08: Store the source contract at `api/extension/v1/tool.proto` and generated Go code at `pkg/extension/v1`.
- DEC-09: Pin Easyp, `mockgen`, `protoc-gen-go`, and `protoc-gen-go-grpc` with standard `tool` directives in the main `go.mod`. Invoke them through `go tool`, commit generated Go files, and exclude Easyp package management, remote plugins, and breaking checks from the prototype workflow.
- DEC-09.1: Override Easyp's retracted transitive `github.com/klauspost/compress v1.18.1` with `v1.19.1` in the main `go.mod`.
- DEC-10: Expose one unary `ListTools` RPC and one server-streaming `Execute` RPC.
- DEC-11: `ListTools` returns each tool's name, description, and one provider-neutral JSON Schema. The host calls it once after extension startup, validates the complete catalog, and caches it until the process stops.
- DEC-12: Reject the complete catalog, stop the extension process, and mark the extension unavailable when a descriptor has an empty or duplicate name, an empty description, a schema that fails Draft 2020-12 compilation, or a schema outside the prototype profile in DEC-38. Do not partially register tools.
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
- DEC-20: Discover exactly one extension executable at `~/.glyph/extensions/glyph-tools`. Do not scan for additional extensions in the prototype.
- DEC-21: Implement the standard TUI with Bubble Tea v2, one TUI state model, and Agent Core events translated into Bubble Tea messages. Keep input and rendering state in the TUI component.
- DEC-22: Store one English coding-agent prompt in a text file owned by the prototype coding-agent configuration, embed it into `glyph`, and inject its content into Agent Core at construction.
- DEC-23: Use TDD with Agent Core unit tests against generated consumer-owned mocks, extension contract tests through go-plugin gRPC test helpers, tool tests in temporary directories, and Codex adapter tests against `httptest` streams. Do not test documentation or source layout.
- DEC-24: Accept the headless request as the remaining `glyph run` command-line argument, write model text to stdout, write tool lifecycle and progress with short textual prefixes, and write terminal errors to stderr.
- DEC-25: Store prototype settings at `~/.glyph/settings.yaml` and extension executables under `~/.glyph/extensions/`.
- DEC-26: Create one application context with `signal.NotifyContext`, derive one context per agent run, cancel the active run before `plugin.Client.Kill`, use `log/slog` for application logs, and keep go-plugin's `hclog` use inside `internal/extensionipc`.
- DEC-27: Start `bash` in its own process group. On cancellation, send immediate `SIGKILL` to the group and fall back to the direct child PID when group termination fails.
- DEC-29: Store OAuth credentials at `~/.glyph/credentials.json` with mode `0600`. Use a versioned map from provider ID to provider-owned opaque JSON payload and replace the file atomically through a temporary file inside `~/.glyph`.
- DEC-30: Define `~/.glyph/settings.yaml` with required `defaultProvider` and `defaultModel` fields and optional `defaultThinkingLevel`. Reject provider values other than `openai-codex`; use the model's default reasoning level when `defaultThinkingLevel` is absent.
- DEC-31: Parse settings through `go.yaml.in/yaml/v3` v3.0.5 with `Decoder.KnownFields(true)`.
- DEC-32: Append TUI-mode structured logs to `~/.glyph/logs/glyph.log`, create `~/.glyph/logs/` with mode `0700`, and write headless operational logs to stderr. Do not rotate prototype logs.
- DEC-28: `edit` shall validate that the exact source fragment occurs once before writing, then replace the file content directly through `os.WriteFile`. Do not create a temporary file or promise atomic recovery from a write failure.

### Data and Control Flow

- STP-01: `glyph` loads configuration and persisted OAuth credentials, discovers `glyph-tools`, negotiates extension protocol version `1`, and caches the validated tool catalog.
- STP-02: The selected interface submits one user message to Agent Core. Agent Core appends it to the in-memory history and requests a streamed model response.
- STP-03: The Codex provider translates the history and cached tool descriptors into a Responses API request and converts provider stream items into Agent Core model events.
- STP-04: Agent Core forwards text deltas to the active interface. When the model finishes a tool call, Agent Core invokes the extension-backed tool contract.
- STP-05: The internal extension adapter sends `Execute`, maps progress events to Agent Core tool-progress events, and returns the terminal result to the history and the next model request.
- STP-06: Agent Core repeats model and tool work sequentially until the model emits a final response without tool calls or the run context is cancelled.
- STP-07: TUI stop or headless `Ctrl+C` cancels the run context. The same context reaches the provider request and active extension `Execute` stream.

## Overengineering and Overspecification Considerations

- TRD-01: Only the wire contract is public. Publishing an SDK before the full extension API exists would freeze unnecessary Go APIs.
- TRD-02: The prototype supports one protocol version, one extension process, and three tools. Backward compatibility, dynamic catalogs, process restart, and multiple extensions are excluded.
- TRD-03: The gRPC contract carries text progress and text results only. Rich content blocks and interface-specific rendering remain outside prototype scope.
- TRD-04: The owner-only local Unix socket is sufficient for the trusted local extension model. Additional transport security would not protect against an extension that already has the user's operating-system permissions.
- TRD-05: Generated protobuf code is committed so normal source builds do not depend on Easyp or protobuf generators.
- TRD-06: Direct `edit` writes avoid transient project-directory files and editor or file-watcher churn. The accepted trade-off is that an I/O failure during `os.WriteFile` can leave an empty or partial file.

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

### QST-06: OpenAI Codex live compatibility
- **Impact:** Static source inspection cannot prove that the selected OAuth and SSE implementations remain accepted by the live ChatGPT Codex backend.
- **Evidence checked:** Pi and the official Codex client establish the OAuth parameters, registered callback ports, token fields, request headers, and Responses behavior used by DEC-33 through DEC-37. Docker Agent and Dagger demonstrate `openai-go` against the same backend. These sources do not replace an end-to-end request using Glyph's selected combination.
- **Resolution:** Validate one live browser login, forced token refresh, streamed response, encrypted reasoning replay, and tool call before closing QST-06.

### QST-07: Agent Core model, history, and event contracts
- **Impact:** These contracts determine whether TUI, headless mode, and future providers remain independent from one another.
- **Recommended answer:** Define provider and tool interfaces in the Agent Core consumer package; use provider-neutral message, tool-call, tool-result, and run-event types; expose streaming through a pull-based `Recv` contract tied to context cancellation.
- **Evidence checked:** Approved ownership places the loop in Agent Core, while provider and extension implementations remain outside it. Consumer-owned interfaces are required by the Go project rules.
- **Resolution:** Conduct a separate Q&A session covering the target directory structure, package ownership, domain types, and event variants.

### QST-08: Go package layout and composition root
- **Impact:** Package ownership determines dependency direction and whether infrastructure types leak into Agent Core.
- **Recommended answer:** Use small feature-oriented packages under `internal`: `app`, `agent`, `host`, `codex`, `tui`, `headless`, `extensionipc`, and `tools`; keep `cmd` entry points limited to startup and use `internal/app` as the concrete composition root.
- **Evidence checked:** The repository currently has only `cmd/glyph` in its build task, while approved decisions require one application executable and one extension executable.
- **Resolution:** Conduct the same dedicated Q&A session as QST-07 before accepting any package tree.

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
