# UNT-03 acceptance evidence

## Result and scope

Added executable evidence for [FRQ-03](requirements.md#functional-requirements), [A3](target-picture.md#acceptance-scenarios), and [D3](decisions.md). The bundled public cases preserve accepted diagnostics and stored entries after rejected result updates and participant errors. Generated Host mocks also prohibit later participation and attempted Commit. Explicit Cancel has its own case.

Production behavior remains unchanged. The diff is uncommitted and awaits the delivery owner's independent behavior and code-quality reviews. This record does not assert review approval, unit completion, or phase completion. The existing plan and other delivery documents remain unchanged. No dependency, toolchain, public API, journal, or architecture change was made. F17 investigation and optional F18 work remain excluded.

Changed files:

- `host/internal/app/manual_result_terminal_integration_test.go`: extract the existing terminal execution and assertions into a test-only helper that accepts the selected policy directory and multiple cause substrings. The reference suite retains its cases and expectations.
- `host/internal/app/standard_compaction_composition_integration_test.go`: share the existing named participant-registration constants within the package.
- `host/internal/app/standard_result_terminal_integration_test.go`: run seven terminal cases with the bundled policy executable and independently registered reference result and outcome participants.
- `plugins/extension/compaction/internal/usecase/policy/result_rejection_test.go`: exercise the bundled Compact entry point with the existing generated MockHost.
- This evidence record.

The existing `testdata/external-plugins/cmd/compaction-policy/result.go` and `result_terminal.go` participants are reused without edits. Their first handler supplies an accepted result replacement. Their second handler supplies the selected terminal action or error. Their last handler writes a distinct artifact if reached.

## Source identity and recording

Starting HEAD: `da9455f9afa4832acb29972c468cb1f0d92b0748`. The [UNT-02 evidence](unt-02-evidence.md) supplies the prior unit baseline. Its results are historical evidence, separate from this unit's runs.

The recorder retained actual command strings, working directories, UTC start/end times, process exits, raw output, and SHA-256 manifests. The manifests include the new tests. A complete original source copy and the original policy service were preserved before sensitivity mutations. Each control used a separate temporary source copy. After each mutation, the recorder restored the original service bytes, compared the entire copy with the original manifest, and ran the unmutated test again. A separate comparison established that the checkout retained the original snapshot bytes. The material commands, results, and source identities appear inline below. Raw execution records were retained locally for independent review.

The original checked-source manifest contains 1,241 Go, module, Taskfile, linter, and interface-guard configuration files. It hashes sorted `SHA256  relative-path` lines with a trailing newline. Its SHA-256 is `5efabed3f54b0192077ae7884c95ba8d78a6f8f3679adf5e6a0c0bfb268abd6a`. The complete sensitivity snapshot manifest, including pre-existing delivery documents, has SHA-256 `0f7b6a103e685cfa42d2528021c7834bdf7ca25e28ede7fffcabbaa380e2726a`. This evidence document was added after those snapshots.

| File | Original checked/control-source SHA-256 |
| --- | --- |
| `host/internal/app/manual_result_terminal_integration_test.go` | `44e72d32faee7ad5c2c33d24d2e36ea581c1fd06b9bf4d95ebf8d4d3456dbbc4` |
| `host/internal/app/standard_compaction_composition_integration_test.go` | `bc0d867b2ec62e31d365e5a036339a64519b2ee5f159685dcb0dc680780763da` |
| `host/internal/app/standard_result_terminal_integration_test.go` | `58876d0b66e38e3526d80efe2edbe07524801dcb36b0f8de973c91955c3b042f` |
| `plugins/extension/compaction/internal/usecase/policy/result_rejection_test.go` | `c412fd12f6928ae47cd7e8fca0d6f854bda1de8e46c9ba71189544059d6153bf` |
| `plugins/extension/compaction/internal/usecase/policy/service.go`, unchanged | `1cb48fbaa74c3ed2b7d3e4d779b8604aaafbb640a5eca7430eaeb00da0b661e6` |
| `testdata/external-plugins/cmd/compaction-policy/result.go`, unchanged | `454665fef6a28009e3a49a49103dda7ba4e6565eb360ee8240b9bb20a8448a1c` |
| `testdata/external-plugins/cmd/compaction-policy/result_terminal.go`, unchanged | `14525bfee137883f79c33db9ada5fa61309a0e6a7f362c502d5e9f3e07602265` |

## Passing observations and assertion coverage

All times are UTC on 2026-10-23. Every command used child `TMPDIR=/var/tmp` directly. Focused checkout observations ran from the repository root:

| Command after `TMPDIR=/var/tmp` | Start | End | Exit |
| --- | --- | --- | --- |
| `go test -count=1 -race ./plugins/extension/compaction/internal/usecase/policy -run "^TestCompactStopsBeforeInstallingRejectedResult$" -v` | 02:44:50.923223 | 02:44:52.344858 | 0 |
| `go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run "^TestStandardResultTerminalActions$" -v` | 02:44:52.345489 | 02:45:01.155756 | 0 |
| `go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run "^TestProgrammaticManualResultHandlers$/TestTerminalActions" -v` | 02:45:01.156311 | 02:45:08.430731 | 0 |

Before the final sensitivity sequence, the frozen original source copy also ran the first two commands above. The coordinator matrix passed from 02:53:30.613806 to 02:53:34.234459, exit 0. The bundled public matrix passed from 02:53:34.235145 to 02:54:23.410852, exit 0. Thus the recorded controls begin with compiling, passing observations of unchanged production behavior and fingerprinted tests.

| Scenario | Public observation | Coordinator observation |
| --- | --- | --- |
| Mixed Cancel/action shape | Failed; cause contains `cancellation must be exclusive` | Validation error; Canceled=false |
| Replace without payload | Failed; payload/disposition mismatch cause | Validation error; Canceled=false |
| Preserve with unexpected payload | Failed; payload/disposition mismatch cause | Validation error; Canceled=false |
| Rejected boundary | Failed; `compaction boundary is outside its branch` | Project returns the boundary cause; ErrorIs retains the projection error |
| Ordinary participant failure | Failed; `result participant source failure` | ErrorIs retains the participant error |
| Live-context cancellation-containing error | Failed; both `context canceled` and `result participant source failure` occur in public failure and observer error | Owning context remains live; both joined causes remain discoverable; Canceled=false |
| Explicit Cancel | Completed with compaction Canceled=true | Nil error; Canceled=true |

For every public row, the shared helper compares the complete observer result, OriginalRequest, and CurrentRequest with the second participant's accepted input. The helper checks the result summary string against `manualResultSummary`. A separate protobuf equality assertion compares the complete accepted result, including its source attribution and details. The helper requires an absent committed receipt and absent `result-m-last.json`. It compares pre/post stored-entry counts and every complete entry with `proto.Equal`.

For every coordinator row, a request handler first supplies a changed request and result. The first result handler then supplies another accepted replacement. The second handler receives that state and the immutable original result. The test compares the entire returned state with the accepted state, checks Preserve=false, and retains each acquired cause. The participant list includes a later handler. MockHost has no expectation for that handler or Commit on any row, so either call fails the test.

Material passing output:

```text
--- PASS: TestStandardResultTerminalActions
    --- PASS: TestStandardResultTerminalActions/cancel
    --- PASS: TestStandardResultTerminalActions/mixed
    --- PASS: TestStandardResultTerminalActions/missing
    --- PASS: TestStandardResultTerminalActions/unexpected
    --- PASS: TestStandardResultTerminalActions/boundary
    --- PASS: TestStandardResultTerminalActions/failure
    --- PASS: TestStandardResultTerminalActions/classified
--- PASS: TestCompactStopsBeforeInstallingRejectedResult
--- PASS: TestProgrammaticManualResultHandlers/TestTerminalActions
```

## Evidence-sensitivity controls

These controls measure assertion sensitivity. They are not original behavior-correction RED chronology. No production correction was made.

Each command below ran from its own temporary source-copy root with child `TMPDIR=/var/tmp`. The exact command pattern was:

```sh
TMPDIR=/var/tmp go test -count=1 -race ./plugins/extension/compaction/internal/usecase/policy -run "^TestCompactStopsBeforeInstallingRejectedResult$/^CASE$" -v
```

`CASE` was replaced with the literal case in this table. Each row records unmutated, mutated, and restored executions, respectively. All mutated executions compiled and reached their intended assertions.

| Control and literal CASE | Mutation in `policy.Service.complete` | Service SHA-256 during mutation | Exits |
| --- | --- | --- | --- |
| Install rejected candidate; `boundary` | Assign `state.Result = mo.Some(replacement)` before candidate projection/validation | `579dfb1f244831f6d2285166c6dc703a14404395e081d47cca6bae897adba2d8` | 0 / 1 / 0 |
| Continue after participant error; `participant-failure` | Replace the return after InvokeResultHandler error with `continue` | `556599d1f0d62148b1e25d9a83924e8ebbdd258ea77bd20b6a4f186139c211ca` | 0 / 1 / 0 |
| Attempt Commit after rejection; `missing-payload` | On action validation error, return `errors.Join(actionErr, s.host.Commit(ctx, state.Result.OrEmpty()))`; add the errors import | `5e7c4e118c4139a2f79034b72cae29c904efab0b88d988ed819554449693532f` | 0 / 1 / 0 |

| Control | Unmutated start/end | Mutated start/end | Restored start/end |
| --- | --- | --- | --- |
| Install rejected candidate | 02:54:23.446699 / 02:54:27.068481 | 02:54:27.070233 / 02:54:28.015233 | 02:54:28.086676 / 02:54:29.524428 |
| Continue after participant error | 02:54:29.556114 / 02:54:33.247754 | 02:54:33.249721 / 02:54:34.160934 | 02:54:34.230684 / 02:54:35.651239 |
| Attempt Commit after rejection | 02:54:35.686033 / 02:54:39.314456 | 02:54:39.316274 / 02:54:40.251667 | 02:54:40.301106 / 02:54:41.732472 |

Material failure diagnostics:

```text
TestCompactStopsBeforeInstallingRejectedResult/boundary
Error: Not equal:
- Summary: (string) (len=8) "accepted",
+ Summary: (string) (len=8) "rejected",

TestCompactStopsBeforeInstallingRejectedResult/participant-failure
Unexpected call to *policy.MockHost.InvokeResultHandler(...)
Got: {extension runtime later 0} (compaction.Participant)
Want: is equal to {extension runtime first 0} (compaction.Participant)
Want: is equal to {extension runtime second 0} (compaction.Participant)

TestCompactStopsBeforeInstallingRejectedResult/missing-payload
Unexpected call to *policy.MockHost.Commit(...)
because: there are no expected calls of the method "Commit" for that receiver
```

The recorder retained complete diagnostics and mutation diffs. Each restored service hash equals the unchanged service hash in Source identity. No control mutation or probe was installed in the checkout.

## Original readiness checks

The original formatter changed only formatting in the two new tests. Every later check in this original batch preserved its checked-source manifest. The fix dry run produced empty output, so there was no proposal to apply and no `task fix` execution.

All commands below used child `TMPDIR=/var/tmp`. Cwd was the repository root, except external-module lint, which ran in `testdata/external-plugins`. The external lint binary was resolved in ROOT, as the Taskfile prescribes:

```sh
TMPDIR=/var/tmp "$(cd ../.. && go tool -n golangci-lint)" run --config ../../.golangci.yml --build-tags=integration ./...
```

| Command | Start | End | Actual exit and result |
| --- | --- | --- | --- |
| `task fmt` | 02:42:20.533145 | 02:42:26.118582 | 0 |
| `task fix_dry_run` | 02:42:26.135947 | 02:42:26.479597 | 0, empty proposals |
| `task lint` | 02:42:26.497083 | 02:42:36.480618 | 201; inner govulncheck exit 3, accepted GO-2026-6809 only |
| External-module lint, command above | 02:42:36.498342 | 02:42:36.996505 | 0, zero issues |
| `go tool golangci-lint run --config .golangci.yml ./plugins/extension/compaction/internal/usecase/policy` | 02:42:37.014051 | 02:42:37.872217 | 0, supplemental non-integration test lint |
| `task test` | 02:42:37.889684 | 02:42:40.232629 | 0, root and external module |
| `task itest` | 02:42:40.245881 | 02:48:19.564883 | 0, root and external module |
| `task test-coverage` | 02:48:19.582463 | 02:52:36.640730 | 0; 84.2%, required 80.0% |
| `task build BUILD_ROOT=<temporary build output>` | 02:52:36.657985 | 02:52:39.240926 | 0, Host and standard plugins |

`BUILD_ROOT=<temporary build output>` abbreviates the temporary-output argument; it is not a literal shell command argument. The build produced the Host and standard plugin binaries outside the checkout, with exit 0 as recorded above. Raw records of the resolved argument and execution were retained locally for independent review. Unit-task output includes cached results; the focused matrices and coverage task used uncached execution.

Material lint and coverage output:

```text
0 issues.
ifaceguard: no errors found
Vulnerability #1: GO-2026-6809
Found in: crypto/x509@go1.27.2
Fixed in: crypto/x509@go1.27.3
Your code is affected by 1 vulnerability from the Go standard library.
This scan found no other vulnerabilities in packages you import or modules you require.
task: Failed to run task "lint": exit status 3
Coverage: 84.2%, required: 80.0%
```

Main lint stopped before its external-module task. The separate external-module command passed. The existing vulnerability exception remains the only scanner exception.

## Executed skip inventory

An additional uncached run executed `TMPDIR=/var/tmp go test -count=1 -race -json -tags=integration -p 1 -parallel 1 ./...` from the repository root, 02:52:40.437686 to 02:59:59.099734, exit 0. The identical command ran from `testdata/external-plugins`, 02:59:59.122701 to 03:00:06.214415, exit 0. Command metadata includes the source fingerprints at each start.

The root run emitted seven individual test skips:

- `host/internal/app.TestRunWithPathsHeadlessPersistenceFailurePreservesContext`: directory permission failure injection requires Darwin permission enforcement.
- `host/internal/app.TestLifecycleObserverThroughStandardTUI`: real PTY acceptance runs on Darwin arm64.
- `host/internal/app.TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath`: immutable-file recovery injection requires Darwin chflags.
- `host/internal/app.TestStandardTUIHostRuntimePersistenceFailures`: real PTY acceptance runs on Darwin arm64.
- `host/internal/app.TestStandardTUIHostSmoke`: real PTY acceptance runs on Darwin arm64.
- `internal/testsupport/tui.TestEarlyFailureCleanup`: real PTY cleanup runs on Darwin arm64.
- `plugins/ui/tui/internal/app.TestStandardTUIPTY`: real PTY acceptance runs on Darwin arm64.

The root run also emitted 55 package-level skip events for packages without integration tests. The external run emitted two package-level skip events and zero individual test skips. Those package events remain in the raw inventory. Every new UNT-03 row executed. The seven platform-limited scenarios remain unexecuted evidence boundaries; this unit does not extend historical claims for them.

## Text corrections and refreshed readiness

The accepted text findings were resolved without changing behavior:

- FND-01: Each of the 14 constants in the shared composition group and seven constants in the bundled terminal group now has an individual one-sentence purpose comment. Values and declaration order remain unchanged.
- FND-02: The assertion description now separates the summary string check from complete-result protobuf equality in two sentences.
- FND-03: Both references that depended on an unidentified handoff were replaced with self-contained evidence wording. Material commands, results, and source identity remain inline. Raw execution records were retained locally for independent review. The build command explicitly abbreviates its temporary-output argument.
- FND-04: The purpose comment now reads `// resultModeEnvironment selects the reference result participant's behavior.` The setting selects accepted result-source behavior as well as terminal actions. The complete 14-constant purpose-comment group was checked against reference registration, result behavior, bundled generator configuration, and outcome artifact writing; the seven terminal-mode comments were also checked against their fixture outcomes.

The final checked-source manifest uses the same 1,241 paths and serialization described in Source identity. Its SHA-256 is `d41b947efbfe60febbc698431dc33250c124fdaeca990a4447cc131a1337ab80`. Only these two source hashes differ from the original checked/control-source table:

| File | Final checked SHA-256 |
| --- | --- |
| `host/internal/app/standard_compaction_composition_integration_test.go` | `83f41bfdc18b9f76874581644d6c3ef31c1a172aa7ce48cd9bda378ec1045e85` |
| `host/internal/app/standard_result_terminal_integration_test.go` | `7f6b1b04113b4877293f52b71d769e5462b8c1c744005f0e0203194e5f0a0503` |

Relative to the preceding comment-corrected source manifest `2ea17aa1c0e792380bac16bb28d46099e6975a36e642ec7d3cff89a3df9a3103`, the final source changes exactly the FND-04 comment. The shared composition file's preceding SHA-256 was `5a610ccde7275bb4fa28854f890731c54b73319f0b618f992bcae55982b74fd9`. Values, code, assertions, and declaration placement are unchanged. Both earlier artifact collections, including their frozen sources and original sensitivity records, were preserved unchanged.

Every other source hash in the original table remains the final hash. A Go token-stream comparison, excluding comments and source positions, passed for both files that differ from the original control source. Their byte differences consist of the purpose comments and declaration alignment. The original control snapshots, manifests, mutation diffs, and execution records were preserved unchanged. The original controls used the byte-identical bundled service and coordinator test that remain in the final source. Their tests and mutations were not rerun or relabeled as TDD. The final focused runs below establish passing execution after the comment edits; they do not replace or redocument the original control chronology.

After the last code-comment edit, the complete readiness set ran again. Every command used child `TMPDIR=/var/tmp` directly. Cwd was the repository root except external-module lint, which ran in `testdata/external-plugins` using the root-resolved binary and exact invocation given in Original readiness checks. The formatter preserved the source manifest. The fix dry run produced empty output, so there was no proposal to apply. Every remaining command also preserved the final source manifest.

| Command after `TMPDIR=/var/tmp` | Start UTC, 2026-10-23 | End UTC | Actual exit and result |
| --- | --- | --- | --- |
| `task fmt` | 03:47:23.281555 | 03:47:28.872729 | 0, unchanged source |
| `task fix_dry_run` | 03:47:28.890253 | 03:47:29.221868 | 0, empty proposals |
| `task lint` | 03:47:29.239266 | 03:47:39.440275 | 201; inner govulncheck exit 3, accepted GO-2026-6809 only |
| `"$(cd ../.. && go tool -n golangci-lint)" run --config ../../.golangci.yml --build-tags=integration ./...` | 03:47:39.457687 | 03:47:39.959464 | 0, external-module lint, zero issues |
| `go tool golangci-lint run --config .golangci.yml ./plugins/extension/compaction/internal/usecase/policy` | 03:47:39.976933 | 03:47:40.444812 | 0, supplemental non-integration test lint |
| `task test` | 03:47:40.462151 | 03:47:41.808499 | 0, root and external module, cached results |
| `task itest` | 03:47:41.826454 | 03:47:56.860049 | 0, root and external module, includes cached package results |
| `task test-coverage` | 03:47:56.878045 | 03:52:15.102935 | 0; 84.2%, required 80.0%, uncached unit and integration execution |
| `task build BUILD_ROOT=<temporary build output>` | 03:52:15.120495 | 03:52:17.492068 | 0, Host and standard plugins |
| `go test -count=1 -race ./plugins/extension/compaction/internal/usecase/policy -run "^TestCompactStopsBeforeInstallingRejectedResult$" -v` | 03:52:17.509674 | 03:52:18.926489 | 0, all seven coordinator rows |
| `go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run "^TestStandardResultTerminalActions$" -v` | 03:52:18.944464 | 03:52:27.654376 | 0, all seven bundled public rows |
| `go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run "^TestProgrammaticManualResultHandlers$/TestTerminalActions" -v` | 03:52:27.671715 | 03:52:34.847315 | 0, reference terminal regressions |

`BUILD_ROOT=<temporary build output>` is an abbreviated temporary-output argument. Raw records of its resolved value, all command strings, working directories, UTC boundaries, exits, output, and current source fingerprints were retained locally for independent review. The build output remained outside the checkout.

Refreshed main lint again reported `0 issues.` and `ifaceguard: no errors found` before reporting GO-2026-6809 as its sole vulnerability. Main lint stopped before external-module lint; the separate external-module invocation passed. No scanner suppression or version change was applied. Coverage again reported `Coverage: 84.2%, required: 80.0%`.

The JSON skip inventory above belongs to the original checked source and was not rerun during the comment-only refresh. Current full integration checks passed. Every row in the final focused matrices executed without a test skip. The seven platform-limited scenarios remain unexecuted evidence boundaries. The retained control and skip records are historical observations of their recorded sources; token equivalence supports unchanged test logic, without claiming byte identity for the two commented public sources.

## Exploratory failures and handoff limits

An initial compiling public run selected unintended reference request handlers and failed with `request original input changed`. The new fixture had omitted the existing separate-request and separate-generator registration settings. `policyRegistration.Run` in the reference fixture registers those handlers unless separated. Applying the settings already used by TestStandardManualComposition let the test reach the intended bundled result paths. No production defect was established. An initial lint run reported three overlong new public-case rows; splitting those literals resolved the errors. These exploratory failures precede the frozen acceptance source and are retained separately from sensitivity evidence.

No new defect outside UNT-03 was investigated or corrected. No workaround, deprecation, or new technical debt was introduced. Public durable-state observations alone do not establish absence of an attempted rejected replacement; the generated coordinator mocks supply that complementary guarantee. The public scenario uses the existing classified joined-cause fixture with a live owner, and does not claim to cover every cancellation mechanism or every client/provider combination.

Independent reviews and final acceptance remain with the delivery owner. No commit or push occurred. Intermediate workflow results proceeded without user confirmation within the authorized boundaries. No unresolved product decision blocks this handoff.
