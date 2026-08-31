# Problem Statement

## Context

The issue was found while tracing branch summarization during session-tree navigation. The trace exposed a broader command-dispatch problem in the standard UI and Programmatic Control: client command receivers execute command work before they resume receiving commands.

## Problem Statement

Glyph client command receivers can execute command work inline. Until that work returns, the affected receiver cannot process later client commands. Client commands therefore lack one asynchronous operation lifecycle that is independent of command receipt.

## Who is affected

- Users of the standard TUI.
- Programmatic controllers that issue commands over the long-lived control stream.
- Glyph developers who add or change client commands.

## Evidence

- `ui.Session.Run` receives commands in its main `select` loop and calls `applyCommand` synchronously.
- `ui.Session.applySessionCommand` calls `navigateSessionTree` inline.
- `ui.Session.navigateSessionTree` waits for `sessionControl.Navigate`, which can wait for branch summarization and a terminal model response.
- While that call is active, `ui.Session.Run` cannot consume the next command from its command channel. The ordinary `CommandStop` path therefore cannot cancel branch summarization.
- `providers.Catalog.Request` calls `Provider.Stream` and returns only after a terminal provider event. Intermediate summary-generation events are not delivered to the client.
- Programmatic Control classifies session commands as immediate. `programmatic.Service.Handle` executes `navigateSessionTree` before returning a response or `controller.Operation`.
- The Programmatic Control receive loop holds `commandWork` while `handleRequest` calls `Service.Handle`. A blocking navigation delays receipt and handling of later commands on that control stream.
- Agent runs already use asynchronous operation lifecycles, which shows that blocking behavior is not required by the client transports.
- Other session commands execute inline and can wait for session storage or extension-backed work.

## Impact

- Any inline command can delay later commands for the same client session.
- The standard UI cannot process an ordinary stop command while inline navigation is blocked.
- Programmatic Control cannot acknowledge an inline command independently from its execution result.
- New client commands can repeat the problem because command receipt and command execution are not separated by one enforced contract.
- Cancellation, shutdown, and operation ownership differ across command paths and clients.

## Reproduction Steps

1. Configure a model provider whose response is delayed.
2. Create a session branch with entries that require summarization.
3. Start session-tree navigation with summarization from the standard UI.
4. Before the model returns, send another UI command, including `Stop`.
5. Observe that the Host UI session does not process the command until navigation returns.
6. Repeat through Programmatic Control and observe that `Service.Handle` does not return an asynchronous operation for the navigation command.

## Current State

Agent runs have explicit acceptance, asynchronous event delivery, cancellation, and terminal settlement. Session commands are handled inline in both UI and Programmatic Control paths. Client commands therefore use different dispatch and lifecycle behavior.

## Desired Outcome

A client command receiver does not execute operation work or wait for it. Each client operation has observable accepted, running, canceled, failed, or completed states. The receiver remains able to receive later commands while earlier operations run, and cancellation prevents a late state commit.

## Success Metrics

- With a deliberately blocked client operation, the Host continues to receive later commands for that client session.
- The standard UI and Programmatic Control report acceptance before execution finishes and report running and exactly one terminal state separately for every accepted client command.
- Cancellation during branch summarization produces no navigation commit.
- Tests cover each client command-processing loop while an operation is blocked, rather than only testing the operation function in isolation.
- No client command executes its operation inline in a client command receiver.

## Scope

- Standard UI command dispatch and operation lifecycle for every client command.
- Programmatic Control command dispatch and operation lifecycle for every client command.
- Session-tree navigation with branch summarization.
- Cancellation, shutdown, result delivery, and atomic state commit behavior for client operations.

## Out of Scope / Non-Goals

- Making every Go function asynchronous.
- Requiring client operations to execute concurrently.
- Running multiple session mutations concurrently.
- Changing provider streaming protocols.
- Changing agent-loop behavior that already has an asynchronous operation lifecycle.
- Adding progress content when an operation only needs acceptance and a terminal result.

## Constraints

- `context.Context` remains the cancellation mechanism for in-process operations.
- Session mutation remains serialized through the existing operation gate.
- Navigation remains atomic and must not commit after cancellation or failure.
- UI and Programmatic Control may use different transport messages but must expose equivalent operation semantics.
- Every accepted client operation has an owner and a join path within its client session.

## Assumptions

- Branch summarization is a confirmed blocking case.
- Asynchronous command receipt and execution do not permit two client operations to mutate session state at the same time.

## Open Questions

None.
