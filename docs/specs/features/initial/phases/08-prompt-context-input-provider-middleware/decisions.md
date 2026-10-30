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

## Open questions

None.
