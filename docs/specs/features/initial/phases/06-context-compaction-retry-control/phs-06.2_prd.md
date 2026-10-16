# Requirements: PHS-06.2 standard context compaction

Status: Approved, including the first-version limits below. The [technical solution](phs-06.2_solution.md) has passed its design dry run. Accounting slice A1 implements [D6](phs-06.2_solution.md#d6-per-request-accounting-for-persisted-compaction). A2 implements bundled manual compaction, and A3 implements automatic compaction. For requests that continue the observed input and completed response unchanged, with matching reasoning, instructions, and tool definitions, the bundled compaction extension reuses reported conversation usage. In A4, the bundled compaction extension implements admitted overflow recovery through standard composition. When no older history remains reducible, the extension fails explicitly. The [delivery plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records A1-A4 behavior and verification limits. [PHS-06.3 residue closure](residue-closure-evidence.md) is complete; PHS-06.4 independent acceptance remains pending.

## Definitions

Use the [domain glossary](../../../../../terms.md) and the [PHS-06 terminology](ticket.md#key-definitions-and-abbreviations). This phase introduces no new domain terms.

## Context and Problem

See the approved [problem statement](phs-06.2_problem.md).

## Goal

Keep long coding sessions usable by reducing model-visible context while retaining essential prior information and access to the original history.

## Scenarios

- A growing conversation reaches the automatic compaction threshold. The next model request uses a summary and recent messages.
- A caller requests manual compaction with guidance. Later compaction updates the preceding summary with the next older portion of retained history.
- A custom extension supplies a complete result or replaces the standard generator.
- A provider reports context overflow. Compaction participates in the existing recovery path without repeating completed tools.
- Session statistics include the successful model requests that contributed to a persisted summary.

## Scope and Non-Scope

PHS-06.2 supplies the standard policy and generator, their startup settings, and per-request accounting for persisted compaction results. The [PHS-06.1 system guarantees](system-integration-evidence.md#user-visible-contract) remain the foundation for original-history retention, structural validation, persistence, restoration, operation cancellation, and committed outcomes. These guarantees correspond to the existing-system item R4 from requirements approval.

Ordinary provider retries, branch summarization, and system support already delivered by PHS-06.1 are outside the new behavior. Existing custom configured-model image requests remain supported.

The standard generator does not promise arbitrary-size source processing, individual-message splitting, or image interpretation. Complete billing for failed or canceled generations is outside this version. The explicit first-version behavior in FRQ-08 supersedes the earlier PHS-06.2 expectation of standard image summarization and capacity-based multi-request processing. System capabilities for custom generators remain unchanged.

## Requirements

### Functional Requirements

- [x] FRQ-01: Standard compaction shall be included and enabled by default in the standard coding agent.
  - Origin: `source`, [PHS-06 FRQ-01.1.1](ticket.md#functional-requirements); approved as R1.
  - Goal: Make compaction available without a separately developed extension.
  - Goal achievement: Partial. Default availability makes the remaining behavior usable in ordinary sessions.
- [x] FRQ-02: The standard policy shall decide automatic compaction from the selected model's limits, response reserve, and available usage observations.
  - Origin: `source`, [PHS-06 FRQ-01](ticket.md#functional-requirements); approved as R2.
  - Goal: Keep growing conversation input within the model's capacity.
  - Goal achievement: Partial. Estimates determine when compaction starts; they do not guarantee provider acceptance.
- [x] FRQ-03: Manual compaction shall use caller guidance and the same composition semantics as automatic compaction.
  - Origin: `source`, [PHS-06 FRQ-02](ticket.md#functional-requirements); approved as R3.
  - Goal: Let the caller focus a compaction on the current task.
  - Goal achievement: Partial. Guidance affects the summary while preserving common handler behavior.
- [x] FRQ-05: The standard policy shall retain recent complete messages using an extension-local configurable target of 20,000 tokens by default. This target shall be separate from the response reserve. A tool call and its results shall remain together; an indivisible group may exceed the target.
  - Origin: `source`, [PHS-06 FRQ-01.11](ticket.md#functional-requirements); approved as R5 and clarified by D3.
  - Goal: Preserve recent detail and coherent tool history.
  - Goal achievement: Partial. Complete recent groups remain available alongside the summary.
- [x] FRQ-06: Repeated compaction shall update the preceding summary from the newly summarized part of the previously retained history.
  - Origin: `source`, [PHS-06 FRQ-01.12](ticket.md#functional-requirements); approved as R6.
  - Goal: Support continued work after successive compactions.
  - Goal achievement: Partial. Each compaction advances through retained history without resummarizing all stored originals.
- [x] FRQ-07: The standard policy shall coordinate request and result handlers in registration order, preserve immutable originals, and validate each update before exposing its new current state. A handler may supply a complete result.
  - Origin: `source`, [PHS-06 FRQ-01.2 through FRQ-01.8](ticket.md#functional-requirements); approved as R7. Those requirements own the detailed action semantics.
  - Goal: Preserve custom extension participation in standard compaction.
  - Goal achievement: Partial. Extensions can customize the request or result without replacing system services.
- [x] FRQ-08: Standard summary input shall include text and the preceding summary. Images in the summarized portion shall be excluded while accompanying text remains. Each complete tool result shall contribute at most 2,000 characters of text, with a marker when text is removed. Other message text shall remain whole. An oversized summary request may fail.
  - Origin: `formulated`, the user approved first-version limitations in O10-1, revising R8 and the earlier multi-request choice. Original session entries remain intact.
  - Goal: Provide standard compaction with explicit first-version information-loss and capacity limits.
  - Goal achievement: Partial. The standard generator reduces source size but can omit information from its input or fail on excessive source.
- [x] FRQ-09: The standard policy shall participate in context-overflow recovery while preserving Host's attempt allowance and completed-tool execution history.
  - Origin: `source`, [PHS-06 FRQ-01 and FRQ-06](ticket.md#functional-requirements); approved as R9.
  - Goal: Recover a model request without repeating completed actions.
  - Goal achievement: Partial. Recovery remains subject to available attempts and compaction success.
- [x] FRQ-10: Cancellation, a handler error, or an invalid handler update shall stop composition. When an error occurs, the invoking client shall receive its complete cause. An outcome after persistence shall retain both the durable result and any subsequent error.
  - Origin: `source`, [PHS-06 FRQ-01.8 through FRQ-01.10](ticket.md#functional-requirements) and [system outcome guarantees](system-integration-evidence.md#user-visible-contract); approved as R10 and D7.
  - Goal: Keep the operation controllable and its outcome explainable.
  - Goal achievement: Partial. Failure cannot silently continue composition or conceal an existing commit.
- [x] FRQ-11: Generator registration shall be independently disableable while the standard policy remains active. A supplied complete result shall bypass generator lookup. Without such a result, the policy shall require exactly one registered generator; zero or several shall fail explicitly.
  - Origin: `source`, [PHS-06 FRQ-01.6](ticket.md#functional-requirements); approved as R11.
  - Goal: Support standard generation and replacement by another extension.
  - Goal achievement: Partial. Generator choice remains explicit and independent of policy activation.
- [x] FRQ-12: A persisted compaction result shall retain accounting for each successful model request that contributed to that result. Host shall calculate each request's estimated cost separately and aggregate session statistics from those records. Missing usage or pricing shall remain distinguishable from zero.
  - Origin: `formulated`, approved in O8.1-1 and retained in D6 and O10-1.
  - Goal: Explain the token usage and estimated cost of successful summary generation.
  - Goal achievement: Partial. Per-request calculation preserves pricing-tier semantics for custom generators that make multiple requests.

### Non-Functional Requirements

- [x] NRQ-01: Compaction shall use asynchronous operations and keep the client interface responsive, including while generation or cancellation is pending.
  - Origin: `source`, project architecture rules and [PHS-06.1 operation lifetimes](solution.md#manual-automatic-and-overflow-integration); retained during requirements approval.
  - Goal: Keep long-session maintenance controllable during model calls.
  - Goal achievement: Partial. The caller can interact with the client while compaction work is active.

## Behavior Examples

- A tool result with two text blocks of 1,500 characters contributes 2,000 characters in total and one truncation marker to standard summary input. Images between those blocks do not reset the limit. The original result remains unchanged.
- A supplied complete result bypasses generation even when no generator is registered. Clearing that result before request handling ends requires exactly one generator again.
- The second compaction uses the preceding summary and the next selected older messages. Earlier summary-generation costs remain attributed to their original persisted result.
- Two model requests are priced separately even when their combined token count would cross a pricing threshold that neither request crosses individually.

## Open Questions

None at the requirements level. The [technical solution](phs-06.2_solution.md#design-dry-run-evidence) records design evidence and verification limits.

## References

- [PHS-06 ticket](ticket.md): shared composition and session guarantees.
- [Migration plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension): phase position and delivery status.
- [Technical solution](phs-06.2_solution.md): approved approach and design-validation outcome.
