# Completed-conversation observation integration evidence

Status: UNT-08 includes the collected corrections and the shared cancellation-settlement implementation. UNT-08 remains unaccepted. PHS-06.1 remains incomplete. Report-only code and text reviews precede the collected correction batch, full verification and fresh independent acceptance.

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

The public retry-then-cancel, current-cause/Core integration and prepared-adapter checks passed uncached after the correction. The public test checks canceled terminal delivery, aborted response/history with prior-provider text, response identity and usage, joined children, idle admission, a single observation and no subsequent provider or tool execution. Model-execution tests check private replay, original internal error identities and no completed-usage accounting write. These earlier cancellation-correction results apply to source `1dba4b5119ff37be55557895defa271471df9e2e7ae785ed38e374e00f9ab727`. Full final verification and independent acceptance are separate completion requirements.

## Shared cancellation settlement and transport

When an observer's remote work settles before Host requests cancellation, the extension can reject that request with `TARGET_NOT_ACTIVE`. The shared runtime settlement method resumes the original operation's wait after local interruption and concurrently waits for cancellation. Host joins both operations. Host retains that rejection as diagnostics only after receiving the original operation's completed, canceled, failed or rejected terminal. When Host receives no original terminal, Host keeps the rejection among the causes that determine the outcome. Callback errors remain current even when the callback returns an SDK terminal-shaped error.

Host decodes acquired completed payloads during interrupted settlement. Host retains the established mapping for an ordinary handler error. Host treats a malformed success payload as a protocol failure. Handler, tool, lifecycle and compaction calls use the same settlement method. The deterministic peer tests withhold the original terminal until cancellation arrives, then exercise both terminal wire orders. They also exercise canceled cleanup, independent original failure or rejection, malformed payloads and a missing original terminal.

The observation fixture returns a configured child's error through the SDK outcome classifier. Its explicit observer-failure mode still returns a declared handler error. After Host began decoding acquired completions during settlement, the public cancellation assertion initially showed that the fixture converted child cancellation into an ordinary handler error. Returning the child error through the SDK restored canceled delivery while preserving the independent-failure assertion. The public test also checks prior-provider text and cancellation text in `Canceled.message`.

The root `internal/errtree` package supplies current-cause traversal and cancellation classification. Its `diagnostic` subpackage constructs the error boundary. The extension SDK classifier preserves current coded failures and their full text. UI SDK initialization and cancellation retain their closure-filtering policy. When the UI cancellation operation fails, the UI SDK retains the unfiltered acquired source. Callers can still find original local causes through `errors.Is` and `errors.As`.

All five terminal encoders write complete canceled source text to the shared `Canceled.message` field. All four event mapper families retain the received message. Extension and UI SDK terminal readers, UI notifications and Host UI initialization preserve received diagnostics with local cancellation identity. Programmatic clients receive the raw field. Go error pointer identity remains local to each process. When a local SDK wait is interrupted, the SDK returns its local cancellation immediately. A later wait with a live context can consume the queued remote terminal under the existing ownership contract.

TUI displays diagnosed cancellation as information and clears foreground ownership. Plain cancellation remains quiet. The production `operation.Owner` retains its established acknowledgement and source-retirement behavior. Host continues to exclude configured execution from conversation observation and enforce synchronous live commit guards. Core's selected-outcome rules are unchanged. Host restores received transport cancellation identity on the path described below.

With corrective overlays that compiled, the test runner reported assertion failures for these changes: clearing canceled text in each actual sender or mapper, skipping completed-payload decoding, or accepting inactivity without an original terminal. These overlays show test sensitivity after implementation; they do not reconstruct first-ever edit order. Compile/setup failures and every historical verification attempt remain in the retained evidence. The extended final matrix contains the earlier preservation commands and additional commands for shared traversal, SDK/UI/TUI, serialized canceled transport and deterministic settlement repetitions. Its final post-review execution and independent acceptance remain pending.

## Source-specific preparation results

The earlier cancellation-correction commands below exited 0 without changing source `1dba4b5119ff37be55557895defa271471df9e2e7ae785ed38e374e00f9ab727`:

- `go test -json -count=1 -timeout=180s -tags=integration ./host/internal/app -run "^TestConversationObservation$/Test(CanceledObservation.*|HeldObservation)$"`
- `go test -json -count=1 -timeout=180s -tags=integration ./host/internal/usecase/host/modelexecution -run "^TestCoreCurrentCancellationRetainsDiagnostics$"`
- `go test -json -count=1 -timeout=180s ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/programmatic ./host/internal/usecase/host/ui ./host/internal/controller/ui ./internal/operation -run "^Test(CurrentCancellationRetainsRecoveredDiagnostics|Observation.*|RunPreparedClassifiesCancellationWithAndWithoutIndependentFailure|PreparedCancellationClassifiesPureAndMixedErrors|HostPreparedRetainsCanceledSource|OwnerRetainsCanceledSourceBeforeReleaseAndTerminal)$"`

On prepared source `1ec171575514bb9b4b74df6244ed66554146d6a8ff3ee85e2ddf7eeeebddd465`, six targeted preparation commands exited 0 without changing the source. The five behavioral runs reported the following leaf test pass events, including repetitions, with zero leaf skips and failures. These counts are events, not distinct scenarios. The external plugin package-list/build check was compile-only.

| Targeted run | Leaf pass events |
| --- | ---: |
| Affected unit packages | 971 |
| Shared SDK/runtime/controller/Core integration | 324 |
| Settlement and callback tests, count 20, CPU 1 and 2 | 2,320 |
| Public observation suite, count 20 | 280 |
| Manual, UI cancellation, threshold-boundary, configured-mode and credential-preflight selections | 127 |

Exact commands for that prepared source:

- `go test -json -race -count=1 -timeout=240s ./internal/errtree/... ./internal/operation ./sdk/plugins/extension/v1 ./sdk/plugins/ui/v1 ./host/internal/usecase/agent/run ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/contextcompaction ./host/internal/usecase/host/extensioncontext ./host/internal/usecase/host/extensionmodels ./host/internal/usecase/host/sessiontree ./host/internal/usecase/host/programmatic ./host/internal/usecase/host/ui ./host/internal/controller/extension ./host/internal/controller/programmatic ./host/internal/controller/ui ./host/internal/infra/plugins/ui/runtime ./plugins/ui/tui/internal/usecase/presentation`
- `go test -json -race -count=1 -timeout=300s -tags=integration ./sdk/plugins/extension/v1 ./sdk/plugins/ui/v1 ./host/internal/controller/programmatic ./host/internal/infra/plugins/ui/runtime ./host/internal/infra/plugins/extension/runtime ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/extensioncontext ./host/internal/usecase/host/contextcompaction ./host/internal/usecase/agent/run`
- `go test -json -race -count=20 -timeout=600s -cpu=1,2 -tags=integration ./host/internal/infra/plugins/extension/runtime -run '^Test(RuntimeCancellationSettlement|RuntimeCallbackProvenance)$'`
- `go test -json -race -count=20 -timeout=1800s -tags=integration ./host/internal/app -run '^TestConversationObservation$'`
- `go test -json -race -count=1 -timeout=600s -tags=integration ./host/internal/app -run '^Test(.*Manual.*|UICancellationFixtureRetainsRequestFailure|ThresholdCompactionPreservesSplitBoundaryCause|PublicConfiguredRequestAcrossApplicationModes|ProgrammaticSummaryPreflightsCredentials.*)$'`
- `bash -c 'cd testdata/external-plugins && go list ./... && go build ./...'`

These preparation results precede the received-transport correction below. The 56-command final post-review matrix and fresh independent acceptance remain pending on the corrected source.

## Received transport cancellation

When the original SDK operation receives a gRPC `codes.Canceled` connection error, Host retains the original status and diagnostic text and projects local `context.Canceled` identity. The projection applies to initial and resumed original receive paths. Host retains `ErrExtensionUnavailable`, so the mixed unavailable error remains a public failure. Host does not infer transport origin from a progress callback's error shape. The SDK's local-wait and remote-terminal semantics are unchanged.

[`Runtime.waitForOperation`](../../../../../../host/internal/infra/plugins/extension/runtime/settlement.go) applies the projection to received original connection errors. [`Runtime.Execute`](../../../../../../host/internal/infra/plugins/extension/runtime/runtime.go) preserves that identity for the Host tool adapters. Core's [`Service.executeCalls`](../../../../../../host/internal/usecase/agent/run/service.go) uses the restored signal to select its established Aborted run outcome and suppress later tool and model work. Core keeps the already-selected ToolUse model response outcome.

`TestRuntimeReceivedTransportCancellationIdentity` compiled and failed on source `23ff7ca4058271a58bb1a1adf5ca079d4c80695b9a769d679fbdf3124830730e`: status, complete text, live caller and unavailable assertions passed, while cancellation identity failed. The runtime assertions passed after projection on source `8e3438af3ed726a04ca99fcd024cddecb07869f75455330bc135a135d9dc121d`. Both initial and resumed original waits use a real helper process and gRPC stream; the resumed case also checks the independent callback source.

The public `TestReceivedTransportCancellationStopsCore` uses the real application, Core, Host tool adapters, extension Runtime and gRPC peer. Only the external model HTTP transport is replaced. A corrective overlay that removed the projection compiled and failed an Aborted-run assertion. This overlay shows causal sensitivity after implementation, not first-ever edit order.

On source `0333a6429ef29f2c74fbad2876cfd175c2664ea22f07017a50f11c48a0d0f7f3`, the public test passed three repetitions with `go test -json -race -count=3 -timeout=300s -tags=integration ./host/internal/app -run "^TestReceivedTransportCancellationStopsCore$"`. The test checks a live caller, two finalized requested calls, execution of only the first call, one model request, the Aborted run summary with received diagnostics, the selected ToolUse response and a failed public terminal with those diagnostics. This is entry-point evidence for the received transport scenario. It does not establish universal preservation or final acceptance.
