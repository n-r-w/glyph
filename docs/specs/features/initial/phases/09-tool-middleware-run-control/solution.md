# Technical Solution: PHS-09 tool middleware and run control

## Problem Statement

The [problem statement](problem.md) describes problems PRB-01 through PRB-07, and the [requirements](prd.md) define FRQ-01 through FRQ-13, NRQ-01, and critical scenarios SCN-01 through SCN-18. The [domain glossary](../../../../../terms.md) defines the run-control terms.

## Proposed Solution

### D1. Tool policy through a tool-call handler chain

This method serves FRQ-01, FRQ-02, and FRQ-03.

The PHS-08 middleware owner in `host/internal/usecase/host/middleware` gets a new handler kind for tool calls. A handler allows the call, replaces its input, or rejects it with a reason. Model selection in `host/internal/usecase/host/modelselection` already implements the same preserve, replace, and reject-with-reason pattern, so the new chain follows it.

`tools.Service.Execute` invokes the chain after it resolves the tool and before it validates the final arguments against the schema and calls the owning extension. This order matches STP-06 in the [architecture](../../architecture.md). The `tools` package declares the chain interface, and the middleware owner implements it. Agent Core does not change for tool policy.

- Rejection: `tools` returns an error result with the policy-rejection category and no Go error, so the agent run continues. The result text contains the complete reason; D2 describes how the model receives the category.
- Handler failure or invalid action: `tools` returns an error that implements the existing `run.FatalOperationFailure` marker with category `EXTENSION_FAILED`. Agent Core already fails the agent run on this marker.
- Arguments: Host validates each input replacement against the tool schema before the next handler runs. A replacement that violates the schema is an invalid action, so no later handler runs and the agent run fails, as FRQ-03 requires. A later handler therefore cannot repair the input of an earlier one. When no handler replaced the input, Host validates the original arguments after the chain, and the model receives the existing invalid-arguments result when they violate the schema.

Rejected method: validating only the final input after the whole chain. A schema-invalid replacement would then reach later handlers, which contradicts the FRQ-03 rule that an invalid action stops the chain.

### D2. Error category and the `terminate` flag in tool results

This method serves FRQ-02, FRQ-04, and FRQ-09.

The domain tool result gets an error category from a closed set and the `terminate` flag. Host assigns the category: policy rejection, unavailable tool, invalid arguments, or tool failure. Tool failure covers both an error result that the tool reported and an error result that Agent Core creates from an execution error, for example a lost connection to the extension process, because the model and the client see the same event. Results that Agent Core creates for skipped calls have no category. The owning extension sets `terminate` in its result. The Programmatic Control tool result exposes both values, and the Extension Contract tool result carries `terminate`. Session persistence keeps the category, so `get_messages` shows it after a restart. The `terminate` flag affects only the current agent run and is not persisted.

The existing PHS-08 finalized-message handler is the tool-result handler. A second result chain in `tools`, as STP-07 describes, would duplicate it, so STP-07 changes in the architecture. The finalized-message payload of a tool result carries the category and `terminate`. A replacement is always complete: a handler that copies the current value and changes only the text keeps the category and the flag, as FRQ-04 requires. Host checks that the category of a replacement belongs to the closed set.

The model receives the category through Host: when Host prepares the provider-neutral model request, before model-context handlers run, it adds the category to the model-visible text of each error result that has one. The model therefore sees the category whatever text a tool-result handler wrote, and provider adapters stay unchanged.

Rejected method: storing the category only inside the result text. Provider adapters send only result contents to the model, so a tool-result handler that rewrites the text would remove the category, and FRQ-02 would not hold.

The UI Plugin Contract gets no separate category field, because the standard TUI has no consumer for it yet. TUI users see the error flag without the category in this phase.

### D3. Tool replacement

This method serves FRQ-05.

A tool descriptor gets a replacement declaration. `tools.Service` resolves name collisions as follows:

- one ordinary descriptor and one declared replacement: the replacement wins, and the original tool is hidden;
- two declared replacements of the same name: Host rejects both replacements, keeps the original tool, and reports the conflict;
- two descriptors without a declaration: Host keeps the current conflict behavior and rejects both registrations completely;
- a declared replacement without an ordinary descriptor of that name, also after an ordinary conflict rejected the other registrations: Host accepts the replacement as an ordinary tool.

No rule depends on load order.

`startup.Service.Load` currently rejects the complete registration for every conflict that `tools.Service.Conflicts` reports. Startup therefore separates two kinds of findings: a rejected replacement, which removes only that tool and keeps the rest of the registration, and an ordinary conflict, which rejects complete registrations as today. Without this separation SCN-07 fails, because the replacing extensions would lose their other tools and handlers.

### D4. Active tool set

This method serves FRQ-06.

`tools.Service` keeps the set of active tool names in process memory. All tools are active by default. `Tools()` returns the tools that are available and active. Two new extension context operations read the registered tools with their active state and set the active tool set; an unknown name rejects the whole operation. Both operations use the ordinary context admission without the operation gate, so they work during an agent run, also from inside a tool or a handler of the calling extension.

Agent Core already reads the tool list once at the start of each turn, and retries and overflow recovery do not read it again. A new set therefore applies from the next model request without another mechanism. A call to an inactive tool, for example from older history, receives the unavailable-tool result.

### D5. Concurrent tool batches

This method serves FRQ-07 and FRQ-08.

A tool descriptor gets a concurrency declaration. Agent Core splits a tool batch in model order into groups: consecutive calls of concurrent tools form one group, and each other call forms its own group. Groups run one after another, and the calls of one group run at the same time. For example, the batch `read a`, `read b`, `edit a`, `read a` runs both first reads together, then the edit, then the last read, which sees the edit.

When a tool of a group finishes, its result passes the tool-result handler chain at once, in the work of that call. The chains of different calls of one group can therefore run at the same time, and a handler failure cancels the other calls of the group without delay. The execution-end event of a call carries its finalized result, as it does today.

Rejected method: running the result-handler chains of one group one after another. A handler failure would be detected later, so sibling calls could finish before cancellation, and events would no longer follow the actual work of each call.

Inside a group, execution-start, progress, and execution-end events follow the actual work of each call. History appends and `tool_result` events follow call order after the group completes. This keeps history in call order and still shows progress as it happens. Agent Core delivers events one at a time, because the one-shot `glyph run` renderer (`headless.Renderer`) is not safe for concurrent calls.

When one call of a group stops the agent run by a fatal failure, a handler failure, or an abort, Agent Core cancels the other calls of the group, waits for them, appends the results that completed and passed their result-handler chains in call order, and ends the agent run.

Host runtime management and the extension SDK already admit several concurrent operations in one extension process, and the bundled tools extension does not limit them. The bundled tools extension declares `read`, `grep`, `find`, and `ls` as concurrent.

### D6. Message queue in Agent Core

This method serves FRQ-09, FRQ-10, FRQ-11, and FRQ-12.

Agent Core owns the queue, as ticket DLV-02 and [PHS-12.3](../12.3-standard-tui-editor-terminal-interaction/ticket.md) require. The queue holds `steer`, `followUp`, and `nextTurn` messages and the queue mode.

Run control in `host/internal/usecase/host/runcontrol` accepts every queued message and every abort request, from Programmatic Control clients and from extensions. It already starts and settles agent runs, owns input processing before Agent Core, and consumes Agent Core through its own interface, which Agent Core implements. Run control implements the interfaces that the Programmatic Control use case and the extension controller declare for these operations, and passes messages and abort requests to Agent Core through its interface to Agent Core. Run control already implements the interface of the one-shot controller in `host/internal/controller/cli/headless` in the same way.

The extension controller in `host/internal/controller/extension` currently imports Agent Core only to recognize the fatal-failure marker `run.FatalOperationFailure`. It is the only controller in `host/internal` that imports a use-case package, which APC-20 forbids, and this import would close the cycle `runcontrol` → `controller/extension` → `run` → `runcontrol`. The controller therefore declares its own interface with the same method, as CNS-05 requires for every consumer and as the same file already does for nested extension failures. Without this import, the extension controller depends only on domain packages.

Rejected method: moving the marker into the shared error package `internal/errtree`. It would also remove the import, but the marker has two consumers, Agent Core and the extension controller, and CNS-05 assigns an interface to each consumer.

Rejected method: declaring the enqueue interface in `host/internal/usecase/host/extensioncontext`. Agent Core would import that package to implement the interface, and the imports would form a cycle: `run` imports `extensioncontext`, which imports `modelexecution`, which imports `run`. A separate Host component for the queue is also rejected, because its only work would duplicate the input processing and Agent Core access that run control already owns.

At a delivery point, Agent Core appends a client message through its existing history store as a user message. For an extension message, Agent Core calls a delivery interface that Agent Core declares and the extension context owner in `host/internal/usecase/host/extensioncontext` implements. That owner performs the incarnation and runtime checks described below, takes the runtime commit guard, and calls the existing extension-message append of the session owner. Agent Core therefore needs no knowledge of extension messages, and history keeps the sender of each message.

Rejected method: the session owner implements the delivery interface. The session owner cannot tell whether an extension runtime instance is still current; today `extensioncontext` gives it the runtime commit guard.

Delivery points in the Agent Core loop:

- After all groups of a tool batch, and after a final model response: when every result has `terminate` and no `steer` message is pending, or when the model gave a final response, Agent Core checks `steer` messages first and `followUp` messages second. Otherwise, pending `steer` messages are delivered before the next turn.
- At the start of a new agent run: Agent Core appends pending `nextTurn` messages to history before the user message.

Delivery appends the message to history. The model receives it in the next model request, because Agent Core reads history again before each turn.

A message can arrive while the agent run is ending. Agent Core checks the queue and moves the run to completion under one lock, so no accepted message is lost. After that transition, and while no agent run is active, enqueueing a `steer` or `followUp` message fails with an explicit error. A `nextTurn` message waits for the next user message and needs no agent run, so Host accepts it also between agent runs, for example from an observer of the settled agent run, while the extension context is valid.

On abort or failure, Agent Core discards pending `steer` and `followUp` messages. It passes the client's messages in the `agent_end` event, in the form in which the client sent them, before input processing, so that the client can show the user the same text again, and Programmatic Control exposes them in the agent-run summary that precedes the operation terminal event. One `glyph` process has at most one Programmatic Control client, so all client messages belong to that client.

The queue mode is one process setting for `steer` and `followUp` messages, as the glossary defines it. The default is `one-at-a-time`.

A client message persists as a user message. A message from an extension keeps its extension origin, and the session owner persists it as the same extension message that `append_extension_message` creates today.

An immediate extension message requires a live invocation of the extension and the current session incarnation. A queued extension message is delivered later, when that invocation may have ended. When Host accepts it into the queue, Host records the session incarnation and the extension runtime instance. At delivery, the session owner persists the message without a live invocation if both are still current, and Host discards the message otherwise. The session incarnation changes when a session is created, resumed, forked, or cloned; tree navigation keeps it, so a pending `nextTurn` message survives navigation. During an agent run no session can change, so in practice this rule affects `nextTurn` messages.

Rejected method: carrying pending `nextTurn` messages into another session. Data about one session would then reach the model in another session, while every other extension context in Glyph is bound to one session.

### D7. Programmatic Control commands

This method serves FRQ-10, FRQ-11, and FRQ-12.

Programmatic Control gets two commands: enqueue a `steer` or `followUp` message with the same text and image content as `user_request`, and select the queue mode. Both commands bypass the operation gate, as `set_retry_enabled` does. The enqueue command completes when the queue accepts the message, not when the agent delivers it.

Before run control enqueues a client message, the message passes the same input processing as `user_request`: user-input handlers, finalized-message handlers, and validation against the input modalities of the selected model. When a user-input handler fully handles the message, Host does not enqueue it, and the command result states this. Messages from extensions skip input processing, as `append_extension_message` does today.

### D8. Extension operations for messages and abort

This method serves FRQ-10 and FRQ-13.

`append_extension_message` gets a delivery point: immediate (the current behavior), `steer`, `followUp`, or `nextTurn`. An immediate append keeps its current result with the committed entry. A queued append completes when the queue accepts the message, with a separate "queued" result that holds no entry, because the message is not persisted yet.

Rejected method: completing a queued append only after delivery. A `steer` message sent from inside a tool would never arrive: delivery waits for the tool to finish, and the tool waits for the operation.

A new extension context operation aborts the current agent run; run control accepts it as described in D6. Agent Core gets its own cancel function for the current run, because no Host component holds such a handle today. The operation completes when Agent Core accepts the cancellation and does not wait for the run to end, so an extension can call it from inside its own tool without a deadlock.

The Programmatic Control operation for `user_request` currently selects the canceled terminal state only when its own operation context is canceled (`programmatic.runPrepared.Run`); an abort by an extension would report a completed operation. Programmatic Control therefore also selects the canceled state when the agent run ends with the aborted outcome, and names the party that aborted it. The UI operation already selects the canceled state from a cancellation error without its own context being canceled (`ui.preparedUIOperation.Run`), so it keeps its current mapping. One-shot `glyph run` already treats every outcome other than completed as a command error.

### D9. Boundaries and documents

This method serves NRQ-01.

Agent Core gets only interfaces with domain types. New protocol types stay in the public contracts and in infrastructure adapters. The architecture update changes CMP-03, CMP-08, STP-06, STP-07, and APC-08, extends the folder-structure description of `middleware` with tool-call handlers, and adds the reference extensions for tool policy, tool override, and terminating output in `testdata/external-plugins` (ticket DLV-03) to the folder tree.

### Scenario coverage

| Scenarios | Methods |
| --- | --- |
| SCN-01 through SCN-04 | D1 |
| SCN-05 | D2 |
| SCN-06, SCN-07 | D3 |
| SCN-08 | D4 |
| SCN-09 | D5 |
| SCN-10, SCN-11 | D2, D6 |
| SCN-12 through SCN-16, SCN-18 | D6, D7, D8 |
| SCN-17 | D9 |

## Overengineering and Overspecification Considerations

Each added element has a reason:

- Agent Core delivers events one at a time, because the one-shot renderer is not safe for concurrent calls.
- Agent Core checks the queue and completes the run under one lock, because messages arrive from other goroutines while the run ends.
- `tools` rejects single replacements instead of complete registrations, because FRQ-05 keeps the other tools of both extensions.
- Agent Core holds its own cancel function, because no other component can abort the current run.
- Host records the session incarnation and the runtime instance of a queued extension message, because a pending `nextTurn` message can outlive a session change or an extension restart.
- Host adds the category to model-visible text at request preparation, because tool-result handlers can rewrite the result text.

The solution adds no separate tool-result chain, no queue-change events, and no category field in the UI Plugin Contract.

The implementer decides field names, category spellings, rejection codes, and the reference extension layout.

## Open Questions

None.

## References

- [Ticket](ticket.md)
- [Target architecture](../../architecture.md), CMP-08, STP-06, STP-07, APC-08, NFQ-03
- [PHS-08 technical solution](../08-prompt-context-input-provider-middleware/solution.md)
- `host/internal/usecase/agent/run`: Agent Core loop, tool execution, and history appends
- `host/internal/usecase/host/tools`: tool registry, ownership, and execution
- `host/internal/usecase/host/middleware`: ordered handler chains
- `host/internal/usecase/host/programmatic` and `host/internal/controller/programmatic`: Programmatic Control commands and terminal outcomes
- `host/internal/controller/extension`: extension context operations
