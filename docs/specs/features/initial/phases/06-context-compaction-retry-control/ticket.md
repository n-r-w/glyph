# Ticket: PHS-06 - Context compaction and retry control

Keep long sessions usable within model context limits.

## Key definitions and abbreviations

- DEF-01: Context compaction. Replacement of an older context prefix in model-visible context with a summary while retaining the original session entries and preserving the remaining suffix.
- DEF-02: Original compaction request. The immutable Host-produced request before any compaction handler runs.
- DEF-03: Current compaction request. The request state produced by the preceding compaction request handlers.
- DEF-04: Current compaction result. The result state produced by an extension or the built-in compaction strategy and then by preceding result handlers.
- DEF-05: Original compaction result. The immutable result present when the result-handler chain starts.
- DEF-06: Original retry decision. The immutable Host-produced decision for one failed model request before retry handlers run.
- DEF-07: Current retry decision. The retry state produced by preceding retry handlers.

## Problem Statement

- PRB-01: Long sessions have no context-budget compaction. They eventually exceed the selected model context and cannot continue through the required automatic or manual flow.
- PRB-02: Extensions cannot inspect the original compaction request, compose changes through a current request and result, replace the built-in compaction result, or observe a terminal compaction outcome.
- PRB-03: Retry policy is closed inside Agent Core, so extensions cannot change one failure classification, retry decision, delay, or attempt limit.

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
- FRQ-01.2: Before manual, threshold, or overflow-recovery compaction, Host shall create an immutable original request and a current request initialized with the same value. Each request shall contain the trigger reason, retry intent, user instructions, selected model descriptor, provider-neutral active-branch projection, proposed summarized prefix and preserved suffix, preceding summary, context token count, model context window, response budget, and cancellation capability.
- FRQ-01.3: Compaction request handlers shall run in registration order. Each handler shall receive the original request, the current request, and the current result when one exists.
- FRQ-01.4: A request handler shall return a state update that independently preserves or replaces the current request and preserves, sets, replaces, or clears the current result. Cancellation shall be an exclusive terminal action.
- FRQ-01.5: The next request handler shall receive the unchanged original request and the current state returned by the preceding handler. A handler can derive its returned state from the original request, the current state, or both.
- FRQ-01.6: When the request-handler chain finishes without a current result, the built-in compaction strategy shall process the final current request. When a current result exists, the built-in strategy shall not run.
- FRQ-01.7: A compaction result shall contain a nonempty summary, the first preserved active-branch entry identifier, a usage state that is absent or contains normalized model usage, and a details state that is absent or contains opaque extension data.
- FRQ-01.7.1: After a result exists, result handlers shall run in registration order. Each result handler shall receive the original compaction request, the final current request, the immutable original result, and the current result returned by preceding result handlers, and shall preserve or replace the current result or cancel compaction. Cancellation shall be an exclusive terminal action.
- FRQ-01.8: Host shall validate every handler action before exposing its returned state to the next handler. An invalid action or ordinary handler error shall be reported, shall preserve the state received by that handler, and shall not stop later handlers or deactivate the extension.
- FRQ-01.9: Host shall validate the final result against the active branch and compaction request before atomically appending one summary entry. Validation shall reject an empty summary, a kept-entry identifier outside the active branch, and a boundary that separates a tool result from its tool call.
- FRQ-01.10: Host shall emit compaction success after persistence and compaction failure after cancellation, built-in strategy failure, final-result validation failure, or persistence failure.
- FRQ-02: Add manual compaction with user instructions and the same request and result handler chains used by automatic compaction.
- FRQ-03: Add configurable retry control, extension retry handlers, and retry accounting through Programmatic Control and the standard TUI.
- FRQ-04: Provider adapters shall classify provider responses and errors as retryable or non-retryable. Host shall own retry-policy configuration, retry-decision coordination, extension dispatch, delay scheduling, final-decision validation, and retry events outside Agent Core.
- FRQ-04.1: After one model request fails, Host shall create an immutable original retry decision from the provider classification, configured built-in policy, completed attempt count, and provider-supplied delay and shall initialize the current retry decision with the same value.
- FRQ-04.2: Retry handlers shall run in registration order. Each handler shall receive the original retry decision and current retry decision returned by preceding handlers and shall preserve, replace, or cancel that retry. Cancellation shall be terminal and shall stop later retry handlers.
- FRQ-04.3: A retry handler shall be able to change retryability, whether another attempt runs, the next delay, and the effective attempt limit for that model request.
- FRQ-04.4: An invalid handler action or ordinary handler error shall be reported, shall preserve the decision received by that handler, and shall not stop later handlers or deactivate the extension.
- FRQ-04.5: Host shall validate the final decision before scheduling a delay or repeating the request. When handlers preserve the initial decision, Host shall apply the configured built-in policy.
- FRQ-04.6: Agent Core shall consume one logical model execution result and shall depend on no retry policy, extension handler, plugin transport, or delay scheduler.
- FRQ-04.7: Agent Core shall declare its minimal logical model-execution interface in `host/internal/usecase/agent/run`. A Host model-execution adapter shall implement retry coordination and provider dispatch through that interface.
- FRQ-04.8: After retry coordination ends, Host shall return one provider-neutral logical model-execution result to Agent Core. A failed result shall contain a closed Glyph category and complete error text that preserves the terminal provider, retry-handler, validation, delay, and delivery causes that contributed to that result.
- FRQ-04.9: Retryability, retry decision, and terminal Glyph category shall remain separate values. A retry decision shall not replace or remove the source error.
- FRQ-05: General abort shall cancel an in-progress provider request or pending retry delay and transition the agent to idle.
- FRQ-06: A retry shall repeat only the failed model request and shall not repeat any completed tool execution. Failed intermediate attempts shall produce operation events and shall not create session messages or enter model context. After retry finishes, Glyph shall persist only the terminal model outcome.
- FRQ-07: Enable retry by default with three retries after the initial request and delays of 1, 2, and 4 seconds. The built-in policy shall cap a provider-supplied `Retry-After` delay at 30 seconds. The built-in retryable HTTP statuses shall be 408, 429, 500, 502, 503, and 504. Transport timeouts, connection resets, and unexpected connection closure before a terminal provider response shall also be retryable.
- FRQ-08: Make the enabled state, maximum retry count, ordered retry delays, `Retry-After` cap, and built-in retryable HTTP statuses configurable through Host.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Host compaction use case and persisted summary entries.
- DLV-01.1: Public compaction request, result, success, and failure contracts with a reference extension that composes with another compaction extension.
- DLV-02: Manual compaction, retry-policy configuration, retry extension contracts, retry client operations, and the Host logical model-execution adapter.

### Acceptance Criteria

- ACC-01: Automatic compaction replaces an older context prefix in model-visible context, retains the original session entries, preserves the remaining suffix, and allows the run to continue.
- ACC-01.1: Two request handlers receive the same original request, while the second receives the current request and current result returned by the first.
- ACC-01.2: A handler can discard preceding request changes by deriving its replacement from the original request, and the next handler observes that replacement as the current request.
- ACC-01.3: An extension-provided current result prevents the built-in strategy from running, and two result handlers transform that result in registration order while each can inspect the immutable original result.
- ACC-01.4: Clearing the current result before the request-handler chain ends makes the built-in strategy process the final current request.
- ACC-01.5: An invalid handler action reports the error, preserves the preceding current state for the next handler, and writes no invalid session entry.
- ACC-01.6: Cancellation by a request or result handler writes no compaction entry and emits one compaction failure event, while successful persistence emits one compaction success event.
- ACC-02: Manual compaction applies user instructions through the same handler chains and persists the final result.
- ACC-03: A compacted session resumes after restart with the same active branch.
- ACC-04: A retryable provider failure produces client retry events and follows the configured policy. The default policy makes three retries after the initial request with delays of 1, 2, and 4 seconds.
- ACC-04.1: Two retry handlers receive the same original retry decision, while the second receives the current decision returned by the first.
- ACC-04.2: A retry handler changes one failure from no-retry to retry with a different delay and attempt limit, and Host applies the validated final decision.
- ACC-04.3: Retry cancellation ends that logical model execution without scheduling another attempt. An invalid retry action preserves the preceding decision for later handlers.
- ACC-04.4: `host/internal/usecase/agent/run` invokes a consumer-owned logical model-execution interface and imports no retry policy, extension handler, plugin transport, or delay scheduler.
- ACC-04.5: Retry exhaustion, retry cancellation, retry-handler failure, and a non-retryable provider failure each produce their defined terminal category and complete error text through every applicable Glyph client interface.
- ACC-05: Retrying a failed model request repeats no completed tool, adds no intermediate attempt to session messages or model context, and persists only the terminal model outcome.
- ACC-06: Abort cancels an in-progress provider request or pending retry delay and leaves the agent idle.

## Overengineering and Overspecification Considerations

The ticket uses the same original-and-current composition rule for compaction and retry. Host coordinates both operations, while Agent Core keeps only the model and tool loop needed to consume their results. No separate override API, retry daemon, or provider-specific policy language is added.

## Constraints and Risks

- RSK-01: Provider token accounting may be absent or approximate. Define one Host compaction-budget calculation based on the selected model descriptor and available usage data.
- RSK-02: An extension can return a boundary that corrupts model-visible tool history. Host validates the final boundary against the active branch before persistence.
- RSK-03: An extension can select a long delay or high attempt limit. Retry events expose the final decision and general abort cancels the request or pending delay.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [Pi extension-surface research](../../../../../artefacts/pi-extension-surface.md) - feature evidence for cancellable and replaceable compaction.
- REF-04: [target architecture](../../architecture.md) - Host retry ownership and Agent Core boundary.
