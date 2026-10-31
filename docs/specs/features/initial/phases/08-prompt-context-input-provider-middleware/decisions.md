# PHS-08 decisions

Decisions below are inside the [confirmed authority](target-picture.md). They supplement the [technical solution](technical-solution.md) and preserve the ticket's scope.

## D8-01: Context composition follows prepared-context revisions

Host applies instruction/context middleware after existing conversation preparation. Ordinary transient retries reuse the effective result. A committed overflow recovery creates new prepared history and therefore a new original/current composition using frozen registrations, selection and captured owner. A failed handler is terminal and is never retried.

An arbitrary replacement F(H) contains no reusable rule for later compacted history C(H). Reusing the old value would ignore recovery; assigning new history directly would discard middleware effects. Recomposition after successful preparation preserves both requirements. The analytical proposal to require one successful invocation across an entire logical execution was stronger than the confirmed ticket and is not adopted.

## D8-02: Serialized replacement uses a confidential projection

Public serialized-request handlers receive safe JSON containing invocation-local references in place of private provider replay. Retained references restore exact private fragments; removed references discard replay. Credentials, account identity and opaque replay remain private. Invalid references fail before dispatch as extension failure.

The SDK's existing `option.WithMiddleware` supplies the HTTP interception point. Raw-attempt callbacks remain provider-neutral. Moving serializers or HTTP execution into Host and adding a second transport interception layer are unnecessary.

This is a contract-shape selection within delegated authority. Complete authenticated raw-request access was never promised and would violate mandatory architecture confidentiality.

## D8-03: Finalization covers canonical messages and configured results

Finalized-user replacement occurs after input processing and before final modality validation. Core applies model/tool-result replacement before append and uses the same final values for events and subsequent behavior. Configured terminal responses use the same owner without conversation persistence. Ordinary conversation model results are finalized once in Core, not again in model execution.

Role and Host-owned accounting/execution identity are preserved. Provider-private context uses local references in public projections. Failed replacement does not append its candidate, conceal completed tool actions, or recursively invoke the failed chain.

## D8-04: Final input incompatibility has a public category

Use `INPUT_INVALID` for well-formed final user input incompatible with the selected descriptor. Invalid handler actions remain `EXTENSION_FAILED`. Both meanings supplement complete source text. Programmatic/UI mappings and command allowlists must distinguish this asynchronous final-input failure from admission-shape rejection and provider failure.

## D8-05: Fully handled input completes without a synthetic run

Runcontrol completes the accepted client operation successfully and releases its reservation. It invokes no Core execution, user append, provider request or synthetic run lifecycle. Existing client completion and availability paths remain responsible for idle recovery.

## D8-06: All supported callers share applicable middleware

Programmatic/UI submit contracts represent ordered text/image blocks. Standard TUI and one-shot retain their current text UX by constructing one text block. Configured model requests share instruction/context, provider-wire and completed-model transformation while retaining their current absence of automatic conversation preparation. Add no legacy text/content fallback or blanket nested-call bypass.

## D8-07: Public headers exclude private authentication state

Provider mapping retains credential, cookie, proxy authentication and account headers privately. Ordinary extensions can transform public headers and add ordinary custom headers. Protected writes fail. Response observations exclude protected authentication/cookie data and redact known authentication secrets. Protected account-header exclusion applies to structured middleware payloads. Error text retains ordinary account, organization and project identifiers because they are not authentication secrets. Host middleware coordination imports no credential or concrete provider package.

## D8-08: Instruction and context handlers are separate transformer kinds

The Extension Contract adds `HANDLER_KIND_SYSTEM_INSTRUCTIONS` and `HANDLER_KIND_MODEL_CONTEXT`. Their `HandleRequest` payloads are `system_instructions` and `model_context`, and their `HandleResponse` actions use the same names. The context payload is named `model_context` because `HandleRequest.context` already carries the issued extension context.

Each invocation carries the immutable `original` and the `current` value from preceding handlers. Each action is a oneof of `preserve = true` or a complete `replace` value. A missing action or `preserve = false` is an invalid action. An instruction replacement can be any string, including an empty string. A history replacement can contain any number of entries, including none.

Host composes the instruction chain before the history chain. Model execution obtains both through its own `ContextMiddleware` interface. That interface captures the registered handlers once per logical execution, so a recomposition after a committed overflow recovery uses the same registrations, as D8-01 requires.

## D8-09: Typed history uses replay references on model content

`ModelContextEntry` contains a user message, a model response or a tool result. User messages and tool results reuse `SessionTreeUserMessage` and `SessionTreeToolResult`. A model response is a `ReplayableModelResponse`: each block wraps one `SessionTreeModelContent` and has an optional `replay_reference`. The session-tree and compaction representations stay unchanged, because their mappings omit private replay and do not restore it.

For each invocation, Host assigns a fresh reference to every content block with private replay in the original and in the current history. Each reference is a random per-invocation prefix plus a sequence number, so it contains no provider data. A replacement block with a known reference receives that private replay again. A block without a reference has no replay. A replacement fails as an invalid action when its reference is unknown, is used twice, or is on a content kind other than the one that owned the replay.

The public projection exposes only model-visible content. A replaced model response therefore contains only its content, its restored replay and the outcome `stop`. Provider, model, usage and response identifiers stay with Host and do not take part in request-local history. Providers build requests from content and replay only.

## D8-10: Core receives a prepared execution capability

Agent Core's effective-context port `EffectiveContextPort.PrepareContext` receives the instructions, projected history and tools that Core assembled. It returns an `EffectiveContext` with the effective request and a `ModelProvider` capability. Core streams that request through that capability. The port replaces Core's former model-snapshot interface, and `modelexecution.Service` no longer implements a direct logical `Stream`.

`PrepareContext` captures the conversation owner and the active catalogue binding once, runs existing conversation preparation and then composes instructions and history. The returned prepared provider binding keeps the raw provider, the captured conversation owner, the captured middleware handlers and the prepared request. It executes without repeating preparation or selection. The catalogue's exact-selection resolution without credential preflight had no other caller and is removed.

A preparation or composition failure follows Core's existing provider-failure path: Core records a failed model response with the complete error text, and no provider request is sent.

## D8-11: Policy consumers receive the prepared request

Model execution keeps the prepared, untransformed request beside the effective request. Completed-attempt observation and overflow recovery receive the prepared request with the base instructions. Initial and overflow preparation rebuild history from the session snapshot, and outgoing-input validation runs inside that preparation; therefore initial and overflow preparation never see middleware results.

The standard compaction policy anchors reported usage to the observed instructions and history. It reuses that usage only when the next policy input starts with the same history. Observation of transformed values would not match the prepared policy input when a handler changes the request, so the policy would ignore reported usage and compact by estimate. An integration probe confirmed this: with observation of the effective request, the reported-usage scenario compacted unexpectedly.

The reported usage still measures the effective request, because the provider counted the transformed values. Request-local entries therefore affect the policy's usage estimate but never its history.

## D8-12: Final-context validation and the reference command

Each handler result is checked only for its payload structure and its replay references. After both chains of a prepared-context revision succeed, model execution validates the final history against the input modalities of the selected model descriptor. The check covers preserved history, executions without context handlers, configured-model requests and every revision composed after a committed overflow recovery. Configured-model requests no longer check their input before composition.

The composer reports whether a history handler replaced the history in the composition. When a handler replaced it, an unsupported final history is `EXTENSION_FAILED` with the complete validation text, because handler output is the final value. When no handler replaced it, model execution returns the plain validation error without a category, as configured-model requests did before this phase; for example, `model gpt-active does not accept image input at message 0 content 1`. A handler may add an image that a later handler removes; only the final history counts.

The public reference for this unit is the separate command `testdata/external-plugins/cmd/context`. A separate command keeps the provider-wire reference scenarios and their assertions unchanged. The command embeds its example project instructions from `project-instructions.md`.

## D8-13: Submit payloads and final-input validation

Programmatic `UserRequest` and UI `SubmitCommand` contain `repeated UserContent content`. Each package already defines `UserContent` as one text or image block for user history, so the submit payload reuses the definition of its own package. Admission rejects a request with `INVALID_ARGUMENT` when it has no blocks, when a block has no payload, when a text block is empty, or when an image has no media type or no data. Both controllers apply these rules through `model.Message.ValidateShape`. Programmatic admission also rejects a text block that contains only whitespace, as it rejected such a text before this phase.

Run control owns the `InputProcessor` interface. `Coordinator.RunPrepared` calls `InputProcessor` after it takes the prepared reservation and before Core. Core appends the message that `InputProcessor` returns. The middleware owner implements the interface and reads the selected descriptor from the active binding of the provider catalogue. When the final message contains an image and the selected model has no image input, the run fails with `INPUT_INVALID` before agent-start. The complete text is `process user input: model gpt-active does not accept image input at content 1` for the second block of a request to the text-only model `gpt-active`.

A run that fails before Core has no settlement. Settlement clears the Programmatic output of a run that Core executed. For every other run, the release of the Programmatic run clears the output of that run; otherwise the next request would be rejected with `BUSY`. UI preparation restores idle availability in its release.

## D8-14: User-input handlers, handled completion and the input reference

The Extension Contract adds `HANDLER_KIND_USER_INPUT`. Its `HandleRequest` payload and `HandleResponse` action are both `user_input`. `UserInputInvocation` carries the immutable `original` and the `current` input as `SessionTreeUserMessage`, the user message of session history, as D8-09 does for history entries. `UserInputAction` is a oneof of `preserve = true`, a complete `replace` value and `handle = true`. A missing action, `preserve = false` or `handle = false` is an invalid action. A replacement must pass `model.Message.ValidateShape`, the rule of D8-13 admission; otherwise it is an invalid replacement.

`middleware.Service.ProcessInput` implements the run-control port of D8-13. It runs the user-input handlers in registration order through the same composition as D8-08, which accepts `handle` only for this kind. A handler error, runtime loss, invalid action or invalid replacement is `EXTENSION_FAILED` with the complete text, for example `middleware handler "input-text" returned invalid user-input action 0`. After the last handler, the final unhandled input is checked as in D8-13. A failure whose current cause is only the cancellation of the owning context keeps no category, and run control reports the aborted outcome for it, so each client follows its existing canceled-operation path. Any other failure keeps its category and the failed outcome, even when the owning context is canceled at the same time.

The port returns no message for handled input. `Coordinator.RunPrepared` then reports a completed outcome without Core execution or settlement. Programmatic release clears the run output as D8-13 describes, UI preparation restores idle availability in its release, and one-shot accepts the completed outcome.

The public reference is the separate command `testdata/external-plugins/cmd/input`. The context reference tests assert the exact order of traced instruction and context handlers, so input handlers in `cmd/context` would change those assertions.

## D8-15: One-shot errors show the current category

The one-shot renderer writes a terminal error as `[error] <CATEGORY>: <complete error text>` when a current cause of the error carries a public category, and as `[error] <complete error text>` otherwise. For example, a failed input handler gives `[error] EXTENSION_FAILED: run headless agent: process user input: middleware handler "input-text": reference input handler failed`. `errtree.CurrentCode` finds the category on current causes only, so a category kept only as diagnostic history is not shown. The error text itself stays without the category, because Programmatic and UI carry the category in a separate field.

## D8-16: Finalized messages are one handler kind with role-preserving entries

The Extension Contract adds `HANDLER_KIND_FINALIZED_MESSAGE`. Its `HandleRequest` payload and `HandleResponse` action are both `finalized_message`. `FinalizedMessageInvocation` carries the immutable `original` and the `current` message as `ModelContextEntry`, the D8-09 entry of a user message, a model response with replay references, or a tool result. `FinalizedMessageAction` is a oneof of `preserve = true` and a complete `replace` entry. A missing action or `preserve = false` is an invalid action.

One kind serves all three roles. An extension registers one handler and preserves the roles that it does not change. Each finalized message runs one chain in registration order. Because the entry can hold any role, a replacement can attempt a role change, and Host rejects it. Separate kinds per role would need three registrations for an extension that changes all roles, and a new message type would repeat `ModelContextEntry`.

The runtime mapping uses the D8-09 replay references: it projects the original and the current message with fresh references for one invocation and restores the references that a replacement keeps. An unknown, duplicated or misplaced reference fails the handler.

`middleware.Service` accepts a replacement only with the role of the original. Each role has its own rule:

- A user message must pass `model.Message.ValidateShape`.
- A model response takes only content and restored replay from the replacement. The outcome, error message, provider, configured model, reported model, response identifier, usage and diagnostics stay those of the original. The content must pass `Response.ValidateTerminalContent`. The replacement must contain the tool calls of the original in the same order, with the same identifiers and names; only their arguments can change. Agent Core selects tool execution by the outcome and runs the calls of the final response, so a removed, added, renamed or reordered call would contradict the outcome or the results that Core records. For example, a tool-use response whose replacement removes its only call fails with `tool calls [] differ from finalized tool calls [id="call" name="read"]`.
- A tool result must keep the call identifier and the tool name of the original, and each image needs a media type and data.

A handler error, runtime loss, invalid action, invalid reference or rejected replacement stops the chain and is `EXTENSION_FAILED` with the complete text, for example `middleware handler "finalized-a" finalized-message replacement: tool "write" differs from finalized tool "read"`. A failure whose current cause is only the cancellation of the owning context keeps no category, as in D8-14.

## D8-17: Agent Core finalizes model responses and tool results through its own port

Agent Core declares `MessageFinalizer` with `FinalizeModelResponse` and `FinalizeToolResult`. `middleware.Service` implements both methods with the D8-16 chain, and the three application assemblies pass the same owner to Core and to run control. One method per role keeps user messages out of the Core port. A single method that takes a history entry would accept a user message and would make Core wrap and unwrap each value.

Core finalizes a value immediately before its append and uses the returned value for the append, the completion events, the turn and run summaries, tool execution and the next model request:

- a successful terminal model response in `runTurn`, before `message_end`;
- a response retained after a provider failure in `finalizeProviderError`;
- each executed tool result in `executeCalls`, before `tool_execution_end` and `tool_result`;
- each tool result that `skipLengthCalls` generates for the calls of a length-limited response.

Core calls the finalizer with the run context and outside `appendHistory`, so a finalization failure keeps `EXTENSION_FAILED` and never becomes `PERSISTENCE_UNAVAILABLE`. A failure other than cancellation ends the run as failed with the complete text. Core appends nothing for the failed value and delivers no completion event for it. Entries committed earlier and tool calls that already ran remain. Core does not invoke the chain again for that value. In particular, the failure does not pass to `finalizeProviderError`, which would finalize the same response a second time.

A response retained after a provider failure is finalized only when its outcome is failed. An aborted response, whose provider error is only cancellation, is recorded without finalization. When finalization of a failed response fails, Core records no response and returns the finalization failure as the current cause, with the provider failure as diagnostic history (`diagnostic.NewCurrentCause`). The run is therefore `EXTENSION_FAILED` in every client mode, and its text contains both complete causes. The Programmatic and UI failure mappings read every category from current causes only: the persistence failure, the model-execution category and, in Programmatic, the compaction category. A persistence, provider or compaction failure kept as diagnostic history therefore does not replace the extension category. Such a failure without a finalization failure keeps its own category, for example `PERSISTENCE_UNAVAILABLE`, `MODEL_FAILED` or `COMPACTION_FAILED`.

Cancellation stops new handler work and leaves terminal bookkeeping to its existing owner. Finalization of a model response or a tool result starts when `middleware.Service` admits the first finalized-message handler; admission issues the handler's extension context.

- When the run is canceled before the owner admits the first handler, the owner returns the value unchanged without error, and Core records it as without finalized-message handlers: the response or the tool result is appended and its completion events are delivered.
- When cancellation arrives after the owner admitted a handler, the chain fails. Core appends no candidate, delivers no completion event for it and does not invoke the chain again. The current cause of the run error is then only cancellation, so the run ends aborted; this includes a response retained after a provider failure, whose provider failure stays as diagnostic history.
- When cancellation arrives after the chain succeeded, Core records the final value.

The owner applies this rule only to the Agent Core methods `FinalizeModelResponse` and `FinalizeToolResult`. User-message finalization in `ProcessInput` and the configured-model method `FinalizeConfiguredResponse` fail on cancellation, as the other chains of the owner do. Runs without finalized-message handlers behave as before this decision, because the owner returns the value unchanged without starting a handler.

## D8-18: User and configured-model finalization and the finalized-message reference

`middleware.Service.ProcessInput` runs the finalized-message handlers on the user message after the user-input handlers and before the final check against the selected model. Fully handled input reaches no finalized-message handler. Agent Core appends the message that `ProcessInput` returns and does not finalize it again. A finalization failure is `EXTENSION_FAILED`, as in D8-14.

The final check of D8-13 covers an image that a finalized-message handler adds, as it covers an image that a user-input handler adds. The final input is well formed, so the failure is `INPUT_INVALID`, for example `process user input: model gpt-active does not accept image input at content 1`. Only a malformed handler result is `EXTENSION_FAILED`, as D8-04 requires.

Model execution declares `ResponseFinalizer` with `FinalizeConfiguredResponse`, and the application assemblies bind the middleware owner through `BindResponseFinalizer`. `RequestConfigured` rejects a request before binding resolution and provider dispatch when no finalizer is bound, with the text `configured model request requires a bound response finalizer`; a successful configured result therefore always passes through the owner. Rejecting the request is a smaller change than adding the finalizer to the constructor of model execution, whose callers construct it before the middleware owner exists.

`RequestConfigured` finalizes the terminal response of a configured-model request before it returns the response; extension configured-model requests and branch summaries both use it. Model execution persists nothing for these responses. A finalization failure is a logical `EXTENSION_FAILED` failure with the text `finalize configured model response: <complete cause>`. A failure whose current cause is only the cancellation of the owning context keeps no category, as in D8-14.

Programmatic and UI clients receive every streamed event of a model message with provider values: content start, text delta and content end, and tool-call start, delta and end, including the arguments of `tool_call_end`. The client events `message_end`, `tool_execution_start`, `tool_execution_end`, `tool_result` and `turn_end`, session history, tool execution and later model requests carry the final value.

One-shot output is permanent, so it shows only final model text when an extension can replace it. After startup, the headless assembly asks the middleware owner whether an accepted extension registered a finalized-message handler. If one did, the assembly switches the one-shot renderer to final-text output: the renderer ignores streamed text deltas and writes the final text of each model message when `message_end` arrives. Without such a handler, one-shot output streams as before. Final-text output delays the visible answer until the model message is finalized.

The public reference is the separate command `testdata/external-plugins/cmd/finalized`. It registers two finalized-message handlers and one tool, so a single extension process demonstrates user, model and tool-result finalization. The executable name selects the failure scenarios.

## Terminology check

| Concept | Candidate | Position | Level | Result |
| --- | --- | --- | --- | --- |
| Request-local provider capability retaining selected provider and captured owner | prepared provider binding | No new domain name requested | Implementation detail | Explain the object in the solution; use an implementation identifier in code; no glossary term |
| New history produced by initial preparation or committed overflow | prepared-context revision | Special case of prepared history | Implementation detail | Explain the successful recomposition condition; no glossary term |
| Public placeholder identifying private replay for one invocation | replay reference | No new domain name requested | Implementation detail | Define its preserve/discard behavior at the public contract; no global domain rename |
| Core-owned interface that returns the effective request and its execution capability | effective-context port | Term of the technical solution | Implementation detail | Code identifier `EffectiveContextPort` in `host/internal/usecase/agent/run`; distinct from the `MODEL_CONTEXT` handler kind |
| Final instructions and history sent to raw provider attempts | effective request | Result of composing a prepared-context revision | Implementation detail | Use beside "prepared request"; no glossary term |
| Instructions and history after conversation preparation and before middleware | prepared request | Original value of a composition | Implementation detail | Policy observation and overflow recovery receive it; no glossary term |
| Compaction-policy owner captured for one logical execution | captured conversation owner | Term of the technical solution | Implementation detail | Use in comments instead of "policy owner"; no glossary term |
| Handler output that changes the history of a composition | history replacement | Selects the failure category of final validation | Implementation detail | Define its category effect in D8-12; no glossary term |
| Run-control interface that returns the final user message before Core | input-processing port | Term of the technical solution | Implementation detail | Code identifier `InputProcessor` in `host/internal/usecase/host/runcontrol`; no glossary term |

## Open questions

None.
