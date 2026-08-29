# Idea: Glyph Minimal Prototype

## Definitions

- `Glyph`: The project name for the independent Go agent platform being defined.
- `Glyph host`: The platform layer that manages extension runtimes and connects them to the agent core and Glyph clients without owning client-specific behavior.
- `agent core`: The required part of an agent platform that provides runtime behavior shared by its agents.
- `agent loop`: The repeated sequence of requesting a model response, executing model-requested actions, and returning their results to the model until the run completes or is stopped.
- `agent run`: One continuous agent-loop execution initiated by a message and ending when no automatic model or tool work remains or the run is stopped.
- `coding agent`: An agent intended to work with source code and related software development tasks.
- `tool`: A typed operation that an agent exposes to a model by name.
- `Glyph plugin`: A separately delivered Glyph component. The defined Glyph plugin kinds are extension and UI plugin.
- `extension`: A Glyph plugin that contributes platform or agent capabilities through extension contracts.
- `UI plugin`: A Glyph plugin that presents Glyph to a person and communicates with Glyph as a Glyph client.
- `extension catalog`: The collection of extensions available to a Glyph host.
- `UI catalog`: The collection of discovered UI plugins considered by a Glyph host during UI selection.
- `extension runtime`: One loaded execution environment for an extension and its in-memory state.
- `Glyph client`: A component connected to a Glyph host that sends commands and receives events. A Glyph client is either a UI plugin or a programmatic controller.
- `programmatic controller`: A Glyph client that controls a headless agent without presenting a UI.
- `headless agent`: A Glyph agent instance controlled programmatically without a UI.
- `model provider`: A local or remote system through which an agent accesses a language model.
- `reasoning level`: A configured setting for model reasoning effort, limited by the selected model's capabilities.
- `session`: A related sequence of user requests, model responses, tool calls, and agent state.
- `UI`: A presentation and input surface through which a person interacts with Glyph.
- `terminal UI`: A UI presented inside a terminal.
- `standard TUI`: The terminal UI plugin distributed with Glyph; it owns terminal-specific rendering, input, and extension capabilities.
- `Go interface`: A Go language type that defines a method set; this term does not refer to a UI plugin, Glyph client, or extension.

## Context and Problem

The target product requirements in `docs/specs/features/initial/prd.md` cover the complete Glyph platform. Implementing that entire scope before running an end-to-end agent would delay validation of the product flow and the boundaries among the Glyph host, agent core, Glyph clients, providers, and extension runtimes.

The project author needs a runnable vertical slice that demonstrates a useful coding-agent task while preserving the target ownership and dependency boundaries. Internal APIs may evolve after the prototype.

## Goal

Deliver a minimal Glyph prototype that runs the same agent core through the standard TUI or in headless mode, uses OpenAI Codex, and executes coding tools supplied by a runtime-loaded Go extension.

## Scenarios

- The author starts Glyph without headless mode, the Glyph host selects and starts the standard TUI from the UI catalog, and the author completes OpenAI Codex OAuth when required, submits a coding task, observes streamed model and tool output, and continues the conversation after the agent becomes idle.
- The agent reads and changes a file, runs a project command, and reports the result.
- The author starts a headless agent with one text request and no UI plugin. It uses the same model configuration, credentials, extensions, and agent-tool loop as the standard TUI.
- The author stops an active model request or tool and can continue using the standard TUI.
- An incompatible or failed extension becomes unavailable without terminating the Glyph host or another extension.
- When the active UI plugin exits, Glyph cancels the active agent run and terminates.

## Scope and Non-Scope

### Scope

- A Go Glyph host, UI-free agent core, UI catalog with the standard TUI, and one-shot headless operation.
- OpenAI Codex with interactive OAuth, one configured provider/model pair, and an optional startup reasoning level.
- One or more runtime-loaded Go extensions; the source repository includes the standard tools extension that provides `read`, `edit`, and `bash`.
- One in-memory linear session and one active agent run.
- macOS on arm64.
- Source-based build and execution.

### Non-Scope

- Persistent or tree-structured sessions and context compaction.
- OpenAI-compatible providers, model selection, model cycling, and runtime reasoning-level switching.
- A stable programmatic control protocol, structured headless output, message queues, concurrent agent runs, and automatic retries.
- Extension installation commands, enablement, disablement, updates, environment reload, lifecycle events, resource contributions, and TUI-specific extension capabilities.
- UI plugin installation commands, enablement, disablement, updates, and environment reload.
- Remote or independently started UI plugins, more than one active UI plugin, and UI plugin replacement without restarting Glyph.
- The bundled resource extension, skills, prompt templates, and context files.
- `write`, `grep`, `find`, and `ls`.
- TUI themes, widgets, session-tree interaction, extension-provided content, user command discovery, and configurable key bindings.
- Linux validation, installers, and release packages.
- Sandboxing, project-trust policy, and tool confirmations.
- Numerical performance targets, load testing, and dedicated accessibility requirements.

## Requirements

### Platform and Delivery

- Glyph prototype code shall be written in Go.
  - **Main PRD:** `Matches` — Platform Requirements require Glyph production code to be written in Go.
- The prototype shall be validated on macOS running on arm64.
  - **Main PRD:** `Differs` — Platform Requirements require macOS and Linux support; the prototype validates only macOS on arm64.
- The prototype shall use platform-independent Go facilities when they provide behavior equivalent to an operating-system-specific facility.
  - **Main PRD:** `Matches` — Platform Requirements contain the same portability requirement.
- The agent core shall run without loading or depending on a UI plugin.
  - **Main PRD:** `Matches` — Platform Requirements contain the same dependency constraint.
- The Glyph host shall run in headless mode without loading a UI plugin, and no UI plugin implementation shall own agent-core behavior.
  - **Main PRD:** `Matches` — Platform Requirements contain the same headless and ownership constraints.
- The prototype shall be buildable and runnable from its source repository through documented commands, with the Glyph host application, standard TUI executable, and standard tools extension executable built separately.
  - **Main PRD:** `No direct match` — the target PRD requires open-source distribution and separate UI plugin behavior but does not define prototype build commands or build outputs.
- The author shall be able to override the extension catalog directory and UI catalog directory independently for one Glyph invocation. Each override shall replace the corresponding default directory for that invocation without changing persisted settings.
  - **Main PRD:** `No direct match` — the target PRD defines separate catalogs but does not define per-invocation directory overrides.
- At every startup, Glyph shall present an informational summary through the active UI plugin or headless output. The summary shall identify the selected UI plugin or headless mode, every successfully loaded extension, and the tools registered by each extension. No loaded extensions or tools shall be reported as a normal state without a warning or error; extension load failures shall be reported separately.
  - **Main PRD:** `No direct match` — the target PRD does not define a startup summary.

### UI Plugin Startup

- Glyph startup shall either enable headless mode or select one UI plugin. Headless mode shall not start a UI plugin, and supplying a UI selection together with headless mode shall fail startup explicitly.
  - **Main PRD:** `Matches` — UI Plugins defines the same startup-mode behavior.
- The Glyph host shall discover locally available UI plugin executables in the UI catalog before selection, and the standard TUI shall be present in that catalog by default.
  - **Main PRD:** `Matches` — UI Plugins defines the same catalog and standard-TUI behavior.
- Before automatic UI selection, the Glyph host shall exclude each UI plugin it identifies as unavailable or incompatible with the running Glyph version and shall report a warning for each exclusion.
  - **Main PRD:** `Matches` — UI Plugins defines the same automatic-exclusion behavior.
- When headless mode is not enabled, the Glyph host shall select a UI plugin in this order: an explicit startup selection, the active UI setting, or the only UI plugin remaining after automatic exclusions.
  - **Main PRD:** `Matches` — UI Plugins defines the same selection order.
- An explicit startup selection or active UI setting that identifies a UI plugin that is unavailable or incompatible with the running Glyph version, or whose UI plugin cannot start, shall fail startup without fallback. Without either selection, having no UI plugin or more than one UI plugin remaining after automatic exclusions shall fail startup explicitly.
  - **Main PRD:** `Matches` — UI Plugins defines the same failure behavior.
- The Glyph host shall start the selected UI plugin and own its lifecycle until the UI plugin or the Glyph host exits.
  - **Main PRD:** `Matches` — UI Plugins defines the same host ownership behavior.
- One Glyph host process shall use at most one UI plugin for its entire lifetime; another UI plugin cannot attach or replace it.
  - **Main PRD:** `Matches` — UI Plugins contains the same cardinality requirement.
- The UI catalog and selected UI plugin shall remain unchanged for the lifetime of the Glyph host process. Changes to the UI catalog or active UI setting shall take effect at the next Glyph start.
  - **Main PRD:** `Matches` — UI Plugins contains the same startup-only catalog and selection behavior.
- When the active UI plugin exits for any reason, the Glyph host shall cancel the active agent run and terminate.
  - **Main PRD:** `Matches` — UI Plugins contains the same termination behavior.

### Provider and Authentication

- The prototype shall provide only the OpenAI Codex model provider.
  - **Main PRD:** `Differs` — Model Providers and Authentication require both OpenAI Codex and a user-configured OpenAI-compatible provider; the prototype includes only OpenAI Codex.
- The OpenAI Codex provider shall own OAuth login, token refresh, and conversion of credentials into request authorization.
  - **Main PRD:** `Matches` — Model Providers and Authentication assign authentication and token refresh to each provider.
- OpenAI Codex OAuth credentials shall persist in a Glyph credential file accessible only to the user running Glyph.
  - **Main PRD:** `Matches` — Model Providers and Authentication require persistent host credential storage, a user-only local credential file, and Codex OAuth persistence through that storage.
- When the standard TUI starts without stored credentials that the provider can use or refresh, it shall start interactive OAuth and allow task input after authentication succeeds.
  - **Main PRD:** `No direct match` — the target PRD requires interactive Codex OAuth but does not define this automatic TUI startup transition.
- A headless start without stored credentials that the provider can use or refresh shall fail explicitly instead of starting OAuth.
  - **Main PRD:** `Differs` — the target PRD permits provider interaction through a Glyph client; the prototype restricts initial OAuth to the standard TUI.
- The settings file shall require `defaultProvider` and `defaultModel`; `defaultProvider` shall be `openai-codex`. Standard-TUI and headless operation shall use the same configured values, and runtime model selection or switching shall be unavailable.
  - **Main PRD:** `Differs` — Model Runtime and Standard TUI Requirements support provider/model changes and user-facing model selection; the prototype reads one provider/model pair at startup and does not change it at runtime.
- The settings file may contain `defaultThinkingLevel`. When present, the prototype shall use that value; when absent, it shall use the configured model's default reasoning level. Runtime reasoning-level switching shall be unavailable.
  - **Main PRD:** `Differs` — Programmatic Control supports reasoning selection at runtime; the prototype supports only one optional startup value.
- Persisted OAuth tokens shall exist only in the user-only credential file. Glyph shall not inject, log, render, serialize, or place OAuth credentials in configuration, model context, or tool parameters.
  - **Main PRD:** `Differs` — Model Providers and Authentication prohibit secret values in provider configuration and restrict credential-file access; the prototype also prohibits Glyph-managed credential exposure through output and execution boundaries.
- Extensions and tools are trusted, unsandboxed executables that run as the same user as Glyph. When Agent Core executes a model-requested tool call, that executable may access any file readable by the user, including the Glyph credential file; the prototype does not add path filters, command filters, or sandboxing.
  - **Main PRD:** `No direct match` — the target PRD does not define a sandbox or filesystem isolation boundary for extension tools.

### Extension Runtime and Tools

- The author shall be able to place separately built extension executables in the extension directory, after which the Glyph host shall discover them at the next application start without rebuilding Glyph.
  - **Main PRD:** `Differs` — Extensions and Glyph Clients require installation, enablement, disablement, and updates without rebuilding Glyph; the prototype uses manual file placement and startup discovery for one or more extensions.
- An empty extension catalog shall be valid and shall produce an empty tool catalog without a warning or startup error.
  - **Main PRD:** `No direct match` — the target PRD does not define behavior when no extensions are available.
- The Glyph host shall independently attempt to start every discovered extension with the application and shall stop every started extension when the application exits.
  - **Main PRD:** `Differs` — the target extension lifecycle also covers enablement, disablement, updates, environment reload, and post-failure unavailability; the prototype supports independent extension startup and application shutdown only.
- The extension contract shall support registration and execution of `read`, `edit`, and `bash`, final results, streamed progress, and cancellation.
  - **Main PRD:** `Differs` — Bundled Standard Tools require seven tools and the ordinary extension lifecycle; the prototype includes three standard tools while retaining the Agent and Tool Runtime requirements for registration, progress, results, and cancellation.
- An incompatible extension, extension startup failure, or extension crash shall leave the Glyph host and every other extension usable, mark only the failed extension unavailable, and report which condition occurred; an active tool call owned by that extension shall fail, and the host shall not restart the extension automatically.
  - **Main PRD:** `Differs` — Environment Reload defines host survival and extension unavailability until Glyph restarts after a runtime crash; the prototype adds independent failure isolation for every discovered extension and applies the same outcome to incompatibility and startup failure.
- Tool names across successfully started extensions shall be globally unique. When two or more extensions register the same tool name, the Glyph host shall mark every extension registering that name unavailable, register no tools from those extensions, report the conflicting tool name and plugin IDs, and keep every non-conflicting extension available.
  - **Main PRD:** `No direct match` — the target PRD does not define tool-name collisions across extensions.
- Every extension shall be trusted, run with the operating-system permissions of Glyph, and execute tools without sandboxing, project-trust checks, or confirmation.
  - **Main PRD:** `Matches` — Extensions and Glyph Clients trust installed extensions, Non-Scope excludes sandbox and project-trust policy, and Bundled Standard Tools execute without agent-core confirmation.
- The directory from which Glyph starts shall be the working project, and relative tool paths shall resolve from it.
  - **Main PRD:** `No direct match` — the target PRD does not define project selection or relative-path resolution.
- `read` shall return the complete contents of a requested text file or an explicit error.
  - **Main PRD:** `No direct match` — Bundled Standard Tools names `read` but does not define its prototype operation.
- `edit` shall replace an exact text fragment only when that fragment occurs once; otherwise it shall return an explicit error without changing the file.
  - **Main PRD:** `No direct match` — Bundled Standard Tools names `edit` but does not define its prototype operation.
- `bash` shall execute one command in the working project, stream `stdout` and `stderr` as they are produced, and return `stdout`, `stderr`, and the exit code when the command completes.
  - **Main PRD:** `No direct match` — Bundled Standard Tools names `bash`, while Agent and Tool Runtime requires tool progress; the target PRD does not define this command or result shape.
- Cancelling `bash` shall stop its active command.
  - **Main PRD:** `Matches` — Agent and Tool Runtime requires an extension tool to respond to cancellation of the active agent run.

### Agent Core

- For each user request, the agent core shall continue the agent loop until the model returns a final response without tool calls or the user stops the agent run.
  - **Main PRD:** `Matches` — Agent and Tool Runtime requires the coding task flow in both headless and standard-TUI operation, and the `agent run` definition ends a run when no automatic model or tool work remains or the run is stopped.
- When one model response requests several tools, the agent core shall execute them sequentially in model-provided order.
  - **Main PRD:** `No direct match` — the target PRD does not select sequential or parallel execution for a model-provided tool batch.
- A completed tool result shall be returned to the model before the next model request.
  - **Main PRD:** `No direct match` — Agent and Tool Runtime requires model-callable tools but does not explicitly define this request ordering.
- A tool execution error shall become a model-visible error result, after which the agent loop shall continue.
  - **Main PRD:** `Matches` — Agent and Tool Runtime contains the same tool-error behavior.
- Model output shall be available incrementally as the provider produces it.
  - **Main PRD:** `Matches` — Agent and Tool Runtime contains the same streaming requirement.
- Tool progress shall be available while a tool runs, including streamed `bash` output from the extension.
  - **Main PRD:** `Differs` — Agent and Tool Runtime requires tool progress generally; the prototype specifically uses streamed `bash` output as tool progress.
- Every model request shall receive the complete in-memory session history, including user messages, model messages, tool calls, and tool results.
  - **Main PRD:** `Differs` — Context and Sessions require persistent tree-structured sessions and context compaction; the prototype uses one complete in-memory linear history.
- The prototype shall not compact or truncate context; provider rejection caused by context size shall become an explicit error.
  - **Main PRD:** `Differs` — Context and Sessions require automatic compaction before the response budget no longer fits; the prototype reports the overflow instead.
- A static system instruction shall be supplied by the prototype's coding-agent configuration and passed to the agent core; the agent core shall not define that instruction.
  - **Main PRD:** `Differs` — Bundled Resource Processing supplies resolved instructions through a replaceable resource extension; the prototype uses static configuration while preserving the target rule that the agent core receives resolved instructions.
- Only one agent run shall be active in a Glyph process; queued messages and parallel runs shall be unavailable.
  - **Main PRD:** `Differs` — Programmatic Control includes queued steering and follow-up messages and queue modes; the prototype permits one active run without queues.
- Stopping an agent run shall cancel the active model request or tool, skip all remaining tool calls, prevent another automatic model request, and return the agent to idle.
  - **Main PRD:** `Differs` — Agent and Tool Runtime requires cancellation of active model and tool work and transition to idle; the prototype additionally defines the disposition of remaining sequential tool calls.
- Messages and tool results completed before cancellation shall remain in the in-memory session.
  - **Main PRD:** `No direct match` — the target PRD requires stopping to return the agent to idle but does not define retention of partially completed run history.

### Standard TUI

- The standard TUI shall provide text input for multiple user turns in one in-memory session.
  - **Main PRD:** `Differs` — the target Standard TUI continues conversations through persistent tree-structured sessions; the prototype provides only a process-local linear session.
- The standard TUI shall render model output and tool progress incrementally.
  - **Main PRD:** `Matches` — Terminal Agent contains the same rendering requirement.
- While an agent run is active, the standard TUI shall not accept another user request.
  - **Main PRD:** `No direct match` — Programmatic Control defines queued messages, but the target PRD does not define standard-TUI input behavior during an active run.
- The author shall be able to stop the active agent run through the standard TUI.
  - **Main PRD:** `Matches` — Terminal Agent contains the same stop requirement.
- After an agent run reaches idle, the standard TUI shall accept another request using the retained in-memory history.
  - **Main PRD:** `Differs` — the target product persists and resumes sessions; the prototype retains history only until process exit.
- When OAuth or a model request fails, the standard TUI shall show the error text, preserve the in-memory history, return to a state from which the failed operation can be retried, and not trigger an automatic retry.
  - **Main PRD:** `Differs` — the target product automatically retries configured transient provider failures in Agent Core, while the prototype ends every failed request without automatic retry.

### Headless Operation

- Headless operation shall invoke the Glyph host and agent core without loading a UI plugin.
  - **Main PRD:** `Matches` — Platform Requirements keep the agent core UI-free and require the Glyph host to run headlessly without a UI plugin.
- Headless operation and the standard TUI shall use one model configuration, credential store, and installed extension set.
  - **Main PRD:** `No direct match` — the target PRD places both modes behind the Glyph host but does not explicitly require these three inputs to be shared.
- A headless invocation shall accept one text request, run one agent loop, and emit a human-readable stream containing model output, tool start and completion, tool progress, and a final error when one occurs.
  - **Main PRD:** `Differs` — Programmatic Control requires a transport-independent correlated command and event contract exposed through bidirectional gRPC on a Unix socket; the prototype provides a one-shot human-readable command with no stable output schema.
- The headless agent shall execute `read`, `edit`, and `bash` itself.
  - **Main PRD:** `Differs` — Programmatic Control requires a headless agent to execute all tools available to it; the prototype makes only `read`, `edit`, and `bash` available.
- `Ctrl+C` during a headless invocation shall cancel the active model request or tool and terminate the invocation with a nonzero exit code.
  - **Main PRD:** `No direct match` — Programmatic Control includes abort but does not define terminal signal handling or process exit codes.
- When headless operation reports an error, it shall print the error text and terminate with a nonzero exit code without an automatic retry.
  - **Main PRD:** `Differs` — the target product automatically retries configured transient provider failures before publishing its terminal operation outcome.

## Open Questions

None.

## Technical Supplement

### Requirement Traceability

- `Matches`: The prototype requirement has the same observable behavior as the cited target requirement.
- `Differs`: The prototype requirement is related to a target requirement but narrows, extends, or otherwise changes its observable behavior; the traceability line states the exact difference.
- `No direct match`: The target PRD has no requirement for the prototype-specific behavior; the traceability line states what the target leaves undefined.

Architecture, API shapes, extension transport, configuration formats, credential-file location, and implementation planning remain deferred to technical design. Component ownership and dependency directions are fixed by the requirements above; internal APIs may evolve.

## References

- `docs/specs/features/initial/prd.md`
- `docs/terms.md`
- `docs/artefacts/go-extension-feasibility.md`
