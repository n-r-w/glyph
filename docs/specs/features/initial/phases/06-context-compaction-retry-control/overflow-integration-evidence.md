# Overflow recovery integration evidence

## Scope

UNT-10 is accepted at `904463b`. The separate shared retry-cancellation correction is `33163d6`. Both commits contain parts of the same reviewed and verified source. This record closes the overflow unit of the [migration plan](migration-plan.md#sequential-implementation-units); UNT-11 and PHS-06.1 final acceptance remain pending.

The [ticket](ticket.md) and [technical solution](solution.md) define the ownership boundary. The reference extension supplies policy behavior for these checks. The bundled policy and QST-01.1 remain in PHS-06.2.

## Implemented request path

A user submits a conversation request. Host captures the policy owner and participants before provider dispatch. If the provider rejects context size and the ordinary retry policy admits another attempt, Host invokes that captured owner with a fresh session snapshot and the complete permitted provider error text.

The extension chooses whether to preserve history, cancel, or request replacement. When the extension chooses to preserve history, Host declines overflow recovery with `CONTEXT_LIMIT`. Host sends neither a replacement notification nor another provider attempt. For replacement, session services validate the candidate, preserve original history and record the durable receipt before publication. Host waits for outcome handlers, captures the resulting outgoing history and invokes the same owner's final-input check with lifecycle-only authority.

Host changes only the retained provider request's history and schedules the admitted retry. Another admitted overflow reaches the same captured owner. The generic retry allowance remains the limit on provider attempts. On success, Host supplies the actual successful attempt input and response to the captured observer before reporting logical success.

The [retry owner](../../../../../../host/internal/usecase/host/modelexecution/retry.go), [captured recovery](../../../../../../host/internal/usecase/host/extensioncontext/conversation_recovery.go) and [shared preparation](../../../../../../host/internal/usecase/host/extensioncontext/conversation_preparation.go) implement this path. All three application assemblies use the captured-policy dependency. Configured model calls remain outside conversation preparation, recovery and observation.

## Public and boundary assertions

| Scenario | Evidence and scope |
| --- | --- |
| A tool completes, then the following model continuation overflows twice within one user request | [TestRepeatedOverflowPreservesCompletedTool](../../../../../../host/internal/app/overflow_conversation_integration_test.go) uses public Programmatic submission and production internal wiring with a substituted provider HTTP transport. It checks four provider calls, two completed model messages, one stored tool result, two replacement receipts and original user history. An append-only tool journal has one entry before the first overflow and remains unchanged after completion. |
| The extension retains completed tool history during both replacements | The same public test checks one matching tool call/result pair in each continuation. The reference policy retains the original User boundary. Retaining that pair in history is separate from executing the tool again; the journal checks execution count. The second policy invocation includes the preceding receipt, and the final observation includes both observer messages and the replacement summary. |
| An outcome handler grows context after durable replacement | [TestRecoveryRetainsCommitAndRejectsRealObserverContextGrowth](../../../../../../host/internal/app/compaction_growth_integration_test.go) checks public failure, complete provider and final-check text, retained receipt and original history, and suppression of another provider attempt. |
| The owner preserves history, the retry allowance ends, or a captured binding changes | [Captured recovery tests](../../../../../../host/internal/usecase/host/extensioncontext/conversation_recovery_test.go), [retry tests](../../../../../../host/internal/usecase/host/modelexecution/context_preparation_test.go) and [overflow integration tests](../../../../../../host/internal/usecase/host/extensioncontext/overflow_execution_integration_test.go) check the layer contracts and composed production use cases. Preserve-specific refusal is established by these checks and the source path; it has no separate public overflow-preserve test. |
| Final-input rejection coexists with cancellation or an independent invocation failure | [TestOutgoingInputRetainsSourcePartition](../../../../../../host/internal/infra/plugins/extension/runtime/outgoing_input_integration_test.go) exercises the real runtime/gRPC boundary. Its four cases distinguish semantic rejection, missing acknowledgement, owning cancellation and independent invocation failure. |
| Commit, observer or runtime failure accompanies overflow | [Commit-source tests](../../../../../../host/internal/usecase/host/extensioncontext/conversation_recovery_commit_test.go) and [observer-source tests](../../../../../../host/internal/usecase/host/extensioncontext/conversation_recovery_outcome_test.go) check receipt authority, complete local causes and category selection with generated mocks. Current persistence failures take precedence; historical diagnostic branches do not select the current category. |

The public invocation and observation assertions exclude provider-private replay bytes while retaining their size metadata where required. Detailed public overflow evidence uses Programmatic submission. Headless and UI share the reviewed assembly and Core path; this record claims no dedicated public overflow execution for each of those modes.

## Shared cancellation correction

The retry owner retains earlier provider causes as diagnostic history when cancellation interrupts retry-progress delivery or the pending delay. The current outcome remains owning cancellation. Independent provider and delivery failures keep their categories and causes.

[Retry cancellation tests](../../../../../../host/internal/usecase/host/modelexecution/retry_cancellation_test.go) cover ordinary and configured calls at both exits and a custom cancellation cause. Seven restored regression bodies retain their accepted category and cause assertions without changes. The new table represents the two former plain progress/delay cases and adds ordinary-request coverage.

Neutral policy-operation annotations contain source categories and complete causes. Only `capturedConversation.RecoverOverflow` converts these annotations to the existing preparation-failure interface. Shared automatic and manual callers therefore retain their category contracts. Host's `selectPolicyOperationCategory` in [source selection](../../../../../../host/internal/usecase/host/extensioncontext/policy_operation_error.go) inspects current error nodes directly, so it does not search historical diagnostic branches. The two `errorlint` suppressions permit these direct-node checks.

## Final verification and acceptance

The verified source identity is `469f7a5fd7c82c63010fa64100d1dfb8ee58d204dd4420a8e97f8c27f854ca25`, with 1,299 file-map entries. Main and a fresh independent Analyst reviewed all 54 changed paths and relevant callers. The Analyst found no remaining source-grounded defect. Main accepted the unit under the approved unit-level authority.

All 68 final command records completed with exit 0 after the last code edit. Main and the Analyst compared all 136 command before/after maps with the accepted source and checked executed selectors. Generation and formatting kept source unchanged; fix dry-run proposed no changes. The separate corrective commit and the overflow commit preserve those checked working-tree bytes; no separate full matrix is claimed for the intermediate corrective commit alone.

| Command | Final result |
| --- | --- |
| `task generate`, twice | Both passed with unchanged source |
| `task fmt` | Passed with unchanged source |
| `task fix_dry_run` | Passed; empty proposal |
| `task lint` | Passed |
| `task test` | Passed; uncached execution is reported separately below |
| `task itest` | Passed with the platform exclusions below |
| `task test-coverage` | Passed: 84.2%, required 80.0% |
| `task build` | Passed |
| `go list ./...` | Passed |
| `go list -tags=integration ./...` | Passed |
| `go build -tags=integration ./...` | Passed |
| `git diff --check` | Passed |
| `bash -c 'cd testdata/external-plugins && go list ./... && go build ./...'` | Passed; package and compilation evidence |
| `go test -json -race -count=1 -timeout=240s ./...` | 1,741 unit leaf passes, no failures or skips |
| `go test -json -race -count=1 -timeout=3600s -tags=integration ./...` | Two complete censuses; each has 1,234 leaf passes and seven skips, with no failures |
| `go test -json -count=1 -run "^$" ./...` | Passed; compilation only |
| `go test -json -count=1 -tags=integration -run "^$" ./...` | Passed; compilation only |

The captured-recovery selection passed 6,000 leaf executions across 30 cases:

```sh
go test -json -race -count=100 -timeout=300s -cpu=1,2 ./host/internal/usecase/host/extensioncontext ./host/internal/usecase/host/modelexecution ./host/internal/infra/plugins/extension/runtime -run "^Test(CapturedOverflow.*|CanceledOverflow.*|Service.*Overflow.*|RetryCancellationRetainsProviderCause|PolicyInput.*)$"
```

The three public overflow/history-freeze tests each passed 20 executions:

```sh
go test -json -race -count=20 -timeout=1800s -tags=integration ./host/internal/app -run "^Test(RepeatedOverflowPreservesCompletedTool|RecoveryRetainsCommitAndRejectsRealObserverContextGrowth|AutomaticFinalCheckFreezesOutgoingHistory)$"
```

The four final-input source-partition cases each passed 200 executions:

```sh
go test -json -race -count=100 -timeout=1800s -cpu=1,2 -tags=integration ./host/internal/infra/plugins/extension/runtime -run "^TestOutgoingInputRetainsSourcePartition$"
```

The [accepted preservation checks](automatic-integration-evidence.md#final-verification-and-acceptance) also ran on this source. The migrated overflow-growth and exhaustion tests now execute in `extensioncontext`; Main and the Analyst checked each test in both the count-20 and uncached integration selections. Restored cancellation regressions execute in the uncached unit census and the count-20 shared retry selection.

Leaf counts exclude suite ancestors and package events. The external fixture module's untagged `go test -json -count=1 ./...` found no tests and supplies compilation evidence only. Package-level no-test results and unmatched packages inside targeted selections are separate from behavioral counts.

## Evidence limits and retained failures

The first matrix launcher reached its configured 3,600-second tool timeout during command 50. The preceding 49 complete command records retained the accepted source. A fresh continuation reran the interrupted command and completed the remaining 18 commands without changing source, test timeouts, repetition counts or selectors. The interrupted record and partial output remain excluded from passes. This was a chosen launcher timeout, not a platform-wide one-hour limit.

Thirteen retained uncached, compiling assertion-failure records establish the behavioral RED evidence. The recovery test already required two owner invocations under its former one-overflow name; independent reconstruction matched the recorded file hash. Restored tests, private renames, comment edits and generated output have the documented characterization or refactoring exceptions. Later green runs do not establish unrecorded first-ever test-first ordering.

Compile/setup failures, source-classification RED runs, lint failures and incorrect test-oracle attempts remain retained. The single-request test initially inherited an incorrect expectation that rebuilt history would omit the completed tool. A temporary fixture boundary at the tool result split the call/result pair and was removed. The final test follows the unchanged User-boundary policy and checks invocation count separately. An audit initially expected behavioral tests in the untagged external fixture module; the corrected audit identifies that command as compilation only.

The seven [platform exclusions and earlier evidence limits](automatic-integration-evidence.md#evidence-limits-and-retained-failures) remain. Darwin permission, `chflags` and PTY behavior is unverified. The earlier broad temporary-cleanup incident and missing original RED records remain historical limitations. These final checks establish neither deleted temporary contents nor retrospective process compliance.

The reference extension and substituted provider transport establish system integration. Live-model-service behavior and the future bundled policy remain outside these checks. Local error-identity assertions apply within the Go process; public wire checks establish transmitted text.

## Open questions

None for UNT-10. The [migration plan](migration-plan.md#sequential-implementation-units) identifies the remaining work.
