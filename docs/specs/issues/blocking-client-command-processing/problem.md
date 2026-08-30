# Problem Statement

## Context

The issue was found while tracing branch summarization during session-tree navigation. Summarization performs a model request before navigation can commit. The same navigation operation is exposed to the standard UI and Programmatic Control.

## Problem Statement

Glyph executes potentially long client operations inline in client command-processing paths. While such an operation waits for storage, extensions, a language model, or another external dependency, the affected command path cannot process later client commands or report an independent operation lifecycle.

## Who is affected

- Users of the standard TUI who start a long session operation.
- Programmatic controllers that issue session commands over the long-lived control stream.
- Glyph developers who add client operations without a clear rule for synchronous versus asynchronous execution.

## Evidence

- `ui.Session.Run` receives commands in its main `select` loop and calls `applyCommand` synchronously.
- `ui.Session.applySessionCommand` calls `navigateSessionTree` inline.
- `ui.Session.navigateSessionTree` waits for `sessionControl.Navigate`, which can wait for branch summarization and a terminal model response.
- While that call is active, `ui.Session.Run` cannot consume the next command from its command channel. The ordinary `CommandStop` path therefore cannot cancel branch summarization.
- `providers.Catalog.CompleteConfigured` calls `Provider.Stream` and returns only after a terminal provider event. Intermediate summary-generation events are not delivered to the client.
- Programmatic Control classifies session commands as immediate. `programmatic.Service.Handle` executes `navigateSessionTree` before returning a response or `controller.Operation`.
- The Programmatic Control receive loop holds `commandWork` while `handleRequest` calls `Service.Handle`. A blocking navigation delays receipt and handling of later commands on that control stream.
- Agent runs already use asynchronous operation lifecycles, which shows that blocking behavior is not required by the client transports.
- No audit has established which other session commands can block long enough to require an asynchronous lifecycle.

## Impact

- Navigation with summarization can appear frozen until the language model returns or the request context ends.
- The standard UI cannot process an ordinary stop command while the navigation handler is blocked.
- A programmatic controller cannot rely on immediate acceptance and later terminal events for long session operations.
- New client commands can repeat the problem because the codebase has no explicit classification rule for inline and asynchronous work.
- Cancellation, shutdown, and operation ownership become harder to reason about across clients.

## Reproduction Steps

1. Configure a model provider whose response is delayed.
2. Create a session branch with entries that require summarization.
3. Start session-tree navigation with summarization from the standard UI.
4. Before the model returns, send another UI command, including `Stop`.
5. Observe that the Host UI session does not process the command until navigation returns.
6. Repeat through Programmatic Control and observe that `Service.Handle` does not return an asynchronous operation for the navigation command.

## Current State

Agent runs have explicit acceptance, asynchronous event delivery, cancellation, and terminal settlement. Session commands are handled inline in both UI and Programmatic Control paths, even when they can reach persistence, extension handlers, or model execution.

## Desired Outcome

Client command processing remains responsive while potentially long operations execute. Each long operation has an observable accepted, running, canceled, failed, or completed lifecycle, and cancellation prevents a late state commit.

## Success Metrics

- With a deliberately blocked model request, the Host continues to receive and handle the defined cancellation and shutdown commands.
- Programmatic Control acknowledges every classified long operation before the operation finishes and reports its terminal result separately.
- Cancellation during branch summarization produces no navigation commit.
- Tests cover the command-processing loop while a long operation is blocked, rather than only testing the operation function in isolation.
- An inventory classifies every client command as bounded inline work or a potentially long operation.

## Scope

- Standard UI command dispatch and operation lifecycle.
- Programmatic Control command dispatch and operation lifecycle.
- Session-tree navigation with branch summarization.
- Other client session commands that can wait for storage, extensions, processes, networks, or language models.
- Cancellation, shutdown, result delivery, and atomic state commit behavior for long client operations.

## Out of Scope / Non-Goals

- Making every Go function asynchronous.
- Running multiple session mutations concurrently.
- Changing provider streaming protocols.
- Changing agent-loop behavior that already has an asynchronous operation lifecycle.
- Adding progress content when an operation only needs acceptance and a terminal result.

## Constraints

- `context.Context` remains the cancellation mechanism for in-process operations.
- Session mutation remains serialized through the existing operation gate.
- Navigation remains atomic and must not commit after cancellation or failure.
- UI and Programmatic Control may use different transport messages but must expose equivalent operation semantics.
- No detached goroutine may outlive its owning client session without an explicit owner and join path.

## Assumptions

- Branch summarization is a confirmed blocking case.
- Persistence and extension-backed session commands may expose the same problem. The client command inventory must verify which commands require an asynchronous lifecycle.

## Open Questions

None.
