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
- CMP-05: The OpenAI Codex provider owns OAuth, token refresh, request authorization, request serialization, and streamed response decoding. Its concrete implementation remains open.
- CMP-06: The internal extension IPC adapter maps Agent Core tool calls to the public protobuf contract and maps streamed protobuf events back to Agent Core events.
- CMP-07: `glyph-tools` is a separately built executable that registers and executes `read`, `edit`, and `bash`.
- CMP-08: `pkg/extension/v1` is the only public Go package in the prototype extension boundary. It contains generated protobuf and gRPC code.

### Approved Decisions

#### Application and Ownership

- DEC-01: Build one `glyph` executable and one separate `glyph-tools` executable. `glyph` starts the TUI by default; `glyph run` performs one headless request.
- DEC-02: Agent Core owns run state, history, the model/tool loop, and cancellation. Glyph Host owns providers and extension processes and supplies them through contracts. TUI and headless code only handle input and events.

#### Extension Process and Transport

- DEC-03: Use `github.com/hashicorp/go-plugin` with gRPC for extension process lifecycle and IPC.
- DEC-04: Use `go-plugin.VersionedPlugins` with exactly protocol version `1`. A version mismatch makes the extension unavailable. The prototype has no compatibility path for older protocol versions.
- DEC-05: Use the Unix domain socket selected by `go-plugin` on macOS. Rely on owner access and the library-required `MagicCookie`; do not add TLS, `AutoMTLS`, checksum verification, or reattach support.
- DEC-06: Keep the go-plugin handshake and client/server adapters in `internal/extensionipc`. Do not publish a prototype extension SDK.
- DEC-07: Start `glyph-tools` with the working directory and environment inherited from `glyph`. OAuth credentials remain in the credential file and are not injected into the environment.

#### Protobuf Contract

- DEC-08: Store the source contract at `api/extension/v1/tool.proto` and generated Go code at `pkg/extension/v1`.
- DEC-09: Pin Easyp, `protoc-gen-go`, and `protoc-gen-go-grpc` in `tools.mod` and `tools.sum`. Invoke them through `go tool -modfile=tools.mod`, commit generated Go files, and exclude Easyp package management, remote plugins, and breaking checks from the prototype workflow. Keep `mockgen` in the main `go.mod` for package-local `go:generate` directives.
- DEC-09.1: Override Easyp's retracted transitive `github.com/klauspost/compress v1.18.1` with `v1.19.1` in `tools.mod`.
- DEC-10: Expose one unary `ListTools` RPC and one server-streaming `Execute` RPC.
- DEC-11: `ListTools` returns each tool's name, description, and JSON Schema. The host calls it once after extension startup, validates the complete catalog, and caches it until the process stops.
- DEC-12: Reject the complete catalog, stop the extension process, and mark the extension unavailable when a descriptor has an empty or duplicate name, an empty description, or syntactically invalid JSON Schema. Do not partially register tools.
- DEC-13: Encode JSON Schema and tool arguments as UTF-8 JSON in protobuf `bytes` fields named `input_schema_json` and `arguments_json`. The extension performs typed argument decoding and validation.

#### Tool Execution Stream

- DEC-14: `Execute` emits zero or more progress events followed by one terminal result and then closes the stream. `ExecuteEvent` uses `oneof progress | result`; progress channels are `STATUS`, `STDOUT`, and `STDERR`.
- DEC-15: Return tool-operation failures in a terminal `ToolResult` with `is_error=true`, so Agent Core can return them to the model and continue the loop. Reserve gRPC status errors for cancellation, protocol violations, and extension-process unavailability.
- DEC-16: Propagate cancellation by cancelling the `Execute` context. Do not add an execution identifier or a separate `Cancel` RPC.
- DEC-17: Treat clean stream completion without a result, a second result, or an event after the terminal result as a protocol violation. Fail the active tool call, stop the extension process, and mark the extension unavailable. A context-driven `codes.Canceled` result is not a protocol violation.
- DEC-18: A nonzero `bash` exit produces a terminal `ToolResult` with `is_error=true` containing `stdout`, `stderr`, and the exit code. Agent Core returns it to the model and continues the loop.

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

## Open Questions

### QST-01: Bash shell invocation
- **Impact:** Determines command semantics and whether user profile scripts can change execution behavior.
- **Recommended answer:** Resolve `bash` through `exec.LookPath` and execute `bash -c <command>` in the working directory without loading login profiles.
- **Evidence checked:** The extension inherits the host environment, including `PATH`; the prototype tool is explicitly named `bash`.
- **Resolution:** Author selects the shell invocation before finalizing the tool design.

### QST-02: Bash process-tree cancellation
- **Impact:** Cancelling only the direct shell can leave child processes running after the agent returns to idle.
- **Recommended answer:** Start `bash` in its own process group, send `SIGTERM` to the group, then send `SIGKILL` after a two-second grace interval when processes remain.
- **Evidence checked:** `exec.CommandContext` does not guarantee termination of descendants; the prototype is currently macOS-only and the target product also includes Linux.
- **Resolution:** Author selects cancellation semantics and the grace interval before finalizing `glyph-tools`.

### QST-03: Atomic edit writes
- **Impact:** An interrupted in-place write can corrupt a source file and violate the requirement that failed edits leave it unchanged.
- **Recommended answer:** Write a temporary file in the source directory, preserve the source mode, close the temporary file, and replace the source with an atomic rename.
- **Evidence checked:** The Go standard library provides all required file and rename operations; no dependency is needed.
- **Resolution:** Author selects the write strategy before finalizing `edit`.

### QST-04: Extension executable selection
- **Impact:** Defines whether the prototype discovery path is fixed or already supports multiple extensions.
- **Recommended answer:** Resolve exactly one executable named `glyph-tools` inside the extension directory. Keep multi-extension scanning outside the prototype.
- **Evidence checked:** The prototype requirements allow one extension and explicitly exclude full extension lifecycle management.
- **Resolution:** Author selects the discovery rule before defining filesystem paths.

### QST-05: User-file layout and configuration format
- **Impact:** Determines locations and permissions for model configuration, credentials, and the extension directory.
- **Recommended answer:** Use one directory derived from `os.UserConfigDir()` with a small JSON configuration, a credential file created with user-only permissions, and an `extensions` child directory.
- **Evidence checked:** The prototype requires platform-independent Go facilities, one configured model, persisted user-only credentials, and manual extension placement.
- **Resolution:** Confirm the directory layout, configuration fields, and atomic credential-write behavior.

### QST-06: OpenAI Codex adapter and OAuth implementation
- **Impact:** This is the remaining external-integration blocker for authentication, token refresh, Responses streaming, and tool calls.
- **Recommended answer:** First compare the local Pi Codex implementation with the official `github.com/openai/openai-go/v3` Responses client. Use the official SDK only when it supports the Codex base URL, OAuth authorization headers, and required streaming events without leaking provider types into Agent Core.
- **Evidence checked:** `openai-go` v3.49.0 supports Go 1.25+, Responses streaming, tool calls, custom headers, and request options; Codex OAuth compatibility has not yet been established.
- **Resolution:** Inspect the full local Pi OAuth/provider flow and the SDK extension points, then present one provider implementation choice to the author.

### QST-07: Agent Core model, history, and event contracts
- **Impact:** These contracts determine whether TUI, headless mode, and future providers remain independent from one another.
- **Recommended answer:** Define provider and tool interfaces in the Agent Core consumer package; use provider-neutral message, tool-call, tool-result, and run-event types; expose streaming through a pull-based `Recv` contract tied to context cancellation.
- **Evidence checked:** Approved ownership places the loop in Agent Core, while provider and extension implementations remain outside it. Consumer-owned interfaces are required by the Go project rules.
- **Resolution:** Review the minimum event variants needed for text, tool calls, progress, completion, and errors with the author.

### QST-08: Go package layout and composition root
- **Impact:** Package ownership determines dependency direction and whether infrastructure types leak into Agent Core.
- **Recommended answer:** Use small feature-oriented packages under `internal`: `app`, `agent`, `host`, `codex`, `tui`, `headless`, `extensionipc`, and `tools`; keep `cmd` entry points limited to startup and use `internal/app` as the concrete composition root.
- **Evidence checked:** The repository currently has only `cmd/glyph` in its build task, while approved decisions require one application executable and one extension executable.
- **Resolution:** Confirm the package map after Agent Core contracts and the Codex adapter are defined.

### QST-09: TUI framework and state model
- **Impact:** Determines terminal event handling, streaming updates, input locking during a run, and cancellation behavior.
- **Recommended answer:** Use Bubble Tea v2 with one TUI state model and translate Agent Core events into Bubble Tea messages. Keep rendering and input state in the TUI package.
- **Evidence checked:** Bubble Tea v2.0.8 is current as of July 2026 and provides an event-driven terminal model that matches the approved interface boundary.
- **Resolution:** Author selects the TUI dependency before defining terminal states and commands.

### QST-10: Application lifecycle and operational logging
- **Impact:** Startup errors, TUI shutdown, headless `Ctrl+C`, and extension cleanup must produce one deterministic lifecycle without corrupting terminal output.
- **Recommended answer:** Create one root context with `signal.NotifyContext`; derive one run context per request; cancel the active run before calling `plugin.Client.Kill`; use `log/slog` for application logs and isolate go-plugin's `hclog` usage inside `internal/extensionipc`.
- **Evidence checked:** `go-plugin.Client.Kill` owns subprocess cleanup, and approved cancellation already propagates through provider and tool contexts.
- **Resolution:** Confirm shutdown ordering and where TUI-mode operational logs are written.

### QST-11: Static coding-agent instruction
- **Impact:** The instruction must remain outside Agent Core while being available identically to TUI and headless operation.
- **Recommended answer:** Store one English prompt text file with the prototype coding-agent configuration and embed it into the application binary; inject the resulting text into Agent Core at construction.
- **Evidence checked:** The prototype PRD requires a static instruction owned by coding-agent configuration and excludes resource extensions.
- **Resolution:** Author approves the storage method and prompt content separately.

### QST-12: Test architecture
- **Impact:** Agent-loop, stream, cancellation, file, and provider behavior need deterministic checks before implementation can safely evolve.
- **Recommended answer:** Use TDD with Agent Core unit tests against generated consumer-owned mocks, extension contract tests through go-plugin gRPC test helpers, tool tests in temporary directories, and Codex adapter tests against `httptest` streams. Do not add snapshot tests for document or source layout.
- **Evidence checked:** The repository already runs `go test -race ./...`; go-plugin exposes gRPC test helpers; project rules require deterministic behavioral tests and temporary filesystem use.
- **Resolution:** Confirm test boundaries after the Agent Core and provider contracts are approved.

### QST-13: Tool schemas and strict argument decoding
- **Impact:** Tool descriptors must match the arguments accepted by typed Go implementations without adding a runtime schema-validation dependency.
- **Recommended answer:** Keep three explicit JSON Schema documents beside the tool implementations and decode arguments with `encoding/json` using `DisallowUnknownFields`; validate required fields in typed constructors or handlers.
- **Evidence checked:** The tool inputs are small and fixed for the prototype, and the extension already owns typed validation.
- **Resolution:** Approve exact `read`, `edit`, and `bash` input fields before implementation.

### QST-14: Headless command input and output formatting
- **Impact:** Defines how one request enters `glyph run` and how human-readable model and tool events appear without creating a stable protocol.
- **Recommended answer:** Accept the request as the remaining command-line argument, print model text to stdout, print tool lifecycle/progress with short textual prefixes, and print terminal errors to stderr.
- **Evidence checked:** The prototype requires a one-shot human-readable command and explicitly excludes structured output and a stable programmatic protocol.
- **Resolution:** Author approves the command syntax and output ownership after the shared run-event contract is defined.

## References

- REF-01: `docs/specs/features/initial/prototype-prd.md` — approved prototype requirements.
- REF-02: `docs/specs/features/initial/prd.md` — target product requirements and ownership boundaries.
- REF-03: `docs/terms.md` — domain terminology source.
- REF-04: `docs/artefacts/go-extension-feasibility.md` — evaluated Go extension mechanisms.
- REF-05: `https://github.com/hashicorp/go-plugin` — selected process and IPC library.
- REF-06: `https://github.com/easyp-tech/easyp` — selected protobuf generation and lint tool.
- REF-07: `https://github.com/openai/openai-go` — candidate Responses API client.
- REF-08: `https://github.com/charmbracelet/bubbletea` — recommended TUI framework candidate.
- REF-09: `tools.mod` — isolated protobuf tooling module.
