# Technical Solution: PHS-07 Extension Context and Lifecycle

## Problem Statement

The [Problem Statement](problem.md) defines the missing public extension access to session-bound Host capabilities. The [PRD](prd.md) defines the approved behavior and scope.

## Proposed Solution

### Solution overview

- Keep extensions and Glyph clients in separate processes. Every new cross-process call uses a protobuf contract.
- Extend the existing `ExtensionService.Open` bidirectional stream with extension-initiated Host operations. Do not add a callback service, listener, or socket.
- Keep state with its implemented owner. `providers` owns model data and active model selection, `sessions` owns session state and persistence, and `extensionruntime` owns runtime availability.
- Add separate Host capability use cases for extension context, lifecycle observers, and model-selection handlers. Agent Core gains no Host, extension, protobuf, or transport dependency.

Requirement coverage:

| PRD requirements | Owning solution section |
|---|---|
| FRQ-01, FRQ-02 | Extension context and runtime binding |
| FRQ-03 | Configured models and providers |
| FRQ-04 | Lifecycle observers |
| FRQ-05, FRQ-06, FRQ-07 | Active model selection |
| FRQ-08, FRQ-09, FRQ-10 | Extension entries and persistence; Glyph client contracts and client visibility |
| FRQ-11 | Session-tree navigation |
| FRQ-12 | Error semantics |

### Design decisions

- Use one symmetric operation stream. Host and extension operation trackers identify an operation by initiator and `operation_id`, so equal IDs in opposite directions do not conflict.
- A configured-model request accepts instructions and ordered provider-neutral text messages. It has no tools, images, or provider-specific options in PHS-07.
- A configured provider projection contains its provider ID and ordered model IDs. The model catalogue owns the descriptor fields listed under Configured models and providers.
- Deliver each Agent Core event to the Glyph client before invoking lifecycle observers. Invoke observers synchronously in registration order before Agent Core proceeds to its next event.
- Emit model-selection and reasoning-selection events only for values changed by a successful commit. Emit reasoning selection before model selection when both change.
- Publish a committed extension message through a client-neutral `SessionEntryAdded` connection event. A post-commit delivery error does not roll back persistence.
- Preserve the PHS-05 branch-summarization commit. A selected extension message uses its parent as the navigation destination, while a created `BranchSummaryEntry` becomes the active leaf.

### Rejected alternatives

- Reject a second Host callback service. It would add another listener, address exchange, shutdown path, and cancellation owner for a local process connection that is already bidirectional.
- Reject a generic extension capability bus. Context operations, lifecycle observation, and model selection have different state, validation, ordering, and failure owners.
- Reject asynchronous lifecycle buffering. It would require queue capacity, overflow, stale-context, ordering, and shutdown behavior without an approved requirement.
- Reject storage of model-visible messages inside opaque extension data. Host must validate, persist, project, and navigate message text and client visibility without decoding extension-owned payloads.

### Extension Contract stream

- `OpenRequest` continues to carry Host-initiated requests and extension operation events. `OpenResponse` continues to carry extension operation events and gains extension-initiated requests.
- Extension-initiated requests cover model and provider catalogue queries, configured-model requests, model and reasoning selection, hidden-entry append, visible-message append, and cancellation.
- Host returns accepted, running, completed, canceled, failed, or rejected states through the shared operation lifecycle from `api/operation/v1`.
- Cancellation targets an operation owned by the receiving peer. Stream close cancels and joins owned operations and pending initiated operations in both directions.
- SDK receive loops validate and route envelopes without running handlers on the receive goroutine. A Host-initiated handler can start and await an extension-initiated Host operation on the same stream without deadlock.
- `RegisterResponse.handlers` keeps one common `HandlerDescriptor`. Its closed kinds are the three implemented session-tree kinds, model-selection request, reasoning-selection request, and every observer kind listed under Lifecycle observers. Startup rejects unspecified or unknown kinds and duplicate handler IDs within one extension.

Add these Extension Contract sources:

- `api/plugins/extension/v1/context.proto` owns extension context identity and context references.
- `api/plugins/extension/v1/model.proto` owns model descriptors, provider projections, configured-model requests and results, complete model selections, selection-handler payloads, and selection events.
- `api/plugins/extension/v1/lifecycle.proto` owns Agent Core and Host lifecycle observer payloads.
- `api/plugins/extension/v1/session.proto` owns extension-entry append requests, extension messages, and client visibility.

`api/plugins/extension/v1/extension.proto` retains the service, stream envelopes, operation lifecycle envelopes, registration, and common handler descriptors. `session_tree.proto` imports model types from `model.proto`. `tool.proto` retains tool registration and execution. No compatibility aliases, reserved fields, or old translation paths are added.

Every protobuf enum has an `UNSPECIFIED` zero sentinel. Host rejects that sentinel for required values, so domain value sets remain closed. Every numeric field that represents a position, size, count, or calculation uses `int64`.

### Extension context and runtime binding

- `ExtensionContext` contains extension ID, runtime instance ID, active session ID, and cwd.
- `ExtensionContextRef` contains runtime instance ID and active session ID. Host derives extension ID from the connected runtime and does not trust an extension-supplied extension ID.
- Add `host/internal/usecase/host/extensioncontext`. It creates context snapshots, validates bindings, and coordinates context operations through consumer-owned interfaces.
- Extend `extensionruntime.Service` with an opaque runtime instance ID assigned before each extension process start. The service accounts for Host-initiated and extension-initiated operations against that runtime instance.
- Host validates the context binding before operation acceptance. It validates the binding again before returning a model result, committing a selection, or committing a session mutation.
- Runtime or active-session replacement makes the preceding context stale. A stale operation commits no selection or session change.
- The Extension SDK exposes cancellation through Go `context.Context`. It sends `CancelOperation` when the caller cancels. Cancellation is not serialized as an `ExtensionContext` field.

The Extension SDK context provides typed methods for catalogue queries, configured-model requests, selection requests, and entry appends. Tool, session-tree, selection, and lifecycle invocations carry one `ExtensionContext`. Registration carries no context because runtime acceptance has not completed.

### Configured models and providers

- The model catalogue result contains active model selection and provider-neutral model descriptors. A descriptor contains provider ID, model ID, input modalities, context window, maximum output tokens, reasoning capabilities, tool capabilities, and pricing when configured.
- The provider catalogue result contains provider ID and ordered model IDs. It excludes provider type, endpoint, API configuration, credential source, credentials, and provider reasoning context.
- A configured-model request contains instructions and at least one ordered message. Each message has a closed `user` or `assistant` role and nonempty text.
- PHS-07 configured-model requests contain no tools, images, or provider-specific options. Later owning phases can replace this contract without compatibility fields.
- A configured-model result contains the provider-neutral terminal response, ordered text, refusal, reasoning, and tool-call content, outcome, usage, and diagnostics. It omits provider reasoning context.
- The provider driver maps every readable reasoning block to visible reasoning content before Host creates the extension result.
- `extensioncontext` declares the smallest provider interface it consumes. `providers.Catalog` implements catalogue queries and an explicit request for one `model.Selection` without changing active model selection.
- Host supplies no tools to the provider request and executes no returned tool call. A returned tool call remains typed terminal response content.
- A provider failure produces a failed contract operation with complete error text. Caller cancellation produces a canceled operation. Neither result changes active model selection or session state.

PHS-06 can replace provider dispatch behind the consumer-owned interface without changing the extension context use case or Extension Contract.

### Lifecycle observers

- Add `host/internal/usecase/host/lifecycle`. It owns lifecycle registration, registration order, context binding, observer invocation, and observer issues.
- Lifecycle kinds are `agent_start`, `agent_end`, `agent_settled`, `turn_start`, `turn_end`, `message_start`, `message_update`, `message_end`, `tool_execution_start`, `tool_execution_update`, `tool_execution_end`, `model_selection`, and `reasoning_selection`.
- `message_update` carries provider-neutral text, refusal, visible reasoning, and tool-call transitions in Agent Core source order.
- `events.Dispatcher` attempts delivery of each Agent Core event to its Glyph client recipient, then calls the lifecycle service regardless of the client-delivery result. The lifecycle service invokes available observers in registration order.
- Agent Core waits for the observer chain before it processes the next event. After observer delivery, `events.Dispatcher` returns the client-delivery error to Agent Core through its implemented event-sink contract. PHS-07 adds no event queue, buffering limit, or asynchronous shutdown protocol.
- An ordinary observer error does not change or stop the observed operation, does not stop later observers, and does not deactivate the extension. Host emits an `ExtensionIssue` connection event with the extension ID, handler ID, issue code, and complete error text. Failure to deliver that issue returns the joined observer and delivery errors through the owning event-sink contract.
- A runtime transport failure makes only that runtime unavailable through `extensionruntime.Service`. Host continues with later available observers and reports the runtime failure through the implemented failure path.

### Active model selection

- Add `host/internal/usecase/host/modelselection`. UI, Programmatic Control, and extension selection requests call this service through interfaces declared by their consuming packages.
- `providers.Catalog` retains catalogue, credential, and active-selection ownership. It adds non-mutating target resolution and one full-selection validation-and-commit operation.
- For a model request, Host forms the original target from the requested provider and model. It preserves the active reasoning choice when the target model supports that choice and otherwise uses the target model default. For a reasoning request, Host combines the active provider and model with the requested reasoning choice.
- Model-selection and reasoning-selection handlers are separate registered kinds. Both receive the immutable original target selection and the current target selection in registration order.
- A handler returns exactly one action. `preserve` keeps the current target, `replace` supplies one complete target, and `reject` stops later handlers.
- An ordinary handler error or invalid action records an issue, preserves the current target received by that handler, continues later handlers, and leaves the runtime available.
- After handlers finish without rejection, Host validates provider-model existence, reasoning support, and credentials against the final current target. `providers.Catalog` commits provider, model, and reasoning choice under one lock.
- Model-selection operations are serialized. A selection request started from a selection handler receives `BUSY` instead of waiting on its own operation.
- Rejection, final validation failure, cancellation, or stale context performs no commit and emits no selection event.
- Host compares the preceding and committed selections. It emits reasoning selection first when reasoning changed, then model selection when provider or model changed. A successful no-op emits neither event.
- A client-initiated request returns a correlated completion. An extension-initiated selection also produces the client-neutral committed-selection connection event needed by the connected Glyph client.
- A post-commit observer or client-delivery error cannot roll back the selection. Host returns committed selection plus ordered issues to every reachable operation initiator.

### Extension entries and persistence

- Keep `session.ExtensionEnvelope` as model-hidden opaque JSON data.
- Add `session.ExtensionMessage` with extension ID, entry type, exact text, and `session.ClientVisibility`.
- `session.ClientVisibility` has only `visible` and `hidden`.
- `sessions.Service` exposes separate append operations for a model-hidden extension entry and a model-visible extension message. Both operations accept the expected active session ID.
- While holding the session lock, the service checks the expected session ID, validates the entry, uses the current active leaf as parent, persists one repository mutation, and publishes the candidate tree only after persistence succeeds.
- Add the JSONL record type `extension_message` with ID, parent ID, timestamp, extension ID, entry type, exact text, and client visibility. The `extension` record keeps its model-hidden meaning.
- Active history excludes model-hidden extension entries. It maps every model-visible extension message to provider-neutral user text regardless of client visibility.
- After persistence commit, Host emits `SessionEntryAdded` with the complete extension message and client visibility. Delivery precedes completion of the extension-initiated append operation.
- Client delivery failure leaves persistence and in-memory state committed. The extension receives the committed result and a `DELIVERY_FAILED` issue.

### Glyph client contracts and client visibility

- Add `ExtensionMessage` and `ClientVisibility` to `api/plugins/ui/v1/session.proto` and `api/programmatic/v1/session.proto`.
- UI Plugin Contract and Programmatic Control carry the extension message in session-tree entries, detailed session entries, active-branch replacements, navigation results, and `SessionEntryAdded` connection events.
- Both client contracts carry exact message text for `visible` and `hidden`. Client visibility is presentation data and is not a security boundary.
- Standard TUI includes `visible` messages in its ordinary transcript and excludes `hidden` messages. It retains both values in session-tree state without adding extension-defined rendering.
- Programmatic Control exposes both values through connection events, the session tree, and detailed entries. Its ordinary `GetMessages` projection excludes `hidden` messages.
- Add client-neutral committed-selection and `ExtensionIssue` connection events to UI Plugin Contract and Programmatic Control. Connection events have no operation ID.

### Session-tree navigation

- Extend `session.Tree.NavigationPreparation` so a model-visible extension message uses its parent as the navigation destination and its exact text as next input.
- The rule applies to `visible` and `hidden` client visibility because both messages remain in the complete session tree.
- Without a branch summary, the navigation destination becomes the active leaf. With a branch summary, `sessions.Service.CommitNavigation` attaches the new `BranchSummaryEntry` to that destination and makes the summary the active leaf.
- Session-tree orchestration keeps its implemented handler chain, final validation, persistence commit, and post-commit observer order. UI and Programmatic controllers return the Host result and never start Agent Core for returned next input.

### Package ownership and composition

Add these packages:

- `host/internal/usecase/host/extensioncontext` owns context binding and context operations.
- `host/internal/usecase/host/lifecycle` owns lifecycle registrations and observer policy.
- `host/internal/usecase/host/modelselection` owns selection handler policy and commit coordination.
- `host/internal/controller/extension` validates and maps extension-initiated protobuf operations to consumer-owned Host interfaces.

Existing owners remain:

- `host/internal/usecase/host/extensionruntime` owns runtime identity, availability, monitoring, and operation accounting.
- `host/internal/usecase/host/providers` owns model data, credentials, provider requests, and active selection state.
- `host/internal/usecase/host/sessions` owns active session state, tree mutation, persistence coordination, and history.
- `host/internal/usecase/host/sessiontree` owns navigation policy and handler orchestration.
- `host/internal/usecase/host/events` owns client-first Agent Core event dispatch.
- `host/internal/infra/plugins/extension` owns gRPC stream mapping and process transport.
- `host/internal/app` owns construction and late binding without business policy.

Each interface is declared in its consuming package. Implementations include compile-time interface assertions after the import graph is checked for cycles. Agent Core imports do not change.

Application composition performs this order:

1. Construct session state and runtime transport bindings.
2. Construct extension context, lifecycle, and model-selection services.
3. Bind extension-initiated operation dispatch before extension registration.
4. Load and accept extensions. Startup partitions registered session-tree, selection, and lifecycle handler kinds and asks each capability owner to validate and commit its registrations.
5. Construct and bind the provider catalogue.
6. Construct client delivery and Agent Core, then bind lifecycle delivery to `events.Dispatcher`.
7. Activate runtime monitoring at the implemented mode-specific point.

App late bindings contain no validation, ordering, or state-transition policy.

### Non-functional considerations and risks

- A slow lifecycle observer delays later Agent Core events. Host delivers the current event to the Glyph client first, and general cancellation cancels the observer operation. No queue is added for this local tool.
- A nested extension-initiated operation can deadlock when a stream receive loop executes handlers directly. Both SDK peers route work from the receive loop to independent operation execution before awaiting callbacks.
- A client delivery failure after session or selection commit leaves the client behind Host state. The committed operation result carries `DELIVERY_FAILED`, and the client can recover authoritative state through model and session queries after reconnection.
- Three public contracts can map extension messages or selection events differently. Contract tests compare exact content, visibility, selection, error category, and complete error text across UI Plugin Contract and Programmatic Control.

Runtime and operation logs use `slog` with context and include operation ID, extension ID, runtime instance ID, and session ID. Logs, protobuf payloads outside provider operations, and diagnostics exclude credentials, authorization headers, OAuth values, and provider reasoning context.

PHS-07 keeps the implemented local capacity of one active session, one active agent run, and one connected Glyph client. It adds no distributed coordination or cross-process shared state.

### Error semantics

All errors follow the shared [Error Semantics](../../prd.md#error-semantics). A code supplements complete error text and never replaces it.

| Operation | Closed failed categories |
|---|---|
| Context and catalogue | `STALE_CONTEXT`, `INTERNAL` |
| Configured-model request | `MODEL_UNAVAILABLE`, `CREDENTIAL_UNAVAILABLE`, `MODEL_FAILED`, `STALE_CONTEXT`, `INTERNAL` |
| Model selection | `MODEL_UNAVAILABLE`, `CREDENTIAL_UNAVAILABLE`, `EXTENSION_REJECTED`, `EXTENSION_UNAVAILABLE`, `STALE_CONTEXT`, `INTERNAL` |
| Extension entry append | `SESSION_UNAVAILABLE`, `PERSISTENCE_UNAVAILABLE`, `STALE_CONTEXT`, `INTERNAL` |

Request rejection uses the closed set `INVALID_ARGUMENT`, `OPERATION_ID_IN_USE`, `NOT_READY`, `BUSY`, `STALE_CONTEXT`, and `INTERNAL`. Cancellation uses the canceled terminal state.

Nonterminal issue codes are `HANDLER_ERROR`, `INVALID_HANDLER_ACTION`, `OBSERVER_ERROR`, and `DELIVERY_FAILED`. An issue contains extension ID, handler ID when applicable, and complete error text.

The Extension Contract transport limits extension-originated external error text to 65,536 UTF-8 bytes at ingress without splitting a UTF-8 sequence. The retained text ends with `\n[external error text truncated]` when truncation occurs. Only secrets are redacted. Later Glyph layers preserve the bounded text and every added context or cause without another truncation.

### Verification

- Add Extension SDK and runtime tests for operations in both directions, nested operations, cancellation, duplicate IDs, connection close, and runtime replacement.
- Add context tests for stale runtime, stale session, cwd, model catalogue, provider catalogue, and credential exclusion.
- Add configured-model tests for ordered history, visible reasoning content, provider-context omission, no tools, cancellation, failure categories, and unchanged active selection.
- Add lifecycle tests for every event kind, client-first delivery, observer registration order, ordinary observer errors, runtime failure, and issue-delivery failure.
- Add selection tests for original and current composition, preserve, replace, rejection, invalid action, atomic commit, concurrent selection, event order, no-op, and post-commit delivery failure.
- Add session domain and persistence tests for both extension record types, exact parent, restart, snapshot replacement, history projection, and failed writes.
- Add UI Plugin Contract and Programmatic Control tests for extension messages, client visibility, `SessionEntryAdded`, hidden transcript filtering, selection events, issues, and equivalent error text and categories.
- Add navigation tests for both client visibility values, exact next input, no-summary and branch-summary active leaves, and zero Agent Core calls.
- Add an external extension fixture that receives `agent_start`, makes a configured-model request, appends its result, transforms one selection, and receives `STALE_CONTEXT` after session replacement.
- Run `task generate` twice and require no diff from the second run. Then run `task fmt`, `task fix_dry_run`, accepted fixes, `task lint`, `task test`, `task itest`, and `task test-coverage`.

Keep `docs/roadmap.md` at Planned until implementation and every verification item pass.

## Overengineering and Overspecification Considerations

The solution reuses one extension stream, the shared operation lifecycle, `host/internal/usecase/host/startup`, session persistence, provider catalogue, and event dispatcher. It adds three capability packages because context operations, lifecycle observation, and model-selection transformation have different policy and state ownership.

The Rejected alternatives section excludes the additional service, generic capability abstraction, event queue, and opaque Host-owned message semantics. The solution also adds no provider-specific model API, compatibility layer, or implementation for middleware, compaction, retry, commands, notifications, provider extensions, or extension-defined rendering.

## Open Questions

None.

## References

- [Problem Statement](problem.md) - approved problem and boundary.
- [PRD](prd.md) - approved PHS-07 requirements.
- [Phase terminology](terms.md) - phase terminology index.
- [Domain Glossary](../../../../../terms.md) - shared Glyph terminology.
- [Target architecture](../../architecture.md) - process, package, state, and dependency ownership.
- [Delivery plan](../../delivery-plan.md) - phase order and dependencies.
- [PHS-05 technical solution](../05-session-tree/solution.md) - session-tree and branch-summarization behavior.
- [PHS-05.1 technical solution](../05.1-extension-boundary-cleanup/solution.md) - implemented extension runtime and capability ownership.
- `api/plugins/extension/v1` - current Extension Contract sources.
- `api/plugins/ui/v1` - current UI Plugin Contract sources.
- `api/programmatic/v1` - current Programmatic Control sources.
- `host/internal/usecase/host` - current Host use cases.
- `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/sessions.md` - feature reference for custom-message tree selection only.
