# Technical Solution: PHS-06.2 standard context compaction

Status: Approved; A1-A4 implemented. Implemented scope: A1, D6 accounting; A2, bundled manual compaction; A3, D2 automatic compaction and conversation observations; A4, D7 admitted overflow recovery. The [delivery plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records behavior and verification limits. Completion-plan A4 implements the approved [prepared-history correction](../../../../issues/compaction-history-projection/solution.md). The [whole-phase record](acceptance-report.md) assesses all 40 clauses after the approved corrective units. Implementation and independent acceptance-conclusion review are complete within the recorded boundaries. [PHS-06.3 residue closure](residue-closure-evidence.md) is complete. UNT-06 text corrections are ready for re-review and commit; final user acceptance remains pending.

## Problem Statement

The approved [problem statement](phs-06.2_problem.md) describes the long-session context limit. The [requirements](phs-06.2_prd.md) own expected behavior and accepted first-version limitations.

[PHS-06.1](system-integration-evidence.md) is the existing system foundation. This solution adds the standard extension and the approved per-request accounting support. Host retains runtime and session services; Agent Core consumes prepared history. The [parent solution](solution.md#target-ownership) owns those boundaries.

## Proposed Solution

### D1: Standard extension and startup configuration

Supply one executable extension under `plugins/extension/`, using the existing catalog and public Extension SDK. Follow the tools extension's separation of executable startup, application assembly, controller, use cases, and external-operation adapters. Keep policy and generation logic independent of wire representations and Host internals.

The `plugins/extension/compaction/internal/transport/extensionwire` package owns conversions between public Extension protobuf messages and compaction domain values. The input controller and outgoing Host adapters use these conversions because both directions carry the same Extension representations. For example, the generator callback and the Host result-handler adapter both encode a `compaction.Result` with `extensionwire.EncodeResult`. The package depends on public protobuf types and the compaction domain; use cases retain their consumer-owned contracts and policy.

The standard build includes the extension alongside the tools extension. Its normal registration exposes the policy and generator. Startup configuration can omit generator registration while retaining the policy, so a custom extension can supply the generator through the same composition path.

Load the retained-history target and generator enablement from extension-prefixed environment variables at startup. The defaults are defined by [FRQ-01, FRQ-05, and FRQ-11](phs-06.2_prd.md#functional-requirements). Invalid configuration produces a startup error. A new Host configuration API, separate generator process, and dedicated summary-model setting are unnecessary for this scope.

This approach reuses `plugins/extension/tools/internal/app/app.go`, `Serve`, the normal catalog loader, and `sdk/plugins/extension/v1/sdk.go`, `Connect`. The SDK's process configuration preserves the Host environment. The existing `--extension-dir` behavior continues to replace the whole catalog.

### D2: Full-input estimates and conversation observations

The policy estimates the actual prepared conversation input, including history, instructions, tool definitions, and Host-supplied sizes of private provider data. It reserves the selected conversation descriptor's `maxTokens` for the response. Automatic compaction starts when the estimate exceeds `contextWindow - maxTokens`; equality fits.

Use a model-neutral content-size estimate rather than a provider tokenizer dependency. Text and JSON sizes, fixed message framing, and image count contribute separately. Encoded image bytes are not text-token evidence: changing compression or transport encoding must not change the estimate for the same image count. Host supplies private-payload sizes but performs no token estimation.

For example, replacing a retained image with a larger encoding while preserving text and image count leaves this estimate unchanged. Images remain part of the ordinary conversation even though the standard generator excludes images from the summarized portion under FRQ-08.

The policy keeps a detached observation anchor in memory for the issued session binding and model. It reuses reported conversation usage only for exact continuation of the observed input plus completed response, with matching reasoning, instructions, and tool definitions. It adds estimates for subsequent input growth. Missing usage, changed history or request shape, compaction, and plugin restart select content-based estimation. Summary-model calls do not update this conversation anchor.

Synchronize access to the in-memory anchors. Each operation evaluates its immutable captured input. A durable observation store would add recovery work without improving correctness of the fallback estimate and is outside D2.

The policy implements all existing owner callbacks: compaction decisions, completed-conversation acknowledgement, and outgoing-input validation. A registered owner participates in ordinary conversation completion even when it decides not to compact. `Runtime.ObserveConversation` requires a matching typed acknowledgement.

### D3: Complete-message retention

Select the retained suffix from prepared conversation history using the configured target and the same content estimator. The implemented [history-source correction](../../../../issues/compaction-history-projection/solution.md) owns source correspondence and legal stored-branch boundaries for reordered and synthetic messages. Its coordinated contract migration supplies prepared history to all compaction consumers.

On repeated compaction, select newly summarized content from the remaining unsummarized prepared history and supply the preceding summary once. The extension excludes superseded original prefixes from fresh summary source. The extension keeps producing responses and their tool results together and treats the target as a recent-context target that an indivisible group may exceed. Host retains structural validation of the selected original-entry boundary.

Manual compaction with no older portion to summarize fails explicitly before persistence. Automatic input below threshold is preserved. Oversized input with no reducible older portion may fail under the accepted first-version limitations. The policy does not invent an alternative boundary that discards required recent context.

### D4: One standard generation request

The standard generator makes one configured-model request through `ExtensionContext.StartConfiguredModel`. It uses the model and reasoning choice from the final composed request after request handlers. That choice does not change the captured model or budget of the main conversation.

Prepare the preceding summary and newly summarized messages as structured source text, with roles and tool identities represented as data. Keep the summary task instruction separate from that source. Include caller guidance for manual compaction. The existing Glyph branch-summary serializer provides a local precedent for representing conversation source as data; the plugin does not import Host internal packages.

Apply the [FRQ-08 input limits](phs-06.2_prd.md#functional-requirements) to standard generation only. A tool result has one shared text allowance across all its ordered text blocks, measured in Unicode code points, followed by one marker when text was removed. An intervening image does not reset the allowance. Original session records remain intact. Custom generators retain the existing public text/image input capability.

Follow `validateSummaryResponse` in `host/internal/usecase/host/sessiontree/summarizer.go`: STOP or LENGTH with nonempty public text and no tool calls is usable. Failed, aborted, empty, or invalid responses fail generation with their complete cause. The standard result contains its actual model selection and reported usage for this request.

A dedicated summary model, general multi-request packing, individual-message fragments, and a separate split-span summary are excluded. One request meets the approved first-version behavior with fewer generation states. Excessive source may therefore fail instead of triggering another generation strategy.

### D5: Composition, projection, and persistence

The policy owns immutable original values, current request/result state, and the order of participant calls. It uses the captured registration order and existing SDK operations to invoke one request handler, generator, or result handler at a time. Only a validated update replaces the last accepted state. The [shared composition requirements](ticket.md#functional-requirements) define preserve, replace, clear, cancel, and generator-cardinality behavior.

A supplied result bypasses generator lookup. A cleared or absent result requires the single registered generator. The policy validates candidates through system projection before exposing an accepted result to later processing or requesting durable replacement.

Before replacement, the policy sizes the candidate history together with the captured conversation instructions and tools. It uses the captured conversation model's budget, independently of any summary-model change by handlers. An oversized candidate fails before persistence.

After automatic or overflow replacement and notifications, Host captures the actual outgoing history and invokes the existing outgoing-input callback. The policy checks that input again, without restarting composition. This preserves the system's distinction between a failure before persistence and a failure after a durable replacement.

The relevant public SDK operations are `CompactionPolicyContext.StartRequestHandler`, `StartGenerator`, `StartResultHandler`, `StartProjectReplacement`, and `StartReplaceHistory`. Host retains runtime identity checks, session/branch validity, concurrent-mutation protection, one-commit authority, publication, and the durable receipt.

### D6: Per-request accounting for persisted compaction

`CompactionSource` in [`host/internal/domain/session/compaction.go`](../../../../../../host/internal/domain/session/compaction.go) contains an extension-only source or an ordered collection of contributing model requests. Each `CompactionModelRequest` contains the actual model selection, optional normalized usage, and optional saved estimated cost. Host calculates each request's cost using its pricing data before aggregating session statistics.

Compaction has its own source types in the Extension, UI, and Programmatic Control contracts and session storage. Branch summaries have a single-source representation and the accounting behavior defined in PHS-05.2. The Extension SDK exposes the generated compaction source messages. Host ignores costs supplied with a result. For model-generated summaries, each persisted marker contains the contributing request records with optional Host-calculated costs. The session repository restores those saved costs without consulting current pricing.

Host applies pricing tiers to each request before aggregation. For example, two requests with seven input tokens each remain below a ten-token pricing threshold. Host prices both requests at the base rate, although their combined input is fourteen tokens. When pricing is available and usage is present with a value of zero, the cost is also present and equals zero. When any request lacks usage, the session service returns unavailable aggregate token and cost totals. When pricing is missing, the session service returns unavailable aggregate cost. Clients can inspect the available accounting of each contributing request.

Each new compaction records only its contributing requests. The preceding summary's generation remains attributed to its original persisted entry. The standard generator contributes one request; custom generators can contribute several. This preserves the per-request accounting decision without adding multi-request generation to the standard plugin.

`Service.priceCompactionSource` in `host/internal/usecase/host/sessions/compaction.go` calls `Service.estimatedUsageCost` for each request. The session codecs persist the ordered request records and their optional costs. The session service sums each persisted entry's own records once, including entries whose summaries have been superseded. The standard TUI retains its summary-only marker display and uses the session-statistics projection for aggregate accounting. Complete billing of failed or canceled generations remains outside [FRQ-12's scope](phs-06.2_prd.md#functional-requirements).

`TestCompactionAccountingPersistsRequestsAcrossClientsAndRestart` in `host/internal/app/compaction_accounting_integration_test.go` exercises a reference extension that makes two configured-model requests. It checks per-request tiers, zero and missing usage, missing pricing, repeated compaction, saved costs after restart with pricing removed, the previous summary passed through the Extension contract, and Programmatic Control and UI SDK projections. The bundled policy and generator remain outside A1.

### D7: Cancellation, failures, and overflow

Use the existing asynchronous operation lifetimes and nested-operation cancellation. Handler failure or an invalid action stops composition with complete error text. The policy invokes no fallback implementation and schedules no extension retry.

Host continues to own ordinary configured-model retries. Only after Host admits a retry for main-conversation overflow recovery does the bundled compaction extension invoke the standard composition, retention, and generation path. When the provider rejects the request, the extension compacts even when its input estimate fits. Participants receive the overflow trigger and retry intent. Host repeats only the failed model request and preserves completed tools.

Host enforces the attempt limit from the final retry decision. With one configured retry and no retry-handler changes, Host admits no further recovery after the provider rejects that retry. When another recovery is admitted but the retained history has no older reducible portion, D3 requires an explicit precommit failure. A preceding committed replacement remains durable; the extension performs no additional generation or commit for that failed recovery.

A precommit failure leaves the original history unchanged. A postcommit failure retains the durable replacement and its receipt with the complete subsequent cause. The policy does not replay an uncertain commit. `capturedConversation.prepare` and `RecoverOverflow` already supply the integration for these distinctions.

## Overengineering and Overspecification Considerations

Reuse the executable catalog, Extension SDK, operation lifetimes, configured-model calls, and session mutation services. Keep all standard policy and generation decisions in the extension. No alternate Host compaction implementation, middleware platform, tokenizer dependency, durable observation checkpoint, or new settings API is needed.

The [requirements](phs-06.2_prd.md#scope-and-non-scope) own accepted information-loss and capacity limitations. Pi 1.2.0 was inspected as a behavior comparison. Its code, algorithms, and architecture are not implementation sources for this design.

## Design dry-run evidence

The independent dry run and subsequent source review found no material blocker in the approved requirements or design. The analysis used repository baseline `00ba72ba4270c2bf423a8a5f2a203885d43ecef4` and traced public entry points, owner callbacks, composition, persistence, accounting, restoration, and client projections.

The review reconciled per-result text truncation, image-count estimation, and the callbacks required by a registered policy owner. These preserve the approved behavior. The temporary implementation simulation is an internal workflow artifact and is not a permanent delivery plan.

The dry run established design readiness only. The [implementation progress record](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) contains the later behavior and project-check evidence, including public extension and application integration paths. Live-provider acceptance and estimate accuracy remain untested.

The authorized A2 implementation uses `github.com/caarlos0/env/v11` at `v11.4.1` for the [startup settings](../../../../../guidelines/configuration.md#bundled-compaction-extension). The [delivery plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension) records implementation progress and remaining work.

## Open Questions

None at the requirements or technical-solution level.

## References

- [Problem statement](phs-06.2_problem.md) and [requirements](phs-06.2_prd.md): approved basis.
- [Migration plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension): phase status and ordering.
- [PHS-06.1 acceptance](system-integration-evidence.md): existing system support.
- [Compaction contract](../../../../../../api/plugins/extension/v1/compaction.proto) and [configured-model contract](../../../../../../api/plugins/extension/v1/model.proto): public integration inputs and operations.
- [Policy SDK](../../../../../../sdk/plugins/extension/v1/manual_policy.go): candidate projection and replacement operations.
- [Conversation preparation](../../../../../../host/internal/usecase/host/extensioncontext/conversation_preparation.go) and [observation transport](../../../../../../host/internal/infra/plugins/extension/runtime/conversation_observation.go): final input checks and owner acknowledgements.
- [Compaction persistence](../../../../../../host/internal/usecase/host/sessions/compaction.go) and [session cost calculation](../../../../../../host/internal/usecase/host/sessions/service.go): existing mutation and pricing owners.
- [Tools extension assembly](../../../../../../plugins/extension/tools/internal/app/app.go): executable plugin precedent.
