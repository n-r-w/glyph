# Idea: Asynchronous contract operations

## Definitions

The terms `contract operation`, `operation initiator`, `operation receiver`, `operation owner`, `operation lifecycle`, and `operation progress` use the definitions in the [Domain Glossary](../../../terms.md).

## Context and Problem

The [Problem Statement](problem.md) describes blocking and inconsistent operation processing across the UI Plugin Contract, Extension Contract, and Programmatic Control.

## Goal

Give every work request across the three public contracts one asynchronous, correlated, cancelable, and owned operation lifecycle without imposing one concurrency policy on all operation kinds.

## Scenarios

- An operation initiator sends a work request. The operation receiver rejects it or reserves its immediate start, accepts it, reports running and one terminal state, and remains able to receive later requests while the accepted work runs.
- An operation initiator sends a cancellation operation for an active target. The cancellation operation completes after the target reaches a terminal state.
- A contract connection or extension runtime closes while operations bound to it remain active. Each operation owner cancels its operations and waits for their work to stop before closure completes.
- A Host operation invokes another operation through the Extension Contract. Each contract boundary applies the operation lifecycle to the work request it receives.

## Scope and Non-Scope

In scope:

- Work requests through the UI Plugin Contract, Extension Contract, and Programmatic Control in either direction.
- Removal of the UI Plugin Contract startup capability and Host terminal recovery path.
- Operation identity, rejection, acceptance, running state, operation progress, cancellation, terminal states, ownership, and waiting for operation work to stop.
- Operation receipt and lifecycle delivery while other operations remain active.

Out of scope:

- Treating connection establishment, connection closure, lifecycle messages, progress messages, or terminal messages as contract operations.
- Making internal Go calls asynchronous solely because they support a contract operation.
- Replacing operation-specific ordering, exclusivity, or atomicity rules with one global concurrency policy.
- Requiring progress content from an operation that has none to report.
- Preserving earlier public contract shapes for backward compatibility.

## Requirements

- The operation initiator shall assign every work request a nonempty `operation_id`. The initiator shall not send another request with an `operation_id` owned by a nonterminal operation on the same contract connection. A rejected request shall not reserve its `operation_id` or affect an accepted operation that already owns the same value.
  Justification: Responses and events from several requests can interleave and require stable correlation.
- Before acceptance, the operation receiver shall perform only these checks: nonempty `operation_id`, no nonterminal operation with the same `operation_id` on the connection, known operation kind, payload compliance with that operation kind's contract, and immediate admission under that operation kind's in-memory state rules.
  Justification: The implementer needs a closed boundary between request receipt and operation work.
- A pre-acceptance check shall not access storage, invoke another contract operation, start a process, use a network, or request a language model. Every such action belongs to accepted operation work.
  Justification: External or unbounded checking would reproduce blocking in the request-receipt path.
- When a pre-acceptance check fails, the receiver shall send one `rejected` response with the request's `operation_id`, emit no lifecycle event for that request, and keep the contract stream open. An empty `operation_id`, unknown operation kind, or invalid payload shall use code `INVALID_ARGUMENT`; an `operation_id` owned by a nonterminal operation shall use `OPERATION_ID_IN_USE`. When immediate admission is unavailable, the receiver shall reject the request rather than queue it and shall use the code defined by that operation kind.
  Justification: A rejected request does not create an operation, and an input error must not terminate unrelated operations on the connection.
- When all pre-acceptance checks pass, the receiver shall reserve immediate execution, become the operation owner, and send exactly one `accepted` event before operation work starts. The owner shall then start the work outside the request-receipt path and send exactly one `running` event when the work starts.
  Justification: `accepted` means the receiver owns an operation that will start without waiting for later admission.
- For each `operation_id`, the receiver shall preserve this event order: `accepted`, `running`, zero or more operation progress events, and exactly one `completed`, `canceled`, or `failed` terminal event. Events for different operations may interleave. No event for an operation shall follow its terminal event.
  Justification: Per-operation ordering makes interleaved asynchronous delivery unambiguous.
- The `rejected` response and every operation lifecycle event shall carry the request's `operation_id`.
  Justification: The initiator must correlate every response and event with one request.
- The request-receipt path shall return to receiving after it sends `rejected` or hands accepted work to its operation owner. It shall not wait for `running`, operation progress, cancellation, a terminal result, or owner cleanup.
  Justification: One blocked operation must not delay later requests on the same contract connection.
- `completed` shall mean that the operation finished successfully, produced its complete result, and completed all effects required before success is reported. `failed` shall mean that accepted operation work stopped because of an error. `canceled` shall mean that accepted operation work stopped because of cancellation. No operation shall produce an effect after its terminal event.
  Justification: Terminal events must describe observable outcomes and establish the point after which work has stopped.
- An operation progress event shall carry its operation's `operation_id` and intermediate fields defined by that operation kind's contract. It shall not change the operation state, replace the terminal result, or be required from an operation that has no intermediate information.
  Justification: Existing streamed updates remain correlated and ordered without forcing artificial progress.
- Cancellation shall be a work request with its own `operation_id` and one target `operation_id`. The receiver shall reject cancellation before acceptance when it does not own a nonterminal target with that `operation_id`.
  Justification: Cancellation must be addressable and follow the same lifecycle as other work requests.
- An accepted cancellation operation shall request cancellation of its target, wait for all target work to stop, and then report `completed` with the target's terminal state. A target that completes or fails before cancellation stops it shall retain that terminal state. A target reported as `canceled` shall produce no later result or effect.
  Justification: Cancellation completion must confirm the target's actual outcome rather than only confirm delivery of a cancellation signal.
- During requested closure of a contract connection or extension runtime, each operation owner bound to it shall cancel every owned nonterminal operation and wait for all owned work to stop before closure completes. After unexpected transport loss, cleanup shall perform the same cancellation and waiting even when terminal events cannot be delivered.
  Justification: Accepted work must not outlive the runtime responsible for it.
- The operation lifecycle shall not require two operations to execute at the same time. Each operation kind shall apply its own in-memory admission rules before acceptance and its own ordering, exclusivity, and atomicity rules during execution.
  Justification: Asynchronous receipt must not introduce conflicting work or replace operation-specific rules.
- UI Plugin Contract, Extension Contract, and Programmatic Control may use different transport messages, but each work request shall expose the identity, rejection, lifecycle order, terminal meanings, cancellation, and ownership behavior defined by this document.
  Justification: Transport differences must not change operation semantics.
- UI SDK and Extension SDK shall own their generated gRPC service, operation lifecycle, writer serialization, cancellation, and closure waiting. An external Go project shall implement only public contract-specific preparation and execution interfaces and shall receive no type from a Glyph internal package through an SDK API.
  Justification: External plugin developers must not reimplement transport concurrency or depend on Glyph implementation packages.
- UI Plugin Contract shall expose no `controls_terminal` field or `GetCapabilities` operation. Successful plugin protocol startup shall establish UI compatibility. Host shall not inspect, capture, reset, or restore terminal state; each UI plugin shall own the presentation resources it opens.
  Justification: The operation-stream replacement must implement the target UI ownership boundary without creating a temporary startup operation.
- For each direction that exposes work requests in the UI Plugin Contract, Extension Contract, or Programmatic Control, an integration test shall keep one accepted operation in `running`, send another request on the same connection, and observe `rejected` or `accepted` for the second request before releasing the first operation. Tests shall also verify targeted cancellation and closure with an active operation.
  Justification: This observation detects a blocked request-receipt path that function-level operation tests cannot detect.

## Open Questions

None.

## Technical Supplement

None.

## References

- [Problem Statement](problem.md)
- [Domain Glossary](../../../terms.md)
- [Target architecture](../../features/initial/architecture.md)
