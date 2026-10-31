# Idea: PHS-09 tool middleware and run control

## Definitions

The [domain glossary](../../../../../terms.md) defines `agent run`, `abort`, `turn`, `tool batch`, `active tool set`, `tool policy`, `queue mode`, `steer`, `followUp`, `nextTurn`, and `terminate`.

## Context and Problem

The [problem statement](problem.md) describes problems PRB-01 through PRB-07.

## Goal

Extension authors control tools and the course of an agent run, and users direct an agent run without aborting it.

## Affected Scenarios

- The model requests tools, and extensions check the calls.
- A user or an extension sends a message to the agent during an agent run.
- An extension replaces a bundled tool.
- An extension aborts an agent run.

## Scope and Non-Scope

In scope:

- Extensions: tool policy, tool replacement, the active tool set, the `terminate` flag, `steer`, `followUp`, `nextTurn`, and abort.
- Programmatic Control clients: `steer`, `followUp`, and queue mode selection.
- Concurrent execution of calls in one tool batch.

Out of scope:

- The UI Plugin Contract command and the standard TUI queue. [PHS-12.3](../12.3-standard-tui-editor-terminal-interaction/ticket.md) owns them.
- Calls from a replacing tool to the tool that it replaces.
- Persistence of the active tool set across restarts. An extension restores the set from its own persisted state, which the Extension Contract recovers since PHS-07.
- Tool-result changes by extensions. They exist since PHS-08.

## Requirements

### Functional Requirements

- [X] FRQ-01: Before a tool call runs, Host invokes extension handlers one by one in registration order. Each handler receives the immutable original call and the current call that preceding handlers returned. A handler allows the call, rejects it, or replaces its input. The tool runs with the final input.
  - Origin: source. Ticket FRQ-01; PRD, Agent and Tool Runtime.
  - Goal: PRB-01.
  - Goal achievement: Full. An extension decides on each call before it runs.
  - Feasibility: Confirmed. Host already invokes ordered handler chains with original and current values since PHS-08.
- [X] FRQ-02: When a handler rejects a call, the tool does not run and no later handler runs. The model and the client receive an error result with a Glyph category from a closed set and the complete text of the reason. The agent run continues.
  - Origin: source. Ticket FRQ-01.1 and ACC-01.
  - Goal: PRB-01.
  - Goal achievement: Full. The model sees the reason for the refusal, and the client tells a refusal from a failure.
  - Feasibility: Confirmed. Tool execution errors already reach the model as results.
- [X] FRQ-03: When a pre-execution handler or a tool-result handler returns an error or an invalid action, no later handler runs, and the agent run fails with the complete error text. Glyph performs no retry. A tool that has not run does not run. A tool that has already run does not run again.
  - Origin: source. Ticket FRQ-01.1, FRQ-02.1, and ACC-01.1; PRD, Extension failure rule.
  - Goal: PRB-01.
  - Goal achievement: Full. A failure of a policy extension lets no unchecked call through.
  - Feasibility: Confirmed. The same rule already applies to PHS-08 handlers.
- [X] FRQ-04: When a tool-result handler changes a tool result without replacing it completely, the error category and the `terminate` flag of the result stay unchanged.
  - Origin: source. Ticket FRQ-02.1.
  - Goal: PRB-01, PRB-05.
  - Goal achievement: Full. A change of the result text keeps the refusal reason and the completion flag.
  - Feasibility: Confirmed. Tool-result handlers already receive the original and the current result.
- [X] FRQ-05: At registration, an extension can declare that its tool replaces the tool with the same name from another extension. The model then sees and calls the replacing tool. The other tools and handlers of both extensions stay available. When two extensions declare a replacement of the same name, Host accepts neither replacement, keeps the original tool, and reports the conflict at startup.
  - Origin: source. PRD, Agent and Tool Runtime; ticket ACC-02; decision on the replacement rule.
  - Goal: PRB-02.
  - Goal achievement: Full.
  - Feasibility: Confirmed. Host already validates all registrations together before it accepts them.
- [X] FRQ-06: An extension can read the registered tools and the active tool set and can set a new active tool set. The new set applies from the next model request and does not change a request that was already sent.
  - Origin: source. Ticket FRQ-02 and ACC-03; PRD, Agent and Tool Runtime.
  - Goal: PRB-03.
  - Goal achievement: Full.
  - Feasibility: Confirmed. Agent Core already reads the tool list before each model request.
- [X] FRQ-07: At registration, a tool can declare that its calls may run concurrently. Consecutive calls of such tools in one tool batch do not wait for each other. A call of another tool waits for all preceding calls of the batch, and the following calls wait for it.
  - Origin: source. Ticket FRQ-03; decision on concurrent execution.
  - Goal: PRB-04.
  - Goal achievement: Full.
  - Feasibility: Confirmed. The extension connection already distinguishes concurrent operations by their identifiers.
- [X] FRQ-08: The results of a tool batch enter history and the next model request in the order of the calls in the model response, regardless of their completion order.
  - Origin: source. Ticket RSK-01.
  - Goal: PRB-04.
  - Goal achievement: Full. Concurrent execution does not change history.
  - Feasibility: Confirmed.
- [X] FRQ-09: An extension tool can return a result with the `terminate` flag. The agent run ends without another model request only when every result of the tool batch has the flag and no `steer` or `followUp` message is pending. The flag does not interrupt other calls of the batch that are still running.
  - Origin: source. PRD, Run Control; ticket ACC-04; glossary definitions of `steer` and `followUp`.
  - Goal: PRB-05.
  - Goal achievement: Full.
  - Feasibility: Confirmed.
- [X] FRQ-10: During an agent run, extensions and Programmatic Control clients can send `steer` and `followUp` messages to the agent. Extensions can send `nextTurn` messages both during an agent run and between agent runs. Agent Core delivers each message at the point that its glossary definition sets. When the model gives a final response without tool calls while a `steer` message is pending, the agent makes another model request with that message.
  - Origin: source. PRD, Run Control and Programmatic Control; decision on message senders.
  - Goal: PRB-06.
  - Goal achievement: Full.
  - Feasibility: Confirmed. Agent Core already reads history again before each model request.
- [X] FRQ-11: A Programmatic Control client selects the queue mode. In mode `all`, the agent delivers all pending messages of one kind at their delivery point; in mode `one-at-a-time`, it delivers one.
  - Origin: source. PRD, Programmatic Control; ticket ACC-05.
  - Goal: PRB-06.
  - Goal achievement: Full.
  - Feasibility: Confirmed.
- [X] FRQ-12: When an agent run is aborted or fails, pending `steer` and `followUp` messages do not reach the model. Host returns to each Programmatic Control client the pending messages that this client sent, so that the client can show them to the user again. Host discards the pending messages that extensions sent. Pending `nextTurn` messages stay.
  - Origin: formulated. Decision on undelivered messages.
  - Goal: PRB-06, PRB-07.
  - Goal achievement: Full. The user keeps the text, and the agent does not act on instructions sent before the abort.
  - Feasibility: Confirmed. Programmatic Control already reports the outcome of the operation that owns the agent run to its client.
- [X] FRQ-13: An extension can abort the current agent run. The result is the same as an abort by a client: the agent run ends with the aborted outcome.
  - Origin: source. PRD, Run Control; ticket FRQ-04.
  - Goal: PRB-07.
  - Goal achievement: Full.
  - Feasibility: Confirmed. Host already aborts an agent run on a client request.

### Non-Functional Requirements

- [X] NRQ-01: Agent Core does not depend on protobuf, gRPC, plugin SDKs, persistence adapters, or TUI packages.
  - Origin: source. Ticket NFQ-02; architecture CNS-03.
  - Goal: keep the architecture boundaries while Agent Core changes for FRQ-07 through FRQ-10.
  - Goal achievement: Full.
  - Feasibility: Confirmed. Agent Core currently imports only domain packages and Host ports.

Ticket NFQ-01 sets the verification order for the implementation; the plan covers it.

## Critical Scenarios

External scenarios observe their result through a Programmatic Control client: in agent-run events, in the outcome of the agent run, or in the request that the model provider receives.

- SCN-01: A policy changes the input and allows the call
  - Requirements: FRQ-01 (input replacement, allow; original and current call).
  - Level: external.
  - Area: Agent Core, Host, two policy extensions, the extension that owns the tool.
  - Flow: the model requests a tool. The first handler replaces the input. The second handler receives the original call and the call that the first handler changed, and allows it. The tool runs with the input from the first handler, and the model receives its result.
- SCN-02: A policy rejects a call while a `steer` message is pending
  - Requirements: FRQ-02; FRQ-10 (`steer` from a Programmatic Control client).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, Host, two policy extensions, model provider.
  - Flow: the model requests a call that the policy forbids. The client sends a `steer` message. The first handler rejects the call. The second handler is not invoked, and the tool does not run. The client sees an error result with the category and the complete text of the reason. The next model request contains this result and the `steer` message. The agent run continues, and the extension stays active.
- SCN-03: A pre-execution handler fails
  - Requirements: FRQ-03 (pre-execution handler).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, Host, policy extension, the extension that owns the tool.
  - Flow: the first handler returns an error. The tool does not run, and the second handler is not invoked. The agent run fails with the complete error text. No model request is repeated.
- SCN-04: A tool-result handler fails
  - Requirements: FRQ-03 (tool-result handler).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, Host, extension with a tool-result handler, the extension that owns the tool.
  - Flow: the tool runs, and the tool-result handler returns an error. The agent run fails with the complete error text. The tool ran exactly once.
- SCN-05: A tool-result handler changes a result
  - Requirements: FRQ-04.
  - Level: external.
  - Area: Agent Core, Host, policy extension, extension with a tool-result handler.
  - Flow: the tool-result handler changes the text of a rejected result and the text of a result with the `terminate` flag. The model and the client see the new text, while the error category and the `terminate` flag stay unchanged. When the handler replaces a result completely, the client sees the category and the flag of the replacement.
- SCN-06: An extension replaces a bundled tool
  - Requirements: FRQ-05 (one replacement).
  - Level: external.
  - Area: Host startup, the bundled tools extension, the replacing extension, model provider.
  - Flow: the extension declares a replacement of one bundled tool. The model request contains this tool once, with the description from the replacing extension. The replacing extension executes its calls. The bundled extension executes the other bundled tools.
- SCN-07: Two extensions replace the same tool
  - Requirements: FRQ-05 (conflicting replacements).
  - Level: external.
  - Area: Host startup, the bundled tools extension, two replacing extensions.
  - Flow: both extensions declare a replacement of the same bundled tool. Host reports the conflict at startup. The model sees the original tool, and the other tools and handlers of all three extensions work.
- SCN-08: An extension changes the active tool set
  - Requirements: FRQ-06.
  - Level: external.
  - Area: extension, Host, Agent Core, model provider.
  - Flow: the extension reads the registered tools and the active tool set. While a model request is in progress, it sets an active tool set without one of the tools. The request in progress contains the previous set, and the next request contains the new set.
- SCN-09: A concurrent tool batch
  - Requirements: FRQ-07, FRQ-08.
  - Level: external.
  - Area: Agent Core, Host, the extensions that own the tools, model provider.
  - Flow: the tool batch contains two calls of a tool that declared concurrent execution and two calls of a tool without the declaration. The first two calls run at the same time, and the second of them completes before the first. The other two calls run one at a time in model order. History and the next model request contain the results in call order.
- SCN-10: Every result of a tool batch has the `terminate` flag
  - Requirements: FRQ-09 (the agent run ends; the flag interrupts no other call).
  - Level: external.
  - Area: Agent Core, Host, the extension that owns the tool, model provider.
  - Flow: every call of the tool batch returns `terminate`. One call completes while another still runs, and the other call runs to its end. The agent run ends with the completed outcome without another model request.
- SCN-11: `terminate` does not end the agent run
  - Requirements: FRQ-09 (the flag is missing on one result; a `steer` message is pending).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, Host, model provider.
  - Flow: in the first case, one result of the tool batch lacks `terminate`, and the agent makes the next model request. In the second case, every result has `terminate`, but a `steer` message is pending, and the agent makes the next model request with it.
- SCN-12: Messages from an extension
  - Requirements: FRQ-10 (`steer`, `followUp`, and `nextTurn` from an extension).
  - Level: external.
  - Area: extension, Host, Agent Core, model provider.
  - Flow: while a tool runs, the extension sends `steer`, `followUp`, and `nextTurn` messages. The `steer` message enters the next model request. The model receives the `followUp` message when no tool calls or `steer` messages remain, and responds to it. The `nextTurn` message starts no agent run and enters the model request together with the next user message.
- SCN-13: Client `steer` and `followUp` after a final response
  - Requirements: FRQ-10 (`steer` after a final response; `followUp` from a Programmatic Control client).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, model provider.
  - Flow: while the model produces a final response without tool calls, the client sends a `steer` message and a `followUp` message. The agent makes a model request with the `steer` message and then, when no `steer` messages remain, a model request with the `followUp` message.
- SCN-14: Queue mode
  - Requirements: FRQ-11 (`all`, `one-at-a-time`).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, model provider.
  - Flow: in mode `one-at-a-time`, the client sends several `steer` messages, and each following model request contains one of them. In mode `all`, the client sends several `steer` messages, and the next model request contains all of them.
- SCN-15: An extension aborts an agent run with pending messages
  - Requirements: FRQ-12 (abort), FRQ-13.
  - Level: external.
  - Area: Programmatic Control client, extension, Host, Agent Core, model provider.
  - Flow: a `steer` message from the client, a `followUp` message from the extension, and a `nextTurn` message from the extension are pending. The extension aborts the agent run. The agent run ends with the aborted outcome. The client receives its `steer` message back. The model receives neither the `steer` message nor the `followUp` message. The `nextTurn` message enters the model request together with the next user message.
- SCN-16: An agent run fails with pending messages
  - Requirements: FRQ-12 (failure).
  - Level: external.
  - Area: Programmatic Control client, Agent Core, Host.
  - Flow: a `steer` message and a `followUp` message from the client are pending, and the agent run fails. The client receives both messages back, and the model receives neither.
- SCN-17: Agent Core dependencies
  - Requirements: NRQ-01.
  - Level: internal. Package dependencies are visible only in the source code.
  - Area: Agent Core packages.
  - Flow: after the changes of this phase, Agent Core imports no protobuf, gRPC, plugin SDK, persistence adapter, or TUI package.
- SCN-18: An extension sends `nextTurn` between agent runs
  - Requirements: FRQ-10 (`nextTurn` between agent runs).
  - Level: external.
  - Area: extension, Host, Agent Core, model provider.
  - Flow: after an agent run ends, the extension sends a `nextTurn` message while no agent run is active. No agent run starts. The next user message starts an agent run, and the model request contains the extension message together with the user message.

Requirements without a scenario: none.

## Open Questions

None.

## References

- [Ticket](ticket.md)
- [Problem statement](problem.md)
- [Target product requirements](../../prd.md), sections Agent and Tool Runtime, Run Control, and Programmatic Control
- [Target architecture](../../architecture.md)
