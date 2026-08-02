# Idea: Glyph Minimal Prototype

## Definitions

- `Glyph`: The project name for the independent Go agent platform being defined.
- `Glyph host`: The platform layer that manages extension runtimes and connects them to the agent core and attached interfaces without owning interface-specific behavior.
- `agent core`: The required part of an agent platform that provides runtime behavior shared by its agents.
- `agent loop`: The repeated sequence of requesting a model response, executing model-requested actions, and returning their results to the model until the run completes or is stopped.
- `agent run`: One continuous agent-loop execution initiated by a message and ending when no automatic model or tool work remains or the run is stopped.
- `coding agent`: An agent intended to work with source code and related software development tasks.
- `tool`: A typed operation that an agent exposes to a model by name.
- `extension`: A component that adds or changes platform behavior through extension contracts without modifying the agent core source code.
- `extension runtime`: One loaded execution environment for an extension and its in-memory state.
- `headless agent`: A Glyph agent instance controlled programmatically without a terminal user interface.
- `model provider`: A local or remote system through which an agent accesses a language model.
- `session`: A related sequence of user requests, model responses, tool calls, and agent state.
- `standard TUI`: The terminal interface distributed with Glyph; it depends on the Glyph host and agent core and owns terminal-specific rendering, input, and extension capabilities.

## Context and Problem

The target product requirements in `docs/specs/features/initial/prd.md` cover the complete Glyph platform. Implementing that entire scope before running an end-to-end agent would delay validation of the product flow and the boundaries among the Glyph host, agent core, interfaces, providers, and extension runtimes.

The project author needs a runnable vertical slice that demonstrates a useful coding-agent task while preserving the target ownership and dependency boundaries. Internal APIs may evolve after the prototype.

## Goal

Deliver a minimal Glyph prototype that runs the same agent core through a basic standard TUI and without a TUI, uses OpenAI Codex, and executes coding tools supplied by a runtime-loaded Go extension.

## Scenarios

- The author starts the standard TUI, completes OpenAI Codex OAuth when required, submits a coding task, observes streamed model and tool output, and continues the conversation after the agent becomes idle.
- The agent reads and changes a file, runs a project command, and reports the result.
- The author starts a headless agent with one text request. It uses the same model configuration, credentials, extension, and agent-tool loop as the standard TUI.
- The author stops an active model request or tool and can continue using the standard TUI.
- An incompatible or failed extension becomes unavailable without terminating the Glyph host.

## Scope and Non-Scope

### Scope

- A Go Glyph host, TUI-free agent core, basic standard TUI, and one-shot headless operation.
- OpenAI Codex with interactive OAuth and one configured model.
- One runtime-loaded Go extension that provides `read`, `edit`, and `bash`.
- One in-memory linear session and one active agent run.
- macOS on arm64.
- Source-based build and execution.

### Non-Scope

- Persistent or tree-structured sessions and context compaction.
- OpenAI-compatible providers, model selection, model cycling, and reasoning-level configuration.
- A stable programmatic control protocol, structured headless output, message queues, concurrent agent runs, and automatic retries.
- Extension installation commands, enablement, disablement, updates, environment reload, lifecycle events, resource contributions, and TUI-specific extension capabilities.
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
- The agent core shall run without loading or depending on the standard TUI.
  - **Main PRD:** `Matches` — Platform Requirements contain the same dependency constraint.
- The standard TUI shall depend on the Glyph host and agent core contracts; neither the Glyph host nor the agent core shall depend on the standard TUI.
  - **Main PRD:** `Matches` — Platform Requirements define the same dependency direction.
- The prototype shall be buildable and runnable from its source repository through documented commands, with the Glyph application and extension executable built separately.
  - **Main PRD:** `No direct match` — the target PRD requires open-source distribution but does not define prototype build commands or separate build outputs.

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
  - **Main PRD:** `Differs` — the target PRD permits provider interaction through any attached interface; the prototype restricts initial OAuth to the standard TUI.
- One model identifier shall be configurable and shared by standard-TUI and headless operation; model selection and model switching shall be unavailable.
  - **Main PRD:** `Differs` — Model Runtime and Standard TUI Requirements require model changes and user-facing model selection; the prototype uses one configured model.
- The prototype shall use the configured model's default reasoning level without exposing reasoning configuration or switching.
  - **Main PRD:** `Differs` — Programmatic Control covers reasoning selection; the prototype provides no reasoning control.
- Persisted OAuth tokens shall exist only in the user-only credential file and shall not appear in configuration, terminal output, logs, model context, or tool parameters.
  - **Main PRD:** `Differs` — Model Providers and Authentication prohibit secret values in provider configuration and restrict credential-file access; the prototype additionally prohibits token exposure through terminal output, logs, model context, and tool parameters.

### Extension Runtime and Tools

- The author shall be able to place one separately built extension executable in the extension directory, after which the Glyph host shall discover it at the next application start without rebuilding Glyph.
  - **Main PRD:** `Differs` — Extensions and Interfaces require installation, enablement, disablement, and updates without rebuilding Glyph; the prototype uses manual file placement and startup discovery for one extension.
- The Glyph host shall start the discovered extension with the application and stop it when the application exits.
  - **Main PRD:** `Differs` — the target extension lifecycle also covers enablement, disablement, updates, environment reload, and post-failure unavailability; the prototype supports only application startup and shutdown.
- The extension contract shall support registration and execution of `read`, `edit`, and `bash`, final results, streamed progress, and cancellation.
  - **Main PRD:** `Differs` — Bundled Standard Tools require seven tools and the ordinary extension lifecycle; the prototype includes three tools while retaining the Agent and Tool Runtime requirements for registration, progress, results, and cancellation.
- An incompatible extension, extension startup failure, or extension crash shall leave the Glyph host usable, mark the extension unavailable, and report which condition occurred; an active tool call shall fail, and the host shall not restart the extension automatically.
  - **Main PRD:** `Differs` — Environment Reload defines host survival and extension unavailability until Glyph restarts after a runtime crash; the prototype applies the same outcome to incompatibility and startup failure.
- The extension shall be trusted, run with the operating-system permissions of Glyph, and execute tools without sandboxing, project-trust checks, or confirmation.
  - **Main PRD:** `Matches` — Extensions and Interfaces trust installed extensions, Non-Scope excludes sandbox and project-trust policy, and Bundled Standard Tools execute without agent-core confirmation.
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
  - **Main PRD:** `No direct match` — the target PRD defines errors for specific operations but does not define this common prototype recovery state.

### Headless Operation

- Headless operation shall invoke the Glyph host and agent core through the contracts used by the standard TUI.
  - **Main PRD:** `Matches` — Platform Requirements keep the agent core TUI-free, and Programmatic Control requires a headless agent in addition to the standard TUI.
- Headless operation and the standard TUI shall use one model configuration, credential store, and installed extension set.
  - **Main PRD:** `No direct match` — the target PRD places both modes behind the Glyph host but does not explicitly require these three inputs to be shared.
- A headless invocation shall accept one text request, run one agent loop, and emit a human-readable stream containing model output, tool start and completion, tool progress, and a final error when one occurs.
  - **Main PRD:** `Differs` — Programmatic Control requires a transport-neutral correlated command and event contract; the prototype provides a one-shot human-readable command with no stable output schema.
- The headless agent shall execute `read`, `edit`, and `bash` itself.
  - **Main PRD:** `Differs` — Programmatic Control requires a headless agent to execute all tools available to it; the prototype makes only `read`, `edit`, and `bash` available.
- `Ctrl+C` during a headless invocation shall cancel the active model request or tool and terminate the invocation with a nonzero exit code.
  - **Main PRD:** `No direct match` — Programmatic Control includes abort but does not define terminal signal handling or process exit codes.
- When headless operation reports an error, it shall print the error text and terminate with a nonzero exit code without an automatic retry.
  - **Main PRD:** `No direct match` — the target PRD defines command acceptance and later execution outcomes but not this one-shot command behavior.

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
