# Technical Solution: PHS-05.1 Extension Boundary Cleanup

## Problem Statement

The [Problem Statement](problem.md) defines the prototype coupling, combined extension responsibilities, and oversized public protobuf sources addressed by this phase. The [PRD](prd.md) defines the approved requirements and behavior-preservation checks.

## Proposed Solution

### Solution overview

- Remove the prototype hook path without adding replacement middleware.
- Replace the combined extension service with explicit runtime-management, tool, and session-tree owners.
- Coordinate extension registration through the existing Host startup use case.
- Split the Extension Contract, UI Plugin Contract, and Programmatic Control sources by responsibility while preserving their public declarations and service behavior.

### Design decisions

- DEC-01: Replace `host/internal/usecase/host/extensions` with `host/internal/usecase/host/extensionruntime`, add `host/internal/usecase/host/tools`, retain capability orchestration in `host/internal/usecase/host/sessiontree`, and coordinate registration in `host/internal/usecase/host/startup`. Keeping the broad `extensions` package was rejected because it would leave one natural owner for unrelated future capability policy.
- DEC-02: Split each protobuf contract by responsibility rather than by line ranges. Add no protobuf file for a capability owned by PHS-07 or a later phase. A minimum line-count split was rejected because it would keep unrelated declarations together and require another split as contracts grow.
- DEC-03: Application composition activates runtime monitoring after its configured runtime-failure sink is ready. Runtime management owns monitoring behavior, while composition owns dependency-readiness order. Buffering failures before UI initialization was rejected because it would mix client delivery with runtime management and require new queue semantics.

### Prototype hook removal

- Delete `host/internal/hooks` and `host/internal/hooks/runner`.
- Remove `hooks.ContextRunner` from `agent/run.Service`, its constructor, and all application compositions.
- Pass `Service.ProjectHistory()` directly as `ModelRequest.History`. Add no context middleware until PHS-08.
- Remove `hooks.ProviderRunner`, the Codex `hookTransport`, the Codex hook configuration and state, and the hook transport wrapping in `Driver.executeRequest`.
- Wrap the configured base HTTP transport directly with the Codex error-capture transport. Keep request creation and response streaming in `Driver.executeRequest`, and keep error composition in the Codex provider error path.
- Remove the unreachable `internal_hook_failed` response path and tests that exercise only the deleted hook contracts. Retain model-context, provider-request, provider-response, lifecycle, cancellation, and error-preservation tests.

### Extension runtime domain

Add `host/internal/domain/extension` for the implemented runtime availability model. Move `RuntimeFailure`, `RuntimeUnavailableCondition`, `RuntimeUnavailableProcessExited`, and their message formatting from `host/internal/domain/tool` without changing the produced message.

Headless, UI, and Programmatic Control failure delivery shall consume `extension.RuntimeFailure`. This removes extension-process state from the tool domain without changing public failure categories or text.

### Extension runtime management

`host/internal/usecase/host/extensionruntime` owns:

- extension discovery and process startup;
- registration operation invocation and raw registration results;
- runtime generation and availability;
- low-level tool and session-handler operation invocation;
- active-operation accounting;
- process monitoring, cancellation, and shutdown;
- one runtime-failure report for each transition to unavailable.

`host/internal/usecase/host/startup` declares the runtime-loading, tool-registration, and session-tree-registration interfaces that it consumes. The handoff types contain raw candidate registrations, issues, and accepted registrations without importing a concrete implementation package. `extensionruntime`, `tools`, and `sessiontree` implement those interfaces and contain compile-time assertions. This dependency graph keeps `startup` independent from the three implementations.

The runtime manager indexes runtimes by extension ID. It does not own tool names, tool conflicts, handler order, handler action policy, or model-visible unavailable-tool results.

The runtime manager directly implements the interfaces declared by `tools` and `sessiontree`. Implementations include compile-time interface assertions. No compatibility alias, forwarding method, or shared generic capability interface is added.

### Tool capability orchestration

Add `host/internal/usecase/host/tools`. Its service owns:

- accepted tool registrations and tool-to-extension ownership;
- deterministic tool-name conflict detection;
- the sorted model-visible tool list;
- tool descriptor field validation and input-schema compilation;
- deterministic JSON argument serialization and schema validation;
- low-level runtime invocation through a consumer-owned interface;
- mapping runtime results to `agent.ToolResult`;
- the model-visible `tool "<name>" is unavailable` result.

`tools.Service` implements `run.ToolRuntime`, so Agent Core continues to depend only on its consumer-owned tool interface. The runtime interface identifies the owning extension explicitly and exposes only availability and tool invocation needed by this service.

`host/internal/infra/plugins/extension/runtime` maps protobuf registration and execution payloads to Host types and enforces stream protocol rules. Tool descriptor policy, schema compilation, and argument validation move out of this transport package into `tools`.

### Session-tree capability orchestration

`host/internal/usecase/host/sessiontree` additionally owns:

- accepted session-tree handler registrations;
- empty and duplicate handler-ID checks;
- handler-kind checks and deterministic order;
- missing and mismatched action checks;
- request and result action application;
- final navigation-state validation and operation issues.

Replace `sessiontree.HandlerRunner` with a narrower consumer-owned runtime interface for availability and invocation of one registered session-tree handler. The session-tree service performs the handler chain and state transitions. Runtime management performs only the selected low-level invocation and runtime-failure handling.

### Extension loading sequence

`host/internal/usecase/host/startup` coordinates registration without owning tool or session-tree policy:

1. Ask the runtime manager to discover, start, and register every candidate as a pending runtime.
2. Ask `tools` to validate each extension's tool descriptor fields, constrained sampling, and input schemas. Exclude extensions that fail local tool validation.
3. Ask `sessiontree` to validate handler registrations from the remaining extensions. Exclude extensions that fail handler validation.
4. Ask `tools` to detect cross-extension conflicts among the remaining registrations. Reject every extension in each conflicting tool-name group.
5. Ask the runtime manager to close rejected pending runtimes without emitting a post-start runtime-failure report.
6. Commit tool and handler registrations only for accepted runtimes, then mark those runtimes available.
7. Sort and return startup issues and loaded-extension information.

This order preserves registration error precedence: local tool validation, handler validation, then cross-extension tool conflicts.

### Runtime monitoring activation

Application composition calls runtime-manager activation after the configured runtime-failure sink is ready:

- Headless activates after successful extension startup because its stderr renderer already exists.
- Programmatic Control activates after successful extension startup because its structured logger sink already exists.
- UI activates through `Session.afterInitialization` after the initialization operation has been delivered. Initialization delivery failure does not activate monitoring.

The UI plugin does not receive runtime-management authority and does not call the runtime manager. The composition callback only establishes that the Host delivery dependency is ready. Runtime management remains independent from Headless, UI Plugin Contract, and Programmatic Control types.

After activation, the runtime manager is the source of runtime availability. Only an accepted runtime that changes from available to unavailable emits a post-start runtime-failure report. `tools` and `sessiontree` check availability when listing registrations and before invocation. A process exit between those checks and an invocation returns an error that wraps `extensionruntime.ErrExtensionUnavailable`. The runtime manager completes active-operation accounting and marks the runtime unavailable. When an active invocation reports `ErrExtensionUnavailable`, the runtime manager closes a runtime that changed from available to unavailable. It reports one runtime failure for that availability transition.

### Protobuf source split

All new files retain their contract's protobuf `package`, Go `go_package`, and edition. Cross-file references use protobuf imports.

#### Extension Contract

- `extension.proto`: `ExtensionService`, stream envelopes, operation lifecycle envelopes, and registration requests and results.
- `tool.proto`: tool descriptors, constrained sampling, execution requests, progress, and results.
- `session_tree.proto`: handler descriptors, handler operations, handler actions, and session-tree payloads.

`ExtensionService` moves from `tool.proto` to `extension.proto`. Its generated Go service name remains `ExtensionService`, and its RPC remains `Open(stream OpenRequest) returns (stream OpenResponse)`.

#### UI Plugin Contract

- `ui.proto`: `UIService`, stream envelopes, initialization, operation lifecycle, and connection events.
- `model.proto`: configured models, reasoning, model selection, and authentication payloads.
- `session.proto`: session commands, results, entries, tree data, and statistics.
- `agent.proto`: agent events, extension availability, model content, tool content, usage, and diagnostics.

#### Programmatic Control

- `programmatic.proto`: `ProgrammaticControlService`, stream envelopes, controller dispatch, operation lifecycle, and run state.
- `model.proto`: model catalogue, reasoning, and selection.
- `session.proto`: session commands, results, entries, tree data, and statistics.
- `agent.proto`: agent events, model content, tool execution, usage, and run outcomes.

The split preserves every moved declaration's fully qualified name, field number, enum value, generated Go package, and exported type or service name. Generated filenames and protobuf file-descriptor symbols can change because they reflect source-file ownership rather than the public contract described by the PRD. No resulting protobuf source file may exceed 500 lines.

### Error preservation

- Runtime invocation errors retain their original causes and every Glyph context message.
- Moving runtime-failure types out of the tool domain does not change public error categories or formatted text.
- Tool unavailability remains a model-visible tool result where the tool use case owns that meaning.
- Session-tree handler errors remain operation issues unless cancellation or the owning session-tree rules make them terminal.
- Only the runtime manager decides that a process became unavailable and emits the corresponding runtime-failure report.

### Test and generation strategy

This phase changes structure without changing behavior. Existing tests form the regression harness, and tests that only exercise deleted hook behavior are removed rather than replaced with absence tests.

When implementation exposes an unintended behavior change, add or update a behavioral test first and observe the expected failure before changing production behavior. For structural moves, run the affected tests before and after each move.

Verification shall include:

- a production-source search with no `host/internal/hooks` or `host/internal/hooks/runner` reference;
- tool registration, schema and argument validation, conflict, execution, progress, unavailability, and runtime-exit tests under the tool and runtime owners;
- session-tree registration, handler order, action validation, observer, cancellation, and runtime-exit tests under the session-tree and runtime owners;
- UI initialization tests that prove initialization delivery failure prevents activation and successful initialization precedes runtime-failure delivery;
- model context, Codex request serialization, streaming response, lifecycle, and complete-error tests after hook removal;
- external extension and UI plugin fixtures against regenerated packages;
- two consecutive `task generate` runs with no diff from the second run;
- `task fmt`, `task fix_dry_run`, accepted fixes, `task lint`, `task test`, `task itest`, and `task test-coverage`.

### Documentation updates

Replace documentation links that name a moved protobuf source with either the owning replacement file or the contract directory. Keep `docs/roadmap.md` at Planned until implementation and all required verification complete.

## Overengineering and Overspecification Considerations

The solution adds one tool use case and one extension runtime domain package because both represent behavior already implemented in the combined service. It reuses the existing startup and session-tree use cases. It adds no generic capability registry, dynamic payload codec, compatibility layer, future capability package, provider migration, or replacement middleware.

The protobuf split uses three files for the Extension Contract and four files for each client contract. Each file corresponds to an implemented responsibility and no file anticipates a later phase.

## Open Questions

None.

## References

- [Problem Statement](problem.md) - approved structural problem and boundary.
- [PRD](prd.md) - approved requirements and verification outcomes.
- [Target architecture](../../architecture.md) - Agent Core, Host, runtime-management, and capability ownership boundaries.
- [Delivery plan](../../delivery-plan.md) - phase order and dependencies.
- [PHS-07 ticket](../07-extension-context-lifecycle/ticket.md) - next extension capability phase.
