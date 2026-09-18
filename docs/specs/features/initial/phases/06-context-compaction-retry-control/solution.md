# Technical Solution: PHS-06 Context compaction and retry control

Status: Approved technical solution; independent dry run completed with no unresolved blocker. Ready for separately authorized implementation.

## Problem Statement

The [ticket](ticket.md#problem-statement) defines the problem. Its [requirements](ticket.md#requirements) include the approved recent-context budget, repeated compaction, configurable retry policy, and terminal extension failures. The [Domain Glossary](../../../../../terms.md) defines the terminology.

## Proposed Solution

### Ownership and dependency direction

Keep `host/internal/usecase/host/modelexecution.Service` as Core's logical model-execution owner. Add `host/internal/usecase/host/contextcompaction.Service` only for budget preparation, extension coordination, final validation, and commit requests. Put summary-generation algorithms and prompts in the bundled extension at `plugins/extension/compaction`. There is no second implementation of Core's model-provider interface and no Host summary-generation fallback.

| Owner | Responsibility |
|---|---|
| Agent Core `run.Service` | Model/tool loop, neutral streamed-response state, terminal response persistence through its history port. No compaction or retry policy. |
| New `contextcompaction.Service` | Active-branch context projection, budget calculation, proposed boundaries, handler/generator dispatch, final result validation, and session commit requests. |
| Bundled compaction extension | Standard summary algorithm, prompts, preceding-summary updates, summary-model requests, and returned result/source metadata. It can change the proposed preserved boundary. |
| New `extensionmodels.Service` | Extension-facing model/provider catalogue queries and configured-model requests, context validation at their boundaries, and public result/error mapping. |
| `extensioncontext.Service` | Issued runtime/session bindings, binding validation, session-bound reads and appends, and commit protection. It does not execute model requests. |
| `modelexecution.Service` | One shared provider-attempt/retry path; requests session-context preparation for agent calls and compaction before an accepted overflow retry. Configured-model calls do not compact the conversation. |
| `sessions.Service` | Atomic compaction-entry persistence, active-branch validation, source accounting, and independent client-history projection. |
| `extensionruntime.Service` | Registration and invocation transport for compaction and retry handlers, not their ordering or failure policy. |
| Provider adapters | One provider attempt, provider-owned failure classification, and `Retry-After` extraction. SDK retries remain disabled. |
| Host client use cases and outputs | Operation admission, progress, terminal failure mapping, and client-neutral compaction/retry controls. |

The agent call path stays `run.Service -> modelexecution.Service.Stream -> ProviderAttempt.Stream`. Before provider dispatch, model execution calls its context-compaction port. That port prepares the active context and coordinates required compaction; it does not execute model requests itself.

The generation path is `Host compaction -> Extension Contract -> bundled extension -> extensionmodels.Service.Request -> modelexecution.Service.Request`. The nested configured request shares provider retries but bypasses active-conversation compaction. It therefore cannot recursively start another conversation compaction. Extension-configured requests and branch summarization keep using the configured-model path.

Model execution declares the context-preparation, completed-usage observation, and overflow-compaction interface it consumes. `contextcompaction.Service` implements it and contains its assertion. It declares its own session, runtime, context-issuer, and output dependencies. Sessions and extension runtime implement those interfaces. Compaction has no outbound Go model-execution interface: the extension owns summary requests across the public process boundary.

Extract `ReadModels`, `ReadProviders`, and the extension-facing `Request` operation from `extensioncontext` into `host/internal/usecase/host/extensionmodels`. Move their catalogue/model-request dependencies, model-query projection, and model-specific failure mapping with the behavior. The new owner declares `Catalog`, `ModelRequester`, `RequestFailure`, and `ContextValidator` interfaces at their consumption sites. Keep context checks before work and before exposing a successful catalogue or model result. The ownership extraction preserves behavior; logical terminal-failure changes belong to the retry work defined below.

Split the extension controller's consumed operation interfaces into model operations and the remaining context/session operations. Both interfaces stay in the controller package. The controller invokes `extensionmodels` directly for model operations. Remove the old model-operation methods, requester field, and `BindModels` dependency from `extensioncontext`; leave no forwarding facade. The three application compositions construct and inject the new owner directly.

The assertion import graph is `extensioncontext -> contextcompaction -> modelexecution -> extensionmodels`, with `extensioncontext -> extensionmodels` for context validation. Catalogue and raw model-execution implementations assert the new consumer contracts. `extensionmodels` imports domain values and controller contracts, not the concrete `extensioncontext` package or its `ContextError` implementation. It uses the controller-owned failure interface and owns its model-operation errors. This moves the real model-access responsibility rather than moving an interface alone to hide a cycle.

This proposed graph was checked against the existing package graph with issuer, validator, catalogue, sessions, and runtime assertions included. The implementation still needs a complete import check after extraction. The bundled plugin imports public contracts and SDK packages, never `host/internal`. These [consumer-owned boundaries](../../architecture.md#programming-interfaces) preserve direct Core model execution without a compaction wrapper.

### Active-branch projection and persistence

Add a `CompactionEntry` payload to `session.Entry`. It contains the summary, first preserved entry ID, result source, optional normalized usage, optional persisted estimated cost, and optional extension details. Model source data identifies the actual provider, model, and reasoning selection used for the summary; extension-only results do not inherit an unused model's usage or cost.

Keep two different projections:

- The client transcript and session tree retain original entries and compaction entries. Add the compaction variant to public session-entry and tree-entry payloads, restored-history and committed-entry mappings, SDK validation, TUI projections, and extension session-tree projection together with persistence.
- Model context uses the latest compaction entry on the selected branch, followed by its preserved suffix. All compaction-marker entries are skipped while projecting that suffix, so neither the selected summary nor older summaries are emitted twice.

For the first compaction, the summarized span starts at the beginning of the active model-visible branch. For later compactions, it starts at the previous `first_kept_entry_id`. The preceding summary is a separate input to summary generation. Messages retained by an earlier compaction can therefore enter a later summary without reopening the already summarized original prefix.

The preserved suffix includes the complete model response and all associated tool results at its first tool boundary. Boundaries are entry-based, not byte offsets. A pending tool batch is not a compaction boundary. Model-hidden extension entries and opaque provider reasoning bytes are not exposed to compaction extensions. The preceding compaction result retains its optional extension details for subsequent compaction handlers.

Add a sessions commit operation that checks the expected session incarnation and active leaf, validates the boundary, and appends one compaction entry atomically. It updates the model-context projection only after persistence succeeds. Client publication occurs through the existing ordered commit/publication boundary. A publication error exposes the committed outcome and its error; it does not claim that persistence was rolled back.

`historyFromEntries` currently feeds both session history and client history through `storedHistoryFromEntries`. Do not make that shared function discard the original prefix. Add an explicit compacted-context projection instead. Expose Core's pure history-projection helper for reuse by both `Service.ProjectHistory` and the Host compaction owner, retaining one implementation of tool-call/result ordering and failed-model filtering.

Session restore, tree navigation, fork, and clone rebuild context from compaction entries on the selected branch. A branch before a compaction entry retains its unsummarized projection. Session accounting counts persisted usage once; projecting a summary into model context does not create another accounting record.

### Host budget calculation

Use the selected descriptor's `maxTokens` as the response reserve. The input budget is `contextWindow - maxTokens`. Trigger threshold compaction when the context-token estimate exceeds that input budget. Equality fits. Cumulative session token usage is never an input to this calculation.

#### Deterministic fallback estimate

Define `S(B, I) = 8 + ceil(B / 4) + 2048 * I` for one model-visible record. `B` is the sum of its text/JSON UTF-8 byte counts and retained opaque provider-payload byte lengths; `I` is its image count. Use integer arithmetic and round once per record, not once per string or streamed delta. The 8-token value is a framing allowance; 2048 tokens is a fixed image weight independent of file compression and encoded byte length. Both are Glyph sizing-policy constants, not measured provider costs or guaranteed upper bounds.

The four-byte text baseline follows the average BPE compression described in the [tiktoken documentation](https://pypi.org/project/tiktoken/). It is an approximation, not a tokenizer dependency.

| Record | Bytes included in `B` | Images included in `I` |
|---|---|---|
| Nonempty system instructions | Exact instruction text | 0 |
| One tool definition | Tool name, description, exact `InputSchemaJSON` bytes, and present grammar definitions and grammar-input property | 0 |
| User message or model-visible extension message | All text blocks in that projected message | All image blocks |
| Model response | Text, visible reasoning, refusal text, each tool call's ID, name, and exact argument JSON bytes, plus lengths of opaque replay payloads in the outbound snapshot | 0 |
| Tool result | Tool-call ID and every text result block | All image result blocks |
| Active compaction summary or branch summary in model context | The complete synthetic user-message text actually included in the projection | 0 |

Absent system instructions, an absent tool catalogue, and entries excluded from model context contribute zero records. Image bytes, timestamps, billing data, diagnostics, and client visibility do not enter `B`. Opaque replay payloads are measured by byte length only; their content is neither interpreted nor exposed to extensions. Provider-specific replay filtering can make this size proxy overestimate a request. The fixed framing allowance covers role, block, and non-text constraint markers without importing provider wire formats. Count grammar variants as declared in the provider-neutral tool descriptor, not by reproducing a provider adapter's grammar-selection policy. Do not parse or reformat JSON for sizing; measure the supplied byte sequence.

The full fallback estimate is the sum of all record estimates for the exact outbound projection, including system instructions and tools. The bundled extension uses the same numeric sizing policy on the summary request it constructs; its serialized source text is measured as that request's text, not as the original messages a second time. Images remain separate image inputs. Host supplies per-entry estimates in compaction input so custom handlers can reason about the proposed boundary without importing Host code.

#### Reusing reported usage

Keep at most one reported-usage baseline for the active conversation. It is process-local and introduces no new persisted metadata. Record it only for a completed, delivered conversation-model response with normalized usage and outcome `stop`, `tool_use`, or `length`. Configured-model calls, including summary generation, never replace this conversation baseline. Failed, aborted, and intermediate retry attempts never create one.

The baseline retains the session ID and incarnation, complete model descriptor and reasoning selection, instructions, tool definitions, the captured request history, the returned model response, and normalized usage. Retain detached values. `modelexecution` reports the completed conversation attempt through a method on its context-compaction dependency; the compaction owner stores the baseline. Core receives no estimation policy or new baseline-management responsibility.

A baseline is reusable only when all these conditions hold:

- Session ID and incarnation, model descriptor, reasoning selection, instructions, and tool definitions equal the captured values.
- The captured input history followed by the completed response is an exact prefix of the next provider-neutral history projection. Compare content values, including image and provider-replay bytes, not only their estimated sizes. A difference introduced by normalization also prevents reuse.
- No compaction has cleared the baseline. Navigation and session changes are checked through identity and exact-prefix equality rather than a separate event subscription. A process restart starts with no baseline.

For a reusable baseline, let `U = InputTokens + CachedInputTokens + CacheWriteTokens + OutputTokens` from normalized `model.Usage`. `ReasoningTokens` is already part of output and is not added again. The new estimate is `U + sum(S(B, I))` over messages after the matched prefix. Do not add system instructions, tools, or the matched prefix again. Absent or invalid usage, or any failed identity/prefix check, selects the full fallback estimate. Explicit present zero usage is not treated as absent.

Store the baseline before returning the logical stream result, but require the prefix check on the next request before using it. A response that was not persisted therefore cannot authorize reuse. Context estimation remains an estimate even when it starts from reported usage, because it predicts the next input rather than reproducing provider billing.

#### Preserved boundary and validation

Use fallback per-record estimates for suffix selection; provider usage gives no exact per-message split. Choose the latest valid entry boundary whose suffix estimate reaches the configured retained-context target. Keep whole messages and complete tool-call/result groups. The active compaction summary, system instructions, and tools do not consume this recent-message target. Entries excluded from model context have zero weight and cannot move the chosen boundary earlier merely by existing.

A history shorter than the retained-context target produces a proposal that retains all messages. This is not a pre-handler rejection: request handlers can still supply a complete result or replace the proposed boundary. When invoked with neither a preceding summary nor new prefix messages, the bundled generator fails with a budget explanation. A preceding summary alone remains eligible for another summary update. The bundled strategy does not discard recent messages to manufacture a summary. Returned extension boundaries remain subject to active-branch, tool-pairing, and final-context validation.

After compaction, invalidate the baseline and size the actual new summary plus preserved suffix, instructions, and tools with the fallback estimate. Validate against the descriptor captured for the agent request, not an extension-supplied context limit. A result that cannot fit fails with its budget details rather than silently truncating the suffix or sending a known-oversized request.

Deterministic examples for tests:

- One text record with `B = 400`, `I = 0` contributes 108 estimated tokens. With `B = 401`, it contributes 109.
- One image-only record contributes 2056 estimated tokens regardless of encoded image byte length. Two image blocks in one record contribute 4104.
- A reusable normalized usage total of 10,000 plus a new 400-byte text record produces an estimate of 10,108. The same record with a changed instruction set uses full fallback instead.
- A 15,000-token newer suffix plus an indivisible 7,000-token tool group retains 22,000 estimated tokens for the default 20,000-token target. It never keeps only that group's results.

Tests also cover UTF-8 byte counts, all input categories in the sizing table, absent versus present-zero usage, source error outcomes, and every baseline invalidation condition. Provider overflow recovery covers disagreement with the estimate. These values must not be shown as exact tokenization, billing, or a conservative upper bound.

### Bundled compaction extension

Add `plugins/extension/compaction/cmd/glyph-compaction` and include its executable in the ordinary bundled-extension build/discovery path. The existing tools extension establishes that process boundary. The compaction plugin uses the Extension SDK for registration, handler execution, cancellation, and configured-model access. Host does not inspect its package, binary name, or prompt to execute the capability.

Add a `compaction_generate` handler kind to the ordinary handler registry. After request handlers finish successfully with no result, Host resolves exactly one registered generation handler and invokes it through `Handle`. Zero or multiple generation registrations produce an explicit compaction error, not a load-order choice. The bundled extension supplies the standard registration; a custom extension can replace that generator while the bundled generator is disabled. A complete result from a custom request handler bypasses generation lookup and invocation entirely; it does not depend on the bundled extension's availability.

The generator receives the original request and final current request, including the proposed prefix/suffix, preceding summary, selected model, budgets, and manual instructions. It returns the same typed compaction result used by custom handlers. Host checks the returned boundary and result; it does not dictate the generator's prompt or synthesize another result after failure.

The bundled strategy serializes the prefix's text, tool calls, and tool results as summary source material and supplies image content as image input, not as a textual substitute. It uses the preceding summary as separate summary input, not as another item in a chain of historical summaries. The prompt is an embedded Markdown resource owned by the extension.

The existing public configured-model request accepts only user/assistant text, although Host history and the provider adapters already support image input. Extend that request's user-message content to typed text/image blocks so moving generation out of Host does not lose image content. Assistant history remains text. The configured-request mapper validates content presence and model input modalities; the SDK exposes the typed input. This does not add image acquisition, resizing, or UI presentation. Compaction's public source projection carries the corresponding image bytes while still excluding opaque provider reasoning data.

The extension, not Host, packs summary input within the selected summary model's input budget. An oversized prefix is processed in consecutive complete-message/tool groups, carrying a working summary between model requests. An indivisible group that cannot fit fails explicitly rather than silently truncating tool output. The extension aggregates usage only when every contributing request reports it and returns the actual model selection with that usage. Host persists only the final compaction result.

A generation invocation may await a configured-model request on the same bidirectional connection. Keep the existing independent operation owners and hold no session mutex during this wait. Cancellation of generation cancels its outstanding configured request. Transient provider failures can be retried inside that nested model execution; an error returned by the generator ends compaction without invoking the generator again or selecting a fallback.

### Automatic, manual, and overflow flows

Before each agent model request, `modelexecution.Stream` invokes its context-preparation dependency. The compaction owner checks the projected context against the input budget. Threshold compaction runs inside the already admitted agent operation, after preceding tools have completed. It does not reacquire the operation gate held by that run. `modelexecution.Request` omits this session-context step.

Manual compaction acquires the existing operation gate through the client use case. A competing run or session mutation is rejected as busy. Model execution and extension calls run outside the sessions mutex. Commit protection covers validation and persistence, not network waits.

Both entry points use this sequence:

1. Snapshot the session, active branch, model descriptor, current summary, budget estimate, and proposed prefix/suffix boundary.
2. Create immutable original request state and an equal current state.
3. Run request handlers in registration order. Each successful action can independently replace the request and preserve, set, replace, or clear the result.
4. Invoke the registered generation extension only after a successful request chain that leaves no result.
5. Run result handlers with original request, final current request, immutable original result, and current result.
6. Validate the final boundary, summary, usage/source data, and resulting context budget. Commit once and publish the result.

Snapshot handler membership and registration order when the operation starts. Loss of a runtime required by that snapshot fails invocation rather than silently skipping the handler. Cancellation or a handler error stops the chain. A handler or generator failure never invokes another compaction implementation as recovery. Complete causes reach the initiating client. Extension success/failure observers follow the shared [extension failure rule](../../prd.md#extension-failure-rule); post-commit errors retain the committed result.

Context overflow is a provider-neutral failure distinct from a transient transport failure. Model execution never retries an unchanged overflowing request. An accepted retry after overflow first invokes compaction with reason `overflow`; only successful compaction permits a new provider attempt with rebuilt context. This repeat consumes the logical execution's existing attempt allowance rather than resetting it. Permit at most one overflow-recovery compaction within that allowance. Disabled retry, a second overflow, or failed compaction ends the request with complete causes. Configured-model requests do not trigger this conversation-mutation path.

### Provider failures and retry coordination

Extend the raw provider-attempt result with typed classification and an optional provider-requested delay. The closed classification is transient provider failure, non-retryable provider failure, or context overflow. Preserve the original error and its complete text. A missing terminal response is a failed attempt, not success.

Adapters classify HTTP responses and transport errors at their source. They do not choose the retry count or schedule delays. A client-delivery error, extension failure, invalid handler action, settings or credential-resolution error, or user cancellation must not be converted into a transient provider error by inspecting its message or a nested network cause.

For each logical execution, snapshot the retry configuration. Disabled retry returns the initial attempt's terminal outcome without invoking retry handlers. For each failed provider attempt with retry enabled:

1. Retain that attempt's terminal result and source error without forwarding it as the logical terminal result.
2. Build original and current retry decisions from source classification, completed attempt count, configured delays, and provider delay.
3. Invoke retry handlers in registration order. Each receives original and current decisions. Cancellation, failure, or invalid action terminates processing without another provider call.
4. Validate the final decision. A provider's requested delay is a lower bound; an extension cannot cause an earlier request. An excessive `Retry-After` ends execution under the agreed maximum-wait rule.
5. Publish the failure and pending delay, reset partial output, wait with cancellation, and repeat the captured provider request. An agent context-overflow retry first requires the successful compaction described above; it is not an unchanged-context retry.

Each retry uses detached copies of the same instructions, history, model selection, and tool catalogue. It does not re-run context compaction, call completed tools, or re-read the active selection. Overflow recovery is the separate context-changing path described above.

Treat `maxRetries` as repeats after the initial request. Validate nonnegative counts and delays. For a configured count longer than its delay sequence, reuse the final configured delay; a positive count requires at least one delay. A shorter count uses only the corresponding prefix. This preserves independent count and delay configuration without adding a second delay-policy language.

The default and configurable values remain owned by [FRQ-07 and FRQ-08](ticket.md#functional-requirements). A provider delay within the configured maximum combines with the policy delay by taking the later time. A provider delay above the maximum terminates execution without shortening it. Retry-handler changes remain subject to these source-delay constraints.

Use `github.com/cenkalti/backoff/v7` at `v7.0.0`, following the project's retry-library convention. The version is listed by `go list -m -versions`; the module is not currently declared in `go.mod`. The user approved adding this exact dependency version during a separately authorized implementation. No dependency files are changed by this design task.

### Stream reset and terminal outcome

Add one provider-neutral logical stream event, `ResponseReset`. It discards only the unfinished model response and tool-call previews. It contains no retry policy. Core clears both partial content and any retained terminal response from that attempt, then emits the equivalent semantic client event. UI Plugin Contract and Programmatic Control carry that event; TUI clears `ActiveModel` and provisional tool-call display state without changing earlier transcript entries.

Forward incremental content during an attempt. Model execution alone holds each terminal provider event until its retry decision and any required overflow compaction are final. An intermediate failed attempt never produces Core's terminal message event or a stored model response. Before the replacement attempt, deliver the reset and retry/compaction progress in order. A failure delivering reset or progress stops execution rather than continuing with an inconsistent client view.

The final successful attempt produces one terminal model response. Exhaustion or another terminal failure preserves the last attempt's retained content under Core's existing failed-response rules and exposes all contributing error text. A user abort during the pending delay has no replacement content to persist. No implementation buffers the whole successful answer before showing it.

This change is required by the existing behavior: `StreamEvent.applyContentStart` rejects reuse of an occupied content position, and TUI `projection.applyModelDelta` appends text at that position. Repeating the raw provider call without reset would not produce a new isolated answer.

### Public operations, events, and extension contracts

Extend the existing bidirectional operation streams rather than adding synchronous RPCs:

| Boundary | Additions |
|---|---|
| UI Plugin Contract and Programmatic Control | Manual `Compact` with instructions; runtime `SetRetryEnabled`; retry-policy projection in initialization/state; compaction progress/result; retry progress; semantic response reset. |
| Extension Contract and SDK | Compaction request/result handlers, generation capability, compaction outcome observers, retry handlers, session-bound manual compaction, and configured-model text/image input with retry progress. |
| Standard TUI | `/compact [instructions]`, `/retry on\|off`, active compaction/retry status, and the approved partial-response reset. |
| Headless output | Ordered compaction/retry diagnostics and complete terminal errors without adding diagnostic text to model context. |

Settings own persistent retry counts, delay sequence, maximum accepted provider delay, and the retained-context budget. Runtime retry enablement is session-process state; it does not rewrite the settings file. A policy snapshot already in execution is unchanged; abort remains the way to stop it immediately.

Compaction request payloads carry the snapshot and state listed in ticket FRQ-01.2, with an explicit estimate marker for token accounting. Result payloads carry summary, first preserved entry ID, source, optional usage, and optional details. Preserve/replace/clear/cancel are explicit action variants. Retry payloads carry the source error, immutable original decision, current decision, completed attempts, next delay, and effective attempt limit.

Host validates action shape before applying any returned state. Failure is terminal, not an operation issue followed by later handlers. Transport cancellation uses the shared operation protocol. An explicit handler cancellation is a completed handler action: compaction reports cancellation without a commit, while retry returns `RETRY_CANCELED` without another provider attempt. Neither action is a substitute for user-abort propagation. Use Protobuf edition 2023 and `int64` for counts, token values, positions, and delay values. Use `mo.Option` for absent Go values and field presence in public contracts.

Configured-model retry progress belongs to that configured-model operation. The bundled generator forwards it as generation progress; Host forwards that progress to the owning compaction operation. It does not reset the active agent answer for a separate summary request. Extend the existing extension registration inventory and runtime invocation projection. Add focused `compaction.proto` and `retry.proto` sources under the owning API directories instead of increasing the existing catch-all files. Keep provider reasoning bytes and credentials outside non-provider extension and client projections.

### Terminal failure mapping

Use provider-neutral typed failure identity in domain values consumed by Core and client use cases. Raw HTTP status and SDK error types never select a public Glyph category directly. Both client use cases implement the same mapping; source text supplements every category.

| Terminal source | Public outcome/category |
|---|---|
| Pure user abort | Operation canceled; no failure code fabricated. |
| Non-retryable provider failure | `MODEL_FAILED`, also used by the configured-model boundary |
| Retryable provider failure with no attempts left | `RETRY_EXHAUSTED` |
| Retry handler cancels the retry | `RETRY_CANCELED` |
| Handler error, invalid handler action, or unavailable extension on the execution path | `EXTENSION_FAILED` |
| Provider-requested delay exceeds the configured maximum | `RETRY_DELAY_EXCEEDED` |
| Context overflow after recovery or no context boundary that fits | `CONTEXT_LIMIT` |
| Compaction preparation or final context validation failure without a more specific extension or persistence identity | `COMPACTION_FAILED` |
| Source-classified session persistence failure | Existing `PERSISTENCE_UNAVAILABLE` |
| Delivery failure or an otherwise unclassified execution failure | Existing `INTERNAL` with complete causes. |

A typed logical failure takes precedence over cancellation sentinels found inside its cause. A provider-attempt timeout with an active caller context remains a provider failure; exhaustion remains `RETRY_EXHAUSTED` even when its cause wraps `context.DeadlineExceeded`. Core consumes the logical failed/aborted outcome without knowing retry policy. UI, Programmatic Control, and extension-model operations must not convert this failure into cancellation merely because `errors.Is` or a leaf-only check finds a cancellation sentinel. Preserve the original cause rather than removing it to change classification.

The existing Core `finalizeProviderError`, UI `isPureCancellation`, and Programmatic `filterRunError` need this distinction when logical failure types are introduced. Their present cancellation-cause checks are not sufficient for the new retry outcomes.

An outer error retains nested provider, extension, validation, persistence, and delivery causes. Source-classified persistence failures keep their existing public precedence. Otherwise the failure that prevents continuation determines the category; preceding attempt errors remain text/diagnostic causes, not replacement categories. Cancellation joined with an independent active failure remains a failure under the existing pure-cancellation rule. Previously handled transient attempts remain diagnostics during a user abort; they do not turn cancellation of a pending retry delay into a failed operation.

Ordinary returned tool-error results remain model-visible tool results. Correcting implemented non-PHS-06 extension chains to the revised global rule is tracked in the [roadmap](../../../../../roadmap.md#extension-failures-stop-the-invoking-operation); this solution does not design or implement those unrelated corrections.

### Implementation order and verification

Each behavior change starts with a compiling, uncached failing behavioral test, then the minimum implementation and an uncached passing run. Extend existing tests before creating another fixture. Unit tests use generated mocks for production-consumed interfaces; filesystem, process, provider transport, and assembled-client tests use the integration build tag.

| Unit | Change and behavioral evidence |
|---|---|
| 0. Extension model-access ownership | Extract the real model operations and update controller wiring and implementation assertions. Reuse the existing catalogue revalidation, exact configured-input, stale-completion, and failure-classification tests. This is behavior-preserving refactoring; it does not need an artificial failing test for a missing feature. Validate the complete import graph with the compaction and retry-context interfaces included. |
| 1. Compacted context | Implement the deterministic sizing records, reported-usage baseline, and invalidation conditions with the numeric cases defined above. Add the entry and separate context projection. Inputs include two compactions, original entries, a preserved tool batch, navigation before/after compaction, fork/clone, and restart. Expect the latest summary plus exact retained suffix, unchanged original records, and accounting counted once. Extend session history and persistence suites. |
| 2. Raw failures | Add source classification and provider delay extraction. Inputs include the fixed retryable statuses, authorization failure, timeout, interrupted stream, context overflow, and consumer callback error. Expect typed provider failures, complete causes, and exactly one adapter attempt. Extend both provider families' failure tests. |
| 3. Retry execution and reset | Extend `modelexecution/service_test.go`, Core stream tests, and TUI projection tests. Inputs include failure after partial text, later success, exhaustion, a provider timeout with a still-active caller context, handler cancellation/failure, changed retry counts/delays, excessive `Retry-After`, and user abort during waiting. Expect ordered reset/progress, no concatenation, one persisted terminal outcome, no repeated tools, and no retry after delivery/extension failure. Use the production timing path inside `testing/synctest` instead of wall-clock sleeps or a production interface added only for tests. |
| 4. Compaction orchestration | Test manual, threshold, and overflow entry points, request/result composition, result clearing, extension-provided summaries, missing or ambiguous generation registration, invalid boundaries, source accounting, cancellation, handler failure, and persistence failure. Expect one commit only after complete validation, generation bypass for a ready result, no fallback after extension failure, and one overflow recovery that consumes the existing retry allowance. |
| 5. Bundled generation and public assembly | Run the real bundled compaction process and a custom replacement through headless, UI, and Programmatic modes. Exercise nested configured-model calls with text and image source content, their retry progress and cancellation, repeated-summary inputs, and complete replacement with the bundled generator unavailable. Verify complete errors, committed-result publication, and response reset through both clients. Test request composition and results, never prompt text. |

The main existing test references are `modelexecution/service_test.go`, Core `provider_failure_test.go` and `history_test.go`, session-tree `handler_composition_test.go`, sessions `summary_history_test.go` and `history_projection_integration_test.go`, provider failure suites, and TUI `state.go` projection tests.

Before implementation closure, run `task generate` twice for contract/mock changes with no second-run diff, then `task fmt`, `task fix_dry_run`, analyze and apply justified fixes, `task lint`, `task test`, `task itest`, and `task test-coverage`. Record skipped platform-dependent tests as missing evidence, not passes. The independent dry run ran focused uncached baseline tests for model execution, extension context, sessions, session tree, Agent Core, TUI presentation, and both OpenAI adapter families; all eight packages passed. These baseline tests do not verify unimplemented PHS-06 behavior. Generation and the full implementation checks listed above remain pending.

## Overengineering and Overspecification Considerations

- The compaction coordinator applies extension results; the ordinary bundled extension owns the default algorithm. `extensionmodels` owns actual model-access operations extracted from `extensioncontext`, not a forwarding layer. Core still calls the existing model-execution owner directly.
- No retry daemon, additional transport, persistent retry queue, provider-specific Host policy, or generic summary framework is introduced.
- Original session storage is retained. Compaction changes the model projection rather than rewriting or copying the complete session history.
- Configuration uses a count and a delay sequence. HTTP classification remains provider-owned and is not configurable by the user.
- Budget estimates are not exact tokenizers. Overflow recovery addresses provider disagreement without adding per-model tokenizer dependencies.
- Pi documentation is a behavior reference for retained context and provider-delay rejection, not a source of algorithms or architecture.

## Open Questions

None.

## References

- [PHS-06 ticket](ticket.md) - owning requirements and acceptance criteria.
- [Target architecture](../../architecture.md) - Core boundaries, Host capability ownership, and consumer-owned interfaces.
- [Product requirements](../../prd.md) - error semantics and extension failure rule.
- [Agent run failure semantics](../../../../issues/agent-run-failure-semantics/problem.md) - cross-phase terminal-category ownership.
- [Model execution](../../../../../../host/internal/usecase/host/modelexecution/service.go) - current agent/configured-model execution paths.
- [Core stream contracts](../../../../../../host/internal/usecase/agent/run/interfaces.go) - logical stream state and provider-neutral interfaces.
- [Configured-model public input](../../../../../../api/plugins/extension/v1/model.proto) and [mapping](../../../../../../host/internal/controller/extension/configured_model.go) - text-only boundary that needs typed image input for extension-owned compaction.
- [Session history projection](../../../../../../host/internal/usecase/host/sessions/entry_history.go) - current client/model history projection source.
- [Session navigation commit](../../../../../../host/internal/usecase/host/sessions/navigation.go) - atomic persistence and ordered publication precedent.
- [TUI projection](../../../../../../plugins/ui/tui/internal/usecase/presentation/state.go) - partial-response accumulation and terminal display.
