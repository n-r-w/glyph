# UNT-04 implementation evidence

## Status

UNT-04 is implemented on top of commit `cb63b80`. It was delivered in three sequential assignments. Part 1 added the public finalized-message operation, its SDK admission, startup acceptance, runtime mapping with replay references and the composition in the middleware owner. Part 2 added the Agent Core finalization port and its use for terminal model responses and tool results. Part 3 added user and configured-model finalization, the public reference extension, the author guide and the public tests. Three rounds of review corrections followed; they are recorded in [Review corrections, round 1](#review-corrections-round-1), [round 2](#review-corrections-round-2) and [round 3](#review-corrections-round-3). After the last correction, the full set of project checks passed; see [Full checks](#full-checks).

The decisions are [D8-16](decisions.md#d8-16-finalized-messages-are-one-handler-kind-with-role-preserving-entries), [D8-17](decisions.md#d8-17-agent-core-finalizes-model-responses-and-tool-results-through-its-own-port) and [D8-18](decisions.md#d8-18-user-and-configured-model-finalization-and-the-finalized-message-reference).

## Implemented path and review entry points

`api/plugins/extension/v1` adds `HANDLER_KIND_FINALIZED_MESSAGE`, the `finalized_message` variants of `HandleRequest` and `HandleResponse`, and `FinalizedMessageInvocation` and `FinalizedMessageAction` in `middleware.proto`. `newHandlerPayloadPredicates` in `sdk/plugins/extension/v1/server.go` admits the payload only for this kind. `RawHandlerKind.isMiddleware` in `host/internal/usecase/host/startup/middleware.go` and the registration bound in `host/internal/usecase/host/extensionruntime/registration.go` include it.

`Runtime.HandleFinalizedMessage` in `host/internal/infra/plugins/extension/runtime/finalized_middleware.go` projects the original and the current message through `replayProjection.projectEntry` and restores a replacement through `restoreEntry`. `extensionruntime.Service.HandleFinalizedMessage` invokes only the bound runtime generation.

`middleware.Service.finalizeMessage` in `host/internal/usecase/host/middleware/finalized.go` runs the chain through `compose` and checks each replacement in `acceptFinalizedReplacement`. Failures get their category in `categorizeChainFailure` in `failure.go`. `FinalizeModelResponse` and `FinalizeToolResult` implement `agentrun.MessageFinalizer` and return the value unchanged when the run is canceled before the first handler is admitted. `FinalizeConfiguredResponse` implements `modelexecution.ResponseFinalizer`. `ProcessInput` in `input.go` finalizes the user message before the final model check.

Agent Core calls the finalizer in `runTurn` in `host/internal/usecase/agent/run/service.go`, in `finalizeProviderError` in `provider_failure.go`, and in `skipLengthCalls` and `executeCalls` in `tool_calls.go`. `RequestConfigured` in `host/internal/usecase/host/modelexecution/service.go` returns the result of `finalizeConfiguredResponse` in `configured_finalization.go`. `bindMiddleware` in `host/internal/app/middleware.go` binds the owner for configured responses, and the three application assemblies pass the owner to Agent Core and run control.

The public reference is `testdata/external-plugins/cmd/finalized`; the author guide is [finalized-message-middleware.md](../../../../../guidelines/finalized-message-middleware.md).

## Focused RED and GREEN

### Public boundary

`host/internal/app/finalized_middleware_integration_test.go` and `finalized_clients_integration_test.go` drive the assembled Programmatic gRPC boundary, a UI plugin process through the public UI SDK and the production one-shot executable. They use real sessions, the reference extension process, its tool and an HTTP provider mock.

| Test | What it checks |
| --- | --- |
| `TestPublicFinalizedMessages` | User, model and tool-result replacements are identical in `GetMessages`, in `message_end`, `tool_result` and `turn_end` events, and in the next two provider requests; the provider requests contain no unreplaced value; the trace shows one handler pair per message and no second user finalization |
| `TestPublicFinalizedMessageFailures` | A tool-result handler error, a changed role, an unknown replay reference and an empty action end the request with `EXTENSION_FAILED` and the complete text after one provider request; no later handler runs; the user entry and, for the tool case, the model response with its executed call remain; a user replacement with an image ends with `INPUT_INVALID` before any provider request; each follow-up request completes |
| `TestPublicFinalizedProviderFailure` | A rejected provider request whose retained response also fails finalization ends with `EXTENSION_FAILED`, and the text contains the provider text and `middleware handler "finalized-mark": reference finalized handler failed`; one provider request, one finalization, no later handler, no recorded model response |
| `TestPublicFinalizedCancellation` | Canceling a blocked model finalization cancels the operation; the trace contains no later handler and no second invocation; no model response is recorded; the follow-up completes |
| `TestPublicFinalizedMessagesUI` | The UI session tree, the first `message_end` event and the next provider request contain the final user and model values; an empty action fails the first request with `EXTENSION_FAILED` |
| `TestPublicFinalizedConfiguredResult` | An extension tool that awaits a configured-model request receives `Configured response. [final model]`; the session stores no configured response |
| `TestPublicFinalizedBranchSummary` | A branch summary stores `Configured response. [final model]` |
| `TestPublicFinalizedOneShot` | Without extensions one-shot writes the streamed text `Request complete.`; with the redaction reference it writes only `[redacted]`; an empty action and a provider failure with a failed finalization exit with status 1 and `[error] EXTENSION_FAILED: run headless agent: ` followed by the complete causes |

RED, taken by making `finalizeMessage` return its input unchanged for one run and restoring it afterwards:

```text
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/app -run '^(TestPublicFinalizedMessages|TestPublicFinalizedMessageFailures|TestPublicFinalizedProviderFailure|TestPublicFinalizedCancellation|TestPublicFinalizedMessagesUI|TestPublicFinalizedConfiguredResult|TestPublicFinalizedOneShot)$' -v
        	            	expected: []string{"user: finalize me [final user]", "model: Calling tool. [final model]", "tool: echo output [final tool]", "model: Request complete. [final model]", "user: follow up [final user]", "model: Request complete. [final model]"}
        	            	actual  : []string{"user: finalize me", "model: Calling tool.", "tool: echo output", "model: Request complete.", "user: follow up", "model: Request complete."}
--- FAIL: TestPublicFinalizedMessagesUI (2.96s)
--- FAIL: TestPublicFinalizedConfiguredResult (0.85s)
--- FAIL: TestPublicFinalizedMessages (0.49s)
--- FAIL: TestPublicFinalizedMessageFailures (2.14s)
--- FAIL: TestPublicFinalizedProviderFailure (0.43s)
--- FAIL: TestPublicFinalizedCancellation (30.42s)
--- FAIL: TestPublicFinalizedOneShot (4.23s)
FAIL    github.com/n-r-w/glyph/host/internal/app 41.556s
```

RED of the configured paths, taken by returning the unfinalized configured response for one run:

```text
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/app -run '^(TestPublicFinalizedBranchSummary|TestPublicFinalizedConfiguredResult)$' -v
--- FAIL: TestPublicFinalizedConfiguredResult (0.87s)
        	            	expected: "Configured response. [final model]"
        	            	actual  : "Configured response."
--- FAIL: TestPublicFinalizedBranchSummary (0.44s)
```

GREEN after the last change:

```text
--- PASS: TestPublicFinalizedMessagesUI (2.99s)
--- PASS: TestPublicFinalizedConfiguredResult (0.92s)
--- PASS: TestPublicFinalizedBranchSummary (0.43s)
--- PASS: TestPublicFinalizedMessages (0.46s)
--- PASS: TestPublicFinalizedMessageFailures (2.16s)
--- PASS: TestPublicFinalizedProviderFailure (0.42s)
--- PASS: TestPublicFinalizedCancellation (0.43s)
--- PASS: TestPublicFinalizedOneShot (1.35s)
ok      github.com/n-r-w/glyph/host/internal/app 27.836s
```

### Part 1: SDK admission, startup and registration

RED with the tests written after the protobuf contract and the startup constant and before the bounds and the predicate changed:

```text
go test -race ./host/internal/usecase/host/extensionruntime/ -run 'TestRegistrationBindsMiddlewareKinds' -v
        	            	expected: []startup.RawHandlerKind{26, 27, 28, 29, 30, 31, 32}
        	            	actual  : []startup.RawHandlerKind{26, 27, 28, 29, 30, 31, 0}
--- FAIL: TestRegistrationBindsMiddlewareKinds (0.00s)
go test -race ./sdk/plugins/extension/v1/
--- FAIL: TestContextMiddlewareHandlerAdmission (0.00s)
        	            	actual  : [][]bool{[]bool{true, false, false, false}, []bool{false, true, false, false}, []bool{false, false, true, false}, []bool{false, false, false, false}}
go test -race ./host/internal/usecase/host/startup/
--- FAIL: TestServiceLoadPassesContextMiddlewareToRegistrar (0.00s)
```

The runtime mapping tests and the owner composition tests failed to compile before their implementation (`undefined: mapFinalizedMessageRequest`, `service.finalizeMessage undefined`). Behavior RED for the replacement rules was taken by accepting every replacement unchanged for one run: `TestFinalizeMessageKeepsHostOwnedModelFields` and the role, user-shape, model-content, call-identifier, tool-name and image cases of `TestFinalizeMessageChainFailure` failed.

GREEN: `TestRegistrationBindsMiddlewareKinds`, `TestFinalizedMessageMiddlewareChecksExactRuntimeGeneration`, `TestFinalizedMessageMapsEveryRole`, `TestFinalizedMessageReplayRoundTrip`, `TestFinalizedMessageRejectsInvalidReferences`, `TestFinalizedMessageMissingAction`, `TestContextMiddlewareHandlerAdmission`, `TestServiceLoadPassesContextMiddlewareToRegistrar` and the `TestFinalizeMessage` tests passed.

### Part 2: Agent Core and the tool-call rule

RED of the tool-call rule before `acceptFinalizedReplacement` compared tool calls:

```text
go test -race ./host/internal/usecase/host/middleware/ -run 'TestFinalizeMessage' -v
--- FAIL: TestFinalizeMessageChainFailure (0.00s)
    --- FAIL: TestFinalizeMessageChainFailure/added_tool_call (0.00s)
    --- FAIL: TestFinalizeMessageChainFailure/renamed_tool_call (0.00s)
    --- FAIL: TestFinalizeMessageChainFailure/reordered_tool_calls (0.00s)
    --- FAIL: TestFinalizeMessageChainFailure/removed_tool_call (0.00s)
```

RED of Agent Core with the port wired into the constructor but not yet called:

```text
go test -race ./host/internal/usecase/agent/run/ -run 'TestRunUsesFinalized|TestRunFinalizesLength|TestRunStopsWhen|TestProviderFailureFinalization' -v
--- FAIL: TestRunStopsWhenModelFinalizationFails (0.00s)
--- FAIL: TestRunUsesFinalizedModelAndToolValues (0.00s)
--- FAIL: TestRunStopsWhenToolResultFinalizationFails (0.00s)
--- FAIL: TestRunFinalizesLengthToolErrors (0.00s)
--- FAIL: TestProviderFailureFinalization (0.00s)
    --- PASS: TestProviderFailureFinalization/canceled_response_is_not_finalized (0.00s)
    --- FAIL: TestProviderFailureFinalization/failed_finalization_appends_nothing (0.00s)
    --- FAIL: TestProviderFailureFinalization/failed_response_is_finalized (0.00s)
```

The canceled-response case passes in RED because it asserts that no finalization happens. `TestRunCanceledDuringFinalization` and `TestFinalizeCoreValues` were written after the code; their RED was taken by removing the call in `runTurn` and by returning the unfinalized tool result for one run. All these tests pass after the change.

### Part 3: user and configured finalization

RED, taken by skipping the user chain in `ProcessInput` and by returning the unfinalized configured response for one run:

```text
go test -race ./host/internal/usecase/host/middleware/ -run 'TestProcessInputFinali' -v
--- FAIL: TestProcessInputFinalizationFailure (0.00s)
--- FAIL: TestProcessInputFinalizesUser (0.00s)
    --- FAIL: TestProcessInputFinalizesUser/text_replacement (0.00s)
    --- FAIL: TestProcessInputFinalizesUser/added_image (0.00s)
go test -race ./host/internal/usecase/host/modelexecution/ -run 'TestRequestConfigured' -v
--- FAIL: TestRequestConfiguredReturnsFinalizedResponse (0.00s)
--- FAIL: TestRequestConfiguredFinalizationFailure (0.00s)
```

GREEN: both packages pass; the `added_image` case reports `INPUT_INVALID` with `model text does not accept image input at content 1`.

## Checks

After the last change:

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0; no proposed changes |
| `task lint` | Exit 0; zero issues; ifaceguard reported no errors |
| `task test` | Exit 0 |
| `go test -race -tags=integration -p 1 -parallel 1` for `modelexecution`, `middleware` and `agent/run` | All passed |
| `go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run` with the finalized-message tests and the user-input, input, context, configured, branch-summary, consumed-child and manual-compaction tests that share their fixtures | Passed |

Parts 1 and 2 also ran the integration tests of the SDK, the extension runtime, the sessions and the Programmatic output packages and the handler-composition tests in `host/internal/app`; they passed.

## Review corrections, round 1

### C1: a failed finalization after a provider failure is `EXTENSION_FAILED`

`finalizeProviderError` in `host/internal/usecase/agent/run/provider_failure.go` returns `diagnostic.NewCurrentCause(finalizeErr, providerErr)`. `modelExecutionFailureCode` in `host/internal/usecase/host/programmatic/prepared.go` and `host/internal/usecase/host/ui/prepared_operations.go` reads model-execution categories from current causes only.

RED with the public expectation changed to `EXTENSION_FAILED` and the new UI test:

```text
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/app -run '^(TestPublicFinalizedProviderFailure|TestPublicFinalizedProviderFailureUI|TestPublicFinalizedOneShot)$' -v
        	            	expected: "EXTENSION_FAILED"
        	            	actual  : "MODEL_FAILED"
--- FAIL: TestPublicFinalizedProviderFailureUI (1.50s)
        	            	expected: "EXTENSION_FAILED"
        	            	actual  : "MODEL_FAILED"
--- FAIL: TestPublicFinalizedProviderFailure (0.44s)
```

The one-shot case already showed `EXTENSION_FAILED`, because the one-shot renderer uses `errtree.CurrentCode`. `TestRunFailureCodePrefersCurrentFinalizationFailure` in both use-case packages failed with `expected: "EXTENSION_FAILED"`, `actual  : "MODEL_FAILED"` when the mapping that ignores current causes was temporarily restored for one run; a provider failure alone keeps `MODEL_FAILED`. All these tests pass after the change.

### C2: cancellation boundary

Round 1 added `finalizeModelResponse` and `finalizeToolResult` in `host/internal/usecase/agent/run/finalization.go`, which started no handler when the run context was already canceled. Round 2 replaced them with the admission rule of the middleware owner; see C8. `TestRunCanceledBeforeFinalization`, `TestRunCanceledBeforeToolResultFinalization`, `TestRunCanceledDuringFinalization` and `TestRunCanceledAfterFinalization` check the three moments with a mock finalizer. `TestCoreFinalizationCancellationBoundary`, then in `host/internal/usecase/host/middleware/core_cancellation_test.go`, ran Agent Core with the real middleware owner and a preserve-only handler. Round 2 moved it to `core_cancellation_integration_test.go` and renamed its first case `canceled before admission`.

RED in round 1, taken by removing the Core cancellation check for one run (the file and the subtest name below are those of round 1):

```text
go test -race ./host/internal/usecase/agent/run/ -run 'TestRunCanceled' -v
--- FAIL: TestRunCanceledBeforeToolResultFinalization (0.00s)
--- FAIL: TestRunCanceledBeforeFinalization (0.00s)
go test -race ./host/internal/usecase/host/middleware/ -run TestCoreFinalizationCancellationBoundary -v
    --- FAIL: TestCoreFinalizationCancellationBoundary/canceled_before_finalization (0.00s)
```

The cases for cancellation while a handler runs and after it succeeds pass with and without the cancellation check; they guard against a change of those rules. `TestPublicFinalizedCancellation` remains the public case for cancellation while a handler runs.

### C3: one-shot writes the final model text

`Renderer.EnableFinalModelText` in `host/internal/infra/headless/renderer.go` (named `RenderFinalModelText` in round 1) switches the renderer to final-text output, and `runHeadlessWithPaths` in `host/internal/app/headless.go` enables it when `middleware.Service.HasFinalizedMessageHandlers` reports a handler. `TestRendererWritesFinalModelText` failed to compile before the method existed. The public RED was:

```text
--- FAIL: TestPublicFinalizedOneShot (0.92s)
    --- PASS: TestPublicFinalizedOneShot/no_handlers (0.03s)
    --- FAIL: TestPublicFinalizedOneShot/redacted_text (0.42s)
        	            	expected: "[redacted]\n"
        	            	actual  : "Request complete.\n"
    --- FAIL: TestPublicFinalizedOneShot/invalid_action (0.41s)
        	Error:      	"Request complete." should not contain "Request complete."
```

### C4: no unbound configured finalizer

`RequestConfigured` rejects a request before binding resolution when no response finalizer is bound. `TestRequestConfiguredRequiresFinalizer` failed before the change with an unexpected `ResolveConfiguredBinding` call. Tests that call `RequestConfigured` bind a pass-through finalizer, and the application fixtures that build model execution directly bind the real owner or a pass-through finalizer.

### C5: public coverage

`TestPublicFinalizedToolArguments` checks that the executed tool receives `{"value":"final"}`, that `message_end`, `GetMessages` and the next provider request contain the replaced arguments, and that `tool_execution_end` and `tool_result` carry the final result. `TestPublicFinalizedReplay` checks that no extension payload contains the private replay, that a replay reference reached the extension, and that the next provider request contains the restored replay. `TestPublicFinalizedConfiguredFailure` checks `EXTENSION_FAILED` and the complete text `finalize configured model response: … middleware handler "finalized-mark": reference finalized handler failed` for an extension tool that awaits a configured request. RED, taken by making `finalizeMessage` return its input unchanged for one run:

```text
--- FAIL: TestPublicFinalizedToolArguments (0.45s)
--- FAIL: TestPublicFinalizedReplay (0.41s)
--- FAIL: TestPublicFinalizedConfiguredFailure (0.89s)
```

### Evidence limits

The Programmatic and UI `agent_end` payloads carry no added history, so `AddedHistory` is checked only in `TestRunUsesFinalizedModelAndToolValues`. Finalized length-limit results, kept accounting and provider identity, and cancellation without finalized-message handlers are checked at unit level only.

### Checks after the corrections

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0; no proposed changes |
| `task lint` | Exit 0; zero issues |
| `task test` | Exit 0 |
| `go test -race -tags=integration -p 1 -parallel 1` for `agent/run`, `modelexecution`, `middleware`, `extensioncontext`, `sessiontree`, `sessions`, Programmatic and UI use cases, `infra/headless`, Programmatic output and the OpenAI provider packages | All passed |
| `go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run` with all finalized-message tests, the input, user-input, context, configured, branch-summary, manual-compaction, consumed-child, headless and handler-composition tests | Passed; `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` skips here because it requires Darwin |

## Review corrections, round 2

### C7: Programmatic compaction category from current causes only

`directCompactionFailureCode` in `host/internal/usecase/host/programmatic/prepared.go` reads compaction categories from current causes only, as `modelExecutionFailureCode` does since C1.

`TestRunFailureCodeIgnoresDiagnosticCompactionFailure` in `host/internal/usecase/host/programmatic/current_failure_code_test.go` was written before the change:

```text
go test -race ./host/internal/usecase/host/programmatic/ -run TestRunFailureCodeIgnoresDiagnosticCompactionFailure -v
        	            	expected: "EXTENSION_FAILED"
        	            	actual  : "COMPACTION_FAILED"
--- FAIL: TestRunFailureCodeIgnoresDiagnosticCompactionFailure (0.00s)
```

`TestPublicFinalizedOverflowFailure` in `host/internal/app/finalized_preservation_integration_test.go` reproduces the public case with the compaction-policy reference: provider overflow, a committed replacement whose final outgoing input the policy rejects with `COMPACTION_FAILED`, and a failed finalization of the retained response. The reference extension runs in mode `finalized-preserve` with `GLYPH_FINALIZED_FAILING_MODEL=3`. RED with the earlier compaction lookup restored for one run:

```text
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/app -run '^TestPublicFinalizedOverflowFailure$' -v
        	            	expected: "EXTENSION_FAILED"
        	            	actual  : "COMPACTION_FAILED"
--- FAIL: TestPublicFinalizedOverflowFailure (0.92s)
```

GREEN: the test passes with `EXTENSION_FAILED`, the finalizer text, the provider text `provider context limit source` and the policy rejection text, and three provider requests. A compaction failure without a finalization failure keeps `COMPACTION_FAILED` in the unit test and in `TestRecoveryRetainsCommitAndRejectsRealObserverContextGrowth`.

### C8: finalization starts at first-handler admission

`compose` in `host/internal/usecase/host/middleware/context.go` returns the unchanged value when the owning context is canceled before the first handler is admitted and the chain permits it. `FinalizeModelResponse` and `FinalizeToolResult` permit it; `ProcessInput` and the new `FinalizeConfiguredResponse`, which model execution now consumes through `ResponseFinalizer`, do not. Agent Core no longer checks cancellation before it calls the finalizer; `finalization.go` was removed.

RED of `TestFinalizeCoreValuesBeforeAdmission` in `host/internal/usecase/host/middleware/finalized_test.go`, after `FinalizeConfiguredResponse` existed and before the admission rule:

```text
go test -race ./host/internal/usecase/host/middleware/ -run TestFinalizeCoreValuesBeforeAdmission -v
        	Error:      	Received unexpected error:
--- FAIL: TestFinalizeCoreValuesBeforeAdmission (0.00s)
```

`TestCoreFinalizationCancellationBoundary` moved to `core_cancellation_integration_test.go` with the `integration` tag. Its provider cancels the run after the successful stream, so cancellation is present when Core calls the owner and before the owner admits the preserve-only handler. RED with the admission rule disabled for one run:

```text
go test -race -tags=integration ./host/internal/usecase/host/middleware/ -run TestCoreFinalizationCancellationBoundary -v
    --- FAIL: TestCoreFinalizationCancellationBoundary/canceled_before_admission (0.00s)
```

The cases for cancellation while the handler runs and after it succeeds pass with and without the rule. All these tests pass after the change. The Core unit tests `TestRunCanceledBeforeFinalization` and `TestRunCanceledBeforeToolResultFinalization` now check that Core records the unchanged value that the finalizer returns after cancellation.

### C9: cancellation while a retained failed response is finalized

`TestProviderFailureCanceledDuringFinalization` in `host/internal/usecase/agent/run/finalization_test.go` lets the provider fail after partial text and the finalizer observe cancellation while it runs. The run ends `RunOutcomeAborted`, the error contains the cancellation and `provider unavailable`, and history contains only the user entry. The D8-17 statement matched the code; the test passed on its first run, and D8-17 now names the reason: the current cause of the run error is only cancellation.

### Checks after round 2

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0; no proposed changes |
| `task lint` | Exit 0; zero issues |
| `task test` | Exit 0 |
| `go test -race -tags=integration -p 1 -parallel 1` for the changed packages outside `host/internal/app` | All passed |
| `go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run` with the finalized-message, input, user-input, context, configured, branch-summary, compaction, headless and handler-composition tests | Passed |

## Review corrections, round 3

### C12: every category lookup of the accepted-run classifiers reads current causes

`failureCode` in `host/internal/usecase/host/programmatic/prepared.go` and `runFailureCode` in `host/internal/usecase/host/ui/prepared_operations.go` check the persistence sentinels with `errtree.HasCurrent`, which compares each current node itself. The compaction and model-execution lookups use `errtree.FirstCurrent`. Both helpers are new in `internal/errtree/current_errors.go` and skip diagnostic history. The precedence among current categories is unchanged: persistence, then compaction (Programmatic only), then model execution, then the extension and input categories.

`TestRunFailureCodeReadsCurrentPersistenceFailure` in both `current_failure_code_test.go` files was written before the change:

```text
go test -race ./host/internal/usecase/host/programmatic/ ./host/internal/usecase/host/ui/ -run TestRunFailureCodeReadsCurrentPersistenceFailure -v
        	            	expected: "EXTENSION_FAILED"
        	            	actual  : "PERSISTENCE_UNAVAILABLE"
    --- FAIL: TestRunFailureCodeReadsCurrentPersistenceFailure/diagnostic_persistence_failure (0.00s)
FAIL    github.com/n-r-w/glyph/host/internal/usecase/host/programmatic
        	            	expected: "EXTENSION_FAILED"
        	            	actual  : "PERSISTENCE_UNAVAILABLE"
    --- FAIL: TestRunFailureCodeReadsCurrentPersistenceFailure/diagnostic_persistence_failure (0.00s)
FAIL    github.com/n-r-w/glyph/host/internal/usecase/host/ui
```

The cases for a persistence failure under wrappers and joins, and for a persistence failure together with a current extension failure, passed before and after the change; they keep `PERSISTENCE_UNAVAILABLE`. All cases pass after the change. `TestFirstCurrent` and `TestHasCurrent` in `internal/errtree/current_errors_test.go` check the helpers.

No public fixture injects a persistence failure into an overflow-recovery commit, so the combined persistence case is checked at unit level only.

### C13 and C14

`transformer.cancellation` in `host/internal/usecase/host/middleware/context.go` holds the `cancellationBoundary` value directly. The evidence above names the current files and functions; earlier RED logs keep the names of their round.

### Checks after round 3

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0; no proposed changes |
| `task lint` | Exit 0; zero issues |
| `task test` | Exit 0 |
| `go test -race -tags=integration -p 1 -parallel 1` for `middleware` and the Programmatic and UI use cases | All passed |
| `go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run` with the finalized-message, compaction, input, user-input, context and configured tests | Passed |
| `go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestClientAppSuites$/^(Programmatic\|UIRunFailure)$/Persistence'` | Passed; `TestResumeRecoveryPersistenceFailureProcessPath` skips here because it requires Darwin |

## Full checks

A separate agent ran the full set of project checks on the working copy after the round 3 corrections. Every command exited 0, and neither `task fmt` nor `task fix_dry_run` changed a file.

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0; no changed files |
| `task fix_dry_run` | Exit 0; no proposed changes |
| `task lint` | Exit 0; zero issues; ifaceguard reported no errors; no vulnerabilities |
| `task test` | Exit 0; 60 packages passed |
| `task itest` | Exit 0; no failed tests; `host/internal/app` passed in 443 s; the whole run took about 10 minutes |
| `task test-coverage` | Exit 0; coverage 85.2%, required 80.0% |
| `task build` | Exit 0 |
