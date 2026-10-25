# Navigation Failure Propagation Verification

## Scope and baseline

This record covers UNT10 under the [implementation supplement](solution.md#session-tree-navigation). Compaction producers and final issue closure remain UNT11 and UNT12. Both cross-cutting issues remain in progress.

The baseline is revision `4e4141bd8536139e99180bf8277526db9f2ee4a4`, with Go 1.27.3 and a clean worktree. Baseline formatting, fix dry run, lint, unit tests, integration tests, coverage, and build passed. Combined baseline coverage was 84.7%. The initial integration and coverage commands used a longer temporary directory and timed out in `TestUIManualCancellationStopsObserverModel`; both short-path reruns passed without source changes.

## Behavioral RED and GREEN

The final regression inputs were executed against baseline production source and against the implementation. Both runs used identical test and external-fixture inputs. Setup and compiler failures are excluded from behavioral RED.

| Source and symbol | Observed baseline RED | Implemented GREEN |
| --- | --- | --- |
| `host/internal/usecase/host/sessiontree/navigation_failure_test.go`, `TestNavigationParticipantFailureStopsChain` | Ordinary failures, invalid actions, malformed responses, and context issuance failures could reach later participants. Observer failures discarded committed results at the client boundary. | Request, result, and observer chains stop. Strict generated mocks prohibit later participation, model generation after a request failure, and precommit persistence. Cause identity and complete text survive; a root commit retains absent destination and leaf values. Pure owning and mixed cancellation remain distinguishable. |
| `host/internal/usecase/host/sessiontree/failure_retention_test.go`, `TestNavigationFailureRetainsEarlierHandlerIssues` | Both client boundaries continued after the first ordinary request failure. | Both client boundaries return the original cause before later handlers, generation, publication, or persistence. |
| `host/internal/usecase/host/sessiontree/publication_failure_test.go`, `TestNavigationPublicationFailureStillObservesCommit` | Postcommit errors allowed later observers and did not produce the required failure. | Mandatory observation follows publication failure. The first observer failure stops its chain. Publication and observer causes retain identity and complete text alongside root metadata. |
| `host/internal/usecase/host/programmatic/navigation_failure_test.go`, `TestNavigationFailureRetainsOnlyCommittedResult` | The prepared producer discarded a committed root result on error. | Failed retains only actual committed results. Owning cancellation, mixed failure, and independent delivery cancellation preserve their distinct terminal behavior. |
| `host/internal/usecase/host/ui/committed_navigation_failure_test.go`, `TestNavigationCommittedFailurePreservesTerminal` | The UI producer discarded committed navigation metadata on error. | Root metadata survives Failed. Only pure owning cancellation retains committed completion; independent and mixed failure remain Failed. |
| `host/internal/app/navigation_failure_public_integration_test.go`, `TestPublicNavigationFailureStopsAndRetainsCommit` | All 12 real-process scenarios failed their acceptance assertions because navigation completed or lost its required failure semantics. | Real Programmatic Control returns Failed, full causes, and the existing typed result only after commit. Public tree queries verify unchanged precommit state or persisted root navigation. |
| `host/internal/app/navigation_failure_ui_integration_test.go`, `TestPublicUINavigationFailurePreservesSDKOutcome` | All 12 real-process scenarios failed their acceptance assertions: ten completed without the required failure, while `result-error` and `result-invalid` returned `EXTENSION_INVALID_RESULT` instead of `EXTENSION_FAILED`. | Real UI SDK wait and notifications agree on full errors and committed result availability. Public tree queries verify persisted state. The SDK exposes `EXTENSION_FAILED`. |

The two public suites use separate request-error, result-error, invalid-output, observer-error, selected process-loss, independent-cancellation, and consumed-child cases. Each suite starts actual UI or Programmatic connections and an external extension process. `handleOperation.failNavigationParticipant` in `testdata/external-plugins/cmd/extension/navigation_failure.go` consumes a failed Extension reasoning-selection operation and returns through its normal handler path. Host still retains the child failure and fails navigation. No SDK settlement mechanism was added.

Public tests inspect the later-participant signal before independent recovery. For ordinary errors, a healthy-participation signal proves that the same extension connection handles a subsequent successful navigation. After selected process loss, a subsequent navigation verifies that the now-unavailable registrations are excluded from its new chain. `TestServiceUsesOneAvailabilityDecisionPerExtensionSnapshot` in `host/internal/usecase/host/sessiontree/registration_test.go` retains the snapshot policy coverage.

The final baseline commands used command-local `TMPDIR=/tmp/u10r`; the implementation commands used `TMPDIR=/tmp/u10g`. The commands below show the implementation executions from the module root. Baseline executions used the same Go arguments with identical regression inputs.

```sh
TMPDIR=/tmp/u10g go test -race -count=1 \
  ./host/internal/usecase/host/sessiontree \
  ./host/internal/usecase/host/programmatic \
  ./host/internal/usecase/host/ui \
  -run '^TestNavigation(ParticipantFailureStopsChain|PublicationFailureStillObservesCommit|FailureRetainsEarlierHandlerIssues|FailureRetainsOnlyCommittedResult|CommittedFailurePreservesTerminal)$' -v
TMPDIR=/tmp/u10g go test -race -count=1 -tags=integration -p 1 -parallel 1 \
  ./host/internal/app -run '^TestPublic(UI)?NavigationFailure' -v
```

Both baseline commands exited 1 with executed behavioral failures. Both implementation commands exited 0.

## Preserved behavior and review corrections

The summary-control integration fixtures previously registered failing participants before successful transformations and expected three accumulated issues. Those deliberate failures were removed from the successful composition fixtures in `host/internal/app/summary_control_fixture_integration_test.go`, `summaryControlRegistration`. The Programmatic and UI summary-control tests retain source attribution, model cost, credential-check, restart, and blocked-observer progress assertions. Their successful results now contain no failure issues. Failure-stop behavior has separate public coverage above.

Existing valid handler cancellation, accepted request and result replacements, built-in generation after clearing a result, original/current values, postcommit observation after caller cancellation, successful navigation, and provider retry tests remain in the passing project suites.

The reviewer strengthened the terminal tests with independent delivery cancellation while the owning context remains live. In the intermediate run, Host returned Completed in the UI case; the Programmatic package had no matching subtest. Host also returned `INTERNAL` instead of the current extension-failure category in the strengthened public UI case. After correction, Host passed both executed UI regressions. In the final baseline and implementation replay, the verifier executed independent-cancellation cases for both producers and confirmed RED/GREEN. A healthy-participation signal replaced the weaker inference that a successful next navigation alone proves extension availability.

`Operation.waitResult` in `sdk/plugins/ui/v1/host.go`, `Controller.Notify` in `plugins/ui/tui/internal/controller/plugin/controller.go`, and `Service.Notify` in `plugins/ui/tui/internal/usecase/presentation/commands.go` retain their UNT08 behavior: return typed result plus error, decode the retained result, apply it, then display the complete failure. Navigation uses those paths without transport or TUI changes.

## Project checks

The final task invocations below used command-local `TMPDIR=/tmp/u10c` from the module root after the final edits. For each invocation, the verifier retained the complete command, working directory, TMPDIR and exit status. All eight commands exited 0.

```sh
TMPDIR=/tmp/u10c task generate
TMPDIR=/tmp/u10c task fmt
TMPDIR=/tmp/u10c task fix_dry_run
TMPDIR=/tmp/u10c task lint
TMPDIR=/tmp/u10c task test
TMPDIR=/tmp/u10c task itest
TMPDIR=/tmp/u10c task test-coverage
TMPDIR=/tmp/u10c task build
```

The fix dry run proposed no changes, so no fix command was needed. Lint passed for the root and external-plugin modules, with no ifaceguard errors or reported vulnerabilities. Unit and integration tasks passed in both modules. Combined coverage was 84.7%, above the required 80.0%. Build passed for Host and the standard plugins. Root and external-module `go.mod` and `go.sum` remain unchanged. No commit or push was made.

## Failed attempts and limits

- Initial longer-path baseline integration and coverage runs timed out; short-path baseline reruns passed.
- An external-fixture compiler failure used a nonexistent reasoning enum. The fixture now uses the named string value from its existing Extension selection contract. An intermediate test compiler failure retained an unused import after continuation expectations were removed. Neither is behavioral RED.
- Early consumed-child fixtures used settings without the required reasoning-selection capability. Corrected final inputs were replayed against baseline and implementation; only those final runs establish the consumed-failure comparison.
- Development lint runs found naming, complexity, line-length, and optional-return issues. The corrections use local navigation methods and ordinary fixture dispatch; no suppressions or dependency changes were introduced.
- Whole-project integration and coverage runs initially failed on continuation expectations in the summary-control fixtures. A focused rerun then found the remaining stale three-issue credential assertion. Final focused and whole-project runs passed after those expectations were adapted.

Complete baseline and final source snapshots, final regression input manifests, module manifests, command working directories, statuses, logs, and final source identity were retained as temporary evidence. Earlier development variants are not the basis of the final source-identical RED/GREEN claim. The active LSP configuration excludes integration-tagged reference queries; primary source inspection and compiled integration tests checked those callers.

Public acceptance uses a generated Programmatic gRPC client and a real UI SDK/plugin process with the external Extension SDK fixture. It does not add a separate Programmatic SDK wrapper or new TUI PTY scenario. The full error and typed-result transport is checked at the public boundaries; the unchanged TUI apply-and-display path is supported by source tracing and the passing TUI suite.

## Open questions

None for UNT10. Remaining delivery scope is recorded in the [solution limits](solution.md#limits-and-remaining-scope).
