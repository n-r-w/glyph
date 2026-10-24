# UNT08: Selection Failure Verification

Status: UNT08 implemented and verified. This record covers model and reasoning selection and the shared typed Failed transport. The [technical solution](solution.md#model-and-reasoning-selection) owns behavior and boundary design. The broader issue remains open under its [remaining scope](solution.md#limits-and-remaining-scope).

## Baseline and source identity

The starting revision was `a4d6197fa33694f315c60cf888caaaa865133d6d`, with a clean working tree and Go `go1.27.3 linux/amd64`. No dependency version or module checksum changed.

The initial commands were `TMPDIR=/tmp task fmt`, `TMPDIR=/tmp task fix_dry_run`, `TMPDIR=/tmp task lint`, `TMPDIR=/tmp task test`, `TMPDIR=/tmp task itest`, `TMPDIR=/tmp task test-coverage`, and `TMPDIR=/tmp task build`. Each exited 0. The fix dry run produced no proposal. Combined coverage was 84.4%, with 80.0% required.

Behavior tracing preceded production edits. The traced path included shared selection preparation, handler eligibility and dispatch, validation and commit, both observer groups, all three public terminal producers, incoming SDK validation, typed waits and wrappers, TUI application, and UNT07 parent failure retention. `Service.compose` in `host/internal/usecase/host/modelselection/handlers.go` already captured handler eligibility. The former `Service.ObserveSelection` in `host/internal/usecase/host/lifecycle/service.go` checked observer eligibility during dispatch and continued after failures. All three selection producer paths exposed committed observer errors as completed data.

The original UNT07 verification, captured-fixture qualifications, and helper compilation limits remain in [its verification record](solution.md#verification). The external handoff retains full command output, captured RED sources, the delivered source archive, and file manifests. Results below identify their baseline, commands, repository symbols, and observable failures independently of those archives.

## Executed RED evidence

### Public selection before production edits

The first RED tests ran before production changes against the starting source with added public tests and external fixture behavior:

```sh
TMPDIR=/tmp go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicSelectionObserverFailureStopsOperation$' -v
TMPDIR=/tmp go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicSelectionHandlerFailureStopsOperation$' -v
```

Both commands exited 1 on behavioral assertions. `TestPublicSelectionHandlerFailureStopsOperation` in `host/internal/app/selection_failure_public_integration_test.go` covered model/reasoning ordinary-error and invalid-action cases. All four received an unexpected successful terminal. The initial observer test covered reasoning selection and also received a successful terminal. Each request used Programmatic Control and a real external extension process.

These initial tests were subsequently extended with state-query barriers, assertions about the result included with Failed, model changes, process loss, and the reasoning-to-model observation transition. The initial RED observation is not a source-identical replay of every final assertion.

### UI and Extension SDK baseline replays

The following commands ran against an isolated copy of the starting production source with the corresponding tests and public fixture additions:

```sh
TMPDIR=/tmp go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^(TestPublicUISelectionFailurePreservesSDKOutcome|TestPublicConsumedSelectionFailureStopsParent)$' -v
```

The command exited 1. `TestPublicUISelectionFailurePreservesSDKOutcome` in `host/internal/app/selection_failure_ui_integration_test.go` failed for ordinary handler error, invalid handler action, ordinary observer error, invalid observer output, and observer process loss, for both selection kinds. The actual SDK caller reported that selection unexpectedly completed without failure. Both precommit process-loss cases already passed at baseline; their unchanged direct failure behavior is a regression check, not a new behavioral RED.

`TestPublicConsumedSelectionFailureStopsParent` in `host/internal/app/consumed_selection_integration_test.go` failed in all six model/reasoning ordinary-error, invalid-action, and postcommit-observer-error cases in the initial replay. The final replay also failed all four independent participant-cancellation cases added during review. The real Extension SDK caller consumed the child result and returned tool success; the parent received an unexpected successful terminal.

The external fixture changes are in `testdata/external-plugins/cmd/extension/selection.go`, `handleOperation.runSelectionHandler` and `handleOperation.runSelectionObserver`, and `testdata/external-plugins/cmd/extension/consumed_selection.go`, `exerciseConsumedSelection`. Lint corrections extracted `selectionFailureResponse` and confined report writing through `os.Root`. Baseline replays were repeated with these final fixture implementations. Earlier captures remain qualified by their recorded fixture variants; the initial Programmatic tests were subsequently extended as described above.

### TUI, cancellation, and current categories

```sh
go test -race -count=1 ./plugins/ui/tui/internal/usecase/presentation ./host/internal/usecase/host/modelselection -run '^(TestFailedSelectionAppliesCommittedState|TestSelectionHandlerCancellationPrecedence)$' -v
go test -race -count=1 ./host/internal/usecase/host/programmatic ./host/internal/usecase/host/ui -run '^TestRunFailureCodeUsesCurrentExtensionMeaning$' -v
```

Both baseline commands exited 1. `TestFailedSelectionAppliesCommittedState` in `plugins/ui/tui/internal/usecase/presentation/selection_failure_test.go` failed in both acknowledgement orders because failure handling did not apply the retained selection. This test exercises the TUI presentation input, not a native terminal.

`TestSelectionHandlerCancellationPrecedence` in `host/internal/usecase/host/modelselection/handler_cancellation_test.go` failed for independent error, invalid action, and selected unavailability combined with owning cancellation, for both selection kinds. The errors lacked explicit extension-failure meaning, or cancellation bypassed invalid-action handling. Both pure-cancellation cases passed at baseline.

`TestRunFailureCodeUsesCurrentExtensionMeaning` in `host/internal/usecase/host/programmatic/current_failure_code_test.go` and `host/internal/usecase/host/ui/current_failure_code_test.go` expected `EXTENSION_FAILED` for an explicit current category but received `INTERNAL`.

### Independently canceled participants through the real SDK

Review of the Extension controller found that its former leaf-only cancellation classifier discarded explicit current failure meaning. Additional real-plugin cases were added before correcting that classifier:

```sh
TMPDIR=/tmp go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicConsumedSelectionFailureStopsParent$' -v
```

Against the then-current typed transport implementation with the former `isSelectionCancellationOnly` in `host/internal/controller/extension/selection.go`, this command exited 1. Both precommit independent-cancellation cases returned `INTERNAL` to the SDK instead of `EXTENSION_FAILED`. Both postcommit independent-observer-cancellation cases completed successfully and allowed the parent to continue. The six previously covered cases still passed. After switching the controller to the shared current-cause classifier, all ten cases passed. Programmatic and UI cases were extended with the same independent-cancellation scenarios. Owning cancellation remains a separate successful regression case.

This RED used the in-progress typed transport source, not the starting production revision. Its source and full output are retained separately in the external handoff. Final baseline replays also include these extended public caller and fixture sources.

New constructor and malformed-envelope tests require the new protocol/API. Their initial compilation would not establish a behavioral RED, and this record makes no such claim. They verify the new contracts alongside the public behavioral RED/GREEN paths.

## GREEN verification

The focused public command is:

```sh
TMPDIR=/tmp go test -race -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app ./host/internal/controller/programmatic -run '^(TestPublicSelection(Handler|Observer)FailureStopsOperation|TestPublicUISelectionFailurePreservesSDKOutcome|TestPublicConsumedSelectionFailureStopsParent|TestFailedSelectionDeliveryValidatesRetainedResult)$' -v
```

The focused unit command is:

```sh
go test -race -count=1 ./host/internal/usecase/host/modelselection ./host/internal/usecase/host/lifecycle ./host/internal/usecase/host/programmatic ./host/internal/usecase/host/ui ./sdk/plugins/extension/v1 ./sdk/plugins/ui/v1 ./plugins/ui/tui/internal/usecase/presentation ./internal/operation
```

Both commands exited 0. The focused public command verifies the final selection scenarios and typed delivery boundaries with uncached race-enabled tests.

| Verification boundary | Repository tests and asserted result |
| --- | --- |
| Programmatic selection | `TestPublicSelectionHandlerFailureStopsOperation` and `TestPublicSelectionObserverFailureStopsOperation` in `host/internal/app/selection_failure_public_integration_test.go` verify failed terminals, unchanged precommit state, actual retained committed selection, no later participant, reasoning-to-model stopping, unchanged-selection success, and exclusion of unavailable registrations on a new chain. `readPublicSelection` in `host/internal/app/selection_failure_fixture_test.go` checks a state-query barrier for duplicate terminals. |
| UI SDK consumers | `TestPublicUISelectionFailurePreservesSDKOutcome` in `host/internal/app/selection_failure_ui_integration_test.go` verifies actual `Operation.Wait` and failed notifications through an assembled Host, real UI process, and real extension process. Retained selections and complete failure text agree between both consumers. |
| Extension SDK and parent retention | `TestPublicConsumedSelectionFailureStopsParent` in `host/internal/app/consumed_selection_integration_test.go` verifies both selection wrappers return committed data with a nonnil remote error, or absent data with error before commit. Host rejects further owned work and fails the parent without another provider call, even after tool success. Independent operations retain extension availability after ordinary errors. |
| Eligibility and cancellation | `TestSelectionObserversCaptureEligibilityBeforeDispatch` and `TestSelectionObserverFailureWinsOwningCancellation` in `host/internal/usecase/host/lifecycle/selection_participants_test.go` verify captured eligibility and current independent failure. `TestSelectionHandlerCancellationPrecedence` covers both handler kinds. Independent participant cancellation is also covered through real public clients. Existing explicit-Reject and committed owning-cancellation tests retain their semantics. |
| Failed-result contracts | `TestFailedSelectionDeliveryValidatesRetainedResult` in `host/internal/controller/programmatic/retained_failure_integration_test.go` verifies typed delivery and diagnoses missing or mismatched retained data with the original cause. `TestFailedHostPayloadValidation` in `sdk/plugins/extension/v1/failed_result_test.go` and `sdk/plugins/ui/v1/failed_result_test.go` verifies mandatory failure data, request-kind matching, and validation of the fields in `HostFailed.result`. `TestFailedOutcomeRetainsAvailableResult` in `internal/operation/failed_result_test.go` verifies meaningful zero data remains present with Failed state. |
| TUI state and settlement | `TestFailedSelectionAppliesCommittedState` in `plugins/ui/tui/internal/usecase/presentation/selection_failure_test.go` verifies committed selection, complete error shown once, no success line, and pending/foreground settlement for either acknowledgement order. |
| Unrelated operations and current causes | Whole-project tests retain navigation and compaction completed-payload behavior, ordinary tool results, cancellation ownership, progress, streaming, and UNT07 authority. Both `TestRunFailureCodeUsesCurrentExtensionMeaning` tests keep historical categories from establishing the new current-category mapping. |

### Verification corrections

An intermediate whole-project run exposed an error-category precedence regression in the new run mapping. `TestThresholdCompactionPreservesSplitBoundaryCause` in `host/internal/app/compaction_threshold_boundary_integration_test.go` received `EXTENSION_FAILED` instead of its existing `COMPACTION_FAILED`. The mapper now checks existing typed compaction and logical-model categories before the current-category fallback. The existing compaction regression and consumed-selection parent tests passed together after this correction.

An intermediate lint run found that adding `FailureCode` to the new selection error accidentally implemented `sessiontree.ModelRequestFailure`. The method was removed. The public run boundaries consume the existing `errtree.CodedError` contract instead. No foreign interface assertion, dependency-direction exception, or ifaceguard suppression was added.

### Whole-project commands

```sh
TMPDIR=/tmp task generate
TMPDIR=/tmp task fmt
TMPDIR=/tmp task fix_dry_run
TMPDIR=/tmp task lint
TMPDIR=/tmp task test
TMPDIR=/tmp task itest
TMPDIR=/tmp task test-coverage
TMPDIR=/tmp task build
git diff --check
```

Every command above exited 0 after the final code corrections. `task lint` reported zero issues, ifaceguard reported no errors, and govulncheck found no vulnerabilities. The uncached combined coverage checks reported 84.5% and 84.6%, above the 80.0% threshold. The fix dry run produced no proposal, so no fix was applied. Declared protobufs and mocks are regenerated; dependency versions are unchanged.

## Limits and later scope

- Native Darwin-arm64 PTY acceptance remains platform-gated on this Linux environment. The TUI regression checks the production presentation input; real-process UI tests independently cover SDK waits and notifications. This record does not claim a new native PTY run.
- Real process loss is exercised through Programmatic and UI selection clients. Consumed Extension SDK tests use ordinary errors and invalid actions while the caller process remains available to consume and report the outcome.
- Shared transport and SDK compaction waits can preserve typed retained data. Compaction and navigation producers retain their existing completed-payload behavior; this unit does not claim those producers now emit postcommit Failed.
- Ordinary agent lifecycle policy is UNT09, session-tree chain semantics are UNT10, compaction postcommit Failed producers are UNT11, and final issue/panic closure is UNT12.
- Integration-tagged and external-module reference queries were attempted through Asteria. When the active index excluded a file, direct source inspection and compiled integration tests checked callers without changing session-wide build flags.
- No outside-task defect was newly confirmed.

## Open Questions

None for the implemented selection behavior. The later units listed above remain outside this delivery.
