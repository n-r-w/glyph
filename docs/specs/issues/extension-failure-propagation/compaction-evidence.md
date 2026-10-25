# Compaction failure verification

## Scope and baseline

UNT11 changes independent postcommit compaction failures to Failed terminals with the existing typed committed result. UNT12 final verification and both issue closures remain pending.

The baseline is `a2bbf27e93b61e22371ad3763b790627995d6c3e`. The worktree was clean. Baseline `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build` each exited 0. Combined coverage was 84.7%, above the 80.0% threshold. Go and dependency versions remain unchanged.

## Source paths

`commandPrepared.Run` in `host/internal/usecase/host/programmatic/prepared.go` returns Failed with the actual compaction response after independent failure. `preparedUIOperation.Run` in `host/internal/usecase/host/ui/prepared_operations.go` returns Failed with the frame retained by `compactionCompletionFrame`. Both producers keep committed completion for pure owning cancellation, including an expired owner deadline. Both producers use the current-cause classifier to keep independent failures Failed when owning cancellation is also present.

`mapCompactionCompletion` in `host/internal/controller/extension/service.go` returns the typed `CompactResult` alongside the classified error after independent committed failure. `contextOperation.runManualCompaction` in `host/internal/controller/extension/context_actions.go` and `contextOperation.runPolicyCandidate` in `host/internal/controller/extension/compaction_candidate.go` preserve both values. Diagnostic fields retain complete error text and category alongside the Failed terminal.

The existing D3 transport remains unchanged:

- `streamDelivery.Terminal` in `host/internal/controller/programmatic/delivery.go` maps Failed with its typed result.
- `hostPrepared.Run` and `hostDelivery.Terminal` in `sdk/plugins/extension/v1/host_owner.go` retain and deliver the Extension result with failure.
- `Operation.waitResult` in `sdk/plugins/ui/v1/host.go` and `contextOperation.wait` in `sdk/plugins/extension/v1/context_initiator.go` return the retained result and remote error.
- `CompactionOperation.Wait` and `CompactionOperation.WaitWithProgress` in `sdk/plugins/extension/v1/context.go`, and `CompactionReplacementOperation.Wait` in `sdk/plugins/extension/v1/manual_policy.go`, already preserve result-plus-error. UNT11 needs no SDK settlement or typed-wait change.
- `Controller.Notify` in `plugins/ui/tui/internal/controller/plugin/controller.go` forwards a retained failed result and its error. The original compaction decoder discarded the saved entry. The [consumer correction evidence](compaction-consumer-evidence.md) records the corrected receipt mapping, identity-based application, and in-process TUI regressions.

`Service.runPolicy` in `host/internal/usecase/host/extensioncontext/manual.go` continues to own receipt retention and child shutdown. `Service.finishPolicyOutcome` in `host/internal/usecase/host/extensioncontext/policy_outcomes.go` continues to stop the observer chain. `Service.prepareCompactionAdmission` in `host/internal/controller/extension/context_admission.go` retains the manual compaction gate and recursive-compaction rejection. UNT11 changes none of these owners or algorithms.

## Identical-input RED and GREEN

This section records the preserved producer-slice executions before the TUI consumer correction. These runs do not establish TUI receipt application. The [consumer correction evidence](compaction-consumer-evidence.md) records the new regressions and final source checks.

Complete baseline and corrected source captures include root and external-plugin `go.mod` and `go.sum`. The regression inputs contain every changed test and external fixture. Before and after each preserved execution, baseline source outside those inputs was checked against the Git baseline, and each regression input was checked against corrected source. The corrected source manifest was also checked before and after project checks. Source and input identities, actual working directories, command-local temporary directories, exits, and complete outputs are retained in the execution ledger outside repository prose.

| Repository file and test symbol | Executed baseline assertion | Corrected outcome |
| --- | --- | --- |
| `host/internal/app/manual_compaction_integration_test.go`, `programmaticManualCompactionTerminalStatesSuite.TestScenario` | Six independent postcommit cases lacked a Failed terminal. | Failed contains the typed committed result and complete cause. Stored originals and exactly one marker remain available. |
| Same file, `uIManualCompactionTerminalStatesSuite.TestScenario` | Six independent postcommit cases reported `Failed=false`. | A real UI SDK process receives a nonnil error together with the retained receipt. |
| `host/internal/app/manual_compaction_extension_integration_test.go`, `extensionManualCompactionTerminalStatesSuite.TestScenario` | Five independent postcommit cases reported `Failed=false`. | A real Extension SDK process receives error plus typed receipt. |
| `host/internal/app/compaction_failure_nested_integration_test.go`, `TestConsumedPostcommitCompactionFailsSelection` | The child SDK report had `Failed=false`. | A legal reasoning-selection observer consumes the child failure and returns success. Host still fails the parent, preserves the receipt and stored marker, stops the later selected observer, makes no extra provider request, and permits an independent healthy compaction on the same extension. |
| `host/internal/controller/extension/service_test.go`, `TestMapCompactionCompletionPreservesCommittedFailure` | Publication, observer, and joined cases returned nil error. | Original causes survive `ErrorIs`; complete text, category, and receipt survive together. |
| `host/internal/usecase/host/ui/compaction_integration_test.go`, `TestManualCompactionRunsThroughUIOperationOwner` | The operation owner emitted Completed. | The owner emits Failed with original cause, category, receipt, and released gate. |
| `host/internal/usecase/host/programmatic/compaction_terminal_integration_test.go` and `host/internal/usecase/host/ui/compaction_cancellation_integration_test.go`, `TestCommittedCompactionTerminalCancellation`; `host/internal/controller/extension/compaction_cancellation_test.go`, `TestCommittedCompactionCancellation` | Each mixed committed-failure case returned no independent terminal error. Pure owning cancellation and expired deadlines already passed. | Mixed failure remains Failed with receipt and original cause. Pure owning cancellation and expired deadlines retain committed completion. |

The baseline Programmatic assertions stop at the missing Failed terminal; they do not execute the later retained-result assertions. The baseline UI and Extension tests stop at the `Failed` assertion, before checking the retained receipt. GREEN executes the receipt, complete-cause, persisted-state, and follow-up assertions. These are behavioral failures, not compiler or no-matching-test results.

The postcommit matrices include policy failure, observer failure, observer unavailability, lost policy diagnostic state, invalid diagnostic state, and handled earlier selection rejection followed by observer failure. Their precommit failure, valid Cancel, handled rejection, and healthy scenarios also pass. `assertManualOutcome` in `host/internal/app/manual_compaction_outcome_fixture_test.go` checks the actual observer outcome and retained diagnostic state. No replacement receipt is invented before commit.

The preserved focused command configurations are:

```sh
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^Test((Programmatic|UI|Extension)ManualCompactionTerminalStates|ConsumedPostcommitCompactionFailsSelection|(Programmatic|UI|Extension)ManualCancellationStopsObserverModel)$' -v
go test -race -count=1 ./host/internal/controller/extension -run '^Test(MapCompactionCompletionPreservesCommittedFailure|CommittedCompactionCancellation)$' -v
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/usecase/host/programmatic ./host/internal/usecase/host/ui -run '^Test(ManualCompactionRunsThroughUIOperationOwner|CommittedCompactionTerminalCancellation)$' -v
```

Each command ran from its source root with a short command-local `TMPDIR` outside module roots. The three baseline commands each exited 1 with the behavioral assertions above. The three corrected commands each exited 0. The ledger preserves the concrete invocation prefix and working directory for each execution.

## Fixture corrections and failed development attempts

`outcomeOperation.Run` in `testdata/external-plugins/cmd/compaction-policy/outcome.go` previously converted configured-model cancellation into a string-valued `HandlerError`. The recorded development integration run failed the Programmatic and Extension observer-cancellation tests. The fixture now returns the original error through the SDK terminal path. The existing Programmatic, UI, and Extension owning-cancellation process tests pass with that exact input.

`nestedCompactionObserver.Run` in `testdata/external-plugins/cmd/compaction-policy/nested_compaction.go` initially waited with the consuming invocation's context. In full-suite execution, Host cancellation of that parent could win the SDK wait before delivery of the child's typed terminal. The fixture now awaits the child through an independent caller context, as the consumed-selection fixture does. The binding still carries the ordinary parent invocation identity; Host retains admission, cancellation, failure retention, and joining. Ten race-enabled repetitions passed after the correction, followed by the identical-input focused checks and project checks.

Development failures remain distinct from final evidence:

- A recorded Programmatic cancellation setup run did not compile because it referenced helpers excluded by integration tags. That run is a setup failure, not behavioral RED.
- The next run reached its assertions but failed a cleanup expectation: closing the acknowledgement writer returns cancellation. The cleanup expectation now checks that result.
- Intermediate lint runs rejected fixture path writes, then reassignment, shadowing, line length, and redundant conversions. The fixture reuses `writePolicyArtifactBytes`, and the final names and formatting pass lint.
- A recorded integration run retained an obsolete UI owner expectation of Completed. That assertion now requires Failed and the original cause.
- Two later full checks exposed the nested caller-context race described above. Their failures are preserved; final claims use corrected identical inputs.

## Project checks and preservation

The producer-slice source passed all required commands: `task generate`, `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build`. Each exited 0 with a short command-local `TMPDIR` outside module roots. `task fix_dry_run` proposed no changes, so `task fix` was unnecessary. Combined coverage is 84.7%, required 80.0%. Root and external-plugin module files match the baseline.

The full integration suite reuses automatic preparation, overflow recovery, transient-provider retry, selected compaction participant stopping, and cancellation coverage. `Service.execute` in `host/internal/usecase/host/modelexecution/retry.go` remains unchanged; a provider retry does not recover an extension failure. The existing policy tests remain unchanged. The implementation adds no session mutation authority, recursive manual compaction, duplicate SDK settlement, retry policy, protocol schema, dependency, commit, or push.

## Limits and remaining work

The active Go index excludes integration-tagged files from reference queries. Symbol overviews and reference queries were attempted; source search and compiled integration executions supplied the fallback. This record makes no exhaustive LSP-reference claim.

The original TUI coverage did not verify application of a receipt from a retained terminal result. The [consumer correction evidence](compaction-consumer-evidence.md) records the new component tests. UNT11 adds no terminal-driving PTY fixture. The public compaction processes verify Programmatic, UI SDK, and Extension SDK boundaries. The legal nested scenario verifies Host retention through the Extension Contract without changing tool-time compaction admission.

No outside-task defect was identified. UNT12 final verification and closure of extension failure propagation and production panic elimination remain pending.
