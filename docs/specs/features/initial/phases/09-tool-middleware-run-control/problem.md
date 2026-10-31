# Problem Statement

## Key definitions and abbreviations

The [domain glossary](../../../../../terms.md) defines `agent run`, `turn`, `tool batch`, `active tool set`, `tool policy`, `queue mode`, `steer`, `followUp`, `nextTurn`, and `terminate`.

## Context

Glyph executes model-requested tools in extension processes that Host manages. [PHS-08](../08-prompt-context-input-provider-middleware/solution.md) added ordered handlers for model-facing data. One of them, the finalized-message handler, can already replace a tool result, including its contents and error flag. The [PHS-09 ticket](ticket.md) covers tool control and the course of an agent run.

## Observed Problem

Each problem below states the current behavior with its source and its consequences.

### PRB-01: An extension cannot allow, reject, or change one tool call before it runs

Current behavior: `tools.Service.Execute` in `host/internal/usecase/host/tools/service.go` checks only the tool name and the arguments against the schema, then calls the owning extension. The handler kinds (`HandlerKind` in `api/plugins/extension/v1/extension.proto`) contain no kind for a tool call. A finalized model-response handler can change call arguments, because `sameToolCallIdentity` in `host/internal/usecase/host/middleware/finalized.go` compares only call IDs and names. It cannot reject one call. A tool result contains only contents and an error flag, without an error category (`ToolResult` in `api/plugins/extension/v1/tool.proto` and `api/programmatic/v1/agent.proto`).

Consequences: a policy extension cannot block a dangerous call, for example `edit` of a protected file. The model and the client cannot tell a policy refusal from a tool failure.

### PRB-02: An extension cannot replace a bundled tool

Current behavior: when several extensions register a tool with the same name, `tools.Service.Conflicts` reports a conflict, and `startup.Service.Load` rejects the complete registration of each of these extensions, with all their other tools and handlers.

Consequences: an extension with its own `edit` disables the whole bundled tools extension. The agent loses `read`, `write`, `bash`, and the other bundled tools, and the extension's own `edit` does not load either.

### PRB-03: An extension cannot see the registered tools or change the active tool set

Current behavior: at the start of each turn, Agent Core (`run.Service.runTurn`) calls `tools.Service.Tools`. This method returns every registered tool whose extension is available. The Extension Contract and the SDK `ExtensionContext` have no operation to read this list or to change the set.

Consequences: an extension cannot hide tools for a mode, for example a read-only mode. Every model request contains all available tools.

### PRB-04: The calls of one tool batch run strictly one after another

Current behavior: `run.Service.executeCalls` runs the calls of one response sequentially, in model order.

Consequences: independent calls, for example three `read` calls, take the sum of their durations.

### PRB-05: After each tool batch, the agent always makes another model request

Current behavior: after a tool batch, `run.Service.endTurn` starts the next turn unless the agent run failed or was aborted. A tool result has no `terminate` field.

Consequences: when a tool result completes the task, the agent still makes one more model request, which costs tokens and time.

### PRB-06: During an agent run, nobody can give the agent a new instruction or assign it work for after the run

Current behavior: when a Programmatic Control client sends a message during an agent run, Host rejects it with `BUSY` (`runcontrol.Coordinator.PrepareRun`). A UI client receives `NOT_READY` or `BUSY`. An extension can only commit a model-visible message to history at once (`append_extension_message`). The model receives this message in the next model request of the current agent run. The extension cannot defer it until the run ends or until the next user message. No contract or package contains a queue of `steer`, `followUp`, or `nextTurn` messages or a queue mode.

Consequences: to give the agent a new instruction, the user must abort the agent run. The tool that runs at that moment gets an error result, and the remaining calls of its batch do not run. Work after the run, for example a check, needs a new user message. An extension cannot attach data to the next user message, because the model sees the data at once.

### PRB-07: An extension cannot abort an agent run

Current behavior: a client aborts an agent run by canceling its operation by ID. An extension can cancel only the operations that it started itself (`Connection.handleHostRequest` in `sdk/plugins/extension/v1/host_owner.go`).

Consequences: an extension that detects an agent run that takes too long or costs too much cannot stop it.

## Affected Audience

- Extension authors who control tools or the course of an agent run.
- Users of the standard coding agent with such extensions in every mode: Programmatic Control, the standard TUI, and one-shot `glyph run`.
- Programmatic Control clients: Host currently rejects their messages during an agent run with `BUSY`.

## Evidence

Each problem in [Observed Problem](#observed-problem) names the code that shows its current behavior.

## Impact

Impact is HIGH for each problem: users and extension authors see the consequences directly, and a rollback of the change removes them.

## Current State

Each problem in [Observed Problem](#observed-problem) describes its current behavior. The finalized-message handler from PHS-08 already changes tool results; each such handler receives the immutable original result and the current result.

## Desired State

- PRB-01: an extension decides on each call before it runs, and the model sees the reason for a refusal.
- PRB-02: an extension replaces one bundled tool while the other bundled tools keep working.
- PRB-03: an extension sees the registered tools and chooses which of them the model sees.
- PRB-04: independent calls of one tool batch do not wait for each other.
- PRB-05: the agent makes no extra model request when the tools of a batch have completed the task.
- PRB-06: the agent receives a new instruction during an agent run, work for after the run, and data for the next user message, without an abort.
- PRB-07: an extension can stop an agent run.

## Problem Boundary

The problems cover model-requested tool calls during an agent run, tool registration conflicts between extensions, and messages that extensions or clients send to the agent during an agent run or after it.

Queued-message presentation and editing in the standard TUI belong to [PHS-12.3](../12.3-standard-tui-editor-terminal-interaction/ticket.md). Extension commands, user interactions, resources, provider registration, and TUI contributions belong to later phases. Tool-result replacement exists since PHS-08, and manual compaction requests by extensions exist since PHS-06.

## Assumptions

None.

## Open Questions

None.
