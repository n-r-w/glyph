# TUI ownership gap

Revised correction 5 closes BLK-01 through BLK-04. BLK-05 remains open for correction 7. Main-agent source verification and repeated project checks passed. U5 has a separate verified local commit; the user authorized the remaining correction units. [U5 evidence](solution.md#u5-bundled-tool-and-tui-ownership) records the implementation, test migration, local regression correction, and verification results.

The stopped attempt above commit `5f9fb7863b67d4e1767d8d62e9243999d3e65265` established this gap. That attempt kept domain `State.Apply` beside direct controller mutations. The revised implementation replaces those owners rather than accepting the stopped attempt. [Ticket FRQ-11 through FRQ-14 and NFQ-05](ticket.md#requirements) remain the acceptance requirements.

## Key definitions and abbreviations

- Transition owner. The application component that decides how Host results and user actions change TUI state.
- Display snapshot. Detached view data produced from one application state for terminal rendering. It does not permit the renderer to mutate application state.
- Existing edge. A package import in the implementation being inspected.
- Proposed edge. An import required by a considered change but absent from that implementation.

## Closed questions

### QST-01: A pure reducer does not establish domain ownership

- The former domain `State.Apply` was pure, but its data included application and interface-specific state. The former presentation service only forwarded to it.
- The [presentation usecase](../../../../../../plugins/ui/tui/internal/usecase/presentation/service.go) now owns private projection and interaction state, admission, pending commands, and runtime policy. Its private reducer is an implementation detail of that owner, not a domain package or service boundary.
- Source of truth: [ticket FRQ-03 through FRQ-05 and FRQ-11](ticket.md#functional-requirements); [target ownership](solution.md#tui).

### QST-02: There is one state instance, but inconsistent control of its transitions

- The stopped attempt had one state instance with transition decisions split across controllers and the reducer. Revised `Service.model` contains the private state and interaction decisions.
- [Projection mutation](../../../../../../plugins/ui/tui/internal/usecase/presentation/projection.go) invalidates detached display data at the mutation owner. [Snapshot publication](../../../../../../plugins/ui/tui/internal/usecase/presentation/snapshot.go) rebuilds changed projection data and reuses it for editor-only transitions.
- [The local name regression](../../../../../../plugins/ui/tui/internal/usecase/presentation/local_name_test.go) exercises `Service.Key` and captures actual `Display.Publish` calls. It prevents a locally changed transcript from remaining invisible until unrelated Host work arrives.
- Source of truth: [ticket FRQ-11](ticket.md#functional-requirements); [SCN-02](ticket.md#scn-02-keep-tui-state-consistent-across-a-session-operation).

### QST-03: One owner must preserve distinct progress and completion transitions

- [Tree interaction](../../../../../../plugins/ui/tui/internal/usecase/presentation/tree_interaction.go) replaces the transcript on committed navigation progress. Terminal metadata supplies exact next input and closes the interaction. Rejected resume retains the draft and preceding transcript.
- [Navigation tests](../../../../../../plugins/ui/tui/internal/usecase/presentation/input_tree_test.go) and [session tests](../../../../../../plugins/ui/tui/internal/usecase/presentation/input_session_test.go) now exercise the application owner. They retain the progress-before-terminal and confirmation-before-replacement assertions.
- Source of truth: [ticket SCN-02 and NFQ-05](ticket.md). Correction 5 preserves behavior; correction 7 separately implements persistence-cause semantics.

### QST-04: The revised target has an acyclic dependency graph

- Actual `go list -json ./plugins/ui/tui/...` dependency closures exclude the implementation for each assertion pair. The usecase implements both input-controller contracts. Terminal runtime implements usecase display/runtime contracts. The Host adapter implements the usecase Host contract and terminal notification-source contract. Terminal device I/O implements the runtime's device/file contracts.
- Neither input controller depends on another TUI implementation. Terminal runtime does not import the concrete device or Host adapter. App constructs and binds them before SDK activation.
- `ReceiveNotification` names the bound adapter operation. No public SDK Host is wired as that port, no forwarding `Receive` method remains on the adapter, and no SDK-to-TUI assertion import was added. `ifaceguard` passes without suppression.
- Source of truth: [target import constraints](solution.md#target-import-constraints); [U5 checks](solution.md#u5-bundled-tool-and-tui-ownership).

## Blockers

### BLK-01: TUI application transitions remain distributed across state and controllers

- Status: Closed by revised U5.
- The [application command owner](../../../../../../plugins/ui/tui/internal/usecase/presentation/commands.go) handles keys, command acknowledgements, pending operations, foreground cancellation, and Host notifications. Private interaction methods apply resume, navigation, fork, clone, and send-failure transitions. Controllers and renderers cannot access private state.
- The [terminal Model](../../../../../../plugins/ui/tui/internal/infra/terminal/model.go) calls input controllers from the existing Bubble Tea event loop. Prepared work captures immutable command data and performs I/O only. No application queue or background state mutation was added.
- Evidence includes [command lifecycle tests](../../../../../../plugins/ui/tui/internal/usecase/presentation/command_lifecycle_test.go), [snapshot isolation tests](../../../../../../plugins/ui/tui/internal/usecase/presentation/snapshot_test.go), the session/navigation tests in QST-03, and the RED/GREEN regression in QST-02.
- Resolution source of truth: [ticket FRQ-11 and NFQ-05](ticket.md#requirements); [TUI solution](solution.md#tui).

### BLK-02: Presentation-domain types still carry interface-specific updates

- Status: Closed by revised U5.
- [Plugin input contracts](../../../../../../plugins/ui/tui/internal/controller/plugin/interfaces.go) contain neutral initialization and correlated notifications. [Tagged payloads](../../../../../../plugins/ui/tui/internal/controller/plugin/payload.go) select typed lifecycle, text, availability, selection, session, or tree groups. Optional payload presence remains distinct from a valid zero-valued payload.
- [Terminal input contracts](../../../../../../plugins/ui/tui/internal/controller/tui/interfaces.go) contain decoded keys and asynchronous work results. [Outgoing application ports](../../../../../../plugins/ui/tui/internal/usecase/presentation/interfaces.go) consume Host commands and coherent [display snapshots](../../../../../../plugins/ui/tui/internal/usecase/presentation/snapshot.go).
- `domain/presentation` is removed. Private `event` and `treeEvent` values are reducer details inside the application owner; they cross no controller or output interface. No shared contract package, type alias, or compatibility callback replaces the removed boundary.
- Resolution source of truth: [ticket FRQ-04, FRQ-07, and FRQ-12](ticket.md#functional-requirements); [target import constraints](solution.md#target-import-constraints).

### BLK-03: SDK input, output, and application cancellation policy remain combined

- Status: Closed by revised U5.
- The [plugin input controller](../../../../../../plugins/ui/tui/internal/controller/plugin/controller.go) validates SDK input and calls its application port. It owns no pending-command map, foreground choice, outbound SDK method, or program lifetime policy.
- The [Host adapter](../../../../../../plugins/ui/tui/internal/infra/host/service.go) retains SDK binding and active context, encodes commands, calls Start/Cancel/Close, and reads notifications. The usecase captures Stop targets before work starts and releases foreground ownership on terminal input, even when dispatch acknowledgement arrives later.
- [SDK lifecycle tests](../../../../../../plugins/ui/tui/internal/infra/host/lifecycle_integration_test.go) retain initialization order, source errors, active-context cancellation, and terminal cleanup. [SDK operation tests](../../../../../../plugins/ui/tui/internal/infra/host/operations_integration_test.go) retain ordered progress/completion through the real input and application owners.
- Resolution source of truth: [ticket FRQ-11 through FRQ-13](ticket.md#functional-requirements); [operation-contract foreground rule](../../../../issues/blocking-contract-operation-processing/solution.md#operation-mapping).

### BLK-04: Tree selection depends on mixed interaction and rendering logic

- Status: Closed by revised U5.
- [Application tree policy](../../../../../../plugins/ui/tui/internal/usecase/presentation/tree.go) owns filtering, folding, ordered visible entries, nearest visible parents, active-branch facts, and selection reconciliation. `VisibleEntry` contains no indentation or connector columns.
- [Terminal geometry](../../../../../../plugins/ui/tui/internal/infra/terminal/tree_geometry.go) computes depth, ancestor continuation columns, and following-sibling connectors from those semantic entries. [Rendering](../../../../../../plugins/ui/tui/internal/infra/terminal/tree_render.go) owns dimensions, normalization, wrapping, and display text.
- [Semantic tree tests](../../../../../../plugins/ui/tui/internal/usecase/presentation/tree_test.go), [geometry tests](../../../../../../plugins/ui/tui/internal/infra/terminal/tree_geometry_test.go), and [selector rendering tests](../../../../../../plugins/ui/tui/internal/infra/terminal/selector_rendering_test.go) retain the corresponding behavior at each owner.
- Resolution source of truth: [ticket FRQ-13](ticket.md#functional-requirements); [TUI solution](solution.md#tui).

### BLK-05: Persistence-cause cleanup still targets the old presentation boundary

- Status: Open for correction 7. Revised U5 changes its location, not its semantics.
- [Audit FND-06](audit.md#fnd-06-tui-persistence-cleanup-depends-on-diagnostic-wording) remains open. [Connection-error input mapping](../../../../../../plugins/ui/tui/internal/controller/plugin/request_mapping.go) still discards the category. Application operation-failure policy still reduces errors to text. The private [projection reducer](../../../../../../plugins/ui/tui/internal/usecase/presentation/state.go) still selects cleanup from the diagnostic prefix.
- Resolution: Correction 7 must preserve the approved category and complete text at the input boundary and make the application owner clear provisional state from the semantic cause. Keep the public category scope and add no connection-event kind. Executable RED/GREEN evidence is still required.
- Resolution source of truth: [ticket FRQ-14](ticket.md#functional-requirements); [source-backed persistence classification](solution.md#source-backed-persistence-classification).
