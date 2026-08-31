# Problem Statement

## Context

The issue was found while tracing branch summarization during session-tree navigation. That trace exposed blocking client command paths in the UI Plugin Contract and Programmatic Control. A review of the Extension Contract found a third lifecycle model for operations that cross a public Glyph process boundary.

## Problem Statement

Operations across the UI Plugin Contract, Extension Contract, and Programmatic Control do not share one asynchronous operation lifecycle. Some contract receivers execute operation work or wait for it before receiving later requests. Contract operations do not consistently expose identity, acceptance, running state, cancellation, terminal state, ownership, and joining.

## Who is affected

- Users of UI plugins, including the standard TUI.
- Programmatic controllers.
- Extension authors.
- Glyph developers who add or change public contract operations.

## Evidence

- `ui.Session.Run` receives UI commands in its main `select` loop and calls `applyCommand` synchronously.
- `ui.Session.navigateSessionTree` waits for `sessionControl.Navigate`, which can wait for extension handlers, branch summarization, a terminal model response, persistence, and post-commit observers.
- While navigation is active, `ui.Session.Run` cannot consume the next UI command. `CommandStop` therefore cannot cancel branch summarization through the ordinary command path.
- Programmatic Control holds `commandWork` while `handleRequest` calls `Service.Handle`. Inline session commands delay receipt and handling of later requests on that control stream.
- Agent runs use acceptance, asynchronous event delivery, cancellation, and terminal settlement. Other Programmatic Control commands return only after inline execution.
- The Extension Contract exposes unary `Register` and `Handle` operations. Host waits for each response before continuing the owning load or handler path.
- Extension Contract `Execute` streams progress and one terminal result, but it has no contract operation identifier, accepted state, or running state.
- The Extension Contract defines no common cancellation message or lifecycle shared by `Register`, `Handle`, and `Execute`.

## Impact

- One blocked contract operation can delay later requests on the same receiving path.
- Cancellation and shutdown may wait behind the operation they need to stop.
- Operation correlation, progress, cancellation, errors, and completion differ by contract and operation kind.
- Operation ownership and joining depend on individual implementations rather than one public contract rule.
- New contract operations can repeat the blocking behavior or add another lifecycle model.

## Reproduction Steps

1. Configure a model provider whose response is delayed.
2. Create a session branch that requires summarization.
3. Start session-tree navigation with summarization from the standard TUI.
4. Send `Stop` before the model returns.
5. Observe that the Host UI session does not process `Stop` until navigation returns.
6. Repeat through Programmatic Control and observe that `Service.Handle` does not return an asynchronous operation for navigation.
7. Register a delayed `session_before_tree` extension handler and observe that Host waits for the unary `Handle` response in the same navigation path.

## Current State

Agent runs have an explicit asynchronous lifecycle. Extension tool execution has progress and a terminal stream result. Other UI Plugin Contract, Extension Contract, and Programmatic Control operations use operation-specific synchronous or partially asynchronous behavior.

## Desired Outcome

A request that passes contract validation creates a contract operation with observable accepted, running, canceled, failed, or completed states. A contract receiver does not execute operation work or wait for it before receiving later requests. Each contract operation has explicit ownership, cancellation, and joining. Operation-specific ordering and state consistency remain defined by the owning domain.

## Success Metrics

- Each accepted contract operation reports acceptance before its work finishes, reports running, and reports exactly one terminal state.
- With a deliberately blocked contract operation, the receiver continues to receive later requests through the same contract connection.
- A cancellation request can reach its target operation while that operation is blocked.
- A canceled operation commits no state after its canceled terminal state.
- For each supported work-request direction, tests keep one operation running and verify that the receiver handles a later request before the first operation finishes.
- An inventory accounts for every operation exposed by the UI Plugin Contract, Extension Contract, and Programmatic Control.

## Scope

- Every request exposed by the UI Plugin Contract, Extension Contract, and Programmatic Control, in either supported direction.
- Contract operation identity, acceptance, running state, progress, cancellation, terminal state, ownership, and joining.
- Request receipt and result or event delivery while other contract operations are active.
- Operation-specific ordering, exclusivity, and atomic state commit rules where asynchronous execution affects them.

## Out of Scope / Non-Goals

- Making every Go function asynchronous.
- Requiring contract operations to execute concurrently.
- Replacing domain-specific ordering, exclusivity, or atomicity rules with one global policy.
- Changing external model-provider protocols.
- Adding progress content to operations that have no progress to report.
- Preserving earlier public contract shapes for backward compatibility.

## Constraints

- `context.Context` remains the cancellation mechanism for in-process calls.
- UI Plugin Contract, Extension Contract, and Programmatic Control may use different transport messages but must expose the same operation lifecycle semantics.
- Each accepted contract operation has one owner and a join path that completes before its owning connection or runtime closes.
- Asynchronous receipt and execution do not permit conflicting state mutations to commit concurrently.
- Navigation remains atomic and must not commit after cancellation or failure.

## Assumptions

- One lifecycle can represent unary and streaming contract operations without requiring identical payloads or progress events.
- Each contract transport can receive later requests while accepted operation work runs independently.

## Open Questions

None.
