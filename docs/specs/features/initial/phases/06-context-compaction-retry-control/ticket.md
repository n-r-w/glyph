# Ticket: PHS-06 - Context compaction and retry control

Keep long sessions usable within model context limits.

## Key definitions and abbreviations

- DEF-01: Context compaction. Replacement of an older context prefix in model-visible context with a summary while retaining the original session entries and preserving the remaining suffix.
- DEF-02: Original compaction request. The immutable Host-produced request before any compaction handler runs.
- DEF-03: Current compaction request. The request state produced by the preceding compaction request handlers.
- DEF-04: Current compaction result. The result state produced by a compaction extension and then by preceding result handlers.
- DEF-05: Original compaction result. The immutable result present when the result-handler chain starts.
- DEF-06: Original retry decision. The immutable Host-produced decision for one failed model request before retry handlers run.
- DEF-07: Current retry decision. The retry state produced by preceding retry handlers.

## Problem Statement

- PRB-01: Long sessions have no context-budget compaction. They eventually exceed the selected model context and cannot continue through the required automatic or manual flow.
- PRB-02: Extensions cannot inspect the original compaction request, compose changes through a current request and result, supply or replace a compaction result, or observe a terminal compaction outcome.
- PRB-03: Host model execution performs one provider attempt without retry coordination, so temporary provider failures end the request and extensions cannot change retry decisions.

## Target Picture

- SOL-01: Keep long sessions usable within model context limits while extensions retain explicit control over composed compaction and retry decisions.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 and DEP-02 are met and two compaction extensions are active.
- Trigger: the active context reaches the compaction threshold.
- Required behavior: each handler receives the immutable original request and the current state from preceding handlers, the final result is validated and persisted, and the unchanged context suffix is sent with the next provider request.
- Example input and expected output: Input: let extension A change the current request, extension B derive a result from the original request, and a result handler refine B's result. Expected output: B sees both request versions, the result handler sees B's result as its current result, and the refined summary replaces the older prefix in model-visible context.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No prompt, user-input, provider-request, tool, or TUI-specific middleware unrelated to compaction.

## Dependencies and Preconditions

- DEP-01: [PHS-07](../07-extension-context-lifecycle/ticket.md) must meet all acceptance criteria.
- DEP-02: [PHS-04.1](../04.1-model-execution-capabilities/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Keep long sessions usable within model context limits.

### Functional Requirements

- FRQ-01: Use the selected provider-neutral model descriptor's `contextWindow` and `maxTokens` to add response-budget and context-window accounting, automatic compaction accounting, and automatic compaction.
- FRQ-01.1: Host shall own compaction coordination, extension dispatch, final-result validation, and persistence outside Agent Core.
- FRQ-01.1.1: Glyph shall distribute a compaction extension enabled by default for the standard coding agent. This extension shall own the standard summary-generation strategy, its instructions, and model requests for generating or updating the summary. Host shall contain no context-summary generation implementation. User extensions shall be able to supply a complete replacement result through the public compaction contract.
  - Origin: `formulated`, approved bundled-compaction ownership decision.
  - Goal: Make standard and custom compaction extension-owned rather than separate Host and extension implementations.
  - Goal achievement: Full. Host coordinates and persists results without implementing the summary-generation strategy.
- FRQ-01.2: Before manual, threshold, or overflow-recovery compaction, Host shall create an immutable original request and a current request initialized with the same value. Each request shall contain the trigger reason, retry intent, user instructions, selected model descriptor, provider-neutral active-branch projection, proposed summarized prefix and preserved suffix, preceding summary, context token count, model context window, response budget, and cancellation capability.
- FRQ-01.3: Compaction request handlers shall run in registration order. Each handler shall receive the original request, the current request, and the current result when one exists.
- FRQ-01.4: A request handler shall return a state update that independently preserves or replaces the current request and preserves, sets, replaces, or clears the current result. Cancellation shall be an exclusive terminal action.
- FRQ-01.5: The next request handler shall receive the unchanged original request and the current state returned by the preceding handler. A handler can derive its returned state from the original request, the current state, or both.
- FRQ-01.6: When the request-handler chain finishes successfully without a current result, Host shall invoke the single registered compaction generator with the final current request. The bundled extension shall supply that generator by default; a custom extension shall be able to replace it. Zero or multiple registered generators shall fail compaction explicitly without a Host fallback or load-order selection. A supplied current result shall bypass generator lookup and invocation entirely.
- FRQ-01.7: A compaction result shall contain a nonempty summary, the first preserved active-branch entry identifier, a usage state that is absent or contains normalized model usage, and a details state that is absent or contains opaque extension data.
- FRQ-01.7.1: After a result exists, result handlers shall run in registration order. Each result handler shall receive the original compaction request, the final current request, the immutable original result, and the current result returned by preceding result handlers, and shall preserve or replace the current result or cancel compaction. Cancellation shall be an exclusive terminal action.
- FRQ-01.8: Host shall validate every handler action before exposing its returned state to the next handler. An invalid action or handler error shall follow the [extension failure rule](../../prd.md#extension-failure-rule): stop compaction, report the complete error text, invoke no later handler, and append no compaction entry. Host shall not retry the extension or invoke another compaction implementation as a fallback after its failure.
- FRQ-01.9: Host shall validate the final result against the active branch and compaction request before atomically appending one summary entry. Validation shall reject an empty summary, a kept-entry identifier outside the active branch, and a boundary that separates a tool result from its tool call.
- FRQ-01.10: Host shall emit compaction success after persistence and compaction failure after cancellation, handler failure, invalid handler action, unavailable compaction implementation, final-result validation failure, or persistence failure.
- FRQ-01.11: Host shall retain a recent unsummarized suffix using a configurable token budget with a default of 20,000 tokens. This budget shall be separate from the response budget. The boundary shall retain complete messages and shall not separate a tool result from its tool call.
- FRQ-01.12: Repeated compaction shall update the preceding summary with the newly summarized part of the previously retained suffix. It shall not summarize the complete original history again. The next model request shall contain the updated summary followed by the preserved suffix, not a chain of preceding compaction summaries.
- FRQ-02: Add manual compaction with user instructions and the same request and result handler chains used by automatic compaction.
- FRQ-03: Add configurable retry control, extension retry handlers, and retry accounting through Programmatic Control and the standard TUI.
- FRQ-04: Provider adapters shall classify provider responses and errors as retryable or non-retryable. Host shall own retry-policy configuration, retry-decision coordination, extension dispatch, delay scheduling, final-decision validation, and retry events outside Agent Core.
- FRQ-04.1: After one model request fails, Host shall create an immutable original retry decision from the provider classification, configured built-in policy, completed attempt count, and provider-supplied delay and shall initialize the current retry decision with the same value.
- FRQ-04.2: Retry handlers shall run in registration order. Each handler shall receive the original retry decision and current retry decision returned by preceding handlers and shall preserve, replace, or cancel that retry. Cancellation shall be terminal and shall stop later retry handlers.
- FRQ-04.3: A retry handler shall be able to change retryability, whether another attempt runs, the next delay, and the effective attempt limit for that model request.
- FRQ-04.4: An invalid retry action or retry-handler error shall follow the [extension failure rule](../../prd.md#extension-failure-rule): fail the logical model execution, report the complete error text, and invoke no later handler. Host shall not retry the extension, restart the chain, or schedule another provider attempt for that execution.
- FRQ-04.5: Host shall validate the final decision before scheduling a delay or repeating the request. When handlers preserve the initial decision, Host shall apply the configured built-in policy.
- FRQ-04.6: Agent Core shall consume one logical model execution result and shall depend on no retry policy, extension handler, plugin transport, or delay scheduler.
- FRQ-04.7: Agent Core shall continue to use its minimal logical model-execution interface in `host/internal/usecase/agent/run`. PHS-06 shall add retry coordination inside the Host model-execution owner established by [APC-11.1](../../architecture.md#contracts) and shall preserve provider dispatch through that interface.
- FRQ-04.7.1: Host model execution shall continue to use its smallest provider-execution interface at the consumption site. The interface shall carry provider-neutral requests, streamed semantic output, typed usage, safe diagnostics, retry classification, and opaque provider reasoning context.
- FRQ-04.7.2: The implemented in-process OpenAI provider adapters shall continue to implement the provider-execution interface until PHS-12 replaces them with extension-runtime adapters. The Host model-execution package, including its retry coordination, shall import no OpenAI Codex, OpenAI-compatible, provider SDK, or provider credential package.
- FRQ-04.8: After retry coordination ends, Host shall return one provider-neutral logical model-execution result to Agent Core. A failed result shall contain a closed Glyph category and complete error text that preserves the terminal provider, retry-handler, validation, delay, and delivery causes that contributed to that result.
- FRQ-04.9: Retryability, retry decision, and terminal Glyph category shall remain separate values. A retry decision shall not replace or remove the source error.
- FRQ-05: General abort shall cancel an in-progress provider request or pending retry delay and transition the agent to idle.
- FRQ-06: A retry shall repeat only the failed model request and shall not repeat any completed tool execution. Failed intermediate attempts shall produce operation events and shall not create session messages or enter model context. After retry finishes, Glyph shall persist only the terminal model outcome.
- FRQ-07: Enable retry by default with three retries after the initial request and delays of 1, 2, and 4 seconds. The built-in policy shall honor a provider-supplied `Retry-After` delay up to a configurable maximum of 30 seconds by default. When the requested delay exceeds that maximum, the logical model execution shall end with an error that states the requested delay and configured maximum. Host shall not shorten the requested delay to the maximum and retry early. The built-in retryable HTTP statuses shall be 408, 429, 500, 502, 503, and 504. Transport timeouts, connection resets, and unexpected connection closure before a terminal provider response shall also be retryable.
- FRQ-08: Make the enabled state, maximum retry count, ordered retry delays, and the maximum accepted `Retry-After` delay configurable through Host. The maximum retry count shall exclude the initial request. Each configured delay shall apply before its corresponding repeat attempt. FRQ-07 defines the default retry count and delays, not fixed limits. Provider adapters shall own HTTP failure classification; the retryable HTTP status set shall not be user-configurable.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, concrete provider packages, and TUI packages. Host model execution and retry coordination must remain independent of concrete provider packages.

### Deliverables

- DLV-01: Host compaction use case and persisted summary entries.
- DLV-01.1: Public compaction request, result, success, and failure contracts, the bundled compaction extension, and a reference custom extension demonstrating composition and complete replacement.
- DLV-02: Manual compaction, retry-policy configuration, retry extension contracts, retry client operations, and retry behavior inside the established Host logical model-execution owner.

### Acceptance Criteria

- ACC-01: Automatic compaction replaces an older context prefix in model-visible context, retains the original session entries, preserves the remaining suffix, and allows the run to continue.
- ACC-01.1: Two request handlers receive the same original request, while the second receives the current request and current result returned by the first.
- ACC-01.2: A handler can discard preceding request changes by deriving its replacement from the original request, and the next handler observes that replacement as the current request.
- ACC-01.3: An extension-provided current result bypasses generator lookup and invocation, including when no generator or multiple generators are registered. Two result handlers transform the result in registration order while each can inspect the immutable original result.
- ACC-01.4: Clearing the current result before the request-handler chain finishes successfully makes the single registered generator process the final current request. The bundled generator works by default; a custom generator works with the bundled generator disabled. Zero or multiple generators fail explicitly without a Host fallback or load-order selection.
- ACC-01.5: A compaction handler error or invalid action reports the complete error text, stops compaction before persistence, invokes no later handler or alternative implementation, and performs no retry.
- ACC-01.6: Cancellation by a request or result handler writes no compaction entry and emits one compaction failure event, while successful persistence emits one compaction success event.
- ACC-01.7: Compaction retains the recent suffix under the default 20,000-token budget and under a changed configured budget without splitting a tool call from its result.
- ACC-01.8: Two successive compactions use the preceding summary and the newly summarized messages. The second model-visible projection contains the updated summary and preserved suffix without duplicate earlier summaries or reintroduced original prefixes.
- ACC-01.9: The standard coding agent invokes the bundled compaction extension through the public Extension Contract to generate its summary. A custom extension can replace the complete result without using a Host summary-generation implementation.
- ACC-02: Manual compaction applies user instructions through the same handler chains and persists the final result.
- ACC-03: A compacted session resumes after restart with the same active branch and the same updated summary and unsummarized suffix in model context.
- ACC-04: A retryable provider failure produces client retry events and follows the configured policy. The default policy makes three retries after the initial request with delays of 1, 2, and 4 seconds.
- ACC-04.1: Two retry handlers receive the same original retry decision, while the second receives the current decision returned by the first.
- ACC-04.2: A retry handler changes one failure from no-retry to retry with a different delay and attempt limit, and Host applies the validated final decision.
- ACC-04.3: Retry cancellation ends that logical model execution without scheduling another attempt. A retry-handler error or invalid action fails the execution with complete error text, invokes no later handler, and schedules no further provider attempt.
- ACC-04.4: `host/internal/usecase/agent/run` invokes a consumer-owned logical model-execution interface and imports no retry policy, extension handler, plugin transport, or delay scheduler.
- ACC-04.4.1: Host model execution invokes a consumer-owned provider-execution interface whose in-process implementations have compile-time interface assertions. Host model execution and retry coordination import no concrete OpenAI provider package, and Agent Core and PHS-07 configured-model request callers remain unchanged when the provider implementation is replaced.
- ACC-04.5: Retry exhaustion, retry cancellation, retry-handler failure, and a non-retryable provider failure each produce their defined terminal category and complete error text through every applicable Glyph client interface.
- ACC-04.6: A configured retry count and delay sequence replace the defaults. Disabling retry produces only the initial provider attempt. The configured retry count does not include that initial attempt.
- ACC-04.7: Under the default policy, a provider-requested delay of 10 seconds is honored. A provider-requested delay of 60 seconds exceeds the default 30-second maximum, ends the logical model execution with complete error text identifying both values, and schedules no further provider attempt. Changing the configured maximum changes the acceptance threshold.
- ACC-05: Retrying a failed model request repeats no completed tool, adds no intermediate attempt to session messages or model context, and persists only the terminal model outcome.
- ACC-06: Abort cancels an in-progress provider request or pending retry delay and leaves the agent idle.

## Overengineering and Overspecification Considerations

The ticket uses the same original-and-current composition rule for compaction and retry. Host coordinates both operations, while Agent Core keeps only the model and tool loop needed to consume their results. No separate override API, retry daemon, or provider-specific policy language is added.

## Constraints and Risks

- RSK-01: Provider token accounting may be absent or approximate. Define one Host compaction-budget calculation based on the selected model descriptor and available usage data.
- RSK-02: An extension can return a boundary that corrupts model-visible tool history. Host validates the final boundary against the active branch before persistence.
- RSK-03: An extension can select a long delay or high attempt limit. Retry events expose the final decision and general abort cancels the request or pending delay.
- RSK-04: Coupling retry coordination to an in-process OpenAI adapter would make PHS-12 replace Host orchestration together with provider transport. FRQ-04.7.1 and FRQ-04.7.2 keep provider replacement behind the Host model-execution boundary.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

The [approved technical solution](solution.md) defines package ownership, compaction persistence and projection, retry coordination, stream reset, and verification. Implementation requires a successful dry run and separate implementation authorization.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [Pi extension-surface research](../../../../../artefacts/pi-extension-surface.md) - feature evidence for cancellable and replaceable compaction.
- REF-04: [target architecture](../../architecture.md) - Host retry ownership and Agent Core boundary.
