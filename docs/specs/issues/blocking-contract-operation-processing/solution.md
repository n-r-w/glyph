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
- SOL-07: Remove the UI startup capability and Host terminal recovery paths. UI plugin startup establishes compatibility through protocol negotiation and `Initialize`.

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
message CloseConnection {}
```

- APC-03: `Rejected.code` and `Failed.code` are nonempty machine codes. They contain no user-facing message. Each operation kind defines a closed set of accepted codes beside its request and result contract.
- APC-04: The common rejection codes are `INVALID_ARGUMENT`, `OPERATION_ID_IN_USE`, `BUSY`, `TARGET_NOT_ACTIVE`, and `NOT_READY`. Operation-specific contracts add codes only for domain failures that cannot use one of these values.
- APC-05: The common failed code is `INTERNAL`. Operation-specific contracts define all classified failure codes. The receiving adapter logs the underlying error with `operation_id`, operation kind, peer kind, and failure code; it sends no error text through the lifecycle message.

### Contract stream shape

Each contract defines one typed message for each direction. `OpenRequest` is sent to the gRPC service implementation, and `OpenResponse` is sent by that implementation. Either message can initiate work with its request payload or report lifecycle events for work requested through the opposite message.

- APC-06: Each stream message has one `oneof content` with the request, event, connection-event, or close types defined for that direction. Operation event messages contain shared lifecycle types and contract-owned progress or completed types. The contracts use no `google.protobuf.Any`.
- APC-07: A request or operation event message requires a nonempty `operation_id`. A cancellation request uses its message identifier for the cancellation operation and `CancelOperation.target_operation_id` for the target. A connection event or close message has no `operation_id`.
- APC-07.1: A connection event enters neither `Owner` nor `Tracker`, produces no operation lifecycle, and never receives a synthetic identifier.
- APC-08: Replace the services with these stream methods:

| Contract | RPC |
|---|---|
| UI Plugin Contract | `UIService.Open(stream OpenRequest) returns (stream OpenResponse)`; Host sends `OpenRequest`, UI sends `OpenResponse` |
| Programmatic Control | `ProgrammaticControlService.Open(stream OpenRequest) returns (stream OpenResponse)`; controller sends `OpenRequest`, Host sends `OpenResponse` |
| Extension Contract | `ExtensionService.Open(stream OpenRequest) returns (stream OpenResponse)`; Host sends `OpenRequest`, extension sends `OpenResponse` |

- APC-09: Remove `UIService.GetCapabilities`, `ExtensionService.Register`, `ExtensionService.Handle`, and `ExtensionService.Execute`. `Register`, `Handle`, and `Execute` become typed operation kinds on `Open`; no operation replaces `GetCapabilities`.
- APC-10: Connection establishment, stream closure, connection events, stream messages, lifecycle messages, and terminal messages do not recursively create operations. Only a typed request payload creates a contract operation.
- DEC-08: Removed protobuf fields and field numbers are reserved, and new stream-message fields use the next unused numbers. The contracts retain no compatibility payload or fallback decoder.
- DEC-08.1: Retained messages reserve both the numeric ranges and the field names they replace:
  - UI `OpenRequest` reserves 1 through 15 and `initialization`, `lifecycle`, `authorization`, `information`, `error`, `model_selection_changed`, `session_list`, `session_changed`, `session_information`, `session_tree`, `session_tree_navigation`, `session_tree_failed`, `session_forked`, `session_cloned`, and `entry_label_set`.
  - UI `OpenResponse` reserves 1 through 16 and `submit`, `stop`, `retry_authentication`, `quit`, `select_model`, `select_reasoning_choice`, `create_session`, `list_sessions`, `resume_session`, `set_session_name`, `get_session_info`, `get_session_tree`, `navigate_session_tree`, `fork_session`, `clone_session`, and `set_entry_label`.
  - Programmatic `OpenRequest` reserves 1 through 20 and `correlation_id`, `user_request`, `abort`, `get_run_state`, `get_messages`, `get_models`, `select_model`, `select_reasoning_choice`, `create_session`, `list_sessions`, `resume_session`, `set_session_name`, `get_session_info`, `get_session_entries`, `get_session_stats`, `get_session_tree`, `navigate_session_tree`, `fork_session`, `clone_session`, and `set_entry_label`.
  - Programmatic `OpenResponse` reserves 1 through 3 and `correlation_id`, `command_response`, and `agent_event`.
- DEC-08.2: Programmatic `AgentEventType` reserves value 17 and `AGENT_EVENT_TYPE_AGENT_SETTLED`. UI `LifecycleType` reserves values 11 and 12 plus `LIFECYCLE_TYPE_AGENT_SETTLED` and `LIFECYCLE_TYPE_AVAILABILITY_CHANGED`. Programmatic `RunStateResult` reserves field 2 and `active_correlation_id`, then adds `active_operation_id = 3`.
- APC-14: Target stream-message fields are fixed as follows:

| Contract message | New fields |
|---|---|
| UI `OpenRequest` | `operation_id = 16`, `HostRequest request = 17`, `HostEvent event = 18`, `HostConnectionEvent connection_event = 19`, `CloseConnection close = 20` |
| UI `OpenResponse` | `operation_id = 17`, `UIRequest request = 18`, `UIEvent event = 19`, `CloseConnection close = 20` |
| Programmatic `OpenRequest` | `operation_id = 21`, `ControllerRequest request = 22` |
| Programmatic `OpenResponse` | `operation_id = 4`, `HostEvent event = 5`, `CloseConnection close = 6` |
| Extension `OpenRequest` | `operation_id = 1`, `HostRequest request = 2`, `CloseConnection close = 3` |
| Extension `OpenResponse` | `operation_id = 1`, `ExtensionEvent event = 2` |

- APC-15: New request `oneof` fields are fixed as follows:
  - UI `HostRequest`: `initialize = 1`, `cancel = 2`.
  - UI `UIRequest`: `submit = 1`, `cancel = 2`, `retry_authentication = 3`, `select_model = 4`, `select_reasoning_choice = 5`, `create_session = 6`, `list_sessions = 7`, `resume_session = 8`, `set_session_name = 9`, `get_session_info = 10`, `get_session_tree = 11`, `navigate_session_tree = 12`, `fork_session = 13`, `clone_session = 14`, `set_entry_label = 15`.
  - Programmatic `ControllerRequest`: `user_request = 1`, `cancel = 2`, `get_run_state = 3`, `get_messages = 4`, `get_models = 5`, `select_model = 6`, `select_reasoning_choice = 7`, `create_session = 8`, `list_sessions = 9`, `resume_session = 10`, `set_session_name = 11`, `get_session_info = 12`, `get_session_entries = 13`, `get_session_stats = 14`, `get_session_tree = 15`, `navigate_session_tree = 16`, `fork_session = 17`, `clone_session = 18`, `set_entry_label = 19`.
  - Extension `HostRequest`: `register = 1`, `handle = 2`, `execute = 3`, `cancel = 4`.
- APC-16: UI `HostEvent`, Programmatic `HostEvent`, and `ExtensionEvent` use `accepted = 1`, `running = 2`, `progress = 3`, `completed = 4`, `canceled = 5`, `failed = 6`, and `rejected = 7`. UI `UIEvent` uses `accepted = 1`, `running = 2`, `completed = 3`, `canceled = 4`, `failed = 5`, and `rejected = 6`. Exhaustive mapping switches reject absent and unknown variants.
- APC-17: Completed and progress `oneof` fields are fixed as follows:
  - UI `HostProgress`: `agent_event = 1`, `authorization = 2`. UI `HostCompleted`: `cancel = 1`, `submit = 2`, `authentication = 3`, `model_selection = 4`, `session_changed = 5`, `session_list = 6`, `session_information = 7`, `session_tree = 8`, `session_tree_navigation = 9`, `session_forked = 10`, `session_cloned = 11`, `entry_label_set = 12`. UI `UICompleted`: `initialized = 1`, `cancel = 2`.
  - Programmatic `HostProgress`: `agent_event = 1`. Programmatic `HostCompleted`: `user_request = 1`, `cancel = 2`, `run_state = 3`, `messages = 4`, `models = 5`, `model_selection = 6`, `session_info = 7`, `sessions = 8`, `session_entries = 9`, `session_stats = 10`, `session_tree = 11`, `session_tree_navigation = 12`, `fork_session = 13`, `clone_session = 14`, `set_entry_label = 15`.
  - Extension `ExtensionProgress`: `tool = 1`. Extension `ExtensionCompleted`: `register = 1`, `handle = 2`, `tool = 3`, `cancel = 4`.
- APC-18: UI `HostConnectionEvent` uses `information = 1`, `error = 2`, and `availability_changed = 3`. `Information` and `Error` retain their current payloads. `AvailabilityChanged` contains `optional Availability availability = 1`. Agent lifecycle values are operation progress, while idle extension-process failure is an `Error` connection event.
- APC-18.1: `Initialized`, `SubmitCompleted`, `AuthenticationCompleted`, and `UserRequestCompleted` are empty acknowledgement messages. `CancelOperation.target_operation_id`, `CancelCompleted.target_state`, `Rejected.code`, `Failed.code`, and `AvailabilityChanged.availability` must be present and non-default. A completed or progress variant must match the request kind tracked for its `operation_id`; a mismatch is `FailedPrecondition` for the stream.
- DEC-09: `sdk/plugins/ui/v1.ProtocolVersion` and `sdk/plugins/extension/v1.ProtocolVersion` change from 1 to 2. The handshake cookie values become `glyph-ui-v2` and `glyph-extension-v2`; plugin names remain `glyph-ui` and `extension`. A process using protocol version 1 is rejected during plugin negotiation.

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
- CMP-03: `internal/operation.Tracker` owns operations initiated on the connection. It validates `Rejected`, lifecycle ordering, progress placement, terminal uniqueness, and unknown identifiers, then writes each event to the operation's bounded inbound queue. A full inbound queue fails the connection with `ResourceExhausted` instead of blocking stream receipt.
- CMP-04: `internal/operation.Prepared[P, R]` is the consumer-owned work interface used by `Owner`. It exposes `Run(context.Context, Reporter[P]) Outcome[R]` and `Release()`. In-repository implementations include compile-time interface assertions.
- CMP-05: `Release` frees the in-memory admission reservation exactly once after `Run` returns or when accepted delivery fails before `Run` starts.
- CMP-06: `Outcome[R]` has constructors for completed, canceled, and failed outcomes. A failed outcome contains its machine code and internal Go error. The runtime sends only the code and logs the error through the receiving adapter.
- CMP-07: `Reporter[P]` enqueues typed progress and returns a delivery error. A delivery error cancels the connection and all operations bound to it. Every `Prepared.Run` implementation must return after its context is canceled; closure waits rather than detaching work that ignores cancellation.

### Public plugin SDKs

- CMP-08: `sdk/plugins/ui/v1` and `sdk/plugins/extension/v1` own their generated gRPC server implementations, stream writer, `internal/operation.Owner`, and `internal/operation.Tracker`.
- APC-11: Replace `sdk/plugins/ui/v1.Serve(uiv1.UIServiceServer)` and `sdk/plugins/extension/v1.Serve(extensionv1.ExtensionServiceServer)` with `Serve` functions that accept the SDK-owned interfaces below.

```go
// Extension SDK API.
type Service interface {
    PrepareRegister(context.Context, *extensionv1.RegisterRequest) (RegisterOperation, error)
    PrepareHandle(context.Context, *extensionv1.HandleRequest) (HandleOperation, error)
    PrepareExecute(context.Context, *extensionv1.ExecuteRequest) (ExecuteOperation, error)
}
type RegisterOperation interface {
    Run(context.Context) (*extensionv1.RegisterResponse, error)
    Release()
}
type HandleOperation interface {
    Run(context.Context) (*extensionv1.HandleResponse, error)
    Release()
}
type ExecuteOperation interface {
    Run(context.Context, *ProgressReporter) (*extensionv1.ToolResult, error)
    Release()
}
type ProgressReporter struct { /* SDK-owned state */ }
func (*ProgressReporter) Report(context.Context, *extensionv1.ToolProgress) error
```

```go
// UI SDK API.
type Service interface {
    PrepareInitialize(context.Context, *uiv1.Initialization) (InitializeOperation, error)
    Run(context.Context, *Host) error
}
type InitializeOperation interface {
    Run(context.Context) (*uiv1.Initialized, error)
    Release()
}
type Host struct { /* SDK-owned state */ }
func (*Host) Start(context.Context, string, *uiv1.UIRequest) (*Operation, error)
func (*Host) Cancel(context.Context, string, string) (*Cancellation, error)
func (*Host) Close(context.Context) error
type Operation struct { /* SDK-owned state */ }
func (*Operation) Wait(context.Context, func(*uiv1.HostProgress)) (*uiv1.HostCompleted, error)
type Cancellation struct { /* SDK-owned state */ }
func (*Cancellation) Wait(context.Context) (*operationv1.CancelCompleted, error)

// Both SDK packages export the same error surface.
func Reject(string) error
func Fail(string, error) error
type RejectionError struct { /* SDK-owned state */ }
func (*RejectionError) Code() string
type FailureError struct { /* SDK-owned state */ }
func (*FailureError) Code() string
func (*FailureError) Unwrap() error
type CanceledError struct { /* SDK-owned state */ }
func (*CanceledError) Unwrap() error
```

- APC-12: Each `Prepare` method performs bounded validation and in-memory admission. It returns `Reject(code)` to produce `Rejected`; every other nonnil preparation error is a connection failure. The SDK calls `Run` only after delivery of `Accepted` and calls `Release` exactly once.
- APC-12.1: `Run` returns a completed payload and `nil` for `Completed`, `context.Canceled` for `Canceled`, `Fail(code, err)` for classified `Failed`, or another error for `Failed` with `INTERNAL`. `Reject` and `Fail` are SDK constructors whose concrete error types expose their machine code through `errors.As`.
- APC-12.2: UI SDK calls `Service.Run` once after `Initialize` completes and its terminal message is delivered. `Host.Start` registers the caller-provided operation identifier before queueing the request, then returns SDK-owned state without waiting for operation acceptance. `Operation.Wait` delivers ordered progress on its calling goroutine and returns the completed payload or `RejectionError`, `CanceledError`, or `FailureError`. `Host.Cancel` creates a separate operation using the caller-provided cancellation identifier. `Host.Close` or return from `Service.Run` starts normal UI connection closure.
- APC-13: SDK public interfaces use only standard-library, SDK-owned, and generated public types. No exported field, method parameter, method result, embedded interface, or generic constraint references `internal/operation` or another Glyph internal package. SDK-defined interfaces are consumed by the SDK; objects consumed by plugin code, including `Host` and `ProgressReporter`, are concrete SDK types.
- CMP-09: Each SDK privately implements the generated gRPC server and adapts its public interfaces to `internal/operation.Prepared`, `Reporter`, and `Outcome`. External plugin code implements no generated gRPC server, operation registry, stream writer, cancellation dispatcher, or closure coordinator.
- CMP-09.1: Each SDK-initiated operation registers its `Tracker` queue before its request is sent. `Operation.Wait` consumes that queue and invokes the progress callback outside the stream receive goroutine. Queue failure cancels and joins bound operations, so a slow plugin callback cannot block request receipt.
- DEC-10: An external project can bypass the SDK and implement the protobuf and gRPC contract directly. Host protocol validation remains authoritative for such implementations.

### Operation receipt and execution

- STP-01: The stream receiver decodes one stream message. A protobuf or transport receive failure follows the stream failure mapping below.
- STP-02: The contract adapter validates message content, `operation_id`, operation kind, and payload fields. It asks the operation-specific consumer to perform bounded in-memory admission and return `Prepared`.
- STP-03: The adapter uses `Owner` to reserve the identifier and admission result. It enqueues `Rejected` on failure or `Accepted` with a writer acknowledgement on success, then returns to `Recv`.
- STP-04: A tracked worker waits for successful gRPC delivery of `Accepted`. It then enqueues `Running` and calls `Prepared.Run`. If delivery of `Accepted` fails, the worker does not call `Run`.
- STP-05: After `Run` returns, the worker calls `Release` and waits for all operation-owned work to stop before it enqueues the selected terminal message. The identifier remains reserved until the writer reports terminal delivery; removing that reservation is lifecycle bookkeeping, not operation work.
- STP-06: Cancellation follows STP-02 through STP-05. Its run function cancels the target context, waits until the target has stopped and its terminal message precedes the cancellation terminal message in writer order, then completes with `CancelCompleted` containing the target state.
- STP-07: On requested closure, the receiver stops accepting requests, cancels all owned operations, waits for their work, drains outbound terminal messages, and closes the stream. On transport loss, it cancels and waits without requiring terminal delivery.

### Stream writer

- CMP-10: Each `Open` implementation owns one writer goroutine and one bounded outbound channel. Each initiated operation owns one bounded inbound event channel. Their capacities are internal named constants, not public contract limits or configuration.
- CMP-11: Event producers map their typed payload before enqueueing and use a nonblocking enqueue. The writer is the only goroutine that calls gRPC `Send`, sends messages in channel order, and resolves requested delivery acknowledgements.
- FLR-01: On a full outbound channel, the producer reports `ResourceExhausted` to the connection closure coordinator and returns. The coordinator resolves pending acknowledgements, cancels and joins bound operations, and terminates the stream without waiting in the producer goroutine. Request receipt never waits for queue capacity, and memory remains bounded.
- FLR-02: A writer `Send` failure preserves its gRPC status when present and otherwise maps to `Unavailable`. The writer resolves pending acknowledgements, reports the failure to the connection closure coordinator, and returns. The coordinator cancels and joins operations without waiting for itself or the writer.
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
| Request message has no request kind or has invalid request fields | `Rejected` with `INVALID_ARGUMENT` | Remains open | No operation is created; accepted operations continue |
| Incoming operation event, connection event, or close message has invalid fields | None | `FailedPrecondition` | Canceled and joined |
| New request received after `CloseConnection` | None | `FailedPrecondition` | Canceled and joined; operation events remain allowed until EOF |
| gRPC `Recv` cannot decode a protobuf frame | None | Incoming status or `InvalidArgument` | Canceled and joined |
| Local stream-message mapping invariant fails | None | `Internal` | Canceled and joined |
| Outbound queue is full | None | `ResourceExhausted` | Canceled and joined |
| Stream transport fails | None | Incoming status or `Unavailable` | Canceled and joined |
| Requested connection or runtime closure | Terminal delivery when transport remains available | Clean closure | Canceled and joined |

### Connection closure

- ALG-03: Normal closure is completed by the gRPC client calling `CloseSend`. A `CloseConnection` from either endpoint stops new requests but permits operation events required to finish accepted work. The client calls `CloseSend` after its queued messages are delivered. The gRPC server cancels and joins its owned operations, drains terminal messages, and returns `nil`; the client receives those messages until EOF. One closure coordinator per endpoint handles local close, peer close, and simultaneous close.
- ALG-04: On UI user quit, the UI SDK sends `CloseConnection`. Host stops new UI requests, cancels and joins Host-owned operations, drains their terminal messages, and calls `CloseSend`. The UI SDK receives EOF, cancels and joins UI-owned operations, drains their terminal messages, and returns from `Open`. Host closes the UI process only after response EOF.
- ALG-05: On Host-requested UI or Extension shutdown, Host sends `CloseConnection`, stops new requests, drains its queued messages, and calls `CloseSend`. The plugin SDK stops request acceptance when it receives `CloseConnection`, cancels and joins plugin-owned operations, drains terminal messages after request EOF, and returns from `Open`. Host closes the plugin process only after response EOF.
- ALG-06: Programmatic controller `CloseSend` produces clean request EOF. Host stops request acceptance, cancels and joins Host-owned operations, drains response terminal messages, and returns `nil`. For Host-requested shutdown, Host first sends `CloseConnection`; the controller then calls `CloseSend` and receives until response EOF.
- FLR-03.1: Transport loss or process exit makes drain unavailable. Each endpoint cancels and joins its owned operations, returns the transport status, and performs process cleanup. It does not attempt terminal delivery.

### Startup operations

- ALG-01: UI startup uses the opened stream in this order:
  1. Host sends `Initialize` as an operation and waits for completed acknowledgement.
  2. Host and UI mark the connection ready for ordinary operation kinds.
- ALG-02: Extension startup uses the opened stream in this order:
  1. Host sends `Register` as an operation and waits for its completed registration payload.
  2. Host validates the returned tool and handler registration.
  3. Host marks the extension runtime ready for `Handle` and `Execute`.
- FLR-04: Before startup completes, an ordinary request receives `Rejected` with `NOT_READY`. A cancellation request for the active startup operation remains allowed.
- FLR-05: A rejected, canceled, failed, or protocol-invalid startup operation closes the plugin stream and marks the plugin unavailable. No compatibility fallback RPC is attempted.
- DEC-01: Programmatic Control has no startup operation. A successfully opened stream can receive controller work requests immediately.

### Operation mapping

- CMP-12: UI commands become `UIRequest` variants. `StopCommand` is replaced by `CancelOperation`, and `QuitCommand` is replaced by `CloseConnection`. Host command results become `HostCompleted` variants, and Host lifecycle updates tied to the command become `HostProgress` variants.
- CMP-13: Programmatic commands become `ControllerRequest` variants. `correlation_id` is replaced by stream-message `operation_id`, and `Abort` is replaced by `CancelOperation`. Query and mutation responses become `HostCompleted` variants.
- CMP-14: Agent-run events become operation progress. Host settlement completes before the owner sends the operation terminal event. Public `AgentSettled` is removed because the terminal event now means that operation work and settlement have stopped.
- CMP-15: `Register`, session-tree request and result handlers, observers, and tool execution become `HostRequest` variants in the Extension Contract. Tool progress becomes `ExtensionProgress`; registration, handler actions, and tool results become `ExtensionCompleted` variants.
- DEC-02: An ordinary extension handler error remains a completed handler result because Host handler composition continues. A handler transport or protocol failure becomes operation `Failed`.
- DEC-03: A tool result with `is_error` remains completed operation data because Agent Core consumes it as a model-visible tool result. Extension transport, protocol, or runtime failure becomes operation `Failed`.
- CMP-16: The standard TUI assigns an identifier to every UI request and stores the identifier of its foreground operation. The stop action sends a new cancellation operation targeting that foreground identifier.
- CMP-17: Programmatic controllers assign identifiers and target any owned nonterminal operation explicitly. `RunStateResult` reports the active agent-run operation identifier when one exists.
- CMP-18: A Host operation that invokes an Extension Contract operation assigns a new identifier, tracks it through `Tracker`, and sends cancellation for that identifier when the parent in-process context is canceled. No public parent identifier is added.

### Operation inventory

- APC-19: Every request first applies common identifier and payload-shape validation. Rejection sets below are closed:
  - `BASE-R`: `INVALID_ARGUMENT`, `OPERATION_ID_IN_USE`.
  - `STARTUP-R`: `BASE-R` plus `BUSY` and `NOT_READY`.
  - `READY-R`: `BASE-R` plus `NOT_READY`.
  - `RUN-R`: `BASE-R` plus `BUSY`.
  - `UI-RUN-R`: `RUN-R` plus `NOT_READY`.
  - `MODEL-R`: `BASE-R` plus `NOT_FOUND` and `REASONING_UNSUPPORTED`.
  - `UI-MODEL-R`: `MODEL-R` plus `NOT_READY`.
  - `SESSION-R`: `BASE-R` plus `BUSY`.
  - `UI-SESSION-R`: `SESSION-R` plus `NOT_READY`.
  - `CANCEL-R`: `INVALID_ARGUMENT`, `OPERATION_ID_IN_USE`, `TARGET_NOT_ACTIVE`.
  - `EXTENSION-R`: `READY-R` plus `BUSY`.
- APC-20: Failed-code sets below are closed. Every set includes `INTERNAL`:
  - `RUN-F`: `CREDENTIAL_UNAVAILABLE`, `MODEL_UNAVAILABLE`, `MODEL_FAILED`, `EXTENSION_INVALID_RESULT`, `EXTENSION_UNAVAILABLE`, `INTERNAL`.
  - `AUTH-F`: `AUTHENTICATION_FAILED`, `INTERNAL`.
  - `MODEL-F`: `CREDENTIAL_UNAVAILABLE`, `INTERNAL`.
  - `SESSION-F`: `SESSION_UNAVAILABLE`, `PERSISTENCE_UNAVAILABLE`, `INTERNAL`.
  - `NAVIGATION-F`: `SESSION_UNAVAILABLE`, `PERSISTENCE_UNAVAILABLE`, `MODEL_UNAVAILABLE`, `CREDENTIAL_UNAVAILABLE`, `MODEL_FAILED`, `EXTENSION_INVALID_RESULT`, `EXTENSION_UNAVAILABLE`, `INTERNAL`.
  - `INTERNAL-F`: `INTERNAL`.
- DEC-12: `BASE-R` preparation validates identifiers and fields without domain work. `READY-R` also requires completed plugin startup. `RUN-R` additionally reserves the agent-run gate. `SESSION-R` additionally reserves the session-mutation gate. `MODEL-R` checks the in-memory model catalogue and reasoning choices; credential access remains in `Run`. Read operations reserve no domain gate. `EXTENSION-R` uses only extension-owned in-memory admission.

UI operations:

| Request | Receiver | Preparation and admission | Progress | Completed | Failed |
|---|---|---|---|---|---|
| `Initialize` | UI SDK | First Host request; `STARTUP-R` | None | `Initialized` | `INTERNAL-F` |
| `CancelOperation` | Host or UI SDK | Target is owned and nonterminal; `CANCEL-R` | None | `CancelCompleted` | `INTERNAL` |
| `SubmitCommand` | Host | Nonempty content and agent-run reservation; `UI-RUN-R` | `AgentEvent` | `SubmitCompleted` | `RUN-F` |
| `RetryAuthenticationCommand` | Host | Authentication is available; `READY-R` | `AuthorizationRequest` | `AuthenticationCompleted` | `AUTH-F` |
| `SelectModelCommand` | Host | Required identifiers and in-memory catalogue match; `UI-MODEL-R` | None | `ModelSelectionChanged` | `MODEL-F` |
| `SelectReasoningChoiceCommand` | Host | Defined supported choice; `UI-MODEL-R` | None | `ModelSelectionChanged` | `MODEL-F` |
| `CreateSessionCommand` | Host | Session-mutation reservation; `UI-SESSION-R` | None | `SessionChanged` | `SESSION-F` |
| `ListSessionsCommand` | Host | `READY-R`; no domain gate | None | `SessionList` | `SESSION-F` |
| `ResumeSessionCommand` | Host | Nonempty session identifier and session-mutation reservation; `UI-SESSION-R` | None | `SessionChanged` | `SESSION-F` |
| `SetSessionNameCommand` | Host | Valid name and session-mutation reservation; `UI-SESSION-R` | None | `SessionInformation` | `SESSION-F` |
| `GetSessionInfoCommand` | Host | `READY-R`; no domain gate | None | `SessionInformation` | `INTERNAL-F` |
| `GetSessionTreeCommand` | Host | `READY-R`; no domain gate | None | `SessionTreeResult` | `INTERNAL-F` |
| `NavigateSessionTreeCommand` | Host | Valid target and summary mode plus session-mutation reservation; `UI-SESSION-R` | None | `SessionTreeNavigationResult` | `NAVIGATION-F` |
| `ForkSessionCommand` | Host | Nonempty target plus session-mutation reservation; `UI-SESSION-R` | None | `SessionForked` | `SESSION-F` |
| `CloneSessionCommand` | Host | Session-mutation reservation; `UI-SESSION-R` | None | `SessionCloned` | `SESSION-F` |
| `SetEntryLabelCommand` | Host | Nonempty target plus session-mutation reservation; `UI-SESSION-R` | None | `EntryLabelSet` | `SESSION-F` |

Programmatic Control operations:

| Request | Receiver | Preparation and admission | Progress | Completed | Failed |
|---|---|---|---|---|---|
| `UserRequest` | Host | Nonempty content and agent-run reservation; `RUN-R` | `AgentEvent` | `UserRequestCompleted` | `RUN-F` |
| `CancelOperation` | Host | Target is owned and nonterminal; `CANCEL-R` | None | `CancelCompleted` | `INTERNAL` |
| `GetRunState` | Host | `BASE-R`; no domain gate | None | `RunStateResult` | `INTERNAL` |
| `GetMessages` | Host | `BASE-R`; no domain gate | None | `MessagesResult` | `INTERNAL-F` |
| `GetModels` | Host | `BASE-R`; no domain gate | None | `ModelsResult` | `INTERNAL` |
| `SelectModel` | Host | Required identifiers and in-memory catalogue match; `MODEL-R` | None | `ModelSelectionResult` | `MODEL-F` |
| `SelectReasoningChoice` | Host | Defined supported choice; `MODEL-R` | None | `ModelSelectionResult` | `MODEL-F` |
| `CreateSession` | Host | Session-mutation reservation; `SESSION-R` | None | `SessionInfoResult` | `SESSION-F` |
| `ListSessions` | Host | `BASE-R`; no domain gate | None | `SessionsResult` | `SESSION-F` |
| `ResumeSession` | Host | Nonempty session identifier and session-mutation reservation; `SESSION-R` | None | `SessionInfoResult` | `SESSION-F` |
| `SetSessionName` | Host | Valid name and session-mutation reservation; `SESSION-R` | None | `SessionInfoResult` | `SESSION-F` |
| `GetSessionInfo` | Host | `BASE-R`; no domain gate | None | `SessionInfoResult` | `INTERNAL-F` |
| `GetSessionEntries` | Host | `BASE-R`; no domain gate | None | `SessionEntriesResult` | `INTERNAL-F` |
| `GetSessionStats` | Host | `BASE-R`; no domain gate | None | `SessionStatsResult` | `INTERNAL-F` |
| `GetSessionTree` | Host | `BASE-R`; no domain gate | None | `SessionTreeResult` | `INTERNAL-F` |
| `NavigateSessionTree` | Host | Valid target and summary mode plus session-mutation reservation; `SESSION-R` | None | `SessionTreeNavigationResult` | `NAVIGATION-F` |
| `ForkSession` | Host | Nonempty target plus session-mutation reservation; `SESSION-R` | None | `ForkSessionResult` | `SESSION-F` |
| `CloneSession` | Host | Session-mutation reservation; `SESSION-R` | None | `CloneSessionResult` | `SESSION-F` |
| `SetEntryLabel` | Host | Nonempty target plus session-mutation reservation; `SESSION-R` | None | `SetEntryLabelResult` | `SESSION-F` |

Extension operations:

| Request | Receiver | Preparation and admission | Progress | Completed | Failed |
|---|---|---|---|---|---|
| `RegisterRequest` | Extension SDK | First Host request; `STARTUP-R` | None | `RegisterResponse` | `INTERNAL-F` |
| `HandleRequest` | Extension SDK | Known handler and matching payload; `EXTENSION-R` | None | `HandleResponse`, including ordinary `HandlerError` | `INTERNAL-F` |
| `ExecuteRequest` | Extension SDK | Nonempty tool name and valid JSON argument bytes; `EXTENSION-R` | `ToolProgress` | `ToolResult`, including `is_error` | `INTERNAL-F` |
| `CancelOperation` | Extension SDK | Target is owned and nonterminal; `CANCEL-R` | None | `CancelCompleted` | `INTERNAL` |

### Admission and domain ordering

- DEC-04: `internal/operation.Owner` is not a global concurrency gate. It only owns lifecycle state.
- DEC-05: Operation-specific consumers retain their admission rules. Agent runs and session mutations reserve their domain gate during preparation and release it through `Prepared.Release`.
- DEC-06: Snapshot reads require no domain gate and can execute while a mutation runs. Their result is the snapshot taken by their operation implementation.
- DEC-07: Storage, extension calls, process work, network work, model requests, and credential access occur only inside `Prepared.Run`.

### TDD and verification

Implementation follows RED, GREEN, REFACTOR, and VERIFY. Each RED test must compile and fail through its expected assertion rather than a timeout.

- TSK-01: Generate the common lifecycle package and replaced contract packages as compile setup. Run `task generate` twice and require no second-run diff before behavioral RED tests import the generated API.
- TSK-02: Add `internal/operation` unit tests. Purpose: prove acceptance order, progress order, terminal uniqueness, targeted cancellation, release, and closure waiting. Inputs: prepared operations and a writer fake controlled by channels. Expected outputs: `Run` remains stopped until the writer acknowledges `Accepted`, release and owned work finish before terminal delivery, and closure joins all work. Edge cases: cancellation before `Run`, commit winning a cancellation race, delivery failure, duplicate identifier rejection, queue overflow from an operation worker, and unexpected transport cleanup. Dependencies: no gRPC or production use case.
- TSK-03: Update Programmatic Control tests. Purpose: prove all command kinds use operations. Inputs: one blocked command followed by a query and cancellation. Expected outputs: the second request receives `Rejected` or `Accepted` before release, cancellation completes after the target terminal state, and connection closure joins work. Edge cases: malformed requests, duplicate identifiers, failed operations, writer overflow, and send failure. Dependencies: generated contract, mocked consumer, and `internal/operation`.
- TSK-04: Update UI runtime and standard TUI tests. Purpose: prove initialization startup, typed command lifecycle, foreground cancellation, connection-event delivery, terminal rendering, and UI-owned presentation resources. Inputs: a real UI stream with one blocked Host operation and an idle extension-process exit. Expected outputs: ordered startup, later request receipt, an `Error` connection event without `operation_id`, and joined shutdown without Host terminal recovery. Edge cases: startup cancellation, `NOT_READY`, invalid lifecycle order, a full inbound operation queue, and UI process exit. Dependencies: generated contract and UI SDK.
- TSK-05: Update Extension runtime and bundled tools tests. Purpose: prove Register, Handle, Execute, progress, cancellation, and process-exit ownership on one stream. Inputs: real test extension with blocked handler and tool operations. Expected outputs: another request is received before release and cancellation joins the target. Edge cases: ordinary handler error as completed data, tool `is_error` as completed data, protocol failure as failed operation, registration failure, queue overflow, and runtime exit. Dependencies: generated contract and Extension SDK.
- TSK-06: Add integration tests for every implemented work-request direction. Purpose: detect blocked receipt at real gRPC boundaries. Inputs: keep one operation in `Running`, send a second request on that operation's contract connection, then cancel and close. Expected outputs: the second request receives `Rejected` or `Accepted` before the first is released, the target reaches one terminal state, and no goroutine survives closure. Edge cases: cancellation racing with completion, UI quit, Host-requested UI and Extension close, Programmatic controller half-close, Host-requested Programmatic close, simultaneous close, process exit, and failed transport. Dependencies: production controllers and real local gRPC streams. These tests use `//go:build integration` and run through `task itest`.
- TSK-07: Add a separate-module integration fixture for an external extension and external UI plugin. Purpose: prove that each plugin compiles and runs through public contract and SDK packages without importing Glyph internal packages or implementing generated gRPC services. Inputs: minimal plugin services in a module with a non-Glyph import path. Expected outputs: startup operations complete and one ordinary operation reaches a terminal state. Edge cases: cancellation and plugin shutdown. Dependencies: generated contracts and public SDKs.
- TSK-08: After each GREEN slice, refactor while focused tests remain green. Final verification runs `task generate` twice, `task fmt`, `task fix_dry_run`, reviews the proposed fixes, runs `task fix` or applies the fixes manually, then runs `task lint`, `task test`, `task itest`, and `git diff --check`.

### Affected areas

- CMP-19: `api/operation/v1`, `pkg/operation/v1`, and `internal/operation` add the shared lifecycle contract and private runtime.
- CMP-20: `api/plugins/ui/v1`, `pkg/plugins/ui/v1`, `sdk/plugins/ui/v1`, Host UI runtime, Host UI selection, and the standard TUI adopt the UI operation stream. Remove `controls_terminal`, `GetCapabilities`, SDK capability retrieval and caching, `ui.Capabilities`, capability mapping and selection state, Host terminal capture and recovery, and standard TUI capability responses.
- DEC-11: Explicit, configured, and sole-candidate UI selection keep their defined success and failure behavior without a capability request. Host does not inspect, capture, reset, or restore terminal state; each UI plugin owns the presentation resources it opens.
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
