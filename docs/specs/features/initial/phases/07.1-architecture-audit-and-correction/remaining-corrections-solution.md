# Technical Solution: Remaining Architecture Corrections

## Problem Statement

The [PHS-07.1 ticket](ticket.md) requires every confirmed architecture violation to be corrected across its affected paths. The audited remaining corrections covered session-tree state, provider execution ownership, error retention and classification, connection cleanup, and the three error terms in the [domain glossary](../../../../../terms.md).

## Proposed Solution

### Implicit-root session-tree state

- A session tree has one active position. The position is either an existing entry or the implicit root before every root entry. `session.Tree.activeLeafID` represents these states as `mo.Some(id)` and `mo.None[string]()`.
- [`session.NewTree`](../../../../../../host/internal/domain/session/tree.go) accepts a nonempty tree at the implicit root and still rejects a present identifier that does not name an entry. No synthetic entry or storage-format change is used.
- [`Tree.Clone`](../../../../../../host/internal/domain/session/tree.go) copies all stored entries and labels and retains the active position. At the implicit root, `Tree.ActiveBranch`, `sessions.Service.ActiveEntries`, and the provider history returned by `sessions.Service.Snapshot` are empty.
- Public session clone has different semantics from `Tree.Clone`. [`sessions.Service.CloneActive`](../../../../../../host/internal/usecase/host/sessions/replacement_operations.go) copies the active branch, so cloning at the implicit root creates an empty replacement session and retains no labels.
- Navigation to a root user message or root model-visible extension message returns the absent parent as the destination and the selected text as the next input. A commit without a branch summary persists the implicit-root position, while a later append creates a new root entry and makes that entry the active leaf.
- [`TestTreeImplicitRootConstructionAndClonePreserveStoredState`](../../../../../../host/internal/domain/session/tree_test.go), [`TestTreeImplicitRootNavigationPreparesExactInput`](../../../../../../host/internal/domain/session/tree_test.go), and [`TestImplicitRootNavigationSurvivesRestartAndStartsANewRoot`](../../../../../../host/internal/infra/persistence/sessions/replacement_restart_integration_test.go) cover construction, domain clone, navigation input, persistence replay, labels, empty history, and append. The session-clone case remains covered in [`replacement_operations_test.go`](../../../../../../host/internal/usecase/host/sessions/replacement_operations_test.go).

The [product PRD](../../prd.md#context-and-sessions) and [architecture](../../architecture.md#data-models) own the active-position and navigation semantics.

### Host model-execution ownership

- The Host has one [`modelexecution.Service`](../../../../../../host/internal/usecase/host/modelexecution/service.go). It implements the logical streaming contract owned by Agent Core and the configured one-response contracts owned by Extension Context and session-tree summarization.
- The Host model-execution package owns one raw [`modelexecution.ProviderAttempt`](../../../../../../host/internal/usecase/host/modelexecution/contracts.go) contract. Each configured provider/model catalogue entry has one raw provider binding. Codex and OpenAI-compatible drivers implement this contract and do not implement a second direct configured-request contract.
- The provider catalogue retains exact selection resolution, reasoning validation, credential preflight, active-selection state, and descriptor snapshots. `modelexecution.Service` validates and clones history before dispatch, passes tools only for Agent Core requests, forwards semantic stream events to Agent Core, and performs configured-request terminal reduction once.
- PHS-07.1 establishes this owner with exactly one raw provider attempt for each logical request. Drivers perform no retry. [PHS-06](../06-context-compaction-retry-control/ticket.md) adds retry decisions, delays, and repeated attempts inside the same Host owner without changing Agent Core, configured consumers, catalogue bindings, or drivers.
- [`TestServiceStreamsOneLogicalRequest`](../../../../../../host/internal/usecase/host/modelexecution/service_test.go), [`TestServiceConfiguredRequestReturnsDetachedTerminal`](../../../../../../host/internal/usecase/host/modelexecution/service_test.go), [`TestServiceConfiguredRequestRejectsMissingTerminal`](../../../../../../host/internal/usecase/host/modelexecution/service_test.go), and [`TestServiceConfiguredRequestPreservesProviderFailure`](../../../../../../host/internal/usecase/host/modelexecution/service_test.go) cover one-attempt dispatch and the shared terminal reduction.

The [architecture model contracts](../../architecture.md#programming-interfaces) own the dependency direction. The [delivery plan](../../delivery-plan.md) owns the phase allocation.

### Provider source ordering and mixed cancellation

- Codex `Driver.streamError` and OpenAI-compatible `Driver.Stream` classify the complete acquired error tree before projecting cancellation. Pure cancellation keeps the canonical aborted result. A mixed provider failure retains cancellation and every independent provider cause, while terminal `model.Response.ErrorMessage` contains the independent provider diagnostic.
- Codex `semanticAssembler.consume` constructs the provider source before content finalization for `response.failed`, non-`max_output_tokens` `response.incomplete`, and `error`. It joins that source with a failed `ContentEnd` finalization or with a later terminal-assembly error, then stops callback use after a callback error.
- After assembler processing, Codex `Driver.Stream` joins the stream error with a failed final terminal callback only when it attempts that callback. The driver performs one attempt and does not retry content or terminal callbacks.
- Agent Core [`visibleErrorMessage`](../../../../../../host/internal/usecase/agent/run/service.go) uses canonical cancellation text only for pure cancellation and exact mixed-error text otherwise. [`Service.executeCalls`](../../../../../../host/internal/usecase/agent/run/service.go) applies the same rule to the tool terminal path.
- Programmatic Control [`filterRunError`](../../../../../../host/internal/usecase/host/programmatic/service.go) returns `nil` only for errors that are pure under its `context.Canceled` policy. Host UI [`preparedUIOperation.Run`](../../../../../../host/internal/usecase/host/ui/prepared_operations.go) returns a canceled outcome only for errors that are pure under its canceled-or-deadline policy. Both consumers return the original mixed error object without rebuilding its cause tree.
- Internal returned errors preserve identity for `errors.Is` and `errors.As`. Agent Core lifecycle text and `model.Response.ErrorMessage` are internal string projections.
- UI Plugin Contract and Programmatic Control fields are public string/category projections. String projections preserve complete text but do not transport Go error identity.
- Public projections preserve the stable category selected by the applicable Host operation mapping.
- [`TestDriverStreamMixedCancellationPreservesAcquiredProviderFailure`](../../../../../../host/internal/infra/providers/openai/codex/failure_test.go), the compatible driver's `TestMixedCancellationPreservesAcquiredProviderFailure` in [`stream_failure_test.go`](../../../../../../host/internal/infra/providers/openai/compatible/stream_failure_test.go), [`TestServiceRunToolMixedCancellationUsesOriginalTerminalText`](../../../../../../host/internal/usecase/agent/run/cancellation_test.go), [`TestRunPreparedReturnsOriginalMixedCancellation`](../../../../../../host/internal/usecase/host/programmatic/prepared_test.go), and [`TestPreparedCancellationRemovesOnlyCancellationLeaves`](../../../../../../host/internal/usecase/host/ui/prepared_operations_test.go) cover the driver, Agent Core, Programmatic Control, and Host UI decision points. [`TestDriverStreamFailureEventsPreserveSourceWhenContentEndFails`](../../../../../../host/internal/infra/providers/openai/codex/failure_test.go) covers provider-source ordering before finalization.

The [PRD Error Semantics](../../prd.md#error-semantics) own complete public error behavior.

### Extension registration publication

- The Extension SDK server commits registration handlers and readiness before the successful registration response enters raw transport `Send`. Therefore, client observation of registration success implies that the server accepts valid registered operations.
- [`TestServerPublishesRegistrationBeforeSuccessfulSendReturns`](../../../../../../sdk/plugins/extension/v1/server_test.go) covers immediate Execute after the registration response becomes observable while its raw `Send` remains blocked. A registration response send failure follows [connection cleanup](#connection-cleanup).

### Connection cleanup

- Programmatic Control and the Extension SDK server wrap raw server sends with `operation.SendWithContext`. On fatal failure, each handler stops admission, cancels work, joins context-aware application work and `operation.Writer.Run`, collects acquired sources, and returns. Neither server uses a production cleanup timeout or waits for nested raw transport goroutines; handler return lets gRPC cancel those raw calls.
- The UI SDK server keeps its context-aware send and logical-cleanup path. It joins logical writer and operation bookkeeping before handler return and does not add a production cleanup timeout.
- Host UI fatal cleanup invokes the runtime-owned [`Service.Cancel`](../../../../../../host/internal/infra/plugins/ui/runtime/operations.go) operation before transport-dependent waits. [`Service.closeFailedOperations`](../../../../../../host/internal/controller/ui/operations.go) joins the writer before `CloseSend`, then joins receive work.
- Host UI unsuccessful startup keeps its close handshake for one second. When that grace period expires, [`Service.closeUnsuccessfulInitialization`](../../../../../../host/internal/infra/plugins/ui/runtime/initialization.go) cancels the actual RPC and completes cleanup.
- Extension SDK client normal close gives the close protocol one second. When that grace period expires, [`Connection.Close`](../../../../../../sdk/plugins/extension/v1/host.go) cancels the actual RPC and retains the timeout in completion reporting. Its fatal path keeps immediate RPC cancellation before joins.
- [`TestFatalCleanupReturnsBeforeUnreadRawTransport`](../../../../../../host/internal/controller/programmatic/real_grpc_cleanup_integration_test.go), [`TestBlockedTransportFailureReturnsExtensionHandler`](../../../../../../sdk/plugins/extension/v1/server_blocked_transport_integration_test.go), and [`TestBlockedTransportFailureReturnsUIServerHandler`](../../../../../../sdk/plugins/ui/v1/server_blocked_transport_integration_test.go) cover server handler return after logical cleanup. [`TestFatalBlockedHostUISendCancelsRPCBeforeCloseSend`](../../../../../../host/internal/infra/plugins/ui/runtime/client_cleanup_integration_test.go), [`TestUnsuccessfulInitializationCleanupUsesGraceThenCancelsRPC`](../../../../../../host/internal/infra/plugins/ui/runtime/client_cleanup_integration_test.go), and [`TestConnectionCloseCancelsBlockedRawSendAfterGrace`](../../../../../../sdk/plugins/extension/v1/client_close_blocked_transport_integration_test.go) cover actual RPC cancellation and the two one-second grace periods.

### Error glossary alignment

The [domain glossary](../../../../../terms.md) owns the meanings of `complete error text`, `original cause`, and `external error data`. Transport message-size and queue limits remain separate constraints and do not redefine retained error completeness.

## Overengineering and Overspecification Considerations

- The implicit root uses the existing optional active-position and persisted navigation representation.
- One Host model-execution owner and one raw provider-attempt contract avoid dual driver routes and duplicate terminal reduction.
- Error handling classifies the existing error tree instead of introducing another error model.
- Server cleanup relies on logical cancellation and handler return. Only the two reproduced client graceful-close paths use a bounded grace period.

## Open Questions

None.

## References

- [PHS-07.1 ticket](ticket.md) - Defines the audit correction and acceptance scope.
- [PHS-07.1 phase solution](solution.md) - Records implementation and verification status.
- [Initial product PRD](../../prd.md) - Owns product behavior.
- [Target architecture](../../architecture.md) - Owns component and contract boundaries.
- [Delivery plan](../../delivery-plan.md) - Owns phase order and allocation.
- [Domain glossary](../../../../../terms.md) - Owns project terminology.
