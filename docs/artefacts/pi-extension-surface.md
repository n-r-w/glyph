# Research: Pi Extension Surface

- PRB-01: What capabilities does Pi expose to extensions, and which of those capabilities are not exercised by `pi-agent-suite`?

## Scope

- ISP-01: Public extension contracts documented or declared by the locally installed Pi package.
- ISP-02: Product-visible lifecycle, interception, replacement, registration, persistence, rendering, and runtime-control behavior.
- ISP-03: Production extension usage under `/Users/rvnikulenk/dev/nrw/pi-harness/pi-package`, excluding test files.
- OSP-01: Private Pi implementation details that do not define public extension behavior.
- OSP-02: Glyph architecture, API shapes, transports, and implementation choices.
- LIM-01: Coverage of `pi-agent-suite` is based on production-source usage. Indirect calls hidden behind aliases can reduce the accuracy of negative findings.

## Classification

- DEF-01: **Observe** means receiving state or a notification without changing the operation.
- DEF-02: **Intercept** means running at a boundary before the default operation continues.
- DEF-03: **Modify** means changing part of the current input, state, or result.
- DEF-04: **Replace** means substituting the complete input, result, component, provider, or operation.
- DEF-05: **Register** means adding a named capability to the active runtime.
- DEF-06: **Persist** means storing extension-owned data beyond the current in-memory extension instance.
- DEF-07: **Render** means contributing user-visible terminal output or interaction.
- DEF-08: **Control** means initiating, stopping, or redirecting agent, tool, model, session, input, or UI behavior.

## Executive Summary

- FND-01: Pi exposes 33 named extension events, 23 non-event methods on its primary extension API, and 27 methods on its terminal UI context.
- FND-02: Production `pi-agent-suite` sources use 14 of the 33 events, 16 of the 23 primary API methods, and 7 of the 27 terminal UI methods.
- FND-03: `pi-agent-suite` demonstrates substantial extensibility but does not cover Pi's complete extension surface.
- FND-04: Pi's extension model is ordered middleware combined with dynamic registration, persistence, rendering, and runtime control. It is not only an event-notification system.
- FND-05: The most material capabilities absent or only partially exercised by `pi-agent-suite` are provider registration, resource contribution, raw input handling, complete tool middleware, session orchestration, detailed streaming lifecycle observation, editor integration, branch-aware persistence, inter-extension communication, and run control.
- FND-06: Pi-specific helpers and every unused API do not automatically represent requirements for another platform.

## Public Surface Inventory

### Events

- OBS-01: Startup and resource events are `project_trust` and `resources_discover`. They support trust interception and dynamic contribution of skills, prompt templates, and themes. [REF-01] [REF-02]
- OBS-02: Session events are `session_start`, `session_info_changed`, `session_before_switch`, `session_before_fork`, `session_before_compact`, `session_compact`, `session_shutdown`, `session_before_tree`, and `session_tree`. They support observation, transition cancellation, compaction replacement, tree-summary replacement, labels, and cleanup. [REF-01] [REF-02]
- OBS-03: Context and provider events are `context`, `before_provider_request`, `before_provider_headers`, and `after_provider_response`. They support context replacement, serialized request replacement, header mutation, and response metadata observation. [REF-01] [REF-02]
- OBS-04: Agent and message events are `before_agent_start`, `agent_start`, `agent_end`, `agent_settled`, `turn_start`, `turn_end`, `message_start`, `message_update`, and `message_end`. They support prompt modification, persistent message injection, streaming observation, and finalized-message replacement. [REF-01] [REF-02]
- OBS-05: Tool events are `tool_execution_start`, `tool_execution_update`, `tool_execution_end`, `tool_call`, and `tool_result`. They support execution observation, progress observation, pre-call input mutation or blocking, and post-result transformation. [REF-01] [REF-02]
- OBS-06: Model events are `model_select` and `thinking_level_select`. They observe active model and reasoning-level changes. [REF-01] [REF-02]
- OBS-07: User-operation events are `user_bash` and `input`. They support shell replacement and observation, transformation, or full handling of user input before agent processing. [REF-01] [REF-02]

### Primary Extension API

- OBS-08: Registration methods are `registerTool`, `registerCommand`, `registerShortcut`, `registerFlag`, `registerMessageRenderer`, `registerEntryRenderer`, and `registerProvider`. `unregisterProvider` removes a dynamically registered provider. [REF-02]
- OBS-09: Messaging and persistence methods are `sendMessage`, `sendUserMessage`, `appendEntry`, `setSessionName`, `getSessionName`, and `setLabel`. [REF-02]
- OBS-10: Runtime and catalogue methods are `exec`, `getActiveTools`, `getAllTools`, `setActiveTools`, `getCommands`, `setModel`, `getThinkingLevel`, and `setThinkingLevel`. [REF-02]
- OBS-11: Configuration methods are `getFlag` and the registration methods that define flags, providers, tools, commands, and shortcuts. [REF-02]
- OBS-12: `pi.events` provides a shared event bus for communication between independently loaded extensions. [REF-01] [REF-02]

### Context and Session Control

- OBS-13: Every handler receives context containing the current directory, run mode, current model, scoped models, thinking level, session view, model registry, cancellation signal, context usage, and current system prompt. [REF-01] [REF-02]
- OBS-14: Context methods can inspect idle and pending-message state, abort active work, request compaction, request shutdown, and inspect project trust. [REF-01] [REF-02]
- OBS-15: Command handlers additionally can wait for idle state, create a session, fork, navigate the session tree, switch sessions, and reload the extension environment. [REF-01] [REF-02]
- OBS-16: Successful session replacement gives post-replacement work a fresh context. Captured objects associated with the previous session are stale. [REF-01]
- OBS-17: Extensions can persist LLM-hidden custom entries, LLM-visible custom messages, branch-aware tool details, session names, labels, and provider-scoped model catalogues. [REF-01] [REF-02] [REF-03]

### Tools and Providers

- OBS-18: A custom tool defines its schema, prompt description, execution mode, execution logic, progress updates, cancellation handling, and call/result rendering. Registering an existing tool name replaces that tool. [REF-01] [REF-02]
- OBS-19: Tool registration and active-tool selection are separate. Extensions can register tools after startup and change which tools are exposed to later model requests. [REF-01] [REF-04]
- OBS-20: Provider registration can define authentication, OAuth, model lists, model refresh, model filtering, request headers, endpoints, and custom streaming behavior. Registration can replace a built-in provider and unregistration restores it. [REF-01] [REF-02]
- OBS-21: Provider model catalog storage is scoped to the registered provider and is separate from session persistence and credentials. [REF-03]

### Terminal UI

- OBS-22: Dialog methods provide selection, confirmation, single-line input, multi-line editing, and notifications. [REF-01] [REF-02]
- OBS-23: Input and editor methods provide raw terminal interception, editor insertion and replacement, editor text access, autocomplete contribution, and custom editor replacement. [REF-01] [REF-02]
- OBS-24: Persistent UI regions include statuses, working indicators, widgets, header, footer, terminal title, and the hidden-thinking label. [REF-01] [REF-02]
- OBS-25: Custom focused or overlay components run inside host-controlled terminal rendering. [REF-01] [REF-02]
- OBS-26: Tool, custom-message, and custom-entry renderers control transcript presentation without changing persisted content. [REF-01] [REF-02]
- OBS-27: Theme methods enumerate, inspect, and switch themes. Tool-output methods inspect and change expanded presentation. [REF-01] [REF-02]
- OBS-28: TUI component APIs and keyboard shortcuts are terminal-specific. RPC exposes only a reduced dialog and notification surface; JSON and print modes expose no interactive UI. [REF-01] [REF-05]

## Behavioral Semantics

- OBS-29: Event handlers are normally awaited in extension load order. Transformations chain so that each handler receives the valid result produced by earlier handlers. [REF-01] [REF-04]
- OBS-30: Cancellable session gates stop on the first cancellation. Input handling stops on the first fully handled result. Tool-call blocking stops tool execution. [REF-01] [REF-04]
- OBS-31: Most ordinary handler failures are reported and later handlers or default behavior continue. A `tool_call` handler failure blocks the tool, while a thrown tool-execution error becomes a model-visible error result. [REF-01] [REF-04]
- OBS-32: Tools and providers can be registered dynamically after startup. Keyboard shortcuts and command-line flags depend on terminal or startup binding and do not share the same activation timing. [REF-01] [REF-04]
- OBS-33: Message delivery distinguishes immediate user input, steering during an active run, follow-up work after a run, and delivery at the next user turn. [REF-01] [REF-02]
- OBS-34: Extension resources should be created for a session after `session_start` and released idempotently during `session_shutdown`. Extension factories can run without a session. [REF-01]
- OBS-35: Reload emits shutdown, reloads settings and resources, creates a new runtime, emits a new session start, and rediscover resources while preserving the session. [REF-01] [REF-04]
- OBS-36: Pi documentation states that old extension contexts become stale after reload. The locally installed compiled reload path replaces future dispatch but does not invalidate the previous runner, so documentation and implementation disagree about mechanical rejection of old calls. [REF-01] [REF-04]

## Capability Coverage by `pi-agent-suite`

| ID | Capability category | Coverage | Material uncovered behavior |
|---|---|---|---|
| COV-01 | Factory and session lifecycle | Exercised | None material |
| COV-02 | Custom tool definition, progress, cancellation, rendering, active set | Exercised | None material |
| COV-03 | Tool pre-call and post-result middleware | Partial | Pre-call blocking and input mutation are unused; complete tool replacement is unused |
| COV-04 | Commands, shortcuts, and flags | Partial | Extension flags and command argument completion are unused |
| COV-05 | User input and shell routing | Not exercised | Input transformation or full handling and user-shell replacement |
| COV-06 | Prompt, context, finalized messages, and injected messages | Partial | Finalized-message replacement is unused |
| COV-07 | Agent, turn, message-stream, and tool-execution lifecycle | Partial | Settled-run, stream-start/update, and detailed tool-progress events are unused |
| COV-08 | Provider request and response middleware | Partial | Header mutation and response metadata observation are unused |
| COV-09 | Provider registration and model catalogues | Partial | Provider registration, authentication, catalog refresh, and streaming implementation are unused |
| COV-10 | Session reads, persistence, rendering, and compaction replacement | Exercised | None material |
| COV-11 | Session transition gates and orchestration | Not exercised | Transition cancellation, session creation/switching, tree control, and fresh replacement contexts |
| COV-12 | Run control and queued communication | Partial | Abort, direct compaction, pending-message inspection, and tool-directed termination are unused |
| COV-13 | Terminal UI | Partial | Raw input, host autocomplete, custom editor replacement, themes, header, title, and working-indicator control are unused |
| COV-14 | Resource contribution | Not exercised | Extension-contributed skills, prompt templates, and themes |
| COV-15 | Project trust | Not exercised | Project trust interception and persistence |
| COV-16 | Inter-extension communication | Exercised | None material |

## Material Interpretation

- INT-01: Mapping every `pi-agent-suite` entry point proves scenario coverage but does not prove that the platform supports Pi's broader provider, input, resource, session, and UI extension boundaries.
- INT-02: A generic `lifecycle events` requirement is insufficient. Observation, ordered transformation, cancellation, partial mutation, and full replacement have materially different extension capabilities.
- INT-03: Provider access and provider registration are distinct. Without provider registration, adding a new authentication flow, model catalogue, or streaming protocol requires a core change.
- INT-04: Resource reload and resource contribution are distinct. Without contribution, an extension package cannot supply skills, prompt templates, or themes through the extension boundary.
- INT-05: Pre-tool interception alone is insufficient for alternate execution environments, result projection, usage correction, or complete tool wrapping. Post-result transformation and tool replacement are separate capabilities.
- INT-06: Session inspection and session orchestration are distinct. Without transition gates and control, checkpoint, handoff, and policy extensions require core changes.
- INT-07: Terminal UI is not one capability. Dialogs, persistent regions, transcript rendering, editor integration, raw input, and theme control have different ownership and portability implications.
- INT-08: Branch-aware persistence and inter-extension communication prevent extensions from relying on process globals, unrelated files, or core-owned special cases.
- INT-09: Detailed streaming lifecycle and settled-run events are required for live status, accounting, moderation, and completion integrations that cannot be implemented from end-only notifications.
- INT-10: Project trust, extension command-line flags, non-terminal modes, and Pi-specific renderer helpers require independent product needs before they justify requirements in another platform.

## Conclusion

- REC-01: Pi's extension model should be evaluated as a set of observable operations rather than copied as a list of TypeScript API names.
- REC-02: `pi-agent-suite` is a useful scenario suite but an incomplete definition of a general agent extension boundary.
- REC-03: The highest-impact capability categories are provider registration, resource contribution, input middleware, complete tool middleware, session orchestration, lifecycle transformations, terminal composition, branch-aware persistence, inter-extension communication, and run control.
- REC-04: Incidental Pi helpers and unused convenience APIs should remain excluded until supported by an independent product need.

## References

- REF-01: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/extensions.md` — documented extension contracts and behavior.
- REF-02: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/dist/core/extensions/types.d.ts` — primary extension, context, event, tool, provider, and UI declarations.
- REF-03: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/models.d.ts` — provider model refresh and provider-scoped storage.
- REF-04: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/dist/core/extensions/runner.js` and `loader.js` — ordered dispatch, chaining, registration, and failure behavior.
- REF-05: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/rpc.md` — non-TUI extension UI behavior.
- REF-06: `/Users/rvnikulenk/dev/nrw/pi-harness/pi-package/package.json` and production source under `pi-package/extensions` and `pi-package/shared` — `pi-agent-suite` coverage evidence.
