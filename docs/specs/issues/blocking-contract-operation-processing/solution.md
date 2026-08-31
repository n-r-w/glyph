# Technical Solution: Asynchronous contract operations

## Problem Statement

- PRB-01: The [Problem Statement](problem.md) and [PRD](prd.md) define blocking and inconsistent operation processing across the UI Plugin Contract, Extension Contract, and Programmatic Control.

## Proposed Solution

### Solution overview

- SOL-01: Replace the operation-bearing RPC shapes in all three public contracts with one bidirectional `Open` stream per contract.
- SOL-02: Add one shared protobuf package for lifecycle states and one transport-neutral Go runtime for operation ownership, sequence validation, cancellation, and closure.
- SOL-03: Keep request, progress, and completed-result payloads inside their owning UI, Extension, or Programmatic Control contract.
- SOL-04: Split every receiving path into bounded preparation and asynchronous execution. Preparation performs only contract validation and in-memory admission; execution owns all storage, process, network, model, and nested contract work.
- SOL-05: Use one bounded writer queue and one writer goroutine per stream. Request receipt and operation execution never call gRPC `Send` directly.
- SOL-06: Remove the synchronous and partially asynchronous command paths. No forwarding method, compatibility field, or adapter preserves an earlier public contract shape.

### Shared lifecycle contract

- APC-01: Add `api/operation/v1/operation.proto` with Go package `github.com/n-r-w/glyph/pkg/operation/v1;operationv1` and generate `pkg/operation/v1`.
- APC-02: The package defines these messages and enum:

```proto
message Accepted {}
message Running {}
message Rejected {
  optional string code = 1;
}
message Canceled {}
message Failed {
  optional string code = 1;
}
message CancelOperation {
  optional string target_operation_id = 1;
}
enum TerminalState {
  TERMINAL_STATE_UNSPECIFIED = 0;
  TERMINAL_STATE_COMPLETED = 1;
  TERMINAL_STATE_CANCELED = 2;
  TERMINAL_STATE_FAILED = 3;
}
message CancelCompleted {
  optional TerminalState target_state = 1;
}
```

- APC-03: `Rejected.code` and `Failed.code` are nonempty machine codes. They contain no user-facing message. Each operation kind defines a closed set of accepted codes beside its request and result contract.
- APC-04: The common rejection codes are `INVALID_ARGUMENT`, `OPERATION_ID_IN_USE`, `BUSY`, `TARGET_NOT_ACTIVE`, and `NOT_READY`. Operation-specific contracts add codes only for domain failures that cannot use one of these values.
- APC-05: The common failed code is `INTERNAL`. Operation-specific contracts define all classified failure codes. The receiving adapter logs the underlying error with `operation_id`, operation kind, peer kind, and failure code; it sends no error text through the lifecycle message.

### Contract stream shape

Each contract defines one typed envelope for each direction. `OpenRequest` is sent to the gRPC service implementation, and `OpenResponse` is sent by that implementation. Either envelope can initiate work with its request payload or report lifecycle events for work requested through the opposite envelope.

```proto
message OpenRequest {
  optional string operation_id = 1;
  oneof content {
    HostRequest request = 2;
    glyph.operation.v1.Accepted accepted = 3;
    glyph.operation.v1.Running running = 4;
    HostProgress progress = 5;
    HostCompleted completed = 6;
    glyph.operation.v1.Canceled canceled = 7;
    glyph.operation.v1.Failed failed = 8;
    glyph.operation.v1.Rejected rejected = 9;
  }
}
```

- APC-06: `HostRequest`, peer request messages, progress messages, and completed messages use contract-owned typed `oneof` fields. The contracts use no `google.protobuf.Any`.
- APC-07: Every envelope carries `operation_id`. A cancellation request uses its envelope identifier for the cancellation operation and `CancelOperation.target_operation_id` for the target.
- APC-08: Replace the services with these stream methods:

| Contract | RPC |
|---|---|
| UI Plugin Contract | `UIService.Open(stream OpenRequest) returns (stream OpenResponse)`; Host sends `OpenRequest`, UI sends `OpenResponse` |
| Programmatic Control | `ProgrammaticControlService.Open(stream OpenRequest) returns (stream OpenResponse)`; controller sends `OpenRequest`, Host sends `OpenResponse` |
| Extension Contract | `ExtensionService.Open(stream OpenRequest) returns (stream OpenResponse)`; Host sends `OpenRequest`, extension sends `OpenResponse` |

- APC-09: Remove `UIService.GetCapabilities`, `ExtensionService.Register`, `ExtensionService.Handle`, and `ExtensionService.Execute`. Their semantic requests become typed operation kinds on `Open`.
- APC-10: Connection establishment, stream closure, request envelopes, lifecycle envelopes, and terminal envelopes do not recursively create operations. Only a typed request payload creates a contract operation.
- DEC-08: Removed protobuf fields and field numbers are reserved, and new envelope fields use the next unused numbers. The contracts retain no compatibility payload or fallback decoder.
- DEC-09: `sdk/plugins/ui/v1.ProtocolVersion` and `sdk/plugins/extension/v1.ProtocolVersion` change from 1 to 2. A process using protocol version 1 is rejected during plugin negotiation.

### Lifecycle sequence

- EVC-01: A successfully prepared request produces exactly one `Accepted`, exactly one `Running`, zero or more contract-owned progress events, and exactly one completed payload, `Canceled`, or `Failed`.
- EVC-02: `Rejected` is a response to a request that did not create an operation. It is not a lifecycle event and does not reserve `operation_id`.
- EVC-03: Per-operation order is strict. Events for different `operation_id` values can interleave in writer-queue order.
- EVC-04: The runtime rejects empty identifiers, unknown operation kinds, invalid payloads, unavailable admission, and duplicate active identifiers without closing the stream. A duplicate rejection does not change the accepted operation that owns that identifier.
- EVC-05: Progress payload fields are defined by the operation kind. Progress cannot change operation state or replace the completed payload.
- EVC-06: No event or operation effect occurs after a terminal event. Operation-specific atomic commit rules determine whether cancellation wins before the terminal event.

### Transport-neutral Go runtime

- CMP-01: Add repository-root package `internal/operation`. It imports no protobuf, gRPC, Host, UI, Extension, Programmatic Control, or plugin SDK package and is not part of the public Go API.
- CMP-02: `internal/operation.Owner[P, R]` owns accepted operations for one contract connection. It stores nonterminal operations by `operation_id`, derives their contexts from the connection context, cancels a target by identifier, and waits for all work during closure.
- CMP-03: `internal/operation.Tracker` owns operations initiated on the connection. It validates `Rejected`, lifecycle ordering, progress placement, terminal uniqueness, and unknown identifiers before delivering typed payloads to the initiator.
- CMP-04: `internal/operation.Prepared[P, R]` is the consumer-owned work interface used by `Owner`. It exposes `Run(context.Context, Reporter[P]) Outcome[R]` and `Release()`. In-repository implementations include compile-time interface assertions.
- CMP-05: `Release` frees the in-memory admission reservation exactly once after `Run` returns or when accepted delivery fails before `Run` starts.
- CMP-06: `Outcome[R]` has constructors for completed, canceled, and failed outcomes. A failed outcome contains its machine code and internal Go error. The runtime sends only the code and logs the error through the receiving adapter.
- CMP-07: `Reporter[P]` enqueues typed progress and returns a delivery error. A delivery error cancels the connection and all operations bound to it.

### Public plugin SDKs

- CMP-08: `sdk/plugins/ui/v1` and `sdk/plugins/extension/v1` own their generated gRPC server implementations, stream writer, `internal/operation.Owner`, and `internal/operation.Tracker`.
- APC-11: Replace `sdk/plugins/ui/v1.Serve(uiv1.UIServiceServer)` and `sdk/plugins/extension/v1.Serve(extensionv1.ExtensionServiceServer)` with `Serve` functions that accept SDK-owned public service interfaces.
- APC-12: Extension SDK public interfaces expose contract-specific preparation and execution for Register, Handle, and Execute operations. UI SDK public interfaces expose capabilities, initialization, Host-request handling, UI-request initiation, and lifecycle delivery.
- APC-13: SDK public interfaces use only SDK-owned and generated public types. No exported field, method parameter, method result, embedded interface, or generic constraint references `internal/operation` or another Glyph internal package.
- CMP-09: Each SDK has a private adapter from its public prepared-operation and outcome types to `internal/operation.Prepared`, `Reporter`, and `Outcome`. External plugin code implements no generated gRPC server, operation registry, stream writer, cancellation dispatcher, or closure coordinator.
- DEC-10: An external project can implement the protobuf and gRPC contract without an SDK. Host protocol validation remains authoritative for such implementations.

### Operation receipt and execution

- STP-01: The stream receiver decodes one envelope. A protobuf or transport receive failure follows the stream failure mapping below.
- STP-02: The contract adapter validates envelope presence, `operation_id`, operation kind, and payload fields. It asks the operation-specific consumer to perform bounded in-memory admission and return `Prepared`.
- STP-03: The adapter uses `Owner` to reserve the identifier and admission result. It enqueues `Rejected` on failure or `Accepted` on success.
- STP-04: After `Accepted` is enqueued, `Owner` starts a tracked goroutine. The goroutine enqueues `Running`, calls `Prepared.Run`, enqueues the selected terminal event, calls `Release`, and removes the operation.
- STP-05: The receive loop returns to `Recv` after STP-03 and never waits for STP-04.
- STP-06: Cancellation follows STP-02 through STP-05. Its run function cancels the target context, waits for the target terminal state, and completes with `CancelCompleted` containing that state.
- STP-07: On requested closure, the receiver stops accepting requests, cancels all owned operations, waits for their work, drains outbound terminal events, and closes the stream. On transport loss, it cancels and waits without requiring terminal delivery.

### Stream writer

- CMP-10: Each `Open` implementation owns one writer goroutine and one outbound channel with capacity 256. The capacity is an internal constant, not a public contract limit.
- CMP-11: Event producers map their typed payload before enqueueing and use a nonblocking enqueue. The writer is the only goroutine that calls gRPC `Send` and sends messages in channel order.
- FLR-01: A full outbound channel terminates the stream with gRPC `ResourceExhausted`, cancels all bound operations, and waits for their work. It never blocks request receipt or grows memory without a bound.
- FLR-02: A writer `Send` failure preserves its gRPC status when present and otherwise maps to `Unavailable`. It cancels every operation bound to the stream and waits for all operation work to stop.
- FLR-03: Requested closure drains queued messages after operation work stops. Transport failure and queue overflow do not drain because delivery is unavailable.

### Stream failure mapping

| Source condition | Per-request result | Stream result | Active operations |
|---|---|---|---|
| Empty `operation_id`, unknown operation kind, or invalid payload | `Rejected` with `INVALID_ARGUMENT` | Remains open | No operation is created; accepted operations continue |
| Identifier owned by a nonterminal operation | `Rejected` with `OPERATION_ID_IN_USE` | Remains open | The accepted operation with that identifier continues |
| Operation-specific admission unavailable | `Rejected` with `BUSY`, `NOT_READY`, or the operation's closed code | Remains open | No operation is created; accepted operations continue |
| Cancellation target is not owned and nonterminal | `Rejected` with `TARGET_NOT_ACTIVE` | Remains open | No operation is created; accepted operations continue |
| Accepted operation returns classified error | `Failed` with the operation's closed code | Remains open | Operation removed after delivery |
| Accepted operation returns unclassified error | `Failed` with `INTERNAL` | Remains open | Operation removed after delivery |
| Incoming lifecycle event violates order or references an unknown identifier | None | `FailedPrecondition` | Canceled and joined |
| Request envelope has no request kind or has invalid request fields | `Rejected` with `INVALID_ARGUMENT` | Remains open | No operation is created; accepted operations continue |
| Incoming lifecycle envelope has invalid fields | None | `FailedPrecondition` | Canceled and joined |
| gRPC `Recv` cannot decode a protobuf frame | None | Incoming status or `InvalidArgument` | Canceled and joined |
| Local envelope mapping invariant fails | None | `Internal` | Canceled and joined |
| Outbound queue is full | None | `ResourceExhausted` | Canceled and joined |
| Stream transport fails | None | Incoming status or `Unavailable` | Canceled and joined |
| Requested connection or runtime closure | Terminal delivery when transport remains available | Clean closure | Canceled and joined |

### Startup operations

- ALG-01: UI startup uses the opened stream in this order:
  1. Host sends `GetCapabilities` as an operation and waits for its completed capabilities payload.
  2. Host sends `Initialize` as an operation and waits for completed acknowledgement.
  3. Host and UI mark the connection ready for ordinary operation kinds.
- ALG-02: Extension startup uses the opened stream in this order:
  1. Host sends `Register` as an operation and waits for its completed registration payload.
  2. Host validates the returned tool and handler registration.
  3. Host marks the extension runtime ready for `Handle` and `Execute`.
- FLR-04: Before startup completes, an ordinary request receives `Rejected` with `NOT_READY`. A cancellation request for the active startup operation remains allowed.
- FLR-05: A rejected, canceled, failed, or protocol-invalid startup operation closes the plugin stream and marks the plugin unavailable. No compatibility fallback RPC is attempted.
- DEC-01: Programmatic Control has no startup operation. A successfully opened stream can receive controller work requests immediately.

### Operation mapping

- CMP-12: UI commands become `UIRequest` variants. `StopCommand` is replaced by `CancelOperation`. Host command results become `HostCompleted` variants, and Host lifecycle updates tied to the command become `HostProgress` variants.
- CMP-13: Programmatic commands become `ControllerRequest` variants. `correlation_id` is replaced by envelope `operation_id`, and `Abort` is replaced by `CancelOperation`. Query and mutation responses become `HostCompleted` variants.
- CMP-14: Agent-run events become operation progress. Host settlement completes before the owner sends the operation terminal event. Public `AgentSettled` is removed because the terminal event now means that operation work and settlement have stopped.
- CMP-15: `Register`, session-tree request and result handlers, observers, and tool execution become `HostRequest` variants in the Extension Contract. Tool progress becomes `ExtensionProgress`; registration, handler actions, and tool results become `ExtensionCompleted` variants.
- DEC-02: An ordinary extension handler error remains a completed handler result because Host handler composition continues. A handler transport or protocol failure becomes operation `Failed`.
- DEC-03: A tool result with `is_error` remains completed operation data because Agent Core consumes it as a model-visible tool result. Extension transport, protocol, or runtime failure becomes operation `Failed`.
- CMP-16: The standard TUI assigns an identifier to every UI request and stores the identifier of its foreground operation. The stop action sends a new cancellation operation targeting that foreground identifier.
- CMP-17: Programmatic controllers assign identifiers and target any owned nonterminal operation explicitly. `RunStateResult` reports the active agent-run operation identifier when one exists.
- CMP-18: A Host operation that invokes an Extension Contract operation assigns a new identifier, tracks it through `Tracker`, and sends cancellation for that identifier when the parent in-process context is canceled. No public parent identifier is added.

### Admission and domain ordering

- DEC-04: `internal/operation.Owner` is not a global concurrency gate. It only owns lifecycle state.
- DEC-05: Operation-specific consumers retain their admission rules. Agent runs and session mutations reserve their domain gate during preparation and release it through `Prepared.Release`.
- DEC-06: Snapshot reads require no domain gate and can execute while a mutation runs. Their result is the snapshot taken by their operation implementation.
- DEC-07: Storage, extension calls, process work, network work, model requests, and credential access occur only inside `Prepared.Run`.

### TDD and verification

Implementation follows RED, GREEN, REFACTOR, and VERIFY. Each RED test must compile and fail through its expected assertion rather than a timeout.

- TSK-01: Generate the common lifecycle package and replaced contract packages as compile setup. Run `task generate` twice and require no second-run diff before behavioral RED tests import the generated API.
- TSK-02: Add `internal/operation` unit tests. Purpose: prove acceptance order, progress order, terminal uniqueness, targeted cancellation, release, and closure waiting. Inputs: prepared operations controlled by channels. Expected outputs: exact ordered states and joined work. Edge cases: cancellation before `Run`, commit winning a cancellation race, delivery failure, duplicate identifier rejection, and unexpected transport cleanup. Dependencies: no gRPC or production use case.
- TSK-03: Update Programmatic Control tests. Purpose: prove all command kinds use operations. Inputs: one blocked command followed by a query and cancellation. Expected outputs: the second request receives `Rejected` or `Accepted` before release, cancellation completes after the target terminal state, and connection closure joins work. Edge cases: malformed requests, duplicate identifiers, failed operations, writer overflow, and send failure. Dependencies: generated contract, mocked consumer, and `internal/operation`.
- TSK-04: Update UI runtime and standard TUI tests. Purpose: prove capabilities and initialization startup operations, typed command lifecycle, foreground cancellation, and terminal rendering. Inputs: real UI stream with one blocked Host operation. Expected outputs: ordered startup, later request receipt, and joined shutdown. Edge cases: startup cancellation, `NOT_READY`, invalid lifecycle order, and UI process exit. Dependencies: generated contract and UI SDK.
- TSK-05: Update Extension runtime and bundled tools tests. Purpose: prove Register, Handle, Execute, progress, cancellation, and process-exit ownership on one stream. Inputs: real test extension with blocked handler and tool operations. Expected outputs: another request is received before release and cancellation joins the target. Edge cases: ordinary handler error as completed data, tool `is_error` as completed data, protocol failure as failed operation, registration failure, queue overflow, and runtime exit. Dependencies: generated contract and Extension SDK.
- TSK-06: Add integration tests for every implemented work-request direction. Purpose: detect blocked receipt at real gRPC boundaries. Inputs: keep one operation in `Running`, send a second request on that operation's contract connection, then cancel and close. Expected outputs: the second request receives `Rejected` or `Accepted` before the first is released, the target reaches one terminal state, and no goroutine survives closure. Edge cases: cancellation racing with completion and clean versus failed transport closure. Dependencies: production controllers and real local gRPC streams. These tests use `//go:build integration` and run through `task itest`.
- TSK-07: Add a separate-module integration fixture for an external extension and external UI plugin. Purpose: prove that each plugin compiles and runs through public contract and SDK packages without importing Glyph internal packages or implementing generated gRPC services. Inputs: minimal plugin services in a module with a non-Glyph import path. Expected outputs: startup operations complete and one ordinary operation reaches a terminal state. Edge cases: cancellation and plugin shutdown. Dependencies: generated contracts and public SDKs.
- TSK-08: After each GREEN slice, refactor while focused tests remain green. Final verification runs `task generate` twice, `task fmt`, `task fix_dry_run`, reviews the proposed fixes, runs `task fix` or applies the fixes manually, then runs `task lint`, `task test`, `task itest`, and `git diff --check`.

### Affected areas

- CMP-19: `api/operation/v1`, `pkg/operation/v1`, and `internal/operation` add the shared lifecycle contract and private runtime.
- CMP-20: `api/plugins/ui/v1`, `pkg/plugins/ui/v1`, `sdk/plugins/ui/v1`, Host UI runtime, and the standard TUI adopt the UI operation stream.
- CMP-21: `api/programmatic/v1`, `pkg/programmatic/v1`, Programmatic controller, use case, app lifecycle, and fixtures adopt the Programmatic operation stream.
- CMP-22: `api/plugins/extension/v1`, `pkg/plugins/extension/v1`, `sdk/plugins/extension/v1`, Host Extension runtime, Extension service, and bundled tools extension adopt the Extension operation stream.
- CMP-23: Host agent-run event delivery, session control, model selection, authentication, and operation admission return prepared operations instead of executing work in receive paths.
- CMP-24: Target architecture, affected phase solutions, issue index, and roadmap link to this solution as the owner of public contract operation lifecycle semantics rather than duplicating them.

## Overengineering and Overspecification Considerations

- TRD-01: One stream per public contract replaces four incompatible RPC patterns. It does not add a broker, daemon, persistence queue, reconnection protocol, or distributed operation store.
- TRD-02: `internal/operation` holds only in-memory state for one local connection. Operation state is not persisted because operations cannot outlive their owning connection or runtime.
- TRD-03: Request, progress, and completed payloads remain contract-specific and typed. The solution adds no `Any`, reflection registry, generic payload codec, or shared business-command model.
- TRD-04: The lifecycle requires concurrent receipt, not concurrent execution. Domain admission remains the only source of execution exclusivity.
- TRD-05: The bounded writer queue fails a slow connection instead of adding an unbounded queue, disk buffering, priority scheduler, or replay.
- TRD-06: No public parent-operation graph is added. In-process context ownership and initiator tracking provide cancellation propagation for nested contract calls.

## Open Questions

None.

## References

- REF-01: [Problem Statement](problem.md) - approved problem and scope.
- REF-02: [PRD](prd.md) - approved requirements.
- REF-03: [Domain Glossary](../../../terms.md) - operation terminology.
- REF-04: [Target architecture](../../features/initial/architecture.md) - Host, Agent Core, client, and plugin ownership boundaries.
- REF-05: [Programmatic Control solution](../../features/initial/phases/02-programmatic-control/solution.md) - implemented Programmatic transport and ownership baseline.
- REF-06: [Session tree solution](../../features/initial/phases/05-session-tree/solution.md) - implemented navigation, extension handler, and atomic commit behavior.
- REF-07: [Initial product requirements](../../features/initial/prd.md) - external extension and UI plugin development requirements.
