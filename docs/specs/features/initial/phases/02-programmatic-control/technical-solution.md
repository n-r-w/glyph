# Technical Solution: PHS-02 Programmatic Control Foundation

## Problem Statement

- PRB-01: `glyph run` accepts one user request and terminates, so a programmatic controller cannot submit multiple operations to one headless agent process.
- PRB-02: Agent Core exposes run state, in-memory history, cancellation, and semantic events only through internal Go APIs. Programmatic Control must expose the required behavior without adding protobuf, gRPC, or UI dependencies to Agent Core.
- PRB-03: The controller owns the programmatic `glyph` process. The process must remain available for multiple commands on one connection and must terminate when that connection closes.

## Proposed Solution

### Solution overview

- SOL-01: Add `glyph rpc` as a third application mode beside the standard TUI and one-shot `glyph run` modes.
- SOL-02: `glyph rpc` hosts one bidirectional gRPC stream over a Unix domain stream socket in the `glyph` process. It starts no UI plugin and no separate Host daemon.
- SOL-03: One controller connection accepts user requests, abort, run-state queries, and message queries. The connection owns the process lifetime.
- SOL-04: Keep Programmatic Control commands and events independent of protobuf and gRPC. Map between transport DTOs and controller-owned Go types at the gRPC controller boundary.

### Source and package placement

```text
api/programmatic/v1/
└── programmatic.proto

pkg/programmatic/v1/
└── generated protobuf and gRPC code

host/internal/
├── controller/programmatic/
│   ├── interfaces.go
│   ├── interfaces_mock.go
│   ├── service.go
│   └── mapping.go
├── usecase/host/programmatic/
│   ├── interfaces.go
│   ├── interfaces_mock.go
│   ├── service.go
│   └── delivery.go
├── infra/programmatic/socket/
│   └── service.go
└── app/
    └── Programmatic Control composition
```

- CMP-01: `api/programmatic/v1` owns the public process contract. It does not import or reuse the UI plugin protobuf contract.
- CMP-02: `host/internal/controller/programmatic` owns the consumer-side `HostSession` Go interface and its transport-independent command, response, and event types. Its gRPC service maps protobuf values to those types and contains no Host orchestration.
- CMP-03: `host/internal/usecase/host/programmatic` implements `HostSession`. It owns active-operation state, cancellation, query orchestration, event correlation, and controller-disconnection behavior.
- DEC-01.1: The use case imports `host/internal/controller/programmatic` only to implement the consumer-owned `HostSession` interface and its method types. The use case imports no generated protobuf or gRPC package. A later controller owns a separate minimal interface and method types instead of adding shared command aggregates to the domain layer.
- CMP-04: `host/internal/infra/programmatic/socket` owns Unix socket creation, permission changes, listener closure, and path cleanup. It supplies a standard `net.Listener` to the gRPC server.
- CMP-05: `host/internal/app` creates concrete socket, gRPC controller, Programmatic Control use case, Host event dispatcher, Agent Core service, provider, and extension runtime dependencies.
- DEC-01: Do not add a handwritten public SDK in PHS-02. The generated gRPC client is the public client API, and a test-only controller fixture exercises the process contract.

### CLI and socket bootstrap

- APC-01: The programmatic command is `glyph rpc [--extension-dir <path>] [--socket <path>]`. UI selection arguments remain invalid in this mode.
- DEC-02: Without `--socket`, `glyph rpc` creates an owner-only temporary directory through `os.MkdirTemp` and listens on `<temporary-directory>/control.sock`.
- DEC-03: With `--socket`, Glyph resolves the supplied path to an absolute path and requires its parent directory to exist. Glyph does not remove or replace an object that exists at the supplied path.
- EVC-01: After the listener opens, Glyph writes exactly one JSON line to stdout before accepting controller commands:

```json
{"socket":"/absolute/path/control.sock"}
```

- CNS-01: Programmatic mode writes no other stdout content. Structured logs and startup errors go to stderr.
- DEC-04: An automatically created socket directory has mode `0700`, and every socket created by Glyph has mode `0600`. Glyph does not inspect or change the mode of a caller-owned parent directory supplied through `--socket`. Programmatic Control uses local gRPC without TLS.
- DEC-04.1: Closing the socket service removes the socket file for automatic and explicit paths. It removes the parent directory only when Glyph created that directory.
- FLR-01: Socket creation, permission, or listener failure terminates startup before EVC-01 and returns a nonzero process status.

### Public gRPC contract

- APC-02: `api/programmatic/v1/programmatic.proto` uses protobuf edition 2023, package `glyph.programmatic.v1`, and Go package `github.com/n-r-w/glyph/pkg/programmatic/v1;programmaticv1`.
- APC-03: The service has one RPC:

```proto
service ProgrammaticControlService {
  rpc Open(stream OpenRequest) returns (stream OpenResponse);
}
```

- APC-04: The public messages and field numbers are fixed by this table. Parenthesized fields belong to the named `oneof`.

| Message | Fields |
|---|---|
| `OpenRequest` | string `correlation_id = 1`; command oneof: `UserRequest user_request = 2`, `Abort abort = 3`, `GetRunState get_run_state = 4`, `GetMessages get_messages = 5` |
| `UserRequest` | string `text = 1` |
| `Abort`, `GetRunState`, `GetMessages` | no fields |
| `OpenResponse` | string `correlation_id = 1`; content oneof: `CommandResponse command_response = 2`, `AgentEvent agent_event = 3` |
| `CommandResponse` | result oneof: `UserRequestAccepted user_request_accepted = 1`, `AbortCompleted abort_completed = 2`, `RunStateResult run_state = 3`, `MessagesResult messages = 4`, `CommandRejected rejected = 5` |
| `UserRequestAccepted`, `AbortCompleted` | no fields |
| `RunStateResult` | `RunState state = 1`, string `active_correlation_id = 2` |
| `MessagesResult` | repeated `HistoryEntry entries = 1` |
| `CommandRejected` | `CommandType command = 1`, `RejectionCode code = 2`, string `message = 3` |
| `HistoryEntry` | entry oneof: `UserMessage user = 1`, `ModelResponse model = 2`, `ToolResult tool_result = 3` |
| `UserMessage` | string `text = 1` |
| `AgentEvent` | `AgentEventType type = 1`, string `run_id = 2`; payload oneof: `ModelContent model_content = 3`, `ToolCallPreview tool_call_preview = 4`, `FinalToolCall final_tool_call = 5`, `ToolExecution tool_execution = 6`, `ToolProgress tool_progress = 7`, `ToolResult tool_result = 8`, `ModelResponse model_response = 9`, `TurnSummary turn = 10`, `AgentSummary agent = 11` |
| `ModelContent` | `ModelContentKind kind = 1`, int32 `position = 2`, string `text = 3` |
| `ToolCallPreview` | string `call_id = 1`, string `name = 2`, int32 `position = 3`, bool `provisional = 4`, repeated `ToolCallPreviewField fields = 5` |
| `ToolCallPreviewField` | string `name = 1`; content oneof: `google.protobuf.Value value = 2`, string `prefix = 3` |
| `FinalToolCall` | string `call_id = 1`, string `name = 2`, int32 `position = 3`, `google.protobuf.Struct arguments = 4` |
| `ToolExecution` | string `call_id = 1`, string `tool_name = 2` |
| `ToolProgress` | `ProgressChannel channel = 1`, string `content = 2` |
| `ToolResult` | string `call_id = 1`, string `tool_name = 2`, repeated `ToolResultContent contents = 3`, bool `is_error = 4` |
| `ToolResultContent` | content oneof: string `text = 1`, `ToolResultImage image = 2` |
| `ToolResultImage` | string `media_type = 1`, bytes `data = 2` |
| `ModelResponse` | string `text = 1`, `ModelOutcome outcome = 2`, string `error_message = 3`, string `provider = 4`, string `model = 5`, string `response_model = 6`, string `response_id = 7`, `ModelUsage usage = 8`, repeated `ModelDiagnostic diagnostics = 9`, repeated `ModelResponseItem content = 10` |
| `ModelResponseItem` | content oneof: `FinalText text = 1`, `FinalText refusal = 2`, `FinalText reasoning = 3`, `FinalToolCall tool_call = 4` |
| `FinalText` | string `text = 1` |
| `ModelUsage` | int64 `input_tokens = 1`, int64 `output_tokens = 2`, int64 `cached_input_tokens = 3`, int64 `cache_write_tokens = 4`, int64 `reasoning_tokens = 5`, int64 `total_tokens = 6` |
| `ModelDiagnostic` | string `code = 1`, string `message = 2` |
| `TurnSummary` | `ModelResponse response = 1`, repeated `ToolResult tool_results = 2` |
| `AgentSummary` | `RunOutcome outcome = 1`, string `error_message = 2` |
- APC-05: Every wire enum defines its zero value as `*_UNSPECIFIED`. Glyph never emits an unspecified enum except `CommandRejected.command = COMMAND_TYPE_UNSPECIFIED` for a correlated `OpenRequest` that has no command payload. That exception sets the enum field as present. The request receives `REJECTION_CODE_INVALID_ARGUMENT`, and the stream remains open. The remaining values and numbers are fixed:

| Enum | Values |
|---|---|
| `CommandType` | `COMMAND_TYPE_UNSPECIFIED = 0`, `COMMAND_TYPE_USER_REQUEST = 1`, `COMMAND_TYPE_ABORT = 2`, `COMMAND_TYPE_GET_RUN_STATE = 3`, `COMMAND_TYPE_GET_MESSAGES = 4` |
| `RejectionCode` | `REJECTION_CODE_UNSPECIFIED = 0`, `REJECTION_CODE_INVALID_ARGUMENT = 1`, `REJECTION_CODE_BUSY = 2`, `REJECTION_CODE_NO_ACTIVE_RUN = 3`, `REJECTION_CODE_CORRELATION_IN_USE = 4`, `REJECTION_CODE_INTERNAL = 5` |
| `RunState` | `RUN_STATE_UNSPECIFIED = 0`, `RUN_STATE_IDLE = 1`, `RUN_STATE_RUNNING = 2` |
| `AgentEventType` | `AGENT_EVENT_TYPE_UNSPECIFIED = 0`, `AGENT_EVENT_TYPE_AGENT_START = 1`, `AGENT_EVENT_TYPE_TURN_START = 2`, `AGENT_EVENT_TYPE_MESSAGE_START = 3`, `AGENT_EVENT_TYPE_MODEL_CONTENT_START = 4`, `AGENT_EVENT_TYPE_MODEL_TEXT_DELTA = 5`, `AGENT_EVENT_TYPE_MODEL_CONTENT_END = 6`, `AGENT_EVENT_TYPE_TOOL_CALL_START = 7`, `AGENT_EVENT_TYPE_TOOL_CALL_DELTA = 8`, `AGENT_EVENT_TYPE_TOOL_CALL_END = 9`, `AGENT_EVENT_TYPE_MESSAGE_END = 10`, `AGENT_EVENT_TYPE_TOOL_EXECUTION_START = 11`, `AGENT_EVENT_TYPE_TOOL_EXECUTION_UPDATE = 12`, `AGENT_EVENT_TYPE_TOOL_EXECUTION_END = 13`, `AGENT_EVENT_TYPE_TOOL_RESULT = 14`, `AGENT_EVENT_TYPE_TURN_END = 15`, `AGENT_EVENT_TYPE_AGENT_END = 16`, `AGENT_EVENT_TYPE_AGENT_SETTLED = 17` |
| `ModelContentKind` | `MODEL_CONTENT_KIND_UNSPECIFIED = 0`, `MODEL_CONTENT_KIND_TEXT = 1`, `MODEL_CONTENT_KIND_REASONING = 2`, `MODEL_CONTENT_KIND_REFUSAL = 3` |
| `ProgressChannel` | `PROGRESS_CHANNEL_UNSPECIFIED = 0`, `PROGRESS_CHANNEL_STATUS = 1`, `PROGRESS_CHANNEL_STDOUT = 2`, `PROGRESS_CHANNEL_STDERR = 3` |
| `ModelOutcome` | `MODEL_OUTCOME_UNSPECIFIED = 0`, `MODEL_OUTCOME_STOP = 1`, `MODEL_OUTCOME_TOOL_USE = 2`, `MODEL_OUTCOME_LENGTH = 3`, `MODEL_OUTCOME_ABORTED = 4`, `MODEL_OUTCOME_FAILED = 5` |
| `RunOutcome` | `RUN_OUTCOME_UNSPECIFIED = 0`, `RUN_OUTCOME_COMPLETED = 1`, `RUN_OUTCOME_ABORTED = 2`, `RUN_OUTCOME_FAILED = 3` |

- APC-06: `OpenRequest.correlation_id` must be nonempty, and `UserRequest.text` must contain at least one non-whitespace character. `OpenResponse.correlation_id` equals the command correlation for command responses and the accepted user-request correlation for agent events.
- ENT-01: `RunStateResult.active_correlation_id` is empty for `RUN_STATE_IDLE` and equals the accepted user-request correlation for `RUN_STATE_RUNNING`. Internal `StatusRunning` and `StatusAwaitingSettlement` both map to `RUN_STATE_RUNNING`.
- EVC-02: Event mappings are exhaustive:

| Agent Core or Host event | `AgentEventType` | Payload |
|---|---|---|
| `EventAgentStart` | `AGENT_EVENT_TYPE_AGENT_START` | none |
| `EventTurnStart` | `AGENT_EVENT_TYPE_TURN_START` | none |
| `EventMessageStart` | `AGENT_EVENT_TYPE_MESSAGE_START` | none |
| `EventContentStart` | `AGENT_EVENT_TYPE_MODEL_CONTENT_START` | `ModelContent` with kind and position; empty text |
| `EventTextDelta` | `AGENT_EVENT_TYPE_MODEL_TEXT_DELTA` | `ModelContent` with kind, position, and delta text |
| `EventContentEnd` | `AGENT_EVENT_TYPE_MODEL_CONTENT_END` | `ModelContent` with kind and position; empty text |
| `EventToolCallStart` | `AGENT_EVENT_TYPE_TOOL_CALL_START` | `ToolCallPreview` |
| `EventToolCallDelta` | `AGENT_EVENT_TYPE_TOOL_CALL_DELTA` | complete replacement `ToolCallPreview` |
| `EventToolCallEnd` | `AGENT_EVENT_TYPE_TOOL_CALL_END` | `FinalToolCall` |
| `EventMessageEnd` | `AGENT_EVENT_TYPE_MESSAGE_END` | `ModelResponse` |
| `EventToolExecutionStart` | `AGENT_EVENT_TYPE_TOOL_EXECUTION_START` | `ToolExecution` |
| `EventToolExecutionUpdate` | `AGENT_EVENT_TYPE_TOOL_EXECUTION_UPDATE` | `ToolProgress` |
| `EventToolExecutionEnd` | `AGENT_EVENT_TYPE_TOOL_EXECUTION_END` | `ToolResult` |
| `EventToolResult` | `AGENT_EVENT_TYPE_TOOL_RESULT` | `ToolResult` |
| `EventTurnEnd` | `AGENT_EVENT_TYPE_TURN_END` | `TurnSummary` |
| `EventAgentEnd` | `AGENT_EVENT_TYPE_AGENT_END` | `AgentSummary` |
| Host settlement | `AGENT_EVENT_TYPE_AGENT_SETTLED` | none |

- EVC-03: `ModelResponse.content` preserves source order for text, refusal, reasoning, and finalized tool calls. It excludes provider-context items. `ModelResponse.text` concatenates finalized public text items. `response_model` is present only when Agent Core has that value. Tool-result text and image blocks preserve source order and exact bytes.
- EVC-04: History mappings are exhaustive: `HistoryEntryUser` maps to `HistoryEntry.user`, `HistoryEntryModel` maps to `HistoryEntry.model`, and `HistoryEntryToolResult` maps to `HistoryEntry.tool_result`. Message queries clone the Agent Core snapshot and exclude the partial model response and provider-context items.
- DEC-05: A Glyph client has no direct shell command. Shell progress and results appear only as correlated agent events when the model invokes the bundled `bash` tool.

### Command responses and rejection

- DEC-06: Each command produces one `CommandResponse`. A user request receives `UserRequestAccepted` before its first `AgentEvent`; its execution outcome arrives through terminal agent events rather than a second command response.
- DEC-07: Run-state and message queries return their data in their single command response. Abort returns `AbortCompleted` only after cancellation and settlement leave the Host idle.
- EVC-05: `CommandRejected.code` has this closed set:

```text
REJECTION_CODE_INVALID_ARGUMENT
REJECTION_CODE_BUSY
REJECTION_CODE_NO_ACTIVE_RUN
REJECTION_CODE_CORRELATION_IN_USE
REJECTION_CODE_INTERNAL
```

- FLR-02: A second user request while a run is active receives `REJECTION_CODE_BUSY`. The active run continues.
- FLR-03: Abort while no run is active receives `REJECTION_CODE_NO_ACTIVE_RUN`.
- FLR-04: A command whose correlation equals the active user-request correlation receives `REJECTION_CODE_CORRELATION_IN_USE`.
- FLR-05: An empty user request, a missing command payload, or a command with an unexpected payload returns `REJECTION_CODE_INVALID_ARGUMENT` and keeps the stream open when the request has a correlation. Failure to allocate a Host run ID before acceptance returns `REJECTION_CODE_INTERNAL` and keeps the stream open.
- DEC-07.3: Command evaluation stops at the first matching condition in this order: empty correlation as terminal gRPC `InvalidArgument`; missing, unexpected, or invalid command payload as `REJECTION_CODE_INVALID_ARGUMENT`; active-correlation reuse as `REJECTION_CODE_CORRELATION_IN_USE`; run-state rejection as `REJECTION_CODE_BUSY` or `REJECTION_CODE_NO_ACTIVE_RUN`; run-ID allocation failure as `REJECTION_CODE_INTERNAL`; command execution.
- FLR-05.1: Terminal-cause evaluation stops at the first matching condition in this order: application-context cancellation follows STP-10; clean client send-side EOF or canceled stream context follows STP-11 and returns no gRPC error; a status error from stream `Recv` or `Send` while both application and stream contexts remain active is returned unchanged; a non-status protobuf mapping or protocol invariant error terminates with `Internal`; every other controller-originated terminal error defaults to `Internal`. A simultaneous second stream terminates independently with `FailedPrecondition`.

### Run and event flow

- STP-01: Before acceptance, the Programmatic Control use case validates the user request and asks `events.Coordinator` to allocate its Host run ID. Allocation failure returns `REJECTION_CODE_INTERNAL` and starts no run.
- STP-02: The use case reserves the correlation and prepared run ID as active, synchronously sends `UserRequestAccepted`, and then starts the prepared run with a context derived from the controller connection.
- DEC-07.1: `events.Coordinator` exposes run-ID preparation and execution of that prepared run for Programmatic Control. `Coordinator.Run(ctx, userText)` delegates to the same prepared-run implementation for one-shot headless and UI controllers.
- STP-03: A Programmatic Control delivery router binds the active correlation and prepared run ID to Agent Core events. One send mutex serializes gRPC sends, and no application event queue is added.
- STP-04: Agent Core and Host deliver lifecycle events synchronously. A slow controller therefore applies backpressure to the active run.
- DEC-07.2: `events.Coordinator` remains the sole owner of Host settlement. It delivers `agent_end`, calls Agent Core `Settle`, and then invokes `DeliverSettled` exactly once for every controller.
- STP-05: The Programmatic Control `DeliverSettled` callback verifies the prepared run ID, atomically clears the active correlation and cancellation handle, and sends the single correlated `AGENT_SETTLED` event. It does not synthesize another settlement event after `Coordinator` returns.
- STP-06: Abort cancels the active run context, waits for `Coordinator` to return after STP-05, and returns `AbortCompleted`. A following run-state query therefore returns `RUN_STATE_IDLE`.
- STP-07: Run-state and message queries read the existing concurrency-safe Agent Core state and history snapshots while a run is active or idle.

### Connection and process lifecycle

- DEC-08: One `glyph rpc` process accepts one `Open` stream. A simultaneous second stream receives gRPC `FailedPrecondition` and does not affect the owning stream.
- DEC-08.1: The gRPC controller publishes one session-completion result to `host/internal/app` after the owning `Open` handler has cancelled and joined its active run. The result identifies clean client closure, protocol failure, transport failure, or cleanup failure.
- DEC-08.2: Application-context cancellation has precedence over stream completion. Stream-context cancellation is clean owner closure only while the application context remains active.
- STP-09: `host/internal/app` owns `grpc.Server.Serve` in a goroutine and waits for session completion, Serve failure, or application-context cancellation. The stream handler never calls `GracefulStop` or `Stop`.
- STP-10: On application-context cancellation, app orchestration asks the Host session to cancel and join its active run through a context that preserves cleanup work, retains the application cancellation as the primary nonzero result, and then calls `grpc.Server.Stop`. The stream cancellation produced by server shutdown does not replace the application result.
- STP-11: On clean client send-side EOF or stream-context cancellation while the application context remains active, the stream handler cancels the active run, waits for settlement, and reports clean session completion. App orchestration then calls `grpc.Server.Stop` and returns status zero unless cleanup fails.
- STP-12: Malformed frames, send or receive failures while both stream and application contexts remain active, Serve failures, and cleanup failures produce a nonzero process result.
- STP-13: After the active run has joined and the gRPC server has stopped, app orchestration closes extension runtimes, removes every socket file created by Glyph, and removes an automatically created socket directory.
- CNS-02: Programmatic mode loads no UI catalog, starts no UI plugin, and creates no independently running Host process.

### Platform boundary

- DEC-09: Socket infrastructure uses standard Go APIs including `net.Listener`, `os`, and `filepath`. No runtime operating-system check rejects Windows.
- CNS-03: PHS-02 tests and guarantees behavior on macOS and Linux. Windows behavior remains untested and is not guaranteed.
- FLR-06: When the operating system rejects a generated socket path, startup returns the operating-system error. The controller can retry with a shorter explicit `--socket` path.

### Test strategy

- DEC-10: Implementation follows RED, GREEN, REFACTOR, and VERIFY. Production behavior is not added before a focused test fails for the expected assertion.
- TSK-01: Add the fixed protobuf source and generate Go code as compile setup. Run `task generate` twice and require no second-run diff before behavioral RED tests import the generated API.
- TSK-02: RED and GREEN CLI parsing and socket lifecycle. Cover `glyph rpc`, unique automatic paths, absolute explicit paths, existing-path rejection, automatic directory mode, socket mode, listener closure, socket removal for automatic and explicit paths, and retention of the caller-owned parent.
- TSK-03: RED and GREEN coordinator and Programmatic Control use-case behavior with generated mocks. Cover prepared run IDs, acceptance-before-event ordering, exactly one post-settlement event, abort-to-idle ordering, queries during a run, event and history mapping, and disconnect cancellation. Table-driven rejection cases cover every overlap among invalid payload, active-correlation reuse, busy state, no active run, and run-ID allocation failure and assert DEC-07.3 precedence.
- TSK-04: RED and GREEN gRPC controller behavior. Cover protobuf mapping for every event and history kind, one-response command behavior, serialized sends, and atomic second-stream rejection. Table-driven terminal cases cover application cancellation plus transport error, stream cancellation plus send or receive error, active-context status pass-through, non-status protocol errors as `Internal`, default controller errors as `Internal`, and the second stream as `FailedPrecondition`. Each case asserts one completion cause according to FLR-05.1.
- TSK-05: RED and GREEN application composition with real gRPC over a Unix socket and the generated client fixture. Drive correlation `c1`, acceptance, events, abort, idle query, correlation `c2`, and owner closure. Assert no UI startup, no blocked goroutine, extension closure, socket cleanup, and process result. Disconnect during an in-flight event send must return status zero. Application-context cancellation during an active run, malformed input, and independent send failure are separate nonzero-result cases.
- TSK-06: REFACTOR after every GREEN slice while its focused tests remain green. Then run `task generate` twice, `go mod tidy -diff`, `task lint`, `task test`, `task build`, and `git diff --check`.

## Overengineering and Overspecification Considerations

- TRD-01: PHS-02 adds only four commands. Queued messages, persistent sessions, model selection, extension-defined commands, interaction requests, and notifications remain in their owning delivery tickets.
- TRD-02: PHS-02 adds no direct shell action, SDK wrapper, reconnection, detached daemon, event queue, TLS, remote endpoint, or transport fallback.
- TRD-03: The public protobuf contract owns its DTOs instead of sharing UI transport DTOs. This duplicates mapping code but keeps UI and Programmatic Control independently evolvable.
- TRD-04: The socket adapter is isolated behind `net.Listener` without adding a speculative named-pipe implementation or generic endpoint scheme.

## Open Questions

None.

## References

- REF-01: [PHS-02 requirements](ticket.md) - owning ticket and acceptance criteria.
- REF-02: [`prd.md`](../../prd.md) - target product requirements and component ownership.
- REF-03: [`../../../terms.md`](../../../../../terms.md) - project domain terminology.
- REF-04: [`../../../../api/plugins/ui/v1/ui.proto`](../../../../../../api/plugins/ui/v1/ui.proto) - existing correlated bidirectional stream used only as a feature-shape comparison.
- REF-05: [`../../../../host/internal/usecase/agent/run/service.go`](../../../../../../host/internal/usecase/agent/run/service.go) - implemented run state, history, cancellation, and settlement behavior.
- REF-06: [`../../../../host/internal/usecase/host/events/coordinator.go`](../../../../../../host/internal/usecase/host/events/coordinator.go) - implemented Host run ID and settlement coordination.
