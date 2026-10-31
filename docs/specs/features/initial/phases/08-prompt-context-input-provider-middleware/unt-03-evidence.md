# UNT-03 implementation evidence

## Status

UNT-03 is implemented in the working copy on top of commit `ffb3c1c`; it is not committed. It was delivered in two sequential assignments and one round of review corrections. Part 1 replaced the text-only submit payloads with ordered text and image blocks, added the run-control input-processing port with final validation against the selected model, and added the `INPUT_INVALID` category. Part 2 added extension user-input handlers to that port, completion of fully handled input, the public reference extension and the author guide. The corrections are recorded in [Review corrections, round 1](#review-corrections-round-1). `task itest`, `task test-coverage` and the whole `host/internal/app` integration package have not run.

The decisions are [D8-13](decisions.md#d8-13-submit-payloads-and-final-input-validation), [D8-14](decisions.md#d8-14-user-input-handlers-handled-completion-and-the-input-reference) and [D8-15](decisions.md#d8-15-one-shot-errors-show-the-current-category).

## Implemented path and review entry points

`UserRequest` in `api/programmatic/v1/programmatic.proto` and `SubmitCommand` in `api/plugins/ui/v1/ui.proto` contain `repeated UserContent content`. `go tool easyp generate` regenerated `pkg/programmatic/v1` and `pkg/plugins/ui/v1`.

`mapUserRequest` in `host/internal/controller/programmatic/request.go` and `mapSubmitMessage` in `host/internal/controller/ui/command_mapping.go` map the blocks in order to `model.Message` and check them with `Message.ValidateShape` in `host/internal/domain/model/behavior.go`. A shape error is an `INVALID_ARGUMENT` rejection. `Command.invalidUserRequest` in `host/internal/controller/programmatic/command_validation.go` rejects a text block that contains only whitespace. The standard TUI mapping in `plugins/ui/tui/internal/infra/host/command_mapping.go`, the external UI fixture command `testdata/external-plugins/cmd/ui` and `headless.Service.Execute` construct one text block.

`Coordinator.RunPrepared` in `host/internal/usecase/host/runcontrol/coordinator.go` takes the prepared reservation, calls `InputProcessor.ProcessInput` and passes the returned message to Core in `Request.Message`. On an input error it returns the failed outcome with the prefix `process user input:` and does not call Core. Agent Core appends `Request.Message` in `host/internal/usecase/agent/run/service.go`.

`middleware.Service.ProcessInput` in `host/internal/usecase/host/middleware/input.go` reads the descriptor from `SelectedModel.ActiveBinding`, which `providers.Catalog` implements. `Descriptor.FindUnsupportedImage` finds an image for a model without image input; the error then has the code `INPUT_INVALID`. Model execution uses the same method in `validateInputModalities`. `bindMiddleware` in `host/internal/app/middleware.go` returns the owner, and the three application assemblies pass it to `runcontrol.NewCoordinator`.

`failureCode` in `host/internal/usecase/host/programmatic/prepared.go` and `runFailureCode` in `host/internal/usecase/host/ui/prepared_operations.go` keep the current `INPUT_INVALID` category, and `failureCodeForCommand` admits it for user requests. `runPrepared.Release` clears the Programmatic output of its run, because a run that fails before Core has no settlement.

### User-input handlers (part 2)

`api/plugins/extension/v1` adds `HANDLER_KIND_USER_INPUT`, the `user_input` variants of `HandleRequest` and `HandleResponse`, and `UserInputInvocation` and `UserInputAction` in `middleware.proto`. `newHandlerPayloadPredicates` in `sdk/plugins/extension/v1/server.go` admits the payload only for this kind. `RawHandlerKind.isMiddleware` in `host/internal/usecase/host/startup/middleware.go` and the registration bound in `host/internal/usecase/host/extensionruntime/registration.go` include it.

`middleware.Service.ProcessInput` in `host/internal/usecase/host/middleware/input.go` runs the user-input handlers through `compose` in `context.go`, which now accepts the `Handle` action for chains that permit it. Each handler receives detached copies of the original and the current input; each replacement must pass `Message.ValidateShape`. A chain failure is `EXTENSION_FAILED` unless it is only owner cancellation. A handled input returns no message; otherwise the final input is validated as in part 1. `extensionruntime.Service.HandleUserInput` invokes only the bound runtime generation, and `Runtime.HandleUserInput` in `host/internal/infra/plugins/extension/runtime/input_middleware.go` maps the payload and the action.

`Coordinator.RunPrepared` reports a completed outcome without Core for handled input, and the aborted outcome when the owner context is canceled during input processing. The public reference is `testdata/external-plugins/cmd/input`; the author guide is [input-middleware.md](../../../../../guidelines/input-middleware.md).

## Focused RED and GREEN

### Public boundary

RED with the test written before the contract changed:

```text
go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicUserInputAcrossClients$' -count=1
host/internal/app/user_input_integration_test.go:144:11: payload.SetContent undefined (type *programmaticv1.UserRequest has no field or method SetContent)
host/internal/app/user_input_integration_test.go:197:47: unknown field Content in struct literal of type "github.com/n-r-w/glyph/pkg/plugins/ui/v1".SubmitCommand_builder
host/internal/app/user_input_integration_test.go:228:48: unknown field Content in struct literal of type "github.com/n-r-w/glyph/pkg/plugins/ui/v1".SubmitCommand_builder
FAIL    github.com/n-r-w/glyph/host/internal/app [build failed]
```

The first run after the implementation failed for the Programmatic text-only scenario. The image request failed with `INPUT_INVALID`, and the following text request was rejected:

```text
--- FAIL: TestPublicUserInputAcrossClients/programmatic/text-only-model (0.00s)
        Error:          Programmatic operation was rejected
        Messages:       code: BUSY
```

Only settlement cleared the Programmatic output. The correction in `runPrepared.Release` is recorded in D8-13.

Behavior RED, taken by replacing the `ProcessInput` call in `Coordinator.RunPrepared` with the submitted message for one run and restoring it afterwards:

```text
ERROR Programmatic operation failed operation_id=image-request ... failure_code=INTERNAL error="model gpt-active does not accept image input at message 0 content 1"
ERROR Programmatic operation failed operation_id=follow-up-request ... failure_code=INTERNAL error="model gpt-active does not accept image input at message 0 content 1"
--- FAIL: TestPublicUserInputAcrossClients (2.21s)
    --- PASS: TestPublicUserInputAcrossClients/programmatic/image-model (0.04s)
    --- FAIL: TestPublicUserInputAcrossClients/programmatic/text-only-model (0.04s)
    --- PASS: TestPublicUserInputAcrossClients/ui/image-model (1.07s)
    --- FAIL: TestPublicUserInputAcrossClients/ui/text-only-model (1.07s)
FAIL    github.com/n-r-w/glyph/host/internal/app 2.242s
```

Without input processing, Core appended the image and failed later with `INTERNAL`. The image then remained in the history, so the following text request failed too.

GREEN:

```text
go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicUserInputAcrossClients$' -count=1 -v
--- PASS: TestPublicUserInputAcrossClients (2.19s)
    --- PASS: TestPublicUserInputAcrossClients/programmatic/image-model (0.04s)
    --- PASS: TestPublicUserInputAcrossClients/programmatic/text-only-model (0.02s)
    --- PASS: TestPublicUserInputAcrossClients/ui/image-model (1.07s)
    --- PASS: TestPublicUserInputAcrossClients/ui/text-only-model (1.06s)
ok      github.com/n-r-w/glyph/host/internal/app 3.224s
```

The test drives the assembled Programmatic gRPC boundary and a UI plugin process through the public UI SDK, with the real Host components and an HTTP provider mock. Each scenario submits text, a PNG image and text, then one text request, and then reads the history through `GetMessages` or `GetSessionTree`.

- With the image-capable `gpt-test`, the image request completes with an agent-start event. The first provider request contains the three blocks in order. The history contains the user message with the image and then the text message.
- With the text-only `gpt-active`, the image request fails with `INPUT_INVALID` and the text `process user input: model gpt-active does not accept image input at content 1`. No agent-start event arrives. The provider receives only the following text request, which completes. The history contains only the text message.

### Domain rules

RED before the methods existed:

```text
go test ./host/internal/domain/model -run 'TestMessageValidateShape|TestDescriptorFindUnsupportedImage' -count=1
host/internal/domain/model/input_test.go:51:24: test.message.ValidateShape undefined (type Message has no field or method ValidateShape)
host/internal/domain/model/input_test.go:72:43: textOnly.FindUnsupportedImage undefined (type Descriptor has no field or method FindUnsupportedImage)
FAIL    github.com/n-r-w/glyph/host/internal/domain/model [build failed]
```

GREEN:

```text
--- PASS: TestDescriptorFindUnsupportedImage (0.00s)
--- PASS: TestMessageValidateShape (0.00s)
ok      github.com/n-r-w/glyph/host/internal/domain/model
```

### Middleware input processing

RED with the test written before the implementation:

```text
go test ./host/internal/usecase/host/middleware -run '^TestProcessInputValidatesSelectedModel$' -count=1
host/internal/usecase/host/middleware/input_test.go:62:14: undefined: NewMockSelectedModel
host/internal/usecase/host/middleware/input_test.go:64:69: too many arguments in call to New
host/internal/usecase/host/middleware/input_test.go:67:26: service.ProcessInput undefined (type *Service has no field or method ProcessInput)
FAIL    github.com/n-r-w/glyph/host/internal/usecase/host/middleware [build failed]
```

GREEN:

```text
go test -race ./host/internal/usecase/host/middleware -run '^TestProcessInputValidatesSelectedModel$' -count=1 -v
    --- PASS: TestProcessInputValidatesSelectedModel/image_model (0.00s)
    --- PASS: TestProcessInputValidatesSelectedModel/text-only_model_with_text (0.00s)
    --- PASS: TestProcessInputValidatesSelectedModel/text-only_model_with_image (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/middleware 1.013s
```

### Run control

`TestCoordinatorExecutesProcessedInput` and `TestCoordinatorFailsBeforeCoreWhenInputFails` were written before the port existed. Their package could not compile then, because its dependencies still referenced the removed text fields, so they have no recorded focused RED. They check that Core receives the processed message, that input processing runs before Core and before the reservation release, and that an input error skips Core, keeps its complete text and releases the reservation once.

```text
--- PASS: TestCoordinatorExecutesProcessedInput (0.00s)
--- PASS: TestCoordinatorFailsBeforeCoreWhenInputFails (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol
```

### Admission and failure mapping

`TestMapOpenRequestMapsUserContent` and `TestUserRequestRejectsBlankText` in `host/internal/controller/programmatic`, and `TestMapCommandSubmitContent` in `host/internal/controller/ui`, check ordered mapping and the `INVALID_ARGUMENT` rejection of a request without blocks, a block without payload, an empty text block and an image without media type or data. `TestRunFailureCodeKeepsInputInvalid` in the Programmatic and UI use-case packages and a new case of `failureCodeForCommand` check the `INPUT_INVALID` category. These tests were written together with the code and have no recorded RED.

### Part 2: public boundary

RED with the reference extension built and the contract generated, before the Host accepted the new kind:

```text
go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^(TestPublicInputMiddlewareProgrammatic|TestPublicInputMiddlewareUI|TestPublicInputMiddlewareHeadless)$' -count=1 -v
INFO loaded extensions directory=... explicit=true extension_count=0 issue_count=1
--- FAIL: TestPublicInputMiddlewareProgrammatic (2.56s)
    --- FAIL: TestPublicInputMiddlewareProgrammatic/removed_image_on_text-only_model (0.41s)
    --- FAIL: TestPublicInputMiddlewareProgrammatic/retained_image_on_text-only_model (0.40s)
    --- FAIL: TestPublicInputMiddlewareProgrammatic/retained_image_on_image_model (0.45s)
    --- FAIL: TestPublicInputMiddlewareProgrammatic/handled_input (0.45s)
    --- FAIL: TestPublicInputMiddlewareProgrammatic/handler_failure (0.42s)
    --- FAIL: TestPublicInputMiddlewareProgrammatic/invalid_action (0.43s)
--- FAIL: TestPublicInputMiddlewareUI (4.37s)
--- FAIL: TestPublicInputMiddlewareHeadless (1.31s)
FAIL    github.com/n-r-w/glyph/host/internal/app 8.276s
```

Startup rejected the reference registration, so no input handler ran and, for example, the image request failed with `INPUT_INVALID` where the handlers should have removed the image.

GREEN:

```text
--- PASS: TestPublicInputMiddlewareProgrammatic (2.54s)
    --- PASS: TestPublicInputMiddlewareProgrammatic/removed_image_on_text-only_model (0.46s)
    --- PASS: TestPublicInputMiddlewareProgrammatic/retained_image_on_text-only_model (0.42s)
    --- PASS: TestPublicInputMiddlewareProgrammatic/retained_image_on_image_model (0.42s)
    --- PASS: TestPublicInputMiddlewareProgrammatic/handled_input (0.42s)
    --- PASS: TestPublicInputMiddlewareProgrammatic/handler_failure (0.42s)
    --- PASS: TestPublicInputMiddlewareProgrammatic/invalid_action (0.41s)
--- PASS: TestPublicInputMiddlewareUI (4.41s)
    --- PASS: TestPublicInputMiddlewareUI/removed_image (1.49s)
    --- PASS: TestPublicInputMiddlewareUI/handled_input (1.47s)
    --- PASS: TestPublicInputMiddlewareUI/handler_failure (1.45s)
--- PASS: TestPublicInputMiddlewareHeadless (1.17s)
    --- PASS: TestPublicInputMiddlewareHeadless/transformed_text (0.39s)
    --- PASS: TestPublicInputMiddlewareHeadless/handled_input (0.38s)
    --- PASS: TestPublicInputMiddlewareHeadless/handler_failure (0.40s)
ok      github.com/n-r-w/glyph/host/internal/app 11.317s
```

Each scenario uses the real reference extension process, the assembled Host and the HTTP provider mock; the trace of the reference records every invocation. Programmatic and UI scenarios submit a first request, then the text request `text follow-up`, and then read the session history. On the text-only `gpt-active` unless stated otherwise:

- `input` with text, image and text: the trace shows `input-handle`, `input-text`, `input-images` in this order. `input-images` receives the submitted blocks as `original` and the text-transformed blocks with the image as `current`. The run starts, and the provider and the session receive `describe this glyph-input`, `[image removed]`, `after image glyph-input`.
- `input-keep-images` with the same request: the request fails with `INPUT_INVALID` and `process user input: model gpt-active does not accept image input at content 1` after all three handlers, without agent-start or provider request. On the image-capable `gpt-test`, the same request runs and the provider receives the image between the transformed text blocks.
- `input` with `glyph-handle-input please`: the request completes; only `input-handle` runs. No agent-start event, provider request or session entry follows. Correction C3 extended this check to every agent event.
- `input-fail` and `input-invalid`: the request fails with `EXTENSION_FAILED`. The complete text contains the cause `reference input handler failed` inside the handler-named text `process user input: middleware handler "input-text": ...`, or is `process user input: middleware handler "input-text" returned invalid user-input action 0`. `input-images` does not run, and no agent event or provider request follows.
- In every scenario, the follow-up request runs, and the provider and the session receive `text follow-up glyph-input`. In UI, the follow-up starts after idle availability returns.
- One-shot: transformed text reaches the provider; handled input exits successfully without a provider request; a handler failure returns the complete error text. Correction C3 replaced this `TestPublicInputMiddlewareHeadless` with `TestPublicInputMiddlewareOneShot`, which runs the production executable.

### Part 2: SDK admission and startup

RED after schema generation:

```text
go test ./sdk/plugins/extension/v1 -run '^TestContextMiddlewareHandlerAdmission$' -count=1
        expected: [][]bool{[]bool{true, false, false}, []bool{false, true, false}, []bool{false, false, true}}
        actual  : [][]bool{[]bool{true, false, false}, []bool{false, true, false}, []bool{false, false, false}}
FAIL    github.com/n-r-w/glyph/sdk/plugins/extension/v1 0.003s
go test ./host/internal/usecase/host/startup -run '^TestServiceLoadPassesContextMiddlewareToRegistrar$' -count=1
host/internal/usecase/host/startup/middleware_test.go:33:39: undefined: RawHandlerKindUserInput
```

GREEN:

```text
--- PASS: TestContextMiddlewareHandlerAdmission (0.00s)
ok      github.com/n-r-w/glyph/sdk/plugins/extension/v1 0.003s
--- PASS: TestServiceLoadPassesContextMiddlewareToRegistrar (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/startup 0.002s
```

### Part 2: middleware owner

RED before the operation existed:

```text
go test ./host/internal/usecase/host/middleware -run 'TestProcessInput' -count=1
host/internal/usecase/host/middleware/input_test.go:136:20: runtime.EXPECT().HandleUserInput undefined (type *MockRuntimeMockRecorder has no field or method HandleUserInput)
host/internal/usecase/host/middleware/input_test.go:175:36: undefined: Handle
host/internal/usecase/host/middleware/input_test.go:183:24: result.IsNone undefined (type model.Message has no field or method IsNone)
FAIL    github.com/n-r-w/glyph/host/internal/usecase/host/middleware [build failed]
```

GREEN:

```text
go test -race ./host/internal/usecase/host/middleware -run 'TestProcessInput|TestContextComposition' -count=1 -v
--- PASS: TestProcessInputHandled (0.00s)
--- PASS: TestProcessInputCancellation (0.00s)
--- PASS: TestProcessInputComposesHandlers (0.00s)
--- PASS: TestProcessInputChainFailure (0.00s)
--- PASS: TestProcessInputValidatesSelectedModel (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/middleware 1.015s
```

The same run passed the existing context-composition tests after `compose` gained the handled result.

### Part 2: tests without recorded RED

`TestCoordinatorCompletesHandledInputWithoutCore` and `TestCoordinatorAbortsWhenInputIsCanceled` in run control, `TestUserInputMiddlewareChecksExactRuntimeGeneration` in `extensionruntime` and `TestUserInputMapping` in the extension runtime infrastructure were written after their code and have no recorded RED. They check completion without Core or settlement, the aborted outcome on owner cancellation, invocation of the exact runtime generation only, and the mapping of the payload and of each action.

## Review corrections, round 1

### C1: one-shot category

`Renderer.WriteError` in `host/internal/infra/headless/renderer.go` prefixes the complete error text with the category that `errtree.CurrentCode` finds on current causes; D8-15 records the format.

RED before the renderer change:

```text
go test ./host/internal/infra/headless -run 'TestRendererWriteErrorShowsCurrentCategory' -count=1 -v
        expected: "[error] EXTENSION_FAILED: run headless agent: process user input: middleware handler \"input-text\": handler failed\n"
        actual  : "[error] run headless agent: process user input: middleware handler \"input-text\": handler failed\n"
        expected: "[error] RETRY_EXHAUSTED: run headless agent: provider unavailable\n"
        actual  : "[error] run headless agent: provider unavailable\n"
--- FAIL: TestRendererWriteErrorShowsCurrentCategory (0.00s)
    --- PASS: TestRendererWriteErrorShowsCurrentCategory/category_only_in_diagnostic_history (0.00s)
    --- PASS: TestRendererWriteErrorShowsCurrentCategory/uncategorized (0.00s)
    --- FAIL: TestRendererWriteErrorShowsCurrentCategory/coded_current_cause (0.00s)
    --- FAIL: TestRendererWriteErrorShowsCurrentCategory/logical_model_failure (0.00s)
```

`TestPublicInputMiddlewareOneShot` builds `host/cmd/glyph` and runs `glyph run --extension-dir <reference> <text>` against a local Responses endpoint. Its RED was taken by disabling the category in `WriteError` for one run and restoring it afterwards:

```text
go test -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicInputMiddlewareOneShot$' -count=1 -v
--- FAIL: TestPublicInputMiddlewareOneShot (2.36s)
    --- PASS: TestPublicInputMiddlewareOneShot/transformed_text (0.43s)
    --- FAIL: TestPublicInputMiddlewareOneShot/handler_failure (0.40s)
    --- FAIL: TestPublicInputMiddlewareOneShot/handler-introduced_image (0.38s)
    --- PASS: TestPublicInputMiddlewareOneShot/handled_input (0.39s)
```

The visible lines were `[error] run headless agent: process user input: middleware handler "input-text": ...` and `[error] run headless agent: process user input: model gpt-active does not accept image input at content 1`.

GREEN:

```text
--- PASS: TestRendererWriteErrorShowsCurrentCategory (0.00s)
--- PASS: TestPublicInputMiddlewareOneShot (0.94s)
    --- PASS: TestPublicInputMiddlewareOneShot/transformed_text (0.42s)
    --- PASS: TestPublicInputMiddlewareOneShot/handled_input (0.41s)
    --- PASS: TestPublicInputMiddlewareOneShot/handler-introduced_image (0.42s)
    --- PASS: TestPublicInputMiddlewareOneShot/handler_failure (0.43s)
```

The failure scenarios exit with status 1, show `[error] EXTENSION_FAILED: run headless agent: process user input: middleware handler "input-text": reference input handler failed` or `[error] INPUT_INVALID: run headless agent: process user input: model gpt-active does not accept image input at content 1`, and send no provider request. Handled input exits with status 0, without an `[error]` line or a provider request.

### C2: aborted outcome only for owner cancellation

RED with the former `ctx.Err() != nil` condition:

```text
go test ./host/internal/usecase/host/runcontrol -run 'TestCoordinatorKeepsInputFailureDuringCancellation|TestCoordinatorAbortsWhenInputIsCanceled' -count=1 -v
--- PASS: TestCoordinatorAbortsWhenInputIsCanceled (0.00s)
        expected: 0x3
        actual  : 0x2
--- FAIL: TestCoordinatorKeepsInputFailureDuringCancellation (0.00s)
```

GREEN after `RunPrepared` reports the aborted outcome only when the current cause is the cancellation of the owning context:

```text
--- PASS: TestCoordinatorAbortsWhenInputIsCanceled (0.00s)
--- PASS: TestCoordinatorKeepsInputFailureDuringCancellation (0.00s)
ok      github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol 1.012s
```

### C3: permanent public tests

The tests in `host/internal/app/input_middleware_lifecycle_integration_test.go` check behavior that already existed, so their RED was taken by changing one mechanism for one run and restoring it afterwards:

- `TestPublicInputMiddlewareOriginalReplacement` uses `input-original`. With `compose` passing the current value as `original`, the provider received `glyph-input-A` before `glyph-input-B`, and the persisted user entry had three blocks instead of two.
- `TestPublicInputMiddlewareCancellation` uses `input-block`. With owner cancellation treated as an ordinary chain failure, the blocked request failed with `EXTENSION_FAILED` instead of being canceled.
- `TestPublicInputMiddlewareRuntimeLoss` uses `input-exit`. With the Programmatic output released only for unstarted runs, the next request was rejected with `BUSY`.

GREEN:

```text
--- PASS: TestPublicInputMiddlewareOriginalReplacement (0.40s)
--- PASS: TestPublicInputMiddlewareCancellation (0.42s)
--- PASS: TestPublicInputMiddlewareRuntimeLoss (0.39s)
```

- Original replacement: the trace shows the three handlers in order, and `input-images` receives `glyph-input-A` in `current`. The provider and the persisted user entry contain the submitted text and `glyph-input-B` without `glyph-input-A`.
- Cancellation: while `input-text` blocks, a second user request is rejected with `BUSY`. Canceling the first request cancels it, and the cancel operation completes with target state canceled. `input-images` does not run; no agent event, provider request or session entry follows. The next request completes.
- Runtime loss covers the runtime-loss path that part 2 supported by code reading only. The extension exits during `input-text`; the request fails with `EXTENSION_FAILED` and a text that names `input-text`. `input-images` does not run, and no agent event, provider request or session entry follows. The next request is admitted and fails with `EXTENSION_FAILED`, because the extension stays unavailable.
- Handled input: the Programmatic and UI observations now record every agent-event payload of the first request, not only agent-start; handled, failed and `INPUT_INVALID` requests have none.

## Checks

After the last change of the review corrections:

| Command | Result |
| --- | --- |
| `task generate` | Exit 0 |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0; no proposed changes |
| `task lint` | Exit 0; zero issues; ifaceguard reported no errors |
| `task test` | Exit 0 |
| `go test -race -tags=integration -p 1 -parallel 1` for each package changed by the corrections outside `host/internal/app` | All passed |
| `go test -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run` with the input, user-input, context, wire, terminal refusal, headless semantic and `TestClientAppSuites` tests | Passed; `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` skips here because its permission failure injection requires Darwin |

Part 2 ran the same `host/internal/app` set with the handler-composition, UI submit and configured request tests; `TestStandardTUIHostSmoke` skips on this platform because its real-PTY acceptance runs only on Darwin arm64. Part 1 ran `TestExternalUIPlugin` in `testdata/external-plugins`; it passed.
