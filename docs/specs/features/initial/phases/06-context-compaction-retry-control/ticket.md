# Ticket: PHS-06 - Context compaction and retry control

Keep long sessions usable within model context limits.

## Key definitions and abbreviations

- DEF-01: Context compaction. Replacement of an older context prefix in model-visible context with a summary while retaining the original session entries and preserving the remaining suffix.
- DEF-02: Original compaction request. The immutable request established by the compaction extension before any compaction handler runs.
- DEF-03: Current compaction request. The request state produced by the preceding compaction request handlers.
- DEF-04: Current compaction result. The result state produced by a compaction handler or generator and then by preceding result handlers.
- DEF-05: Original compaction result. The immutable result present when the result-handler chain starts.
- DEF-06: Original retry decision. The immutable Host-produced decision for one failed model request before retry handlers run.
- DEF-07: Current retry decision. The retry state produced by preceding retry handlers.
- DEF-08: Compaction policy. The extension-owned decisions for when to compact, how to size context, which active-branch boundary to retain, how to generate the replacement summary, and how to respond to compaction-specific context overflow.
- DEF-09: Prepared history. The provider-neutral model history projected from the active session branch before Agent Core consumes it.
- DEF-10: Compaction entry. The persisted replacement entry that contains the summary and the first preserved active-branch entry identifier.
- DEF-11: Session validity. Structural checks on the session identity, active branch, replacement entry, accounting, and tool-call/result integrity that do not select compaction policy.

## Problem Statement

- PRB-01: Glyph has no accepted extension-owned context-budget compaction. Long sessions can exceed the selected model context without the required target flow.
- PRB-02: The Host-owned compaction checkpoint duplicates policy that belongs to the extension and puts compaction-specific orchestration in Host's Agent Core model-execution path.
- PRB-03: Host model execution performs one provider attempt without retry coordination, so temporary provider failures end the request and extensions cannot change retry decisions.

## Target Picture

- SOL-01: Keep long sessions usable within model context limits while the policy-owning compaction extension coordinates participating compaction capabilities, Host retains runtime and durable session services, and Agent Core consumes prepared history without compaction knowledge.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 and DEP-02 are met and two compaction extensions are active.
- Trigger: the policy-owning extension determines that prepared history has reached its compaction threshold.
- Required behavior: the policy-owning extension coordinates the ordered request and result handlers, selects the retained boundary, obtains or generates the replacement content, and requests persistence. The session owner validates and appends one compaction entry, and the next model request uses the replacement summary followed by the preserved suffix.
- Example input and expected output: Input: extension A changes the current request, extension B derives a complete result from the original request, and a result handler refines B's result. Expected output: B sees both request versions, the result handler sees B's result as its current result, the complete result bypasses the bundled generator, original entries remain stored, and Agent Core receives only the prepared compacted history.

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

- FRQ-01: The compaction extension shall use the selected provider-neutral model descriptor's `contextWindow` and `maxTokens` and available usage observations to own response-budget and context-window accounting, automatic compaction accounting and triggering, and compaction-specific overflow handling.
- FRQ-01.1: The compaction extension shall own compaction policy and request/result composition coordination outside Agent Core. Host shall provide extension transport and invocation, cancellation, session-bound access, model access, durable history mutation, committed-entry publication, and existing accounting without implementing a second compaction policy.
- FRQ-01.1.1: Glyph shall distribute a compaction extension enabled by default for the standard coding agent. This extension shall own the complete standard compaction policy, including summary generation. User extensions shall be able to participate in the composition defined by FRQ-01.2 through FRQ-01.7.1, supply a complete replacement result, or replace the bundled generator without a Host generation fallback.
  - Origin: `formulated`, approved extension-owned compaction decision and preserved composition requirements.
  - Goal: Keep the agent kernel minimal while retaining standard and custom compaction composition.
  - Goal achievement: Full. The extension owns policy and composition while Host supplies runtime and durable session services.
- FRQ-01.2: Before manual, threshold, or overflow-recovery compaction, the policy-owning extension shall establish an immutable original compaction request and an equal initial current compaction request. Each request shall contain the trigger reason, retry intent, user instructions, selected model descriptor, provider-neutral active-branch projection, proposed summarized prefix and preserved suffix, preceding summary, context token count, model context window, response budget, and cancellation capability. The exact mechanism for invoking participating extension capabilities remains technical design work under QST-01.
- FRQ-01.3: Compaction request handlers shall run in registration order. Each handler shall receive the original compaction request, the current compaction request, and the current result when one exists.
- FRQ-01.4: A request handler shall return a state update that independently preserves or replaces the current compaction request and preserves, sets, replaces, or clears the current result. Cancellation shall be an exclusive terminal action.
- FRQ-01.5: The next request handler shall receive the unchanged original compaction request and the current state returned by the preceding handler. A handler can derive its returned state from the original compaction request, the current state, or both.
- FRQ-01.6: When the request-handler chain finishes successfully without a current result, the policy-owning extension shall invoke the single registered compaction generator with the final current compaction request. The bundled extension shall supply that generator by default, and a custom extension shall be able to replace it. Zero or multiple registered generators shall fail compaction explicitly without a Host fallback or load-order selection. An existing current result shall bypass generator lookup and invocation entirely.
- FRQ-01.7: A current compaction result shall contain a nonempty summary, the first preserved active-branch entry identifier, a usage state that is absent or contains normalized model usage, and a details state that is absent or contains opaque extension data.
- FRQ-01.7.1: After a result exists, result handlers shall run in registration order. Each result handler shall receive the original compaction request, the final current compaction request, the immutable original compaction result, and the current result returned by preceding result handlers, and shall preserve or replace the current result or cancel compaction. Cancellation shall be an exclusive terminal action.
- FRQ-01.8: The policy-owning extension shall validate every handler action before exposing its returned state to the next handler. Host shall validate transport shape without applying compaction policy. An invalid action or handler error shall follow the [extension failure rule](../../prd.md#extension-failure-rule): stop compaction, report the complete error text, invoke no later handler, and append no compaction entry. Host shall not retry the failed extension or invoke another compaction implementation as a fallback.
- FRQ-01.9: The session owner shall enforce structural and session validity against the final current result before atomically appending one compaction entry. Validation shall reject an empty summary, a kept-entry identifier outside the active branch, a boundary before the latest applicable compaction boundary, inconsistent source accounting, and a boundary that separates a tool result from its tool call. These checks shall not select or revise compaction policy.
- FRQ-01.10: Host shall publish compaction success only after compaction-entry persistence and shall publish compaction failure after cancellation, handler failure, invalid handler action, unavailable compaction capability, structural and session validity failure, or persistence failure.
- FRQ-01.11: The bundled compaction extension shall retain a recent unsummarized suffix using a configurable token budget with a default of 20,000 tokens. This budget shall remain separate from the response budget. The selected boundary shall retain complete messages and shall not separate a tool result from its tool call.
- FRQ-01.12: For repeated compaction, the policy-owning extension shall update the preceding summary with the newly summarized part of the previously retained suffix. It shall not summarize the complete original history again. The next prepared history shall contain the updated summary followed by the preserved suffix, not a chain of preceding compaction summaries.
- FRQ-01.13: The session owner shall retain every original session entry and project the latest applicable compaction entry as its summary followed by the preserved active-branch suffix for model requests, restoration, navigation, fork, and clone.
- FRQ-01.14: Agent Core shall consume prepared history and shall receive no compaction policy, request/result composition, threshold, boundary-selection rule, extension transport, or compaction-specific control flow.
- FRQ-02: Manual compaction shall pass user instructions to the policy-owning extension and shall use the same composition semantics and session-owned persistence and projection mechanics as automatic compaction.
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

- DLV-01: Extension-owned compaction coordination together with session-owned compaction entries, durable mutation, restoration, navigation, and prepared-history projection.
- DLV-01.1: The target compaction integration contracts selected under QST-01, the bundled compaction extension, and a reference custom extension demonstrating composition and complete replacement.
- DLV-02: Manual compaction, retry-policy configuration, retry extension contracts, retry client operations, and retry behavior inside the established Host logical model-execution owner.

### Acceptance Criteria

- ACC-01: Automatic compaction replaces an older context prefix in model-visible context, retains the original session entries, preserves the remaining suffix, and allows the run to continue.
- ACC-01.1: Two request handlers receive the same original compaction request, while the second receives the current compaction request and current result returned by the first.
- ACC-01.2: A handler can discard preceding request changes by deriving its replacement from the original compaction request, and the next handler observes that replacement as the current compaction request.
- ACC-01.3: An extension-provided current result bypasses generator lookup and invocation, including when no generator or multiple generators are registered. Two result handlers transform the result in registration order while each can inspect the original compaction result.
- ACC-01.4: Clearing the current result before the request-handler chain finishes successfully makes the single registered generator process the final current compaction request. The bundled generator works by default; a custom generator works with the bundled generator disabled. Zero or multiple generators fail explicitly without a Host fallback or load-order selection.
- ACC-01.5: A compaction handler error or invalid action reports the complete error text, stops compaction before persistence, invokes no later handler or alternative implementation, and performs no extension retry.
- ACC-01.6: Cancellation by a request or result handler writes no compaction entry and emits one compaction failure event, while successful persistence emits one compaction success event.
- ACC-01.7: The bundled extension retains the recent suffix under the default 20,000-token budget and under a changed configured budget without splitting a tool call from its result.
- ACC-01.8: Two successive compactions use the preceding summary and the newly summarized messages. The second model-visible projection contains the updated summary and preserved suffix without duplicate earlier summaries or reintroduced original prefixes.
- ACC-01.9: The standard coding agent uses the bundled compaction extension for complete compaction policy and default generation. A custom extension participates in ordered composition and can supply the complete result without the bundled generator or a Host summary-generation implementation.
- ACC-01.10: The session owner atomically appends the extension-selected compaction entry, retains every original entry, and rebuilds the same prepared history after restart and branch navigation.
- ACC-01.11: The policy-owning extension decides triggering, sizing, retained-boundary selection, generation, and compaction-specific overflow handling. Agent Core receives only prepared history, and Host runs no duplicate compaction policy.
- ACC-02: Manual compaction passes user instructions to the policy-owning extension, applies the composition semantics in ACC-01.1 through ACC-01.6, and persists the valid replacement through the same session-owned mechanics used by automatic compaction.
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

Compaction reuses ordered extension composition, the extension runtime, and the session entry/projection model. It adds no Host compaction algorithm, Agent Core compaction branch, generic middleware platform, retry daemon, or provider-specific policy language. QST-01 leaves only the minimum integration mechanism unresolved. Retry retains its approved Host-owned composition rule.

## Constraints and Risks

- RSK-01: Provider token accounting may be absent or approximate. The compaction extension shall define how its policy combines model descriptors, usage observations, and estimates.
- RSK-02: An extension can select a boundary that corrupts model-visible tool history. The session owner shall enforce structural and session validity before persistence without selecting a different policy boundary.
- RSK-03: An extension can select a long delay or high attempt limit. Retry events expose the final decision and general abort cancels the request or pending delay.
- RSK-04: Coupling retry coordination to an in-process OpenAI adapter would make PHS-12 replace Host orchestration together with provider transport. FRQ-04.7.1 and FRQ-04.7.2 keep provider replacement behind the Host model-execution boundary.

## Assumptions

None.

## Open Questions

### QST-01: Minimal compaction integration contract

- Impact: The extension cannot own the complete policy and preserve composition until the technical design defines how it invokes participating request, generator, and result capabilities, receives session-bound inputs, observes model usage, runs before provider requests, handles provider context overflow, and requests one durable replacement entry.
- Required answer: A minimal contract and call flow through which the policy-owning extension invokes ordered request, generator, and result capabilities, uses existing Host runtime and session services, keeps Agent Core unaware of compaction, and introduces no second Host policy implementation or general middleware platform.
- Current evidence: The unaccepted unit 4 checkpoint proves the replacement-entry and projection mechanics but implements the rejected Host-owned policy boundary.
- Resolution point: Resolve in the PHS-06 technical design before production migration.

## Technical Supplement

The [technical solution](solution.md) separates the approved target boundary from the unaccepted Host-owned checkpoint and its implementation evidence. Its [tool-call argument representation](solution.md#tool-call-argument-representation) records the implemented single JSON value. QST-01 remains unresolved; PHS-06 implementation is not complete. The [migration plan](migration-plan.md) records the resume point, ordered work, and exit criteria after checkpoint `690827f`.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [Pi extension-surface research](../../../../../artefacts/pi-extension-surface.md) - feature evidence for cancellable and replaceable compaction.
- REF-04: [target architecture](../../architecture.md) - Host retry ownership and Agent Core boundary.
