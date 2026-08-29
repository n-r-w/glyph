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
- `bundled provider extension`: A bundled extension that supplies one or more model provider implementations through the ordinary extension contract and runtime.
- `extension contract`: A documented operation, data type, event, or registration point through which an extension interacts with Glyph.
- `extension point`: A documented boundary at which an extension handler can observe, block, modify, or replace an operation.
- `extension runtime`: One loaded execution environment for an extension and its in-memory state.
- `extension context`: Host-provided access to one extension runtime and its active session.
- `agent run`: One continuous agent-loop execution initiated by a message and ending when no automatic model or tool work remains or the run is stopped.
- `standard coding agent`: The coding-agent configuration distributed with Glyph; it enables the bundled tools extension and bundled resource extension and can run headlessly or through the standard TUI.
- `context`: The information sent to a model to produce its next response or tool request.
- `context compaction`: Replacement of an older context prefix in model-visible context with a summary while retaining the original session entries and preserving the remaining context suffix.
- `session`: A related sequence of user requests, model responses, tool calls, and agent state.
- `session tree`: A session structure whose entries form parent-child branches and have one active leaf.
- `active leaf`: The session-tree entry from which subsequent entries continue.
- `navigation destination`: The session-tree position selected before an optional `BranchSummaryEntry` becomes the active leaf.
- `model-visible extension message`: An extension-created session message associated with one session-tree branch and included in model context.
- `model-hidden extension entry`: An extension-created session entry associated with one session-tree branch and excluded from model context.
- `UI`: A presentation and input surface through which a person interacts with Glyph.
- `terminal UI`: A UI presented inside a terminal.
- `standard TUI`: The terminal UI plugin distributed with Glyph; it owns terminal-specific rendering, input, and extension capabilities.
- `headless agent`: A Glyph agent instance controlled programmatically without a UI.
- `Glyph client`: A component connected to a Glyph host that sends commands and receives events. A Glyph client is either a UI plugin or a programmatic controller.
- `programmatic controller`: A Glyph client that controls a headless agent without presenting a UI.
- `programmatic control contract`: The transport-independent correlated commands, acceptance responses, asynchronous execution events, interaction requests, and notifications for a long-lived headless agent.
- `Programmatic Control transport`: The bidirectional gRPC stream over a Unix socket that exposes the programmatic control contract from the current `glyph` application's headless composition.
- `queue mode`: A setting with values `all` and `one-at-a-time` that controls delivery of queued `steer` and `followUp` messages.
- `steer`: A queued message intended to influence an active agent run.
- `followUp`: A queued message intended for delivery after an active agent run.
- `nextTurn`: A queued message intended for the next user turn.
- `terminate`: A tool-result signal that controls whether the agent performs another automatic model request.
- `interaction request`: A request from an extension through the Glyph host to a Glyph client that expects a result.
- `notification`: Information sent by an extension through the Glyph host to a Glyph client without expecting a user response; the host reports delivery success or an error.
- `credential source`: The name of an environment variable or local credential-file entry from which Glyph reads an API key; it is not the secret itself.
- `reasoning capability`: A model's ability to produce reasoning content separately from its final answer.
- `reasoning control`: The single user-facing control whose available choices reflect the selected model's reasoning capabilities.
- `reasoning choice`: One value available through reasoning control: `off`, `on`, or a supported reasoning effort.
- `visible reasoning content`: Provider-returned reasoning text intended for typed conversation history and client presentation.
- `provider reasoning context`: Opaque or encrypted provider-owned session data persisted with its model response, restored unchanged, retained for compatible request replay, and not exposed to clients.
- `reasoning compatibility key`: An optional nonempty model identifier that explicitly permits provider reasoning context replay between models with the same provider instance, API, and key.
- `Go interface`: A Go language type that defines a method set; this term does not refer to a UI plugin, Glyph client, or extension.
- `skill`: A reusable instruction resource contributed by an extension.
- `prompt template`: A reusable user-request template contributed by an extension.
- `context file`: A file containing project instructions contributed by an extension.
- `resource contribution`: A typed skill, prompt template, or context file supplied by an active extension to the Glyph host.
- `response budget`: The token capacity reserved for the next model response.
- `branch summarization`: Creation of a summary for entries on the branch that the user leaves during session-tree navigation.
- `BranchSummaryEntry`: The persisted session entry produced by branch summarization.
- `input modality`: A content kind accepted by a model. Glyph defines `text` and `image`.
- `retry decision`: The Host-owned decision that determines whether and when Glyph repeats one failed model request.
- `reference scenario`: Behavior from an existing system that is used to evaluate Glyph requirements or extension contracts without requiring source compatibility with that system.

## Context and Problem

`docs/specs/features/initial/problem.md`

## Goal

Deliver an independent Go agent platform with a UI-free agent core, a plugin-managing host, a standard TUI, and programmatic control.

## Scenarios

- A programmatic controller runs and controls a headless Glyph agent without loading the standard TUI.
- A user gives the standard terminal coding agent a task. The agent inspects the project, invokes tools, changes files, runs commands, reports the result, and continues the conversation.
- A user authenticates with OpenAI Codex or configures one or more OpenAI-compatible provider instances, selects a model, and changes the model without leaving the session.
- A user selects only the reasoning choices supported by the active model, inspects visible reasoning content, and continues a conversation whose compatible provider reasoning context is replayed without client exposure.
- A user resumes a saved session, navigates its tree, and continues from an earlier point without deleting another branch.
- A Go developer installs a compatible extension without rebuilding Glyph. Its core capabilities work headlessly, and its terminal capabilities activate only with the standard TUI.
- An extension requests interaction or sends a notification through the Glyph host to a Glyph client.
- A user reloads the Glyph environment without ending the active session.

## Scope and Non-Scope

### Scope

- Glyph host, agent core, locally managed UI plugins including the standard TUI, and programmatic control.
- OpenAI Codex and user-configured OpenAI-compatible provider instances.
- Bundled standard coding tools.
- Runtime-installable Go extensions.
- A UI catalog separate from the extension catalog.
- Persistent tree-structured sessions and context compaction.
- macOS and Linux support.
- Open-source distribution under the MIT license.

### Non-Scope

- Source, API, configuration, session, or extension compatibility with Pi.
- Porting or implementing the existing `pi-agent-suite` extensions.
- Built-in subagents, workflows, or MCP support. These capabilities remain implementable through extensions.
- First-class parent-agent, child-agent, advisor, council, or workflow semantics in the agent core.
- A universal extension renderer shared by the standard TUI and future UI plugins.
- Remote or independently started UI plugins.
- Windows support.
- Sandboxing, project trust, or another security policy for trusted extensions.
- Host inspection, snapshot, reset, or restoration of terminal state and automatic restart of a terminated TUI.
- Glyph-client direct shell actions. Shell execution remains available when the model invokes the bundled `bash` tool.
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
- Extension process contracts and provider-scoped operations shall organize API ownership and prevent accidental conflicts; they shall not claim to isolate credentials or user-readable files from a trusted extension.
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
- A Glyph client shall not request direct shell execution. Shell execution shall occur only when the agent invokes an available tool.

#### Bundled Resource Processing

- Glyph shall distribute a bundled resource extension enabled by default for the standard coding agent.
- The user shall be able to disable, update, and replace the bundled resource extension under the ordinary extension lifecycle.
- The Glyph host shall supply collected resource contributions to the bundled resource extension.
- The bundled resource extension shall convert skills and context files into system instructions and model context and make prompt templates available through Glyph clients.
- The agent core shall receive only resolved instructions and context and shall not depend on the resource types.

#### Model Providers and Authentication

- Glyph shall distribute OpenAI Codex and OpenAI-compatible provider implementations as bundled provider extensions enabled by default.
- The user shall be able to disable, update, and replace each bundled provider extension under the ordinary extension lifecycle.
- Glyph shall support multiple configured provider instances of the OpenAI-compatible provider type, each with a unique identifier.
- Every bundled or separately delivered provider implementation shall register and unregister through the same extension contract and shall run through the ordinary extension runtime. The contract shall carry provider authentication, model catalogue, streaming behavior, failure classification, and provider reasoning context replay.
- The Glyph host shall contain no concrete provider authentication, API request serialization, response decoding, streaming, usage mapping, failure classification, or provider reasoning context replay implementation.
- Each provider implementation shall own its authentication behavior, including API-key resolution, OAuth login, token refresh, and conversion of credentials into request authorization.
- The Glyph host shall provide provider implementations with generic credential storage and interaction through a Glyph client.
- Credential storage operations shall use the registered provider identity as their namespace to prevent accidental cross-provider reads, writes, and identifier conflicts. This namespace shall not be a sandbox or a security boundary against trusted extensions.
- Host-provided credential storage shall persist credentials in the local credential file.
- When two or more active extensions register the same provider identifier, the Glyph host shall reject every provider registration in that duplicate group. Load order shall select no winner.
- The local credential file shall be accessible only to the user running Glyph.
- The OpenAI Codex provider shall use interactive OAuth authentication and persist its OAuth credentials through host-provided credential storage.
- Each OpenAI-compatible provider instance shall be configured with a base URL, an explicit model list, and an optional API key.
- An OpenAI-compatible provider instance API key shall be a literal value, an environment-variable reference, or a local credential-file entry reference. API-key resolution shall not execute a command.
- The OpenAI-compatible provider type shall support OpenAI Chat Completions and OpenAI Responses.
- Provider configuration shall select the OpenAI wire API through an explicit `api` field, and a model configuration shall be able to override it.
- Settings shall explicitly declare each model's reasoning capability, available reasoning choices, and default reasoning choice without runtime capability probing.
- Settings shall require each model to declare a nonempty ordered `input` list from the closed values `text` and `image`, a positive integer `contextWindow`, and a positive integer `maxTokens` that does not exceed `contextWindow`.
- Settings loading shall reject an empty input list, missing `text`, an unknown or duplicate modality, a nonpositive limit, and `maxTokens` greater than `contextWindow`.
- The provider-neutral model descriptor shall own `input`, `contextWindow`, and `maxTokens` for bundled and separately delivered provider extensions. The Host model catalogue and Programmatic Control shall expose all three values.
- Settings loading shall reject incomplete or contradictory reasoning configuration.
- A model without reasoning capability shall expose no reasoning control; an on/off model shall expose only `off` and `on`; an effort model shall expose only its configured efforts and `off` when reasoning can be disabled; and fixed reasoning shall expose no selectable alternative.
- TUI and Programmatic Control shall use one Host-owned reasoning capability model and shall expose only choices that affect the selected model.
- Selecting an unsupported reasoning choice shall fail without changing the active model selection.
- Model switching shall preserve a semantically compatible reasoning choice and otherwise use the target model's explicit default reasoning choice.
- Visible reasoning content shall remain typed model content in conversation history and client events and shall remain in model-visible history for later requests.
- A provider driver shall replay visible reasoning through a native reasoning field when the target provider format supports it and shall otherwise convert the visible reasoning to ordinary assistant text.
- Provider reasoning context is session data attached to its model response. Glyph shall persist it unchanged, restore it with the session, and retain it for compatible replay after application restart.
- Provider reasoning context shall not be exposed to Glyph clients. An owning provider implementation may parse and serialize its API item structure, but provider-owned opaque values shall remain unchanged and shall be replayed only to a compatible model request.
- Provider reasoning context replay shall require the same provider instance and API plus either the same model identifier or the same nonempty reasoning compatibility key.
- A reasoning compatibility key shall add cross-model compatibility and shall not disable replay to the same model identifier.
- OpenAI Codex and OpenAI-compatible Responses shall own Responses reasoning behavior without a reasoning format setting.
- OpenAI-compatible Chat Completions reasoning shall use the adapter-private `openai-chat` or `openrouter` format. Shared settings shall pass `reasoning.format` as an opaque string, and the adapter shall validate accepted values and API compatibility during construction.
- `openrouter` shall use nested request reasoning control, map streamed visible reasoning into typed content, preserve `reasoning_details` as opaque provider context, and replay compatible details on later assistant messages.
- Together, DeepSeek, Qwen, chat-template reasoning controls, and thinking token budgets shall remain unsupported until a later feature adds and verifies their provider formats.
- An OpenAI-compatible provider instance without an API key shall remain available and shall use no request authorization.
- Selecting a model whose referenced API key cannot be resolved shall fail immediately, preserve the active provider, model, and reasoning choice, and produce an error.

#### Programmatic Control

- The Glyph host shall support a programmatically controlled headless agent in addition to the standard TUI.
- Programmatic Control shall expose each model's input modalities, context window, maximum output tokens, reasoning capability, available reasoning choices, and the active reasoning choice for the selected model without exposing provider reasoning context.
- Each programmatic command shall carry a correlation identifier.
- The host shall accept or reject a programmatic command independently from its later execution outcome.
- Asynchronous events caused by an accepted command shall be attributable to the controlled operation.
- The programmatic control contract shall not depend on the standard TUI or a selected transport technology.
- The current `glyph` application's headless composition shall expose the programmatic control contract through bidirectional gRPC over a Unix socket.
- The `glyph` application shall host the Programmatic Control transport in its own process and shall not create a separate Host daemon.
- Programmatic control shall cover user requests, queued steering and follow-up messages, abort, state and message queries, model selection and adaptive reasoning control, queue modes, compaction, retry control, session statistics, session creation and switching, forking, cloning, tree navigation, session entries and naming, command discovery, execution events, interaction requests, and notifications.
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
- Every Glyph-owned prompt sent to a model shall be stored in a Markdown file and embedded into its owning Go binary with `//go:embed`. Go source shall contain no built-in prompt text.

### Glyph Host Agent Orchestration Requirements

#### Retry

- Provider adapters shall classify provider responses and errors as retryable or non-retryable.
- The Glyph host shall own retry-policy configuration, retry-decision coordination, extension dispatch, delay scheduling, final-decision validation, and retry events outside Agent Core.
- After one model request fails, the Glyph host shall create an immutable original retry decision from the provider classification, configured built-in policy, completed attempt count, and provider-supplied delay, and shall initialize the current retry decision with the same value.
- Retry handlers shall run sequentially. Each handler shall receive the original retry decision and the current retry decision returned by preceding handlers and shall be able to preserve, replace, or cancel that retry. Cancellation shall be terminal and shall stop later retry handlers.
- An invalid retry action or ordinary handler error shall be reported, shall preserve the decision received by that handler, and shall not stop later retry handlers or deactivate the extension.
- The Glyph host shall validate the final retry decision before it schedules a delay or repeats the model request. When handlers preserve the built-in decision, Glyph shall apply the configured built-in policy.
- Agent Core shall expose only the minimum mechanism needed to consume one logical model execution result and shall not depend on retry policy, extension handlers, plugin transport, or delay scheduling.
- Glyph clients shall receive retry events and choose how to present them.
- General abort shall cancel an in-progress provider request or pending retry delay and transition the agent to idle.
- A retry shall repeat only the failed model request and shall not repeat any completed tool execution.
- Failed intermediate attempts shall produce operation events and shall not create session messages or enter model context. After retry finishes, Glyph shall persist only the terminal model outcome.
- Retry shall be enabled by default with three retries after the initial request. The default delays shall be 1, 2, and 4 seconds.
- A provider-supplied `Retry-After` delay shall be capped at 30 seconds by the built-in policy.
- The built-in retryable HTTP statuses shall be 408, 429, 500, 502, 503, and 504. Transport timeouts, connection resets, and unexpected connection closure before a terminal provider response shall also be retryable.
- The retry policy configuration shall include the enabled state, maximum retry count, ordered retry delays, `Retry-After` cap, and built-in retryable HTTP statuses.

#### Extension Capabilities

- Glyph extension contracts shall support tools, lifecycle events, system-prompt changes, context transformations, sessions, and model access without requiring terminal capabilities or exposing plugin transport types to Agent Core.
- Each extension point shall declare whether it is an observer, transformer, gate, or replaceable operation and which actions its handlers can return.
- Multiple transformations of the same operation shall run sequentially. Each handler shall receive both the immutable original input and the current value returned by preceding handlers, and shall be able to preserve the current value or replace it with a value derived from either input.
- When an ordinary transforming handler fails, the next handler shall receive the same current value that the failed handler received.
- After model context assembly and before every provider request, Agent Core shall request effective context through its consumer-owned interface. The Host adapter shall invoke active context transformations sequentially exactly once and return the final provider-neutral context.
- A context transformation shall affect only the outbound context for that provider request; persisted session state shall change only when an extension separately adds a session entry through the session contract.
- Extensions shall be able to observe, transform, or fully handle user text and images before agent processing.
- An extension shall be able to initiate and cancel session creation, resumption, forking, cloning, and tree navigation.
- Before tree navigation, the Glyph host shall invoke `session_before_tree` request handlers with the immutable original navigation request, the current request returned by preceding handlers, and the current branch summarization result when one exists. The initial request shall contain a summary model selection copied from the active provider, model, and reasoning choice.
- A `session_before_tree` request handler shall be able to preserve or replace the current request, including selecting another configured provider, model, and reasoning choice, preserve, set, replace, or clear the current result, or cancel navigation. Cancellation shall be terminal and shall stop later branch summarization handlers. When handlers end without a result and the current request requires branch summarization, the built-in branch summarizer shall use the final validated summary model selection.
- Branch summarization result handlers shall receive the original request, final current request, immutable original result, and current result returned by preceding handlers and shall preserve, replace, or cancel the result. Cancellation shall be terminal and shall stop later branch summarization handlers.
- The Glyph host shall validate the final tree target and `BranchSummaryEntry`, atomically commit navigation and summary persistence, and emit `session_tree` only after commit. Cancellation or invalid final state shall change neither the active leaf nor persisted entries.
- After session replacement, the extension shall receive a context bound to the replacement session.
- An extension shall be able to persist model-hidden entries and model-visible messages and associate both with the active session branch.
- Extensions shall be able to inspect configured models and providers and make model requests through them for extension-owned behavior.
- A Glyph client or extension shall be able to request a change to the active conversation model or active reasoning choice through Host-owned selection operations.
- Before a model or reasoning selection commits, selection handlers shall run sequentially with the immutable original target selection and the current selection returned by preceding handlers. A handler shall be able to preserve, replace, or reject the selection. Rejection shall be terminal and shall stop later selection handlers.
- The Glyph host shall atomically validate model existence, reasoning capability, and authentication before it commits the complete provider, model, and reasoning selection. A rejected or invalid selection shall preserve the active selection. Selection events shall be emitted only after commit.
- An extension shall be able to change provider request headers, replace the serialized provider request, and observe provider response status and headers.
- Before each manual, threshold, or overflow-recovery compaction, the Glyph host shall create an immutable original compaction request and a current compaction request initialized with the same value.
- Compaction request handlers shall run sequentially. Each handler shall receive the original request, the current request, and the current compaction result when one exists.
- A compaction request handler shall be able to replace the current request and set, replace, or clear the current result independently in one state update. It shall also be able to preserve the complete current state or cancel compaction. The next handler shall receive the unchanged original request and the current state returned by the preceding handler.
- When the compaction request handlers finish without a current result, the built-in compaction strategy shall process the current request.
- After an extension or the built-in strategy produces a compaction result, compaction result handlers shall run sequentially. Each result handler shall receive the immutable original request, the final current request, the immutable original result, and the current result returned by preceding result handlers and shall be able to preserve or replace the current result or cancel compaction.
- The Glyph host shall validate the final compaction result before session persistence. An invalid handler result shall be reported, shall not replace the preceding current state, and shall not deactivate the extension.
- Extensions shall receive events for agent start, agent end, agent settled, turn start, turn end, message start, message update, message end, tool-execution start, tool-execution update, tool-execution end, model selection, reasoning selection, compaction success, and compaction failure.
- An extension shall be able to replace a finalized message without changing its role; multiple replacements shall run sequentially.
- Except for a pre-execution tool-handler error, an ordinary extension-handler error shall be reported while later handlers and the agent-core or Host operation associated with that extension point continue and the extension remains active.
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

- The Glyph host shall initiate automatic compaction when the remaining model context cannot accommodate the response budget.
- Manual compaction shall accept user instructions for the summary and shall use the same extension point as automatic compaction.
- Compaction coordination, extension dispatch, final-result validation, and session persistence shall remain outside Agent Core.
- Context compaction shall append a summary entry that replaces an older context prefix in model-visible context while preserving the remaining suffix unchanged and retaining the original session entries.
- Glyph shall automatically save sessions and allow them to resume after application restart.
- Session information shall include message and tool counts, normalized token usage, persisted estimated cost, and provider-model cost breakdown. Counts shall remain available independently. Token totals shall be available only when every stored model response has usage. Estimated cost shall be available only when every stored model response has persisted cost.
- Each `BranchSummaryEntry` shall own its branch boundary, provider, model, reasoning choice, and explicit optional states for normalized token usage and persisted estimated cost. Usage shall be absent when the provider does not report it. Estimated cost shall be absent when usage or configured pricing is unavailable. Context compaction, retry, and context-window behavior shall own their accounting. The standard TUI shall present all available session values.
- Session entries shall form a tree with parent-child relationships and one active leaf.
- Continuing from an earlier entry shall create a new branch without deleting existing branches.
- The session model shall support creation, resumption, tree navigation, forking, cloning, naming, information retrieval, branch summarization, and entry labels.
- Tree navigation shall return a client-neutral result containing the committed active leaf and optional next-input text. It shall return `busy` without changing the session during an active agent run.
- Selecting a user message or model-visible extension message shall use its parent as the navigation destination and shall return its exact text as next input without submitting it. Selecting any other entry shall use that entry as the navigation destination and shall return no next input.
- When branch summarization creates a `BranchSummaryEntry`, the Glyph host shall attach it as a child of the navigation destination and make it the active leaf. Without a `BranchSummaryEntry`, the navigation destination shall become the active leaf.
- Forking shall copy the path through the selected user message's parent and return that message as next input. Cloning shall copy the complete active branch.
- Tree navigation shall support no branch summarization, built-in branch summarization, or branch summarization with custom focus, and shall attach a created `BranchSummaryEntry` at the selected position.

### Standard TUI Requirements

#### Terminal Agent

- The standard TUI shall provide a terminal UI for the standard coding agent without owning agent-core behavior.
- The standard TUI shall own terminal initialization, input dispatch, modes, rendering, cleanup, and editor behavior.
- The Glyph host shall not inspect, snapshot, reset, or restore terminal state and shall not restart a terminated TUI. After TUI termination, the user shall start the Glyph client again.
- The UI Plugin Contract shall expose no terminal-ownership capability or startup-capability operation. Successful plugin protocol startup shall establish UI compatibility.
- For every user-invokable action, the standard TUI shall send a Host command and render Host events or results. It shall not execute agent behavior.
- The standard TUI shall satisfy the transcript, viewport, editor, completion, clipboard, selector, and terminal-lifecycle requirements in `docs/specs/features/initial/standard-tui.md`.
- The standard TUI shall render model output incrementally and keep tool progress visible while a tool runs.
- The standard TUI shall render visible reasoning content as reasoning blocks that are collapsed by default.
- One display action shall expand or collapse all reasoning blocks in the session without changing the active reasoning choice or provider request.
- The user shall be able to stop the active run through the standard TUI.
- The standard TUI shall expose model selection, model cycling, environment reload, context compaction, and session operations through user-invokable actions.
- Every TUI key binding shall be user-configurable.
- Standard command names and the initial keybinding baseline are recorded in `docs/specs/features/initial/tui-defaults.md`.
- A keybinding-baseline entry shall apply only when Glyph independently supports the corresponding action; it shall not add that action to product scope.
- When the Glyph host performs environment reload, an active standard TUI shall reload its themes and key bindings.

#### Session Interaction

- The standard TUI shall present the complete session tree and preserve all existing branches when the user continues from an earlier position.
- Selecting a user message or model-visible extension message shall use its parent as the navigation destination and place its text in the editor for resubmission.
- Selecting the root user message shall use an empty conversation as the navigation destination and place the root prompt in the editor.
- Selecting another entry shall use that entry as the navigation destination and leave the editor empty.
- When leaving a branch, the standard TUI shall offer no branch summarization, built-in branch summarization, and branch summarization with custom focus. No branch summarization shall be the default.
- The standard TUI shall provide tree search, filters, branch folding, and entry labels according to `docs/specs/features/initial/tui-defaults.md`.

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

This document selects only Programmatic Control's bidirectional gRPC transport over a Unix socket. `docs/specs/features/initial/architecture.md` owns target process, component, dependency, contract, and package boundaries. Feature-specific API shapes and implementation details remain in phase Technical Solutions. Runtime extension feasibility is analyzed in `docs/artefacts/go-extension-feasibility.md`.

### Glyph public-behavior traceability

This matrix traces each Glyph-owned public behavior group to its owning PRD section, delivery ticket, and public-contract scenario. It does not require Pi compatibility or external entry-point coverage.

| Glyph-owned public behavior group | Owning PRD section | Owner ticket | Public-contract scenario |
|---|---|---|---|
| Standard coding tools | Bundled Standard Tools | PHS-01 | A headless agent invokes each bundled tool and receives its result. |
| Headless control | Programmatic Control | PHS-02 | A controller submits, observes, aborts, and resubmits through `glyph`. |
| Provider selection and authentication | Model Providers and Authentication | PHS-03 | A client configures a provider, authenticates, selects a model, and changes it. |
| Persistent sessions | Context and Sessions | PHS-04 | A saved session resumes after application restart. |
| Model execution capabilities | Model Providers and Authentication; Programmatic Control | PHS-04.1 | A controller inspects exact input modalities and token limits for each configured model. |
| Session-tree navigation and branch summarization | Context and Sessions; Session Interaction; Extension Capabilities | PHS-05 | An extension composes with or replaces branch summarization while a client navigates without deleting another branch. |
| Extensible compaction and retry | Context and Sessions; Retry; Extension Capabilities | PHS-06 | Extensions compose compaction results and retry decisions while a client observes both operations. |
| Extension lifecycle and active selection | Extensions and Glyph Clients; Extension Capabilities | PHS-07 | An extension uses a session-bound context and changes the active model through ordered selection handlers. |
| Input and provider middleware | Extension Capabilities | PHS-08 | An extension transforms model-facing input, Host validates final modalities, and provider middleware changes one request. |
| Tool middleware and run control | Agent and Tool Runtime; Run Control | PHS-09 | An extension changes a tool call or result and controls run continuation. |
| Commands, interactions, notifications, and events | Extensions and Glyph Clients; Extension Capabilities | PHS-10 | A client discovers an extension command and receives its event, interaction, or notification. |
| Resource contributions | Extensions and Glyph Clients; Bundled Resource Processing | PHS-11 | An active extension contributes a resource used by the standard coding agent. |
| Bundled and extension-defined providers | Model Providers and Authentication | PHS-12 | Bundled and separately delivered providers use one extension registration and execution path. |
| TUI transcript rendering | Standard TUI Requirements | PHS-12.1 | The standard TUI renders ordered Host events and results. |
| TUI viewport navigation | Standard TUI Requirements | PHS-12.2 | The user navigates and searches the rendered transcript during streaming. |
| TUI editor and terminal interaction | Standard TUI Requirements | PHS-12.3 | The TUI dispatches a Host command and renders its result without executing it. |
| TUI presentation extensions | TUI Extension Capabilities | PHS-13 | An extension supplies passive terminal presentation while the TUI retains terminal ownership. |
| Interactive TUI extensions | TUI Extension Capabilities | PHS-14 | An extension uses focused interaction and editor integration through the standard TUI. |
| Extension installation and state | Extensions and Glyph Clients | PHS-15 | A user installs, enables, disables, updates, or removes a compatible extension. |
| Environment reload | Environment Reload | PHS-16 | A client reloads the environment while retaining the active session. |

## References

- `docs/specs/features/initial/problem.md`
- `docs/specs/features/initial/architecture.md`
- `docs/terms.md`
- `docs/specs/features/initial/tui-defaults.md`
- `docs/specs/features/initial/standard-tui.md`
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
