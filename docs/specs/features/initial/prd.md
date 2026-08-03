# Idea: Glyph Agent Platform

## Definitions

- `Glyph`: The project name for the independent Go agent platform being defined.
- `Glyph host`: The platform layer that manages extension runtimes and connects them to the agent core and Glyph clients without owning client-specific behavior.
- `agent core`: The required part of an agent platform that provides runtime behavior shared by its agents.
- `Glyph plugin`: A separately delivered Glyph component. The defined Glyph plugin kinds are extension and UI plugin.
- `extension`: A Glyph plugin that contributes platform or agent capabilities through extension contracts.
- `UI plugin`: A Glyph plugin that presents Glyph to a person and communicates with Glyph as a Glyph client.
- `extension catalog`: The collection of extensions available to a Glyph host.
- `UI catalog`: The collection of discovered UI plugins considered by a Glyph host during UI selection.
- `compatible extension`: An extension that conforms to the contracts exposed by the running Glyph version.
- `bundled extension`: A compatible extension distributed and enabled by default with Glyph while retaining the ordinary extension lifecycle.
- `bundled tools extension`: The bundled extension that registers `read`, `write`, `edit`, `bash`, `grep`, `find`, and `ls` for the standard coding agent.
- `bundled resource extension`: The bundled extension that converts collected resource contributions into system instructions and model context and makes prompt templates available through Glyph clients.
- `extension contract`: A documented operation, data type, event, or registration point through which an extension interacts with Glyph.
- `extension point`: A documented boundary at which an extension handler can observe, block, modify, or replace an operation.
- `extension runtime`: One loaded execution environment for an extension and its in-memory state.
- `extension context`: Host-provided access to one extension runtime and its active session.
- `agent run`: One continuous agent-loop execution initiated by a message and ending when no automatic model or tool work remains or the run is stopped.
- `standard coding agent`: The coding-agent configuration distributed with Glyph; it enables the bundled tools extension and bundled resource extension and can run headlessly or through the standard TUI.
- `context`: The information sent to a model to produce its next response or tool request.
- `context compaction`: Replacement of an older context prefix with a summary while preserving the remaining context suffix.
- `session`: A related sequence of user requests, model responses, tool calls, and agent state.
- `session tree`: A session structure whose entries form parent-child branches and have one active leaf.
- `active leaf`: The session-tree entry from which subsequent entries continue.
- `UI`: A presentation and input surface through which a person interacts with Glyph.
- `terminal UI`: A UI presented inside a terminal.
- `standard TUI`: The terminal UI plugin distributed with Glyph; it owns terminal-specific rendering, input, and extension capabilities.
- `headless agent`: A Glyph agent instance controlled programmatically without a UI.
- `Glyph client`: A component connected to a Glyph host that sends commands and receives events. A Glyph client is either a UI plugin or a programmatic controller.
- `programmatic controller`: A Glyph client that controls a headless agent without presenting a UI.
- `programmatic control contract`: A transport-independent contract for correlated commands, acceptance responses, asynchronous execution events, interaction requests, and notifications.
- `queue mode`: A setting with values `all` and `one-at-a-time` that controls delivery of queued `steer` and `followUp` messages.
- `steer`: A queued message intended to influence an active agent run.
- `followUp`: A queued message intended for delivery after an active agent run.
- `nextTurn`: A queued message intended for the next user turn.
- `terminate`: A tool-result signal that controls whether the agent performs another automatic model request.
- `interaction request`: A request from an extension through the Glyph host to a Glyph client that expects a result.
- `notification`: Information sent by an extension through the Glyph host to a Glyph client without expecting a user response; the host reports delivery success or an error.
- `credential source`: The name of an environment variable or local credential-file entry from which Glyph reads an API key; it is not the secret itself.
- `reasoning level`: A configured setting for model reasoning effort, limited by the selected model's capabilities.
- `Go interface`: A Go language type that defines a method set; this term does not refer to a UI plugin, Glyph client, or extension.
- `skill`: A reusable instruction resource contributed by an extension.
- `prompt template`: A reusable user-request template contributed by an extension.
- `context file`: A file containing project instructions contributed by an extension.
- `resource contribution`: A typed skill, prompt template, or context file supplied by an active extension to the Glyph host.
- `response budget`: The token capacity reserved for the next model response.
- `reference scenario`: Behavior from an existing system that is used to evaluate Glyph requirements or extension contracts without requiring source compatibility with that system.

## Context and Problem

The project owner maintains `pi-agent-suite`, a set of extensions for Pi. Pi demonstrates that a small agent core can support extensive customization, but `pi-agent-suite` cannot change behavior outside Pi's extension contracts and requires a TypeScript-based platform.

Glyph provides an independently owned Go platform whose host, agent core, Glyph clients, and extension contracts can evolve without depending on Pi.

## Goal

Deliver an independent Go agent platform with a UI-free agent core, a plugin-managing host, a standard TUI, and programmatic control.

## Scenarios

- A programmatic controller runs and controls a headless Glyph agent without loading the standard TUI.
- A user gives the standard terminal coding agent a task. The agent inspects the project, invokes tools, changes files, runs commands, reports the result, and continues the conversation.
- A user authenticates with OpenAI Codex or configures an OpenAI-compatible API, selects a model, and changes the model without leaving the session.
- A user resumes a saved session, navigates its tree, and continues from an earlier point without deleting another branch.
- A Go developer installs a compatible extension without rebuilding Glyph. Its core capabilities work headlessly, and its terminal capabilities activate only with the standard TUI.
- An extension requests interaction or sends a notification through the Glyph host to a Glyph client.
- A user reloads the Glyph environment without ending the active session.

## Scope and Non-Scope

### Scope

- Glyph host, agent core, locally managed UI plugins including the standard TUI, and programmatic control.
- OpenAI Codex and a user-configured OpenAI-compatible API.
- Bundled standard coding tools.
- Runtime-installable Go extensions.
- A UI catalog separate from the extension catalog.
- Persistent tree-structured sessions and context compaction.
- macOS and Linux support.
- Open-source distribution under the MIT license.

### Non-Scope

- Source, API, configuration, session, or extension compatibility with Pi.
- Porting or implementing the existing `pi-agent-suite` extensions.
- Built-in subagents, workflows, MCP support, or specialized context-compaction behavior. These capabilities remain implementable through extensions.
- First-class parent-agent, child-agent, advisor, council, or workflow semantics in the agent core.
- A universal extension renderer shared by the standard TUI and future UI plugins.
- Remote or independently started UI plugins.
- Windows support.
- Sandboxing, project trust, or another security policy for trusted extensions.
- Direct user-entered shell syntax in the standard TUI outside the bundled `bash` tool.
- Extension-defined startup arguments and command argument completion.
- Technical architecture, API shapes, transport selection, and implementation planning.

## Requirements

### Platform Requirements

- Glyph production code shall be written in Go.
- Glyph shall be published under the MIT license.
- Glyph shall support macOS and Linux.
- Glyph shall use platform-independent Go facilities instead of operating-system-specific facilities when they provide equivalent behavior.
- The agent core shall run without loading or depending on a UI plugin.
- The Glyph host shall run in headless mode without loading a UI plugin.
- No UI plugin implementation, including the standard TUI, shall own agent-core behavior or be required by the agent core.
- The agent core shall remain minimal and shall not define a specific agent workflow.
- Glyph shall not depend on Pi or provide compatibility with Pi contracts or persisted formats.

### Glyph Host Requirements

#### Extensions and Glyph Clients

- The Glyph host shall own extension installation and runtime lifecycle.
- A compatible extension shall be installable, and the user shall be able to enable, disable, and update it without rebuilding Glyph.
- Installed extensions shall be trusted and shall run with the operating-system permissions of Glyph.
- An extension shall load without the standard TUI. Its non-terminal capabilities shall remain active, while terminal capabilities shall be unavailable.
- Attempting to use an unavailable terminal capability shall return an explicit error.
- An extension shall be able to request interaction through a Glyph client without prescribing how that client presents the request.
- When no Glyph client is connected, an interaction request shall fail explicitly; the host shall not fabricate a successful result.
- An extension shall be able to send a notification through the host to a Glyph client.
- The receiving Glyph client shall choose how to present a notification. A programmatic controller shall receive it as an event.
- Notification delivery shall succeed when the host transfers the notification to the Glyph client and shall report an error if transfer fails; success shall not require presentation, human observation, or a user response.
- Notification delivery without a connected Glyph client shall fail explicitly.
- Active extensions shall be able to exchange events that are not persisted.
- The Glyph host shall provide one typed resource-contribution contract through which active extensions contribute skills, prompt templates, and context files.
- At startup and environment reload, the host shall request resource contributions from active extensions and replace each extension's previously collected contributions.
- Extensions shall be able to register commands that Glyph clients can discover and invoke; user-facing command syntax shall belong to the receiving client.
- Glyph public extension contracts shall map every extension entry point declared in `pi-package/package.json` at `https://github.com/n-r-w/pi-agent-suite` to at least one contract without requiring an agent-core change or suite-specific core behavior.

#### UI Plugins

- The Glyph host shall maintain a UI catalog separate from the extension catalog.
- The UI catalog shall contain locally available UI plugin executables discovered before Glyph selects a UI plugin.
- The standard TUI shall be distributed as a UI plugin and shall be present in the UI catalog by default.
- Glyph startup shall either enable headless mode or select one UI plugin.
- Headless mode shall not start a UI plugin. Supplying a UI selection together with headless mode shall fail startup explicitly.
- Before automatic UI selection, the Glyph host shall exclude each UI plugin it identifies as unavailable or incompatible with the running Glyph version and shall report a warning for each exclusion.
- When headless mode is not enabled, the Glyph host shall select a UI plugin in this order: an explicit startup selection, the active UI setting, or the only UI plugin remaining after automatic exclusions.
- An explicit startup selection or active UI setting that identifies a UI plugin that is unavailable or incompatible with the running Glyph version, or whose UI plugin cannot start, shall fail startup without selecting another UI plugin.
- When neither an explicit startup selection nor an active UI setting exists, having no UI plugin or more than one UI plugin remaining after automatic exclusions shall fail startup explicitly.
- The Glyph host shall start the selected UI plugin and own its lifecycle until the UI plugin or the Glyph host exits.
- One Glyph host process shall use at most one UI plugin for its entire lifetime; another UI plugin cannot attach or replace it.
- The UI catalog and selected UI plugin shall remain unchanged for the lifetime of the Glyph host process. Changes to the UI catalog or active UI setting shall take effect at the next Glyph start.
- When the active UI plugin exits for any reason, the Glyph host shall cancel the active agent run and terminate.

#### Bundled Standard Tools

- Glyph shall distribute a bundled extension that registers `read`, `write`, `edit`, `bash`, `grep`, `find`, and `ls`.
- The bundled tools extension shall be enabled by default for the standard coding agent.
- The user shall be able to disable, update, and replace the bundled tools extension under the ordinary extension lifecycle.
- Tools from the bundled extension shall execute without confirmation from the agent core.

#### Bundled Resource Processing

- Glyph shall distribute a bundled resource extension enabled by default for the standard coding agent.
- The user shall be able to disable, update, and replace the bundled resource extension under the ordinary extension lifecycle.
- The Glyph host shall supply collected resource contributions to the bundled resource extension.
- The bundled resource extension shall convert skills and context files into system instructions and model context and make prompt templates available through Glyph clients.
- The agent core shall receive only resolved instructions and context and shall not depend on the resource types.

#### Model Providers and Authentication

- Glyph shall provide an OpenAI Codex provider and a user-configured OpenAI-compatible provider.
- Each provider shall own its authentication behavior, including API-key resolution, OAuth login, token refresh, and conversion of credentials into request authorization.
- The Glyph host shall provide provider implementations with generic credential storage and interaction through a Glyph client.
- Host-provided credential storage shall persist credentials in the local credential file.
- The Glyph host shall allow an extension to register and unregister a provider implementation, including its authentication, model catalogue, and streaming behavior.
- The local credential file shall be accessible only to the user running Glyph.
- The OpenAI Codex provider shall use interactive OAuth authentication and persist its OAuth credentials through host-provided credential storage.
- The OpenAI-compatible provider shall be configured with a base URL, an explicit model list, and an optional credential source.
- The OpenAI-compatible provider shall support OpenAI Chat Completions and OpenAI Responses.
- Provider configuration shall select the OpenAI wire API through an explicit `api` field, and a model configuration shall be able to override it.
- A provider configuration without a credential source shall use no authorization.
- Provider configuration shall not contain secret values.
- Selecting a model whose configured credential source cannot be resolved shall fail immediately, preserve the active model, and produce an error.

#### Programmatic Control

- The Glyph host shall support a programmatically controlled headless agent in addition to the standard TUI.
- Each programmatic command shall carry a correlation identifier.
- The host shall accept or reject a programmatic command independently from its later execution outcome.
- Asynchronous events caused by an accepted command shall be attributable to the controlled operation.
- The programmatic control contract shall not depend on the standard TUI or a selected transport technology.
- Programmatic control shall cover user requests, queued steering and follow-up messages, abort, state and message queries, model and reasoning selection, queue modes, compaction, retry control, programmatic shell execution, session statistics, session creation and switching, forking, cloning, tree navigation, session entries and naming, command discovery, execution events, interaction requests, and notifications.
- Queue mode `all` shall deliver every queued `steer` or `followUp` message at its defined delivery point; `one-at-a-time` shall deliver one queued message at each respective point.
- A headless agent shall execute its available tools itself.

#### Environment Reload

- The Glyph host shall reinitialize the Glyph environment without ending the active session.
- Environment reload shall run only while no agent run or context compaction is active; a busy request shall be rejected with a warning.
- Reload shall cover host settings other than the active UI setting, provider registrations, extension runtimes, and resource contributions.
- Reload shall preserve the active session and its history.
- Reload shall discard extension state held only in memory and preserve extension state stored in the session or files.
- When the reinitialized environment fails to load, Glyph shall preserve the session, report the error, require application restart, and not restore the previous environment.
- After reload or session replacement, the previous extension context shall become invalid and calls through it shall fail.
- Events and commands after reload or session replacement shall receive a new extension context bound to the active extension runtime and session.
- An extension-runtime crash shall end the operation that invoked it with an error while Glyph remains usable; the failed extension shall remain unavailable until Glyph restarts.

### Agent Core Requirements

#### Agent and Tool Runtime

- The agent core shall support the task flow described in the second scenario through both headless and standard-TUI operation.
- Model output shall be available incrementally as it is produced.
- Tool execution progress shall be available while the tool runs.
- Stopping an active agent run shall cancel in-progress model and tool work and transition the agent to idle.
- Extensions shall be able to register model-callable tools, inspect registered and active tools, and change the active set for subsequent model requests.
- An extension tool shall be able to report progress and respond to cancellation of the active agent run.
- Extensions shall be able to intercept a tool call before execution and allow it, reject it, or change its input.
- Extensions shall be able to change a tool result before it is returned to the model.
- An extension shall be able to replace a registered tool by registering the same tool name.
- A tool execution error shall become a model-visible error result, after which the agent shall continue.

#### Model Runtime

- The agent core shall execute model requests through configured providers without implementing provider-specific authentication.
- Changing the model shall preserve session history and affect subsequent model requests.

#### Core Extension Capabilities

- Agent-core extension contracts shall support tools, lifecycle events, system-prompt changes, context transformations, sessions, and model access without requiring terminal capabilities.
- Each extension point shall declare whether handlers can observe, block, modify, or replace the affected operation.
- Multiple transformations of the same operation shall run sequentially, with each handler receiving the result returned by the preceding handler.
- After model context assembly and before every provider request, the agent core shall invoke active context transformations sequentially.
- A context transformation shall affect only the outbound context for that provider request; persisted session state shall change only when an extension separately adds a session entry through the session contract.
- Extensions shall be able to observe, transform, or fully handle user text and images before agent processing.
- An extension shall be able to initiate and cancel session creation, resumption, forking, cloning, and tree navigation.
- After session replacement, the extension shall receive a context bound to the replacement session.
- An extension shall be able to persist model-hidden entries and model-visible messages and associate both with the active session branch.
- Extensions shall be able to inspect configured models and providers and make model requests through them for extension-owned behavior.
- An extension shall be able to change provider request headers, replace the serialized provider request, and observe provider response status and headers.
- Extensions shall receive events for agent start, agent end, agent settled, turn start, turn end, message start, message update, message end, tool-execution start, tool-execution update, tool-execution end, model selection, and reasoning-level selection.
- An extension shall be able to replace a finalized message without changing its role; multiple replacements shall run sequentially.
- Except for a pre-execution tool-handler error, an ordinary extension-handler error shall be reported while later handlers and the agent-core operation associated with that extension point continue and the extension remains active.
- A pre-execution tool-handler error shall be reported, block that tool, and leave the extension active.

#### Run Control

- An extension shall be able to stop the active agent run and request context compaction.
- An extension shall be able to send a message as `steer`, `followUp`, or `nextTurn`.
- A `steer` message shall be delivered before the next model request after the active tools finish.
- A `followUp` message shall be delivered after the active agent run finishes.
- A `nextTurn` message shall be delivered with the next user turn.
- An extension tool shall be able to return `terminate`.
- The agent core shall skip the next automatic model request only when every completed result in the active tool batch contains `terminate`.
- `terminate` shall not interrupt another tool already running in the same batch.

#### Context and Sessions

- The agent core shall compact context automatically when the remaining model context cannot accommodate the response budget.
- Manual compaction shall accept user instructions for the summary.
- Context compaction shall replace an older context prefix with a summary and preserve the remaining suffix unchanged.
- Extensions shall be able to replace the default compaction behavior.
- Glyph shall automatically save sessions and allow them to resume after application restart.
- Session entries shall form a tree with parent-child relationships and one active leaf.
- Continuing from an earlier entry shall create a new branch without deleting existing branches.
- The session model shall support creation, resumption, tree navigation, forking, cloning, naming, information retrieval, branch summaries, and entry labels.
- Branch navigation shall support no summary, a default summary, or a summary with custom focus, and shall attach a created summary at the selected position.

### Standard TUI Requirements

#### Terminal Agent

- The standard TUI shall provide a terminal UI for the standard coding agent without owning agent-core behavior.
- The standard TUI shall own terminal input dispatch, terminal rendering, and editor behavior.
- The standard TUI shall render model output incrementally and keep tool progress visible while a tool runs.
- The user shall be able to stop the active run through the standard TUI.
- The standard TUI shall expose model selection, model cycling, environment reload, context compaction, and session operations through user-invokable actions.
- Every TUI key binding shall be user-configurable.
- Standard command names and the initial keybinding baseline are recorded in `docs/specs/features/initial/tui-defaults.md`.
- A keybinding-baseline entry shall apply only when Glyph independently supports the corresponding action; it shall not add that action to product scope.
- When the Glyph host performs environment reload, an active standard TUI shall reload its themes and key bindings.

#### Session Interaction

- The standard TUI shall present the complete session tree and preserve all existing branches when the user continues from an earlier position.
- Selecting a user message or model-visible extension message shall move the active leaf to its parent and place its text in the editor for resubmission.
- Selecting the root user message shall move the active leaf to an empty conversation and place the root prompt in the editor.
- Selecting another entry shall move the active leaf to that entry and leave the editor empty.
- When leaving a branch, the standard TUI shall offer the three branch-summary choices supported by the session model.
- The standard TUI shall allow users to set and clear labels on session-tree entries.

#### TUI Extension Capabilities

- The standard TUI shall expose terminal-specific extension capabilities only while it is the active UI plugin.
- An extension shall determine its content and internal layout within the area supplied by the standard TUI.
- The standard TUI shall retain control of terminal input, the render loop, focus, and placement of the extension area or overlay.
- Extensions shall be able to provide custom and overlay content, statuses, working indicators, widgets, headers, footers, terminal title, and hidden-reasoning labels.
- Extensions shall be able to register terminal renderers for tool calls, tool results, custom messages, and custom session entries.
- Extensions shall be able to inspect and change tool-result expansion, receive forwarded terminal input, and integrate with the active editor.
- Editor integration shall support reading, replacing, and inserting text, contributing autocomplete, and replacing the editor component.
- Extensions shall be able to contribute, enumerate, and switch themes.
- Extensions shall be able to register TUI-specific shortcuts whose bindings remain user-configurable.
- The standard TUI shall present notifications delivered by the Glyph host.
- A TUI-specific renderer shall not be required to work in another UI plugin. Future UI plugins may expose their own extension-rendering contracts.

## Open Questions

None.

## Technical Supplement

No technical solution is selected in this document. The mechanism connecting the Glyph host, agent core, Glyph clients, and extension runtimes is deferred to technical design. Runtime extension feasibility is analyzed in `docs/artefacts/go-extension-feasibility.md`.

### Reference Scenario Coverage

This matrix provides traceability for the 20 current `pi-agent-suite` entry points. It maps each scenario to existing generic Glyph requirements and does not add product behavior or suite-specific core concepts.

| `pi-agent-suite` entry point | Existing Glyph requirement coverage |
|---|---|
| `extensions/system-prompt/index.ts` | Core Extension Capabilities: system-prompt changes and lifecycle events |
| `extensions/project-rules/index.ts` | Core Extension Capabilities: system-prompt changes |
| `extensions/mcp-wrapper/index.ts` | Agent and Tool Runtime: tool registration; Extensions and Glyph Clients: commands |
| `extensions/enable-tools/index.ts` | Agent and Tool Runtime: registered and active tool inspection and active-set changes |
| `extensions/footer/index.ts` | TUI Extension Capabilities: footers; Core Extension Capabilities: model and lifecycle events |
| `extensions/codex-fast/index.ts` | Core Extension Capabilities: provider-request middleware; Extensions and Glyph Clients: commands |
| `extensions/codex-verbosity/index.ts` | Core Extension Capabilities: provider-request middleware |
| `extensions/codex-quota/index.ts` | Core Extension Capabilities: configured-provider access; TUI Extension Capabilities: statuses |
| `extensions/custom-compaction/index.ts` | Context and Sessions: compaction replacement; TUI Extension Capabilities: session-entry renderers |
| `extensions/context-projection/index.ts` | Core Extension Capabilities: per-request context transformations and branch-aware session entries |
| `extensions/mermaid/index.ts` | TUI Extension Capabilities: custom session-entry renderers; Core Extension Capabilities: model-visible session messages |
| `extensions/completion-sound/index.ts` | Core Extension Capabilities: lifecycle events; Extensions and Glyph Clients: notifications |
| `extensions/cmux/index.ts` | Core Extension Capabilities: lifecycle and tool-result events; Extensions and Glyph Clients: notifications |
| `extensions/main-agent-selection/index.ts` | Extensions and Glyph Clients: commands; TUI Extension Capabilities: custom content |
| `extensions/run-subagent/index.ts` | Agent and Tool Runtime: tool registration; Core Extension Capabilities: model access and session persistence; TUI Extension Capabilities: renderers and widgets |
| `extensions/workflow/index.ts` | Agent and Tool Runtime: tool registration; Core Extension Capabilities: context transformations and session entries; TUI Extension Capabilities: widgets |
| `extensions/structured-prompt/index.ts` | Extensions and Glyph Clients: commands and interaction requests; TUI Extension Capabilities: custom content and editor integration |
| `extensions/ask-llm/index.ts` | Extensions and Glyph Clients: commands; Core Extension Capabilities: configured-model requests; TUI Extension Capabilities: custom content |
| `extensions/consult-advisor/index.ts` | Agent and Tool Runtime: tool registration; Core Extension Capabilities: configured-model requests and session entries |
| `extensions/convene-council/index.ts` | Agent and Tool Runtime: tool registration; Core Extension Capabilities: configured-model requests and session entries |

## References

- `docs/specs/features/initial/problem.md`
- `docs/terms.md`
- `docs/specs/features/initial/tui-defaults.md`
- `docs/artefacts/go-extension-feasibility.md`
- `docs/artefacts/pi-extension-surface.md`
- `https://github.com/n-r-w/pi-agent-suite`
- `@earendil-works/pi-coding-agent/docs/sdk.md`
- `@earendil-works/pi-coding-agent/docs/sessions.md`
- `@earendil-works/pi-coding-agent/docs/session-format.md`
- `@earendil-works/pi-coding-agent/docs/compaction.md`
- `@earendil-works/pi-coding-agent/docs/extensions.md`
- `@earendil-works/pi-coding-agent/docs/rpc.md`
- `@earendil-works/pi-coding-agent/docs/tui.md`
- `@earendil-works/pi-coding-agent/docs/keybindings.md`
