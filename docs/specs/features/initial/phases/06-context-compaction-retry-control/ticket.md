# Ticket: PHS-06 - Context compaction and retry control

Keep long sessions usable within model context limits.

The [PHS-06.2 problem statement](phs-06.2_problem.md), [requirements](phs-06.2_prd.md), and [technical solution](phs-06.2_solution.md) are approved, and the design dry run is complete. Accounting slice A1 implements [D6](phs-06.2_solution.md#d6-per-request-accounting-for-persisted-compaction). A2 implements bundled manual compaction, and A3 implements automatic compaction with conversation observations. In A4, the bundled compaction extension implements admitted overflow recovery through standard composition within Host's attempt allowance. The [delivery plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records A1-A4 behavior and verification limits. [PHS-06.3 residue closure](residue-closure-evidence.md) is complete. The [whole-phase record](acceptance-report.md) assesses all 40 clauses after the approved corrective units. Implementation is complete and independent acceptance-conclusion review approves all 40 clauses within the recorded boundaries. Final text review is approved and UNT-06 is complete in documentation commit `e7dd665cfc2dd50ab00182ad5be188dbe5619d27`. Final user acceptance remains pending. This ticket owns the shared composition, session, and retry requirements.

Completion-plan unit A3 implements terminal-refusal preservation for OpenAI-compatible Responses under FRQ-04.8.1. The [whole-phase dispositions](acceptance-report.md#findings) record the independently approved Codex correction in UNT-01 and ownerless-overflow correction in UNT-02. The [evidence record](a3-terminal-failure-evidence.md) contains passing public-client regressions, mandatory checks, and independent unit review. The history-source unit acceptance is recorded below. Independent acceptance-conclusion review is approved; final user acceptance remains pending.

For completion-plan unit A4.1, Agent Core accounts for appended history independently of prepared-history length. Its [evidence record](a4.1-run-history-evidence.md) records regression and check results. A4.1 is accepted after code-quality, text-quality, and independent unit review. Completion-plan A4 is accepted after code-quality, text-quality, and independent unit review. Its [evidence record](a4-prepared-history-evidence.md) records verification and the two scoped acceptance exceptions. The [A5 evidence record](a5-retry-acceptance-evidence.md) contains public retry observations, check results, and review status. A5 is accepted after code-quality, text-quality, and independent unit review under the specific vulnerability exception and recorded evidence limits. The [A6 evidence record](a6-abort-idle-evidence.md) records ordinary conversation abort, public idle, and same-connection recovery through Programmatic Control and the UI-plugin protocol. A6 is accepted after code-quality, text-quality, and independent unit review under the specific vulnerability exception and recorded evidence limits. Independent PHS-06 acceptance-conclusion review is approved within the recorded boundaries. Final user acceptance remains pending. PHS-06.2 slice A4 covers the earlier overflow-recovery implementation.

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
- DEF-11.1: Compaction operation binding. Host process-local reference for one parent compaction or conversation-preparation operation, its captured input and participating runtime identities, nested work, and actual commit outcome. The [PHS-06.1 design](solution.md#phs-061-integration-contract) describes this integration concept.

## Problem Statement

The observations below describe the historical completion problems at the named revisions. The [whole-phase review outcome](acceptance-report.md#independent-review-outcome) records the later approved acceptance conclusion and remaining delivery steps.

- PRB-01: Standard compaction can select summary source and recent-history retention from original records whose content differs from prepared conversation history. Users can receive summaries based on discarded responses and retain less useful recent context. The [history-source problem statement](../../../../issues/compaction-history-projection/problem.md) owns the detailed scope and evidence.
- PRB-02: Extension-owned compaction and Host-owned retry coordination are implemented, but PHS-06.4 independent acceptance remains incomplete. Some acceptance conditions have source traces without direct executable observations, so the complete phase outcome remains unverified.
- PRB-03: After a terminal provider refusal followed by HTTP stream truncation, users can lose the refusal's text and receive a transport-based retry outcome. The reproduced request repeated after Glyph had received a context-limit refusal, and the client received only transport causes.

### Current acceptance observations

At revision `7e48788092fec01e255ae7179fdedd10b15755ef`, a public Programmatic Control probe used the OpenAI-compatible Responses API. The provider sent a complete `response.failed` event with code `context_length_exceeded` and message `provider terminal rejection full unique text`. Its declared HTTP body length exceeded the bytes sent by 100 bytes, so a later read produced `unexpected EOF`. No compaction policy or retry handlers were registered; settings allowed one retry with zero delay.

Glyph made two provider requests. The public terminal category was `RETRY_EXHAUSTED`, and its message contained two copies of `OpenAI-compatible request failed: unexpected EOF`. The provider's terminal refusal text was absent. This observation establishes a failure of terminal cause preservation under ACC-04.5; it is separate from the history-source correction.

Explicit request and result Cancel probes terminated composition without persisting a replacement. A participant error containing `context.Canceled` followed the error path. FRQ-01.4 and FRQ-01.7.1 require an exclusive terminal Cancel action; those clauses do not require conversion of a participant error into that action. These observations establish no separate violation of those cancellation requirements.

The desired outcome is continued work with summaries based on conversation context, access to the original journal, and complete failure causes. The phase boundary includes compaction composition, restoration and navigation, cancellation, committed failures, and retry control. PHS-08 and changes to the approved standard-generator limits remain outside this phase.

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
- ISP-01.1: PHS-06.1 includes system contracts, caller migration, Host-policy removal, and verification of the system contract with a reference extension. See the [migration plan](migration-plan.md#phased-plan) for phase boundaries, sequential behavior slices, and final cleanup.

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
- FRQ-01.7: A current compaction result shall contain a nonempty summary, the first preserved active-branch entry identifier, explicit source accounting, and a details state that is absent or contains opaque extension data. The [PHS-06.2 accounting requirement](phs-06.2_prd.md#functional-requirements), FRQ-12, defines per-request accounting for persisted compaction results.
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
- FRQ-04.8.1: When a stream fails after Glyph receives a terminal provider refusal, Glyph shall preserve the complete refusal text and the subsequent stream error through every applicable client interface. Glyph shall determine the failure category and retry decision from the received terminal refusal under the existing retry policy and handler actions, retaining the subsequent stream failure as an additional cause.
  - Origin: source. FRQ-04, FRQ-04.1 through FRQ-04.5, FRQ-04.8, FRQ-04.9, FRQ-07, and ACC-04.5; the approved PRB-03 terminal-failure preservation scope.
  - Goal: Preserve the meaning and complete causes of a terminal provider refusal during classification, retry coordination, and public reporting.
  - Goal achievement: Full. Users receive the provider's explanation and the subsequent transport cause, with retry behavior based on the received refusal.
  - Feasibility: Confirmed. Glyph receives the terminal refusal before stream truncation and already distinguishes provider classification, retry decisions, and public failure categories.
- FRQ-04.9: Retryability, retry decision, and terminal Glyph category shall remain separate values. A retry decision shall not replace or remove the source error.
- FRQ-05: General abort shall cancel an in-progress provider request or pending retry delay and transition the agent to idle.
- FRQ-06: A retry shall repeat only the failed model request and shall not repeat any completed tool execution. Failed intermediate attempts shall produce operation events and shall not create session messages or enter model context. After retry finishes, Glyph shall persist only the terminal model outcome.
- FRQ-07: Enable retry by default with three retries after the initial request and delays of 1, 2, and 4 seconds. The built-in policy shall honor a provider-supplied `Retry-After` delay up to a configurable maximum of 30 seconds by default. When the requested delay exceeds that maximum, the logical model execution shall end with an error that states the requested delay and configured maximum. Host shall not shorten the requested delay to the maximum and retry early. The built-in retryable HTTP statuses shall be 408, 429, 500, 502, 503, and 504. Transport timeouts, connection resets, and unexpected connection closure before a terminal provider response shall also be retryable.
- FRQ-08: Make the enabled state, maximum retry count, ordered retry delays, and the maximum accepted `Retry-After` delay configurable through Host. The maximum retry count shall exclude the initial request. Each configured delay shall apply before its corresponding repeat attempt. FRQ-07 defines the default retry count and delays, not fixed limits. Provider adapters shall own HTTP failure classification; the retryable HTTP status set shall not be user-configurable.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`. The [two approved A4 exceptions](a4-prepared-history-evidence.md#review-and-remaining-acceptance) apply only to the named original test-first deviation and the specific development vulnerability finding.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, concrete provider packages, and TUI packages. Host model execution and retry coordination must remain independent of concrete provider packages.
- NRQ-01: PHS-06 completion shall have executable evidence for every ACC-01 through ACC-06 condition, including subcriteria, independent acceptance, and passing mandatory project checks. The acceptance report shall identify skipped scenarios and limits of the evidence.
  - Origin: source. The [PHS-06.4 exit criteria](migration-plan.md#phase-phs-064---independent-acceptance) and the approved PRB-02 phase-completion scope.
  - Goal: Establish completion of the whole phase against observable behavior.
  - Goal achievement: Full. Phase status depends on verified acceptance conditions and explicitly bounded evidence.
  - Feasibility: Confirmed. Public integration checks, project-wide checks, and independent review are available.

- NRQ-01.1: For completion-plan A4 only, reviewers may accept the named original test-first deviation in the [A4 evidence record](a4-prepared-history-evidence.md#fresh-accepted-baseline-repeat). The acceptance report shall distinguish original RED runs from later assertion-sensitivity controls. All other test-first obligations, including A5, A6, and subsequent behavior corrections, shall remain in effect.
  - Origin: approved A4 acceptance exception O14-1.
  - Goal: Preserve the recorded test chronology while permitting acceptance of the named deviation.
  - Goal achievement: Full. Reviewers retain the chronology limitation and evaluate the separately recorded runtime results.
  - Feasibility: Confirmed. The evidence record separates original RED runs from later controls.

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
  - Terminal-refusal example: with no compaction policy owner or retry-handler override, a `response.failed` event containing `context_length_exceeded` and `provider terminal rejection full unique text`, followed by `unexpected EOF`, produces `CONTEXT_LIMIT` with both causes and one provider request. This example verifies FRQ-04.8.1 without changing registered-owner overflow recovery or retry-handler authority.
- ACC-04.6: A configured retry count and delay sequence replace the defaults. Disabling retry produces only the initial provider attempt. The configured retry count does not include that initial attempt.
- ACC-04.7: Under the default policy, a provider-requested delay of 10 seconds is honored. A provider-requested delay of 60 seconds exceeds the default 30-second maximum, ends the logical model execution with complete error text identifying both values, and schedules no further provider attempt. Changing the configured maximum changes the acceptance threshold.
- ACC-05: Retrying a failed model request repeats no completed tool, adds no intermediate attempt to session messages or model context, and persists only the terminal model outcome.
- ACC-06: Abort cancels an in-progress provider request or pending retry delay and leaves the agent idle.

## Overengineering and Overspecification Considerations

Compaction reuses ordered extension composition, the extension runtime, and the session entry/projection model. It adds no Host compaction algorithm, Agent Core compaction branch, generic middleware platform, retry daemon, or provider-specific policy language. QST-01 records the approved minimum integration mechanism. Retry retains its approved Host-owned composition rule.

## Constraints and Risks

- RSK-01: Provider token accounting may be absent or approximate. The compaction extension shall define how its policy combines model descriptors, usage observations, and estimates.
- RSK-02: An extension can select a boundary that corrupts model-visible tool history. The session owner shall enforce structural and session validity before persistence without selecting a different policy boundary.
- RSK-03: An extension can select a long delay or high attempt limit. Retry events expose the final decision and general abort cancels the request or pending delay.
- RSK-04: Coupling retry coordination to an in-process OpenAI adapter would make PHS-12 replace Host orchestration together with provider transport. FRQ-04.7.1 and FRQ-04.7.2 keep provider replacement behind the Host model-execution boundary.

## Assumptions

None.

## Open Questions

### QST-01: Minimal compaction integration contract

- Status: Resolved for system-contract design. See the approved [target system contract and scoped dry-run evidence](solution.md#phs-061-system-contract-dry-run-evidence), [implementation progress](migration-plan.md#sequential-implementation-units), and the separate [QST-01.1 decision](solution.md#qst-011-standard-summary-input-above-the-model-budget).
- Impact: The extension cannot own the complete policy and preserve composition until the technical design defines how it invokes participating request, generator, and result capabilities, receives session-bound inputs, observes model usage, runs before provider requests, handles provider context overflow, and requests one durable replacement entry.
- Required answer: A minimal contract and call flow through which the policy-owning extension invokes ordered request, generator, and result capabilities, uses existing Host runtime and session services, keeps Agent Core unaware of compaction, and introduces no second Host policy implementation or general middleware platform.
- Current evidence: The unaccepted unit 4 checkpoint proves the replacement-entry and projection mechanics but implements the rejected Host-owned policy boundary.
- Resolution point: The [phased plan](migration-plan.md#phased-plan) defines system implementation and acceptance before bundled-extension implementation.

## Technical Supplement

The approved [PHS-06.4 completion corrections](solution.md#phs-064-completion-corrections), D1 through D4, cover prepared-history selection, terminal-failure preservation, and public-boundary acceptance. The [migration plan](migration-plan.md#phase-phs-064---independent-acceptance) records implementation progress. The [correction plan](acceptance-corrections/plan.md#plan-units) records five approved corrective units and the UNT-02 follow-up. Independent acceptance-conclusion review is approved. Final text review approved both resolved Minor findings, and the owner committed UNT-06 as `e7dd665cfc2dd50ab00182ad5be188dbe5619d27`. Final user acceptance remains pending.

The [technical solution](solution.md) contains the approved target boundary, the unaccepted Host-owned checkpoint evidence, and the [tool-call argument representation](solution.md#tool-call-argument-representation). The [migration plan](migration-plan.md#sequential-implementation-units) owns implementation progress and links to the phase order and exit criteria. The [PHS-06.2 technical solution](phs-06.2_solution.md) resolves [QST-01.1](solution.md#qst-011-standard-summary-input-above-the-model-budget) and defines standard generation and per-request accounting.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [Pi extension-surface research](../../../../../artefacts/pi-extension-surface.md) - feature evidence for cancellable and replaceable compaction.
- REF-04: [target architecture](../../architecture.md) - Host retry ownership and Agent Core boundary.
