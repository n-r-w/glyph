# UNT-02 implementation evidence

## Status

UNT-02 is implemented in the working copy and is not committed. It was delivered in two sequential assignments, called part 1 and part 2 below, followed by one round of review corrections. Part 1 added the public contract, SDK admission, startup registration, the middleware owner and runtime mapping. Part 2 connected them to Agent Core, model execution, configured-model callers, the three application assemblies, the public reference extension and the author documentation. The review corrections are recorded in [Review corrections, round 1](#review-corrections-round-1). `task itest`, `task test-coverage` and the whole `host/internal/app` integration package have not run yet.

The decisions are [D8-08 through D8-12](decisions.md).

## Implemented path and review entry points

`api/plugins/extension/v1` adds the `HANDLER_KIND_SYSTEM_INSTRUCTIONS` and `HANDLER_KIND_MODEL_CONTEXT` kinds, their `HandleRequest` and `HandleResponse` variants and the typed history messages. `go tool easyp generate` regenerated `pkg/plugins/extension/v1`. `newHandlerPayloadPredicates` in `sdk/plugins/extension/v1/server.go` admits each payload only for its registered kind.

`RawHandlerKind.isMiddleware` in `host/internal/usecase/host/startup/middleware.go` passes both kinds to the middleware registrar. `middleware.Service.ValidateMiddlewareHandlers` and `CommitMiddlewareHandlers` accept them in registration and descriptor order.

`middleware.Service.NewContextComposer` captures the registered handlers and returns the `modelexecution.ContextComposer` declared by model execution. The composer applies the instruction chain and then the history chain. For every handler, `compose` in `host/internal/usecase/host/middleware/context.go` issues an extension context and passes detached copies of the original and current values. It keeps the current value on preserve, validates the structure of each replacement with `agent.ValidatedCloneHistory` and reports whether a history handler replaced the history. On the first handler error or invalid action, it returns an error with the handler ID and the complete cause and invokes no later handler.

`extensionruntime.Service.HandleSystemInstructions` and `HandleModelContext` invoke only the runtime generation named by the issued context. `extensionruntime.Service` maps registered kinds up to `RawHandlerKindModelContext`; with the earlier bound, startup rejected both new kinds as unknown. `runtime.Runtime.HandleSystemInstructions` and `HandleModelContext` send the typed payload over the existing operation stream through `Runtime.handleMiddleware`, which `HandleWire` also uses now. `replayProjection` in `host/internal/infra/plugins/extension/runtime/replay_projection.go` replaces private reasoning replay with invocation-local references and restores retained references when it decodes a replacement.

Agent Core requests `EffectiveContextPort.PrepareContext` in `host/internal/usecase/agent/run/service.go` and streams the returned `EffectiveContext.Request` through `EffectiveContext.Provider`. `modelexecution.Service.PrepareContext` captures the conversation owner and the active binding, runs `prepareProviderContext`, and then calls `composeRequest` with the composer from `ContextMiddleware`. `composeRequest` maps a failed chain to `EXTENSION_FAILED` and validates the final history with `validateInputModalities` in `input_modalities.go`.

`preparedProviderBinding.Stream` in `host/internal/usecase/host/modelexecution/prepared_provider_binding.go` executes with the retained provider, captured conversation owner and composer. `execute` sends `preparedRequest.effective` to every raw attempt and reports `preparedRequest.base` to completed-attempt observation. After a committed overflow recovery, `selectCapturedOverflowRecovery` composes the recovered prepared request with the same composer. `RequestConfigured` composes the caller's instructions and history without capturing an owner. `bindMiddleware` in `host/internal/app/middleware.go` binds the owner for registration, wire callbacks and context composition in all three assemblies.

The public reference is `testdata/external-plugins/cmd/context`; the author guide is [context-middleware.md](../../../../../guidelines/context-middleware.md).

## Focused RED and GREEN

### SDK admission

RED after schema generation, before the SDK predicates existed:

```text
go test ./sdk/plugins/extension/v1 -run '^TestContextMiddlewareHandlerAdmission$' -count=1
--- FAIL: TestContextMiddlewareHandlerAdmission (0.00s)
    context_middleware_test.go:31:
        Error:      Not equal:
                    expected: true
                    actual  : false
        Messages:   kind HANDLER_KIND_SYSTEM_INSTRUCTIONS payload 0
FAIL    github.com/n-r-w/glyph/sdk/plugins/extension/v1 0.003s
```

GREEN after adding the predicates:

```text
go test ./sdk/plugins/extension/v1 -run '^TestContextMiddlewareHandlerAdmission$' -count=1
ok      github.com/n-r-w/glyph/sdk/plugins/extension/v1 0.003s
```

### Startup registration

RED with the new raw kind constants and the old middleware kind range:

```text
go test ./host/internal/usecase/host/startup -run '^TestServiceLoadPassesContextMiddlewareToRegistrar$' -count=1
--- FAIL: TestServiceLoadPassesContextMiddlewareToRegistrar (0.00s)
    service.go:154: Unexpected call to *startup.MockToolRegistrar.Conflicts([[]]) ...
        Got: [] ([]startup.AcceptedRegistration)
        Want: is equal to [{extension /extension [] [{context 30} {agent 4} {instructions 29} {headers 26}]}]
    controller.go:97: missing call(s) to *startup.MockMiddlewareRegistrar.ValidateMiddlewareHandlers(...)
FAIL    github.com/n-r-w/glyph/host/internal/usecase/host/startup 0.002s
```

Startup rejected the registration as an unknown kind, so the middleware registrar received nothing.

GREEN after extending the middleware kind range:

```text
go test ./host/internal/usecase/host/startup -run '^TestServiceLoadPassesContextMiddlewareToRegistrar$' -count=1
ok      github.com/n-r-w/glyph/host/internal/usecase/host/startup 0.002s
```

### Middleware owner

RED with the declared contracts and generated mocks, before the composer existed:

```text
go test ./host/internal/usecase/host/middleware -run '^TestContextComposition' -count=1
host/internal/usecase/host/middleware/context_test.go:47:22: service.NewContextComposer undefined (type *Service has no field or method NewContextComposer)
FAIL    github.com/n-r-w/glyph/host/internal/usecase/host/middleware [build failed]
```

GREEN after implementation:

```text
go test -race ./host/internal/usecase/host/middleware -run '^TestContextComposition' -count=1 -v
--- PASS: TestContextCompositionRejectsInvalidHistory (0.00s)
--- PASS: TestContextComposition (0.00s)
--- PASS: TestContextCompositionStopsChain (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/middleware 1.012s
```

The output above predates `TestContextCompositionAcceptsIntermediateImages` and `TestContextCompositionReportsPreservedHistory`, which correction C1 added; their run is listed under the corrections.

`TestContextComposition` registers handlers of two extensions with interleaved kinds. It checks registration order, original/current values, detached copies, preserve, replacement from the original, the unchanged caller request and that a registration committed after capture is not invoked. `TestContextCompositionStopsChain` covers a handler error and an invalid action. `TestContextCompositionRejectsInvalidHistory` covers a replacement entry without its payload.

### Runtime mapping and replay references

RED with the runtime-management interface extended, before the infrastructure mapping existed:

```text
go test ./host/internal/infra/plugins/extension/runtime -run '^(TestSystemInstructionsMapping|TestModelContextReplay)' -count=1
host/internal/infra/plugins/extension/runtime/runtime.go:45:43: cannot use (*Runtime)(nil) ... (missing method HandleModelContext)
host/internal/infra/plugins/extension/runtime/context_middleware_test.go:30:13: undefined: mapSystemInstructionsRequest
host/internal/infra/plugins/extension/runtime/context_middleware_test.go:62:16: undefined: newReplayProjection
FAIL    github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime [build failed]
```

GREEN after implementation:

```text
go test -race ./host/internal/infra/plugins/extension/runtime -run '^(TestSystemInstructionsMapping|TestModelContextReplay)' -count=1 -v
--- PASS: TestSystemInstructionsMapping (0.00s)
--- PASS: TestModelContextReplayRejectsInvalidReferences (0.00s)
    --- PASS: TestModelContextReplayRejectsInvalidReferences/altered (0.00s)
    --- PASS: TestModelContextReplayRejectsInvalidReferences/unknown (0.00s)
    --- PASS: TestModelContextReplayRejectsInvalidReferences/duplicated (0.00s)
    --- PASS: TestModelContextReplayRejectsInvalidReferences/misplaced (0.00s)
--- PASS: TestModelContextReplayRoundTrip (0.00s)
ok      github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime 1.029s
```

`TestModelContextReplayRoundTrip` checks that the encoded request contains no private replay bytes. It also checks that a replacement copied from the original restores replay that the current value no longer has, that a replacement without a reference discards replay, and that preserve returns no replacement.

### Exact runtime generation

`TestContextMiddlewareChecksExactRuntimeGeneration` in `host/internal/usecase/host/extensionruntime` was written after `HandleSystemInstructions` and `HandleModelContext` existed, so it has no recorded RED. It checks that the current generation receives both operations and a stale generation gets `ErrExtensionUnavailable` without an invocation.

```text
go test -race ./host/internal/usecase/host/extensionruntime -run '^TestContextMiddlewareChecksExactRuntimeGeneration$' -count=1
ok      github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime 1.024s
```

### Replacement validation in part 2

Part 2 added `TestContextCompositionRejectsUnsupportedReplacement` together with a per-replacement validation function of the composer, without a recorded RED. Correction C1 removed both, because the final history is now validated after composition.

### Agent Core effective-context port

RED with the port declared and Core still using the former model snapshot:

```text
go test ./host/internal/usecase/agent/run -run '^(TestRunStreamsEffectiveContext|TestRunFailsWhenEffectiveContextFails)$' -count=1
host/internal/usecase/agent/run/service.go:41:10: undefined: ModelRuntime
host/internal/usecase/agent/run/accounting_test.go:33:15: undefined: NewMockModelRuntime
FAIL
```

GREEN after Core used the port:

```text
go test -race ./host/internal/usecase/agent/run -run '^(TestRunStreamsEffectiveContext|TestRunFailsWhenEffectiveContextFails)$' -count=1 -v
--- PASS: TestRunFailsWhenEffectiveContextFails (0.00s)
--- PASS: TestRunStreamsEffectiveContext (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/agent/run 1.011s
```

### Model-execution composition

RED before `PrepareContext` and `BindContextMiddleware` existed:

```text
go test ./host/internal/usecase/host/modelexecution -run '^(TestPrepareContextComposesAfterPreparation|TestPreparedRetryReusesEffectiveContext|TestOverflowRecoveryComposesRecoveredContext|TestContextCompositionFailureStopsBeforeDispatch|TestConfiguredRequestComposesContext)$' -count=1
host/internal/usecase/host/modelexecution/effective_context_test.go:84:29: service.BindContextMiddleware undefined (type *Service has no field or method BindContextMiddleware)
host/internal/usecase/host/modelexecution/effective_context_test.go:90:28: service.PrepareContext undefined (type *Service has no field or method PrepareContext)
FAIL
```

GREEN after implementation:

```text
--- PASS: TestPrepareContextComposesAfterPreparation (0.00s)
--- PASS: TestContextCompositionFailureStopsBeforeDispatch (0.00s)
--- PASS: TestConfiguredRequestComposesContext (0.00s)
--- PASS: TestOverflowRecoveryComposesRecoveredContext (0.00s)
--- PASS: TestPreparedRetryReusesEffectiveContext (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution 1.014s
```

The tests check that composition receives the prepared history, that observation and overflow recovery receive the untransformed request, that a transient retry reuses one composition, that overflow recovery composes again with the same composer, that a rejected replacement reports `EXTENSION_FAILED` without a provider attempt, and that configured requests compose their explicit input. Correction C1 replaced `TestContextCompositionFailureStopsBeforeDispatch` with the final-context tests listed under the corrections.

### Public behavior

The application tests in `host/internal/app/context_middleware_integration_test.go` were written after the production code. RED was taken by removing the `BindContextMiddleware` call from `bindMiddleware` for one run and restoring it afterwards:

```text
go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^(TestPublicContextMiddleware|TestPublicContextMiddlewareAcrossModes|TestPublicContextMiddlewareFailure|TestPublicContextMiddlewareRetry|TestPublicContextMiddlewareAutomaticCompaction|TestPublicContextMiddlewareOverflow|TestPublicContextConfiguredAcrossModes)$' -count=1
--- FAIL: TestPublicContextMiddleware (0.47s)
--- FAIL: TestPublicContextMiddlewareAcrossModes (1.47s)
--- FAIL: TestPublicContextMiddlewareFailure (0.85s)
--- FAIL: TestPublicContextMiddlewareRetry (0.42s)
--- FAIL: TestPublicContextMiddlewareAutomaticCompaction (1.83s)
--- FAIL: TestPublicContextMiddlewareOverflow (1.46s)
--- FAIL: TestPublicContextConfiguredAcrossModes (1.98s)
FAIL
```

The provider received the base instructions without the project instructions and history without `glyph-context-B`; the failure scenarios completed instead of failing.

The first GREEN attempt failed too: startup rejected the reference extension because `extensionruntime` mapped kinds 29 and 30 to unspecified. After the registration bound changed, the same command passed:

```text
--- PASS: TestPublicContextMiddleware (0.45s)
--- PASS: TestPublicContextMiddlewareAcrossModes (1.47s)
--- PASS: TestPublicContextMiddlewareFailure (0.86s)
--- PASS: TestPublicContextMiddlewareRetry (0.44s)
--- PASS: TestPublicContextMiddlewareAutomaticCompaction (1.87s)
--- PASS: TestPublicContextMiddlewareOverflow (1.34s)
--- PASS: TestPublicContextConfiguredAcrossModes (2.02s)
```

These tests cover:

| Test | Observable result |
| --- | --- |
| `TestPublicContextMiddleware` | Programmatic: two requests carry the original instructions plus the project instructions and exactly one `glyph-context-B`, never an `A` marker; session entries contain no marker |
| `TestPublicContextMiddlewareAcrossModes` | One-shot and UI requests carry the same effective values |
| `TestPublicContextMiddlewareFailure` | Handler error and invalid action: `EXTENSION_FAILED`, complete cause, zero provider requests; the next request succeeds with one provider request |
| `TestPublicContextMiddlewareRetry` | Both attempts after HTTP 429 carry `glyph-context-B-1` |
| `TestPublicContextMiddlewareAutomaticCompaction` | After initial compaction the request carries the summary history and the marker; with low reported usage the policy keeps the history |
| `TestPublicContextMiddlewareOverflow` | The retried request after committed recovery carries the summary and a new marker instead of `glyph-context-B-2` |
| `TestPublicContextConfiguredAcrossModes` | Outer and configured requests in all three modes carry the composed values; the configured request keeps its own base instructions |

### Observation of the prepared request

With completed-attempt observation temporarily switched to the effective request, `TestPublicContextMiddlewareAutomaticCompaction/reported-usage-preserves` failed at its history-length assertion: the policy compacted although the provider reported low usage. With observation of the prepared request it passes. This supports D8-11.

## Review corrections, round 1

### C1: final-context modality validation

Model execution now validates the final history after each successful composition. The composer reports whether a history handler replaced the history; the category follows D8-12. As a consequence, a conversation whose history holds user images fails before dispatch, with the plain validation error, when the selected model declares no image input. `TestClientAppSuites/Programmatic/TestSessionLifecycleRoundTrip` restores such history and expects the image to reach the provider, so its fixture model now declares `input: [text, image]` through `imageRestartSelectionSettings`.

Behavioral RED before the change, with no context middleware bound:

```text
go test ./host/internal/usecase/host/modelexecution -run '^TestFinalContextRejectsImagesWithoutHandlers$' -count=1
--- FAIL: TestFinalContextRejectsImagesWithoutHandlers (0.00s)
    retry.go:73: Unexpected call to *modelexecution.MockProviderAttempt.Stream(...) because: there are no expected calls of the method "Stream" for that receiver
FAIL
```

The other C1 tests use the changed composer contract, so they were written together with it and have no separate RED. GREEN:

```text
go test -race ./host/internal/usecase/host/modelexecution -run '^(TestFinalContext|TestConfiguredFinalContextValidation|TestOverflowRevisionValidatesFinalContext)' -count=1 -v
--- PASS: TestFinalContextRejectsImagesWithoutHandlers (0.00s)
--- PASS: TestOverflowRevisionValidatesFinalContext (0.00s)
--- PASS: TestFinalContextValidationCategory (0.00s)
    --- PASS: TestFinalContextValidationCategory/preserved (0.00s)
    --- PASS: TestFinalContextValidationCategory/replaced (0.00s)
--- PASS: TestConfiguredFinalContextValidation (0.00s)
    --- PASS: TestConfiguredFinalContextValidation/handler_adds_image (0.00s)
    --- PASS: TestConfiguredFinalContextValidation/preserved_image (0.00s)
    --- PASS: TestConfiguredFinalContextValidation/handler_removes_image (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution 1.015s
```

| Test | Checked behavior |
| --- | --- |
| `TestFinalContextRejectsImagesWithoutHandlers` | Without context middleware, image history for a text-only model fails with the plain validation error and no provider attempt |
| `TestFinalContextValidationCategory` | Preserved image history fails with the plain error; replaced image history fails as `EXTENSION_FAILED`; neither is dispatched |
| `TestConfiguredFinalContextValidation` | Configured input with an image succeeds when a handler removes it; a preserved image fails with the plain error; an added image fails as `EXTENSION_FAILED` |
| `TestOverflowRevisionValidatesFinalContext` | A revision composed after committed overflow recovery is validated and fails before a second attempt |
| `TestContextCompositionAcceptsIntermediateImages` (`middleware`) | Handler A adds an image, handler B removes it, and the composition succeeds |
| `TestContextCompositionReportsPreservedHistory` (`middleware`) | Preserving handlers report no history replacement |

Existing `TestContextCompositionStopsChain` and `TestContextCompositionRejectsInvalidHistory` still show that a handler error, an invalid action and a structurally invalid replacement stop the chain immediately.

### C2 and C3: public caller, replay and cancellation tests

These tests cover behavior that already existed, so they passed on their first run and have no RED. `TestPublicContextBranchSummaryFailureAcrossClients/ui` first failed because the UI fixture did not yet dispatch its new behavior name; the fixture now does.

| Test | Observable result |
| --- | --- |
| `TestPublicContextConfiguredAcrossModes` | In each mode, one configured request (model `gpt-test`) carries `extension instructions` plus the project instructions and marker B; two outer requests carry the coding instructions plus the project instructions and marker B |
| `TestPublicContextConfiguredFailure` | Programmatic: the second `instructions-a` invocation, inside the configured request, fails; the run reports `EXTENSION_FAILED` with the complete handler text; no configured provider request is sent; the trace shows no later handler of that chain; the next request succeeds |
| `TestPublicContextBranchSummaryAcrossClients` | UI and Programmatic: both summary attempts carry the composed instructions and marker B |
| `TestPublicContextBranchSummaryFailureAcrossClients` | UI and Programmatic: the first summary fails with `EXTENSION_FAILED` and the complete text before any provider request; only `instructions-a` ran for it; the next navigation commits |
| `TestPublicContextReplayReferences` | The extension trace contains replay references and no private replay; the next provider request contains the private replay with `context` and omits it with `context-drop-replay` |
| `TestPublicContextInvalidReplayReference` | An unissued reference fails as `EXTENSION_FAILED` with `unknown private replay reference "unknown"`, no provider request and no `context-b` invocation; the next request succeeds |
| `TestCompositionCancellationKeepsAcquiredFailure` (`modelexecution`) | Pure owning cancellation stays cancellation; a handler failure acquired under cancellation is `EXTENSION_FAILED` with cancellation as a contributing cause |

Runtime loss and cancellation have no public test, as the correction list allows.

## Checks after the last code edit

| Command | Result |
| --- | --- |
| `task fmt` | exit 0 |
| `task fix_dry_run` | exit 0, no proposed changes |
| `task lint` | exit 0, 0 issues |
| `task test` | exit 0 |
| `task build` | exit 0 |
| `go test -race -tags=integration -p 1 -parallel 1` on `agent/run`, `modelexecution`, `extensioncontext`, the extension runtime package, `infra/programmatic/output` and `sessions` | all ok |
| `host/internal/app` with `-run` for all `TestPublicContext*` tests, `TestPublicConfiguredRequestAcrossApplicationModes`, `TestConsumedNestedCompactionFailureStopsCore`, `TestStandardOverflowRecovery`, `TestStandardOverflowSuppliedResult`, `TestStandardAutomaticCompaction`, `TestNavigationSummaryRetryProgressAcrossClients`, `TestNavigationSummaryTerminalFailureAcrossClients`, `TestPublicWire*` and `TestSessionTreeComposesRealGRPCHandlers` | all passed |

## Verification limits

`task itest`, `task test-coverage` and the whole `host/internal/app` integration package have not run.
