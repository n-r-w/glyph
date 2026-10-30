# Analysis Results: PHS-08 current behavior

## Scope

Current behavior for every requirement in [the ticket](ticket.md) and scenario in [the target picture](target-picture.md), before implementation. This report records inspected code and existing test assertions. It selects no implementation design.

## Key definitions and abbreviations

Use the [domain glossary](../../../../../terms.md), especially original extension input, current extension value, prepared history, and configured-model request.

## Executive Summary

The assembled clients submit text to the shared Host run coordinator. Agent Core creates the user message and model request. Host model execution prepares conversation history and coordinates provider attempts and retries. Public extension declarations currently cover tree, lifecycle, selection, retry, and compaction handlers. Concrete in-process provider adapters construct their own HTTP requests through the OpenAI SDK.

## Background and context

PHS-08 is planned after the completed PHS-06 and the two verified cross-cutting corrections. The ticket specifies additional model-facing extension operations and a consumer-owned effective-context port. Target architecture statements are requirements, not evidence of current implementation.

## Method and data sources

Three read-only investigations traced input/Core, model/provider, and registration/runtime paths. Primary declarations and central execution bodies were re-read when composing this report. No additional tests ran during fact collection. Existing assertions below are inspected test source, not new runtime observations. [Project baseline](project-state.md) records the independently executed whole-project checks.

## Observations

### FCT-01: Public user input and admission

Programmatic `UserRequest` contains text. Its controller maps nonempty text into `Command.UserText`. UI `SubmitCommand` also contains text, and `Command.SubmittedText` checks nonempty text. Standard TUI trims its editor text before sending submit. One-shot parsing preserves the supplied argument after checking that its trimmed value is nonempty.

Sources:
- `api/programmatic/v1/programmatic.proto`, `UserRequest`, lines 58-61.
- `host/internal/controller/programmatic/request.go`, `mapStandardRequest`, lines 62-74.
- `api/plugins/ui/v1/ui.proto`, `SubmitCommand`, lines 251-254.
- `host/internal/controller/ui/validation.go`, `Command.SubmittedText`, lines 11-17.
- `plugins/ui/tui/internal/usecase/presentation/update.go`, `interaction.updateEnter`, lines 303-384.
- `host/internal/controller/cli/headless/command.go`, `Parse`, lines 20-50.

All three modes reach `runcontrol.Coordinator`. `PrepareRun` reserves the shared operation gate and run ID. Programmatic and UI prepare work before public acceptance, then execute after acceptance acknowledgement. `Session.prepareSubmit` calls `RunPrepared` with the original string. Core `Service.Run` emits agent-start and appends `model.TextMessage(request.UserText)` before entering the turn loop.

Sources:
- `host/internal/usecase/host/runcontrol/coordinator.go`, `Coordinator.PrepareRun`, `RunPrepared`, and `Run`, lines 71-123.
- `internal/operation/operation.go`, `Owner.Start` and `Owner.run`, lines 246-365.
- `host/internal/usecase/host/programmatic/prepared.go`, `runPrepared.Run`, lines 225-275.
- `host/internal/usecase/host/ui/prepared_operations.go`, `Session.prepareSubmit`, lines 290-337.
- `host/internal/usecase/agent/run/service.go`, `Service.Run`, lines 88-119.

### FCT-02: Images and current capability checks

The provider-neutral `model.Message` contains ordered `InputContent` with text or image media type and bytes. The configured-model extension operation already maps text/images to that representation and rejects structurally invalid blocks. `RequestConfigured` rejects user images for a descriptor without image modality before calling its execution engine. The public user-submit paths in FCT-01 pass a string and have no image collection or fully-handled-input result.

Sources:
- `host/internal/domain/model/model.go`, `Message` and `InputContent`, lines 129-154.
- `host/internal/controller/extension/configured_model.go`, `mapConfiguredRequest` and `mapConfiguredInput`, lines 26-125.
- `host/internal/usecase/host/modelexecution/service.go`, `Service.RequestConfigured`, lines 245-294.

Inspected test `TestPublicConfiguredRequestAcrossApplicationModes` covers configured text/image operations in headless, UI and Programmatic modes. Its helper checks structural and unsupported-image errors and provider call counts. This tests configured-model input, separately from public user submission.

Sources:
- `host/internal/app/configured_request_modes_integration_test.go`, `TestPublicConfiguredRequestAcrossApplicationModes`, lines 35-133.
- `host/internal/app/configured_input_assertions_integration_test.go`, `assertConfiguredInputErrors`, lines 13-39.

### FCT-03: Instructions and effective context

Each assembly passes embedded coding-agent instructions into `agentrun.New`. Core stores the instruction string. Each `runTurn` gets tools and the model snapshot, then constructs `ModelRequest` with those instructions and `s.ProjectHistory()`. Core's current outgoing ports include history snapshot/append, model snapshot/stream, tools, and events. They contain no separate effective-context operation.

Sources:
- `host/internal/config/codingagent/prompt.go`, `Instructions`, lines 8-15.
- `host/internal/app/headless.go`, lines 103-106; `programmatic.go`, lines 115-118; `ui.go`, lines 143-147.
- `host/internal/usecase/agent/run/interfaces.go`, `ModelRequest`, `HistoryStore`, `ModelRuntime`, `ModelProvider`, `ToolRuntime`, `EventSink`, lines 25-60 and 400-417.
- `host/internal/usecase/agent/run/service.go`, `Service.runTurn`, lines 180-289; `Service.ProjectHistory`, lines 156-158.

Host model execution captures the conversation owner, resolves the captured selection, clones request history, and calls `prepareProviderContext`. The latter invokes the captured owner's `Prepare` and replaces only local request history. The captured implementation snapshots session compaction history and can invoke the policy owner before returning prepared history.

Sources:
- `host/internal/usecase/host/modelexecution/service.go`, `Service.Stream` and `Service.prepareProviderContext`, lines 129-242.
- `host/internal/usecase/host/extensioncontext/conversation_preparation.go`, `capturedConversation.Prepare` and `prepare`, lines 13-76.

### FCT-04: Request history and persisted entries

Core's `ProjectHistory` clones retained entries, excludes failed/aborted model responses, orders tool results by finalized calls, and synthesizes skipped results for missing calls. It returns a projection without appending history. Sessions maintains separate context and client snapshots. Model execution clones validated history before each provider attempt.

Core appends through `HistoryStore.Append`; sessions creates and validates candidate durable entries, invokes repository `Apply`, and updates active state after success. The repository encodes a mutation, opens the session JSONL file, writes the payload, checks short writes, synchronizes, and closes the file. Errors retain original causes through wrapping/joining.

Sources:
- `host/internal/usecase/agent/run/history.go`, `ProjectHistory`, lines 38-105.
- `host/internal/usecase/agent/run/service.go`, `Service.appendHistory`, lines 708-722.
- `host/internal/usecase/host/sessions/service.go`, `Service.Snapshot`, `ClientSnapshot`, `Append`, and `appendEntryLocked`, lines 254-302 and 451-486.
- `host/internal/usecase/host/modelexecution/service.go`, `ProviderRequest.clone` and `cloneHistory`, lines 297-341.
- `host/internal/infra/persistence/sessions/service.go`, `Service.Apply`, `Service.persistPayload`, and `persist`, lines 89-99 and 197-252.

### FCT-05: Finalized messages and observers

After successful streaming, Core normalizes the terminal model response, appends it to history, delivers message-end, and applies its outcome. Its provider-error path also forms and persists a terminal response before message-end. The existing message-end extension capability is an observer. The event dispatcher sends client output first, then lifecycle observers, joining failures. These operations return no replacement message.

Sources:
- `host/internal/usecase/agent/run/service.go`, `Service.runTurn`, `Service.finalizeProviderError`, and `Service.appendModel`, lines 260-289, 327-401, and 692-697.
- `host/internal/usecase/host/events/service.go`, `Dispatcher.Deliver`, lines 37-47.
- `host/internal/usecase/host/lifecycle/service.go`, `Service.observe` and `invokeObserver`, lines 233-301.
- `api/plugins/extension/v1/lifecycle.proto`, `MessageEnd`, lines 155-160.

### FCT-06: Public declarations, SDK and registration order

`HandlerKind` currently enumerates tree, lifecycle, selection, retry, and compaction capabilities. `HandleRequest.payload` and `HandleResponse.outcome` have corresponding typed variants. The complete inspected declaration, startup-acceptance and SDK dispatch sets contain no generic input, system-instruction, context, finalized-message, provider-header, serialized-request or provider-response middleware kind.

Registration returns ordered handler descriptors. Discovery sorts accepted extensions by normalized identifier, rejects duplicate-ID groups, and startup preserves each registration's descriptor order when partitioning/merging capabilities. SDK validation matches registered handler identity and kind to its typed payload before invoking extension-owned admitted work.

Sources:
- `api/plugins/extension/v1/extension.proto`, `HandlerKind` and `RegisterResponse`, lines 193-253.
- `api/plugins/extension/v1/session_tree.proto`, `HandleRequest` and `HandleResponse`, lines 15-89.
- `host/internal/usecase/host/extensionruntime/discovery.go`, `acceptDiscovery`, lines 14-68.
- `host/internal/usecase/host/startup/service.go`, `Service.Load`, `validateHandlerIdentities`, `partitionHandlers`, and `mergeHandlers`, lines 91-313.
- `sdk/plugins/extension/v1/server.go`, `completeRegistration`, `validateHandle`, `newHandlerPayloadPredicates`, and `handlerKindMatches`, lines 413-534.
- `sdk/plugins/extension/v1/service.go`, `Service`, `HandleOperation`, and `handlePrepared.Run`, lines 34-65 and 139-170.

Selection and session-tree capability owners already invoke sequential handlers with detached original/current values. They apply validated preserve/replace actions and stop on failure or a terminal rejection/cancellation. Runtime invocation checks exact instance generation and current availability. Ordinary handler errors do not themselves invalidate a runtime; runtime-unavailable errors do.

Sources:
- `host/internal/usecase/host/modelselection/handlers.go`, `Service.compose` and `applyAction`, lines 97-179 and 237-258.
- `host/internal/usecase/host/sessiontree/handlers.go`, request/result composition, lines 17-201.
- `host/internal/usecase/host/extensionruntime/service.go`, invocation and `beginOperation`, lines 301-440; completion accounting, lines 541-596.
- `host/internal/infra/plugins/extension/runtime/settlement.go`, `waitForOperation`, lines 30-79.

### FCT-07: Model execution and provider wire boundaries

Both Core and configured-model callers use `modelexecution.Service`. Its raw `ProviderAttempt.Stream` accepts provider-neutral instructions, descriptor, reasoning, history and tools and emits semantic stream events. The current raw contract has no HTTP headers, serialized request, response status or response-header callback.

Current application provider assembly constructs in-process Codex or compatible adapters according to configured provider type and rejects unsupported types. This assembly does not construct an extension-runtime provider adapter. Extension-defined providers remain owned by PHS-12 in the delivery plan.

Sources:
- `host/internal/usecase/host/modelexecution/contracts.go`, `ProviderRequest`, `StreamEvent`, `ProviderAttempt`, and `CatalogResolver`, lines 19-81 and 258-291.
- `host/internal/usecase/host/extensionmodels/interfaces.go`, `ModelRequester`, lines 24-36.
- `host/internal/app/provider_catalog.go`, `newProviderCatalog`, lines 35-137.
- [Delivery plan](../../delivery-plan.md), PHS-12.

Compatible `requestOptions` supplies its HTTP client, base URL, attribution headers, credentials and SDK retries set to zero. Chat/Responses helpers build SDK parameters and call `NewStreaming`. Codex creates an error-capturing transport and HTTP client, then calls the SDK with access token, account, beta, originator and user-agent headers. That transport acquires the serialized request and raw response. SDK serialization occurs before its transport call. Current public middleware payloads for these wire values are absent from the inspected contract sets in FCT-06.

Sources:
- `host/internal/infra/providers/openai/compatible/service.go`, `Driver.requestOptions`, lines 145-158.
- `host/internal/infra/providers/openai/compatible/chat.go`, `Driver.streamChatCompletions`, lines 79-115; `responses.go`, `Driver.streamResponses`, lines 83-135.
- `host/internal/infra/providers/openai/codex/provider.go`, `Driver.executeRequest`, lines 155-230.
- `host/internal/infra/providers/openai/codex/transport.go`, `errorCaptureTransport.RoundTrip`, lines 36-71.
- Declared dependency `github.com/openai/openai-go/v3@v3.49.0`, `internal/requestconfig/requestconfig.go`, `NewRequestConfig`, lines 188-298, and `responses/response.go`, `ResponseService.NewStreaming`, lines 79-90.

Credentials are resolved inside concrete adapters. Provider reasoning context can occur in serialized request replay. Public configured-model result mapping omits opaque provider reasoning context. The current code therefore supplies no established public wire middleware credential/replay projection behavior; that contract is a technical-design topic for PHS-08.

Sources:
- `host/internal/infra/providers/openai/codex/refresh.go`, `Driver.resolveCredentials`, lines 36-57.
- `host/internal/infra/providers/openai/compatible/service.go`, `Driver.Stream`, lines 194-233.
- `host/internal/infra/providers/openai/codex/history.go`, `codexReasoningInput` and `reasoningInput`, lines 226-274.
- `host/internal/controller/extension/configured_model.go`, `MapModelResponse` and `mapConfiguredContent`, lines 128-217.

### FCT-08: Retry and full errors

Host `execute` clones each raw attempt request, captures terminal output, coordinates ordered retry handlers, and handles overflow recovery. A retry-handler error or invalid action terminates execution as extension failure. `LogicalFailureError` and `ProviderFailureError` retain the cause and supplement it with category/classification. Concrete adapters classify provider failures and disable SDK retries.

Programmatic and UI map model-execution and extension failures to closed public categories while carrying `err.Error()` in the failure message. One-shot `execute` prints `Renderer.WriteError(runErr)` and returns its failure exit code. `WriteError` prefixes the complete error string with `[error] `; it adds no separate machine-category field.

Sources:
- `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute`, `applyRetryAction`, and `logicalFailure`, lines 33-240, 332-356, and 435-437.
- `host/internal/usecase/host/modelexecution/contracts.go`, failure types, lines 176-256.
- `host/internal/usecase/host/programmatic/prepared.go`, `failureCode`, lines 389-446.
- `host/internal/usecase/host/ui/prepared_operations.go`, `runFailureCode`, lines 340-355.
- `host/internal/controller/programmatic/delivery.go`, `streamDelivery.Terminal`, lines 79-145.
- `host/internal/infra/plugins/ui/runtime/operations.go`, `operationDelivery.Terminal`, lines 65-129.
- `host/cmd/glyph/execute.go`, `execute`, lines 22-40.
- `host/internal/infra/headless/renderer.go`, `Renderer.WriteError`, lines 271-273.

### FCT-09: Existing execution and generation infrastructure

Real application integration fixtures support Programmatic gRPC over a Unix socket, UI SDK submit/wait operations, real extension processes, controllable HTTP provider responses and persisted-session inspection. Existing observer tests assert full failure causes, skipped later observers, suppressed early provider dispatch and subsequent successful runs. External-plugin tests use a separate Go module importing only public contracts and SDK. `task generate` runs protobuf generation followed by Go generation; protobuf generation uses the opaque API.

Sources:
- `host/internal/app/programmatic_fixture_test.go`, `startProgrammaticFixtureAtPath` and operation helpers, lines 69-118 and 316-406.
- `host/internal/app/ui_protocol_fixture_test.go`, `runSemanticFixture` and `waitUIOperation`, lines 136-224.
- `host/internal/app/ordinary_observer_failure_integration_test.go`, `TestPublicOrdinaryObserverFailure`, lines 70-150.
- `host/internal/app/ordinary_observer_ui_integration_test.go`, `TestPublicUIOrdinaryObserverFailure`, lines 103-157.
- `host/internal/app/handler_composition_integration_test.go`, `TestSessionTreeComposesRealGRPCHandlers`, lines 72-170.
- `testdata/external-plugins/external_plugins_integration_test.go`, `TestExternalExtensionPlugin`, lines 1-142.
- `Taskfile.yml`, `generate`, `test`, `itest`, `build`; `easyp.yaml`, generation configuration.

## Analysis and interpretations

None. This stage collects facts only.

## Hypotheses and tests

No new runtime hypotheses tested. Existing assertions are identified above. Baseline executions are recorded separately.

## Options and trade-offs

None selected during fact collection.

## Recommendation

None selected during fact collection.

## Action plan

No implementation actions selected during fact collection.

## Assumptions

None for the stated current behavior.

## Open questions

None about the current paths described here. Follow-up inspection resolved one-shot rendering and the persistence adapter call path. Wire middleware payload shape, its placement relative to retry/compaction, and safe credential/replay projection are new contract decisions for the technical-solution stage. There is currently no such public contract in the inspected declaration sets. PHS-12 owns production extension-provider registration; PHS-08 requires independence from the implementation of the raw provider interface.

## References

- [Ticket](ticket.md): required phase behavior and acceptance.
- [Target picture](target-picture.md): confirmed boundaries and acceptance scenarios.
- [Project state](project-state.md): executed baseline checks.
