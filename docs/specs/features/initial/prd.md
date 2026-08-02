# Idea: Glyph Agent Platform

## Definitions

- `Glyph`: The project name for the independent Go agent platform being defined.
- `extension`: A component that adds or changes platform behavior through extension contracts without modifying the agent core source code.
- `session`: A related sequence of user requests, model responses, tool calls, and agent state.
- `reference scenario`: Behavior from an existing system that is used to evaluate Glyph requirements or extension contracts without requiring source compatibility with that system.

## Context and Problem

The project owner maintains `pi-agent-suite`, a set of extensions for Pi. Pi demonstrates that a small agent core can support extensive customization, but `pi-agent-suite` cannot change behavior outside Pi's extension contracts and requires a TypeScript-based platform.

Glyph provides an independently owned Go platform whose agent core and extension contracts can evolve in one codebase.

## Goal

- Provide a minimal, extensible agent platform for Go developers who build agents and extensions.
- Provide a standard interactive terminal coding agent for developers using any programming language.
- Allow workflow-specific capabilities to remain outside the agent core.

## Scenarios

- A user gives the standard coding agent a task. The agent inspects the project, invokes tools, changes files, runs commands, reports the result, and continues the conversation.
- A user authenticates with OpenAI Codex or configures an OpenAI-compatible API, selects a model, and changes the model without leaving the session.
- A user resumes a saved session, navigates its tree, and continues from an earlier point without deleting another branch.
- A Go developer installs a compatible extension without rebuilding Glyph.
- A user runs `/reload` to apply environment and extension changes without ending the session.
- An extension adds tools, commands, keyboard shortcuts, lifecycle behavior, context changes, model or session integration, and terminal UI elements through Glyph contracts.

## Scope and Non-Scope

### Scope

- Independent agent platform implemented in Go.
- Standard interactive terminal coding agent built on the platform's public contracts.
- OpenAI Codex and a user-configured OpenAI-compatible API.
- Built-in coding tools.
- Runtime-installable Go extensions.
- Persistent tree-structured sessions.
- macOS and Linux support.
- Open-source distribution under the MIT license.

### Non-Scope

- Source, API, configuration, session, or extension compatibility with Pi.
- Porting or implementing the existing `pi-agent-suite` extensions.
- Built-in subagents, workflows, MCP support, or specialized context compaction behavior. Glyph exposes extension contracts for these capabilities.
- Windows support.
- Sandboxing of trusted extensions.
- Direct user shell commands outside the `bash` tool.
- Extension-defined startup arguments and command argument completion.
- Technical architecture and implementation planning.

## Requirements

### Platform

- Glyph production code shall be written in Go.
- Glyph shall be published as an open-source project under the MIT license.
- Glyph shall support macOS and Linux.
- Glyph shall use platform-independent Go facilities instead of operating-system-specific facilities when they provide equivalent behavior.
- Glyph shall include an agent platform and a standard terminal coding agent.
- Glyph shall provide a programmatically controlled headless agent mode in addition to the interactive terminal mode.
- The standard coding agent shall use the same public contracts available to other agents and extensions.
- The agent core shall remain minimal and shall not define a specific agent workflow.
- Subagents, workflows, MCP integration, and specialized context compaction shall be implemented through extensions.
- Glyph shall not depend on Pi or provide compatibility with Pi contracts and persisted formats.

### Agent Interaction

- The standard coding agent shall provide an interactive terminal user interface.
- The agent shall support the task flow described in the first scenario.
- Model output shall appear as it arrives.
- Tool execution progress shall appear while the tool runs.
- The user shall be able to stop the active agent run.
- Glyph shall provide `read`, `write`, `edit`, `bash`, `grep`, `find`, and `ls` without extensions.
- Built-in tools shall execute without confirmation from the Glyph core.
- Extensions shall be able to intercept a tool call before execution and allow it, reject it, or change its input.
- Extensions shall be able to change a tool result before it is returned to the model.
- An extension shall be able to replace a built-in or extension tool by registering the same tool name.
- Extensions shall be able to observe, transform, or fully handle user text and images before agent processing.
- Glyph shall retain exclusive control of terminal input and rendering.
- Extensions shall add terminal UI elements only through Glyph extension contracts.
- Every keyboard shortcut shall be user-remappable.

### Extension Terminal UI

- Extensions shall be able to show list selection, confirmation, single-line input, multi-line editing, notifications, and host-rendered custom or overlay components.
- Extensions shall be able to provide statuses, working indicators, widgets, headers, footers, the terminal title, and the label for hidden reasoning content.
- Extensions shall be able to register renderers for tool calls, tool results, custom messages, and custom session entries.
- Extensions shall be able to inspect and change whether tool results are expanded.
- Extensions shall be able to receive raw terminal input forwarded by Glyph.
- Extensions shall be able to read, replace, and insert text in the active editor.
- Extensions shall be able to contribute editor autocomplete and replace the editor component.
- Extensions shall be able to enumerate and switch themes.
- Glyph shall retain ownership of terminal input dispatch and rendering while extensions use these capabilities.

### Models and Credentials

- Glyph shall provide an OpenAI Codex model provider with interactive OAuth authentication.
- Glyph shall persist OpenAI Codex OAuth credentials in a local credential file accessible only to the user running Glyph.
- Glyph shall provide an OpenAI-compatible model provider configured with a base URL, an optional API key, and an explicit model list.
- The OpenAI-compatible model provider shall support OpenAI Chat Completions and OpenAI Responses.
- Provider configuration shall select the OpenAI wire API through an explicit `api` field.
- A model configuration shall be able to override the provider's `api` value.
- The OpenAI-compatible API key shall come from an environment variable or the local credential file.
- Provider configuration shall not contain secret values.
- `/model` and `Ctrl+L` shall open model selection.
- `Ctrl+P` and `Shift+Ctrl+P` shall cycle through configured models in forward and reverse order.
- Model keyboard shortcuts shall be defaults that the user can remap.
- Changing the model shall preserve the session history and affect subsequent model requests.
- Selecting a model without available credentials shall fail immediately, preserve the active model, and show an error.

### Extensions

- A compatible extension shall be installable, enableable, disableable, and updateable without rebuilding Glyph.
- Glyph extension contracts shall support tools, commands, keyboard shortcuts, lifecycle events, system prompt changes, context transformations, terminal UI elements, sessions, and model access.
- Each extension point shall declare whether handlers can observe, block, modify, or replace the affected operation.
- Multiple transformations of the same operation shall run sequentially, with each handler receiving the valid result of the preceding handler.
- An extension shall be able to initiate and cancel session creation, resumption, forking, cloning, and tree navigation.
- After session replacement, the extension shall receive a context bound to the replacement session.
- An extension shall be able to persist model-hidden session data, add model-visible session messages, and associate persisted data with the active session branch.
- Extensions shall be able to exchange non-persistent events within the Glyph process.
- An extension shall be able to register and unregister a model provider, including its authentication, model catalogue, and streaming behavior.
- An extension shall be able to change provider request headers, replace the serialized provider request, and observe the provider response status and headers.
- An extension shall be able to contribute reusable instruction resources, prompt templates, and themes during startup and `/reload`.
- Installed extensions shall be trusted and shall run with the operating-system permissions of the Glyph process.
- Extensions shall receive events for agent start, agent end, agent settled, turn start, turn end, message start, message update, message end, tool execution start, tool execution update, tool execution end, model selection, and reasoning-level selection.
- An extension shall be able to replace a finalized message without changing its role.
- Multiple finalized-message replacements shall run sequentially.
- Glyph shall remain usable after an extension runtime failure.
- The operation using the failed extension shall end with an error.
- A failed extension shall remain unavailable until Glyph restarts.
- An ordinary extension handler error shall be reported while later handlers and the base operation continue and the extension remains active.
- An error in a pre-execution tool handler shall block that tool.
- An extension tool execution error shall become a model-visible error result, after which the agent shall continue.
- Glyph shall not require a security policy for trusted extensions.
- Glyph shall map every extension entry point declared in `pi-package/package.json` at `https://github.com/n-r-w/pi-agent-suite` to at least one Glyph extension contract without requiring an agent core change.

### Programmatic Agent Control

- Each programmatic command shall carry a correlation identifier.
- Glyph shall respond that a programmatic command was accepted or rejected independently from its later execution outcome.
- Glyph shall report execution progress and outcomes through asynchronous events associated with the controlled operation.
- Parent and child agents shall be able to send correlated requests to each other through the same logical control contract.
- The programmatic control contract shall not depend on a terminal user interface.

### Extension Run Control

- An extension shall be able to stop the active agent run.
- An extension shall be able to request context compaction.
- An extension shall be able to send a message as `steer`, `followUp`, or `nextTurn`.
- A `steer` message shall be delivered before the next model request after the current tools finish.
- A `followUp` message shall be delivered after the current agent run finishes.
- A `nextTurn` message shall be delivered with the next user turn.
- An extension tool shall be able to return `terminate`.
- Glyph shall skip the next automatic model request only when every completed result in the current tool batch contains `terminate`.
- `terminate` shall not interrupt another tool already running in the same batch.

### Environment Reload

- `/reload` shall reinitialize the Glyph environment without ending the current session.
- `/reload` shall run only while the agent is idle.
- Glyph shall reject `/reload` with a warning while a model response or context compaction is active.
- `/reload` shall reload settings, model providers, extensions, reusable instruction resources, prompt templates, themes, keyboard shortcuts, and context files.
- `/reload` shall preserve the current session and its history.
- `/reload` shall discard extension state held only in memory.
- `/reload` shall preserve extension state already stored in the session or files.
- When the new environment fails to load, Glyph shall preserve the session, report the error, and require an application restart.
- Glyph shall not restore the previous environment after a reload failure.
- After `/reload` or session replacement, the previous extension context shall become invalid and calls through it shall fail.
- Events and commands after `/reload` or session replacement shall receive a context bound to the active runtime and session.

### Context Management

- Glyph shall compact context automatically when the remaining model context cannot accommodate the next response budget.
- `/compact` shall trigger context compaction manually and accept user instructions for the summary.
- Context compaction shall replace earlier model context with a summary while retaining recent context.
- Extensions shall be able to replace the default compaction behavior.

### Sessions

- Glyph shall automatically save sessions and allow them to be resumed after restarting the application.
- Session entries shall form a tree with parent-child relationships and one active leaf.
- Navigating to an earlier entry and continuing shall create a new branch without deleting existing branches.
- `/new` shall start a new session.
- `/resume` shall select and continue a saved session.
- `/tree` shall navigate the complete session tree and continue from the selected position.
- `/tree` shall offer an optional summary of the branch being left.
- Session tree entries shall support user-defined labels.
- `/fork` shall create a separate session from an earlier user message.
- `/clone` shall create a separate session from the active branch.
- `/name` shall set a human-readable session name.
- `/session` shall show information about the active session.
- `/export` shall export a session as HTML or JSONL.
- `/share` shall publish the session as a private GitHub gist with a shareable HTML view.

## Open Questions

- Which operations shall the programmatic agent-control contract expose?
- Which parent operations shall a child agent be allowed to invoke?

## Acceptance Criteria

- On macOS and Linux, a user can authenticate or configure a supported provider, select a model, complete the standard coding-agent scenario, observe streamed model and tool progress, and stop an active run.
- A headless parent and child agent can exchange correlated commands and asynchronous operation events in both directions without a terminal user interface.
- The standard agent exposes `read`, `write`, `edit`, `bash`, `grep`, `find`, and `ls` without extensions and executes them without core confirmation.
- An extension can intercept a tool call, change its result, and replace its implementation without an agent core change.
- An extension can handle user input without starting an agent run.
- An extension can provide the approved dialogs, persistent terminal regions, transcript renderers, editor integration, raw-input handling, and theme control without taking terminal ownership.
- An extension can add a model provider, modify provider requests, observe provider responses, and contribute reusable instruction resources, prompt templates, and themes without an agent core change.
- An extension can observe the approved execution lifecycle and replace a finalized message without changing its role.
- Extension handler, pre-tool, tool-execution, and extension-runtime failures follow their approved distinct behaviors.
- An extension can control session transitions, chain transformations, persist branch-aware state, exchange process-local events, and use the approved run controls without an agent core change.
- A compatible extension can be installed and activated without rebuilding Glyph, and `/reload` applies environment changes while preserving the session.
- Every extension entry point declared in `pi-package/package.json` at `https://github.com/n-r-w/pi-agent-suite` maps to a public Glyph extension contract without requiring an agent core change.
- A user can resume a saved tree session, navigate to an earlier position, create another branch, and retain the original branch.
- A user can summarize and label session branches, compact context, export a session, and share it through a private GitHub gist.
- Model selection fails without credentials and does not change the active model.
- Reload and extension-runtime failures follow the requirements above without losing the session.
- Calls through an extension context from before `/reload` or session replacement fail, while later events and commands use the active context.
- All default keyboard shortcuts can be remapped.

## Technical Supplement

No technical solution is selected in this document. Runtime extension feasibility is analyzed in `docs/artefacts/go-extension-feasibility.md`.

## References

- `docs/specs/features/initial/problem.md`
- `docs/terms.md`
- `docs/artefacts/go-extension-feasibility.md`
- `https://github.com/n-r-w/pi-agent-suite`
- `@earendil-works/pi-coding-agent/docs/sessions.md`
- `@earendil-works/pi-coding-agent/docs/extensions.md`
