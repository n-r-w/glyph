# Completed-conversation observation integration evidence

Status: UNT-08 has the collected code and text corrections and remains unaccepted. PHS-06.1 remains incomplete. Closure requires full verification of the corrected source and fresh independent acceptance.

## Scope

The [target solution](solution.md#target-ownership) defines ownership and observation contracts. This slice includes the integration of logical conversation capture, successful provider completion, dedicated policy observation, nested-work settlement, and Core/session terminal delivery through the public protocol and SDK. Headless, UI and Programmatic assemblies use the same capture dependency.

The private provider completion contains the final attempt input, response and prior provider causes. Host invokes the captured policy-owner runtime generation for observation. The policy owner uses ordinary issued session/model access. During observation, the policy owner has no compaction mutation, projection or participant authority. The public mapper preserves response content, identity, diagnostics and optional normalized usage while omitting replay bytes and reporting raw private byte counts.

Configured-model calls, including generator and observer-owned requests, remain outside conversation observation. The directly renamed internal accounting writer, `RecordCompletedConversationUsage`, remains for the [unmigrated automatic and overflow consumers](migration-plan.md#decomposition-justification). Its delivery condition and exact-prefix/session-incarnation checks remain in place. No standard extension baseline, sizing or summary-packing algorithm is implemented here.

## Public assertions

`TestConversationObservation` uses real application, runtime, SDK and session wiring with only the external provider replaced. The tests check:

- Held observation before logical success and persistence, preserved response/usage/causes on failure, and prior failed provider attempts.
- Rebuilt overflow input and committed observer messages, plus absent, multiple and disappeared owners.
- Owning cancellation, independent nested failure and acknowledged failed nested work.
- Absent versus present-zero usage and the Length outcome, when a response reaches the provider response limit.
- The ToolUse outcome, when the model requests finalized tool calls, with Host skipping tool execution when observation fails.
- Visible reasoning and private-byte exclusion.

The generator image regression test also checks that Host invokes no conversation observer for the generator's configured request.

The affected-layer tests cover isolation from provider mutation, private replay and diagnostics, precedence for classified cancellation, categorized session causes, joining during normal teardown, and rejection of child admission after the observation lifetime ends.

## Post-shutdown preservation

Under the approved O6-1 rule, Host returns a terminal error when a registered owner is unavailable. Five manual terminal-state follow-ups previously expected conversation success after deliberately shutting down that owner. Their follow-up expectations now require logical failure, unavailable-owner diagnostics and the exposed failure category. After the failed follow-up, the tests also check that Host has not dispatched the provider for that follow-up, the run is idle, and durable state remains.

The deliberate shutdown and original manual receipt, diagnostic and original-history assertions remain. For committed scenarios with a live owner, the tests through Programmatic, UI and Extension routes check the summary projection in the next provider request. The UI helper preserves its original manual outcome report. Protected cancellation/shutdown fixtures are unchanged. No reload, owner filtering, fallback or fresh-host restart was added.

## Preparation evidence

The initial test of the public assertion compiled and failed because the owner received no observation. For uncached causal test runs, temporary source changes then removed observation, owner capture, SDK payload acceptance, successful-input propagation and Length observation, or incorrectly added observation to configured execution. The corresponding assertions failed. The mutation scripts restored each changed source file. These runs show causal behavior. They do not establish first-ever edit order.

Fresh assertion RED/GREEN also covered acknowledged failed nested work and classified logical/session failure precedence. The original preservation failure is retained as evidence of obsolete follow-up expectations. It does not qualify as implementation RED. A separate temporary overlay that treated an unavailable registered owner as absent made all five migrated follow-up assertions fail. Restoring the approved owner rule made those cases pass.

On prepared source `8c81480fe2144ad8396bd0f4e43488a301e2ba8e8e4aa5b98733b5890d9fb05e`, the following targeted commands exited 0. The public observation run had 45 Test run/pass events across three repetitions; manual preservation had 137. Neither had a Test skip. These counts include suite ancestors.

Targeted preparation commands:

- `go test -json -race -count=3 -timeout=180s -tags=integration ./host/internal/app -run ^TestConversationObservation$`
- `go test -json -race -count=1 -timeout=240s -tags=integration ./host/internal/app -run "^Test(.*Manual.*|UICancellationFixtureRetainsRequestFailure|ThresholdCompactionPreservesSplitBoundaryCause)$"`
- Affected model-execution, accounting, context, runtime, session, startup, controller and SDK unit packages with uncached race checks.
- Shared runtime/SDK/session/Core integration packages and configured application modes.
- Generation stability, formatting, empty fix dry-run and scoped lint.

Codex unit/integration preservation, the public reference fixture, interface guard and both compile-only build variants passed on the earlier preservation-resume source. Those checks are separate from final post-correction verification. Compile-only `-run ^$` commands are not behavioral evidence.

Command logs and exact file maps for failed, skipped, compile/setup, mutation and historical attempts are retained in temporary local storage outside the repository. This document records commands, results and source identities; the raw records are not included in the repository. Before UNT-08 can close or UNT-09 can start, the collected correction batch, full post-review project verification and fresh independent acceptance must be complete.

## Collected cancellation correction

The prepared source lost earlier provider-attempt diagnostics when the client canceled during observation. A layer-only probe reproduced that loss. The extended public `TestCanceledObservation` then failed an uncached assertion because its canceled response contained only the fixed abort label and omitted the earlier provider cause.

The corrected model-execution service supplies a neutral current-cause boundary. Core determines terminal disposition from current causes and preserves recovered diagnostics in response and agent-event text. Callers can use `errors.Is` and `errors.As` to find the original internal causes. Independent current siblings and classified wrappers remain failures. When later terminal validation, persistence or delivery fails, Core keeps the selected response outcome. The prepared adapters classify independent later failures as failed operations.

Programmatic and UI prepared adapters retain diagnosed cancellation through `Outcome.SourceError`. UI outcome conversion preserves that source. Plain cancellation keeps its existing fixed text and source-free outcome.

The public retry-then-cancel, current-cause/Core integration and prepared-adapter checks passed uncached after the correction. The public test checks canceled terminal delivery, aborted response/history with prior-provider text, response identity and usage, joined children, idle admission, a single observation and no subsequent provider or tool execution. Model-execution tests check private replay, original internal error identities and no completed-usage accounting write. These targeted results establish the correction's behavior; full final verification and independent acceptance are separate completion requirements.
