# PHS-06.1 system acceptance

## Scope and result

PHS-06.1 is complete. All eleven units of the [migration plan](migration-plan.md#sequential-implementation-units) are accepted. The final implementation commit is `9555cda`; the separate text correction is `10e1e49`, and the peer-close fixture correction is `4d720d9`.

The system supports an extension-owned compaction policy through the public contracts. The standard bundled policy, generator, default enablement and QST-01.1 remain PHS-06.2 work. This acceptance does not complete PHS-06 or authorize that next phase.

The [technical solution](solution.md#target-ownership) defines the ownership boundary. Earlier unit records contain the detailed [manual](manual-integration-evidence.md), [request-handler](request-integration-evidence.md), [generator](generator-integration-evidence.md), [result-handler](result-integration-evidence.md), [observation](observation-integration-evidence.md), [automatic preparation](automatic-integration-evidence.md) and [overflow](overflow-integration-evidence.md) evidence. Those records retain their original source and execution scope.

## User-visible contract

A registered extension controls manual compaction, automatic preparation and recovery from provider context overflow. Host invokes the captured policy owner and participants. The extension decides whether to preserve history, replace it or cancel. Host retains ordinary retry admission and scheduling. During an admitted overflow retry, Host keeps the original allowance. It repeats only the failed model request and preserves completed tool executions.

Session services preserve original entries and supply the replacement summary plus retained suffix to model requests. They validate the captured branch and tool groups, check the live runtime at persistence, and record the durable receipt before publication. A later observer failure does not erase that receipt.

The captured policy owner receives the actual successful attempt input and response before logical success. Configured model calls remain isolated from conversation compaction and observation. With no registered owner, ordinary requests proceed without compaction and manual compaction fails explicitly. An unavailable registered generation remains a failure.

Host and Agent Core contain no alternate compaction algorithm. Complete error text and local causes remain available across the affected paths. Host distinguishes current failures from earlier diagnostic causes when selecting the final category.

## Cleanup and preservation

During UNT-11, the implementer removed the `contextcompaction` package, its Host sizing and orchestration, process-local usage baseline, obsolete callback, retained-token setting and assembly connections. The runtime manager and transport adapters reuse `extensioncontext.Participant`, `Request`, `Result` and their action types. `HandlerSet`, `PolicyState` and `OutcomeInvocation` belong to that existing consumer. The manager and transport adapters use those values without compatibility aliases or duplicate DTO conversions.

Composition estimates and budgets remain extension-supplied public data. The [configuration guide](../../../../../guidelines/configuration.md) retains `compaction.participantTimeout`, its omitted default of five minutes and positive-value validation. Ordinary retry settings remain unchanged.

The session service's [`CaptureCompaction` and `CommitReplacement`](../../../../../../host/internal/usecase/host/sessions/manual_compaction.go) preserve coherent detached input and model-history projection. The migrated [commit suite](../../../../../../host/internal/usecase/host/sessions/compaction_commit_test.go) retains structural, persistence, publication and accounting assertions. Its accounting case checks 15 total tokens from the original model response and replacement source, counted once. Actual usage and cost accounting remain separate from the deleted token-estimation baseline.

The independent reviewer reconciled all 38 retired or renamed test declarations. Tests of removed Host policy were retired; retained contracts have current assertion paths. These include producer rejection with preceding state, candidate rejection before installation, generator bypass and clear-to-generator behavior, cancellation causes, committed failures, session boundaries and runtime shutdown. The successful rebuilt-input assertion now executes in the captured `Observe` expectation. No retained assertion was dropped merely because its former mock or method was removed.

The unit census changed from 1,741 to 1,682 leaf passes: 60 old leaves were removed and one renamed delivery-failure leaf was added. The integration census changed from 1,234 to 1,231 leaf passes after retiring three Host-orchestrator tests. Renaming the participant-timeout parsing method changed no test count.

## Peer-close fixture correction

A UI peer can violate the protocol by sending a new request after `CloseConnection`. Host returns `FailedPrecondition` and joins owned work. The original `TestRunOperationsPeerCloseFailsNewRequests` fixture made the close and forbidden request available without ordering Host's response. Host could finish protocol-failure cleanup first, after which the test waited indefinitely for an outbound Close that had not been sent.

The [corrected test](../../../../../../host/internal/infra/plugins/ui/runtime/operations_transport_test.go) supplies the late request only after mock Send observes Host Close. It retains every failure, status, cause, Close and no-preparation assertion. Production code, test timeouts and retry counts are unchanged.

Fresh race diagnostics passed 30,000 isolated target executions and 3,000 target executions among neighboring operation tests. The formerly failing shared selection then passed. In each complete final matrix, that target also passed 20 times in the shared selection. The observed timeouts are diagnostic failures. They do not supply assertion RED evidence. The intended fixture-order correction is eligible for the behavior-preserving test-refactoring exception.

## Final verification and reviews

The source identity for both complete final matrices was `75ea4cae63df86dcfc1b8125bfc0de9db61bcdb6117abec39f085d37e3d5c9e2`, with 1,281 file-map entries. The first matrix preceded the separate corrective and cleanup commits. A fresh verifier repeated all 68 commands after unit acceptance and commits at clean HEAD `9555cda`. Each command completed with exit 0 and unchanged source. No separate full matrix is claimed for the intermediate corrective commits alone.

Main and the independent reviewers checked exact command strings, raw Go events and complete source maps. Generation and formatting were stable, and fix dry-run produced no proposal. The final documentation closure changes no executable source.

The full-phase CodeQuality review covered 383 physical paths, including six cleanup-only deletions and two old rename paths beyond the 375-path phase manifest. Generated files and protected user-owned rules had explicit exclusions. TextQuality covered the changed documentation and comments. The four accepted earlier-phase wording corrections were applied before final checks. Nonunique blank identifiers in the raw declaration inventory were reconciled against file bytes and were not counted as source changes or review coverage.

A fresh Analyst accepted all 108 unit paths, test retirements and preserved caller paths. After the fresh verification repeat, an independent Critic examined cross-component behavior across all eleven units and found no additional source-grounded acceptance blocker. The coordinator accepted PHS-06.1 within the scope and limits in this record.

| Command | Result in each final matrix |
| --- | --- |
| `task generate`, twice | Passed with unchanged source |
| `task fmt` | Passed with unchanged source |
| `task fix_dry_run` | Passed; empty proposal |
| `task lint` | Passed |
| `task test` | Passed; uncached census below |
| `task itest` | Passed with the platform exclusions below |
| `task test-coverage` | Passed: 84.3%, required 80.0% |
| `task build` | Passed |
| `go list ./...` | Passed |
| `go list -tags=integration ./...` | Passed |
| `go build -tags=integration ./...` | Passed |
| `git diff --check` | Passed |
| `bash -c 'cd testdata/external-plugins && go list ./... && go build ./...'` | Passed; package and compilation evidence |
| `go test -json -race -count=1 -timeout=240s ./...` | 1,682 leaf and 243 parent passes; no failures or skips |
| `go test -json -race -count=1 -timeout=3600s -tags=integration ./...` | Two complete censuses; each has 1,231 leaf and 178 parent passes, seven skips and no failures |
| `go test -json -count=1 -run "^$" ./...` | Passed; compilation only |
| `go test -json -count=1 -tags=integration -run "^$" ./...` | Passed; compilation only |

The [automatic](automatic-integration-evidence.md#final-verification-and-acceptance) and [overflow](overflow-integration-evidence.md#final-verification-and-acceptance) preservation selections also ran. The removed package is absent from three shared selections: two unit selections and one integration selection. The migrated producer check now uses the public assertion for rejection with preceding state, with 20 executions per matrix:

```sh
go test -json -race -count=20 -timeout=1800s -tags=integration ./host/internal/app -run '^TestProgrammaticManualRequestHandlers$/^TestReplacementProducerIdentity$/^other-extension$'
```

The previously failing shared transport selection passed unchanged in both complete matrices:

```sh
go test -json -race -count=20 -timeout=1800s -tags=integration ./sdk/plugins/extension/v1 ./sdk/plugins/ui/v1 ./host/internal/controller/programmatic ./host/internal/infra/plugins/ui/runtime
```

The exact moved overflow-growth and exhaustion roots ran at count 20 and uncached count 1. The three public overflow/history-freeze roots each ran 20 times; four final-input source-partition cases each ran 200 times; seven client EOF cases each ran 200 times. The execution evidence is limited to those named scenarios.

A leaf is an executed test with no executed descendant in the same package. Parent tests and package events are counted separately. The untagged external-fixture `go test -json -count=1 ./...` found no tests and supplies compilation evidence only. No-test or unmatched packages inside targeted selections are not behavioral passes.

## Remaining limits and historical failures

The same seven tests were skipped in each explicit integration census:

| Test | Platform requirement |
| --- | --- |
| `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Darwin permission enforcement |
| `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Darwin `chflags` for immutable-file recovery injection |
| `TestLifecycleObserverThroughStandardTUI` | Darwin arm64 PTY execution |
| `TestStandardTUIHostSmoke` | Darwin arm64 PTY execution |
| `TestStandardTUIHostRuntimePersistenceFailures` | Darwin arm64 PTY execution |
| `TestEarlyFailureCleanup` | Darwin arm64 PTY execution |
| `TestStandardTUIPTY` | Darwin arm64 PTY execution |

Public reference-extension checks use production internal wiring and real runtime transport with substituted provider HTTP behavior. They do not establish live-model-service or bundled-algorithm behavior. Detailed public overflow evidence uses Programmatic submission. Headless/UI share inspected assembly and Core wiring, without a dedicated detailed overflow run for each mode. Preserve-specific overflow refusal has source, layer and composed-use-case evidence, without a separate public preserve test. Local error identity applies inside the Go process. Wire checks establish transmitted text and categories.

The accepted debt from storing the parent context in [`compactionOperation.context`](../../../../../../host/internal/usecase/host/extensioncontext/manual_operation.go) remains until an explicit-context parent-owner handoff preserves synchronous checks, complete causes, skipped-Run release and joins. The [production panic issue](../../../../issues/production-panic-elimination/ticket.md) remains separate work.

After 47 successful commands, the unordered peer-close test hung in the next command. The verification driver stopped when that command timed out. Its failed command and unrun rows are excluded from acceptance. The subsequent complete matrix and fresh phase repeat ran after the last correction. Unused intermediate matrix definitions remain distinct from executed evidence.

Historical evidence limitations remain recorded in the earlier unit documents. They include missing original RED records, unreserved diagnostics, unknown contents removed by a broad temporary-cleanup command, two source-manifest filenames that collided with active recorder results, and a derived reviewer JSON that was written again. Completed final raw logs and command/source maps remained available. Initial audit, selector, count-label and rename-accounting mistakes were corrected from those primary records. Passing final matrices does not reconstruct lost artifacts or make earlier handling compliant.

## Open questions

None for PHS-06.1. PHS-06.2 preparation and bundled implementation require separate authorization under the [migration plan](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension).
