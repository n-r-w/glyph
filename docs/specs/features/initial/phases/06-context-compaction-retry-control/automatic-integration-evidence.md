# Automatic conversation preparation integration evidence

## Scope

Main accepted UNT-09 at `e4865d8aa1519557026b39e572a607b00575c1b7` after independent review of the complete change from `5086722d3978cf978e3c8ebf4785327101959ecb`. The accepted source also includes the separate client transport and executable-fixture corrections described below.

The [system integration design](solution.md#phs-061-integration-proposal) defines ownership and contracts. The [migration plan](migration-plan.md#sequential-implementation-units) defines the unit order. This acceptance covers automatic preparation through a reference extension. Overflow migration and legacy cleanup remain pending in UNT-10 and UNT-11. The bundled policy and QST-01.1 belong to PHS-06.2.

## Implemented request path

Programmatic, UI and Headless assemblies inject `modelexecution.Service` and `extensioncontext.Service` into ordinary conversation execution. `Service.Stream` in [model execution](../../../../../../host/internal/usecase/host/modelexecution/service.go) captures the policy owner once and uses that captured object for preparation and completed-conversation observation. Configured-model requests use their separate execution path.

`capturedConversation.Prepare` in [conversation preparation](../../../../../../host/internal/usecase/host/extensioncontext/conversation_preparation.go) gives the policy a session snapshot and the frozen conversation binding. When the policy returns a preserve result, Host uses that snapshot's history and sends no compaction outcome notification. With no registered owner, Host dispatches ordinary history without automatic compaction. When a registered owner is unavailable, Host returns an error.

For replacement, the session owner retains structural validation, projection, synchronous commit guards and durable original history. It records the receipt before publication. Host joins policy and outcome-observer work, then captures the actual outgoing history. The same policy owner receives that history for final validation through a lifecycle-only invocation. This invocation has no compaction mutation or participant-composition authority. Host retains the model, reasoning, instructions and tools selected for the conversation. Appends during final validation enter a later request.

The [policy completion check](../../../../../../host/internal/usecase/host/extensioncontext/policy_decision.go) compares the reported disposition with the independently recorded receipt. Host retains valid acquired cancellation metadata alongside an independent failure. A failure after durable replacement remains a logical preparation failure, so Core receives the original cause without acquiring compaction-specific behavior.

## Public and boundary assertions

| Source | Checked behavior |
| --- | --- |
| [Automatic conversation tests](../../../../../../host/internal/app/automatic_conversation_integration_test.go) | `TestAutomaticConversationPreparation` checks replacement, outgoing history after outcome notification and final validation failure. `TestAbsentOwnerDispatchesOversizedHistory` checks ordinary dispatch without a policy owner. |
| [Composition test](../../../../../../host/internal/app/automatic_composition_integration_test.go) | `TestAutomaticCompositionPreservesConversationBinding` checks policy-owned composition while retaining the conversation binding. |
| [Outgoing-history test](../../../../../../host/internal/app/automatic_freeze_integration_test.go) | `TestAutomaticFinalCheckFreezesOutgoingHistory` checks that appends during final validation do not enter the already captured provider request. |
| [Decision tests](../../../../../../host/internal/app/automatic_decision_integration_test.go) | `TestAutomaticDecisionRetainsDurableOutcome` checks disposition/receipt agreement, provider suppression and acquired canceled metadata with an independent failure. |
| [Lifetime tests](../../../../../../host/internal/app/automatic_lifetime_integration_test.go) | `TestAutomaticPreparationLifetime` checks pre-commit cancellation, post-commit failure, independent child failures, owner loss, joined children and preserved stored history. |
| [Application-mode test](../../../../../../host/internal/app/automatic_modes_integration_test.go) | `TestAutomaticPreparationAcrossApplicationModes` checks preserve and replacement through the Headless and UI assemblies; the detailed scenarios above use Programmatic submission. |
| [Runtime diagnostic test](../../../../../../host/internal/infra/plugins/extension/runtime/policy_diagnostics_integration_test.go) | `TestPolicyCancellationDecisionRetainsDiagnostic` uses real gRPC to check acquired disposition and complete independent error text. |
| [Captured-policy tests](../../../../../../host/internal/usecase/host/modelexecution/captured_policy_test.go) and [preparation tests](../../../../../../host/internal/usecase/host/extensioncontext/conversation_preparation_test.go) | Layer checks cover captured-session history, owner availability, frozen binding and lifecycle authority limits. |

Application tests use production internal assemblies, runtime processes, SDK, session persistence and a substituted HTTP provider transport. They establish internal wiring and serialized provider requests. They do not exercise a live remote model service or the future bundled algorithm.

## Client transport and fixture corrections

### Remote status after client send EOF

Corrective commit `aa7f0fd166a1db9462b4f6f12cdf0ec5af402207` changes the Extension Host client and UI Host operation stream. When generated-client Send returns EOF, each client waits for its sole receiver to resolve the remote outcome before publishing a send-derived result. The Extension receiver releases waiting senders before joining acknowledgement-dependent Host work. The UI controller supplies one idempotent request half-close for EOF resolution and cleanup.

The correction preserves immediate non-EOF failure handling, clean receive-EOF semantics, complete remote and independent local causes, original local error identities and joined cleanup. UI initialization retains its pending-receive joins and unsuccessful-initialization drain. Shared `operation.Writer`, Programmatic server transport and plugin SDK server transports are unchanged.

[Extension EOF tests](../../../../../../sdk/plugins/extension/v1/host_eof_integration_test.go) check pending invocation settlement, stopped-writer producers, subsequent initiation, clean EOF and acknowledgement-dependent cleanup. [UI EOF tests](../../../../../../host/internal/infra/plugins/ui/runtime/operations_eof_integration_test.go) check buffered input, directly received status, requested close, terminal enqueue and independent half-close failure. All seven cases use channel/synctest coordination. Six cases check complete multiline Unicode causes. `TestConnectionCleanReceiveEOFResolvesOwnedAcknowledgements` checks clean receive-EOF settlement and release of acknowledgement-dependent Host work. Each case passed 200 executions in final verification.

The original [real-peer settlement tests](../../../../../../host/internal/infra/plugins/extension/runtime/settlement_integration_test.go) retain their complete-cause, cancellation-classification and typed-rejection assertions.

### Executable fixture creation

Corrective commit `ac80f52f7109d6e62d1d7d8b69ec93a7ce8508d8` makes `TestFactoryRuntimeSurvivesStartupContextCancellation` use the existing guarded writer in [shutdown integration tests](../../../../../../host/internal/infra/plugins/extension/runtime/shutdown_integration_test.go). The helper holds `syscall.ForkLock.RLock` during script creation, writing and close. This prevents a concurrent fork from retaining the writable descriptor while the test starts the script.

The correction preserves script content, startup cancellation, registration, shutdown and timing assertions. It introduces no retry or sleep. The helper's executable mode remains inside the private `t.TempDir` parent. The factory case passed all 20 executions in the final shared-package repetition.

## Final verification and acceptance

The verified source has identity `18bf718092dc53ef8a618b392d898b2dc09cf5e22e35be0347cc694e4d5e805e` and 1,288 file-map entries. The implementation commit contains that source after the two corrective commits. This documentation closure follows code acceptance.

All 65 final command records completed with exit 0 after the last code edit. The verifier reserved exclusive log/result pairs before the checks. Main and a fresh independent Analyst compared all 130 command before/after maps and both driver maps with the frozen source. They independently checked raw Go test events and individual migrated selectors. Generation and formatting kept source unchanged; fix dry-run produced no proposal.

The Analyst reviewed all 97 changed paths, including the separate corrections, and found no remaining source defect or missing required application wiring. Main accepted the unit under the approved unit-level authority, with the limits below retained. PHS-06.1 remains incomplete.

| Command | Final result |
| --- | --- |
| `task generate`, twice | Both passed with unchanged source |
| `task fmt` | Passed with unchanged source |
| `task fix_dry_run` | Passed; empty proposal |
| `task lint` | Passed |
| `task test` | Passed |
| `task itest` | Passed with the platform exclusions below |
| `task test-coverage` | Passed: 84.2%, required 80.0% |
| `task build` | Passed |
| `go list ./...` | Passed |
| `go list -tags=integration ./...` | Passed |
| `go build -tags=integration ./...` | Passed |
| `git diff --check` | Passed |
| `bash -c 'cd testdata/external-plugins && go list ./... && go build ./...'` | Passed; package and compilation evidence |
| `go test -json -race -count=1 -timeout=240s ./...` | 1,718 unit leaf passes, no failures or skips |
| `go test -json -race -count=1 -timeout=3600s -tags=integration ./...` | Two complete censuses; each has 1,229 leaf passes and seven skips, with no failures |
| `go test -json -count=1 -run "^$" ./...` | Passed; compilation only |
| `go test -json -count=1 -tags=integration -run "^$" ./...` | Passed; compilation only |

The broader automatic selection passed 460 leaf executions, including 20 executions of each of the five lifetime cases:

```sh
go test -json -race -count=20 -timeout=1800s -tags=integration ./host/internal/app -run "^Test(Automatic.*|AbsentOwnerDispatchesOversizedHistory|ThresholdCompactionPreservesSplitBoundaryCause)$"
```

The two canceled-disposition/independent-error regressions each passed 200 executions:

```sh
go test -json -race -count=100 -timeout=1800s -cpu=1,2 -tags=integration ./host/internal/app ./host/internal/infra/plugins/extension/runtime -run "^Test(AutomaticDecisionRetainsDurableOutcome|PolicyCancellationDecisionRetainsDiagnostic)$/^canceled-diagnostic$"
```

The seven client EOF cases passed 1,400 leaf executions:

```sh
go test -json -race -count=100 -timeout=300s -cpu=1,2 -tags=integration ./sdk/plugins/extension/v1 ./host/internal/infra/plugins/ui/runtime -run "^(TestConnection(SendEOFWaitsForReceive|CleanReceiveEOFResolvesOwnedAcknowledgements)|TestRunOperationsSendEOFRetainsReceiveStatus)$"
```

Preservation checks also covered manual composition, participant lifetimes, session projection, configured execution, received cancellation, Core outcomes and SDK/UI/TUI consumers. The runtime settlement/received-status selection passed 12,200 leaf executions. Explicit migrated selectors checked policy-owned replacement and final outgoing-budget rejection. Separate selectors retained legacy overflow-growth and terminal second-overflow checks for UNT-10. The policy-owned split-boundary test retained its structural rejection assertions.

Leaf counts exclude suite ancestors and package events. External default `go test -json -count=1 ./...` found no tests and supplies compilation evidence only. Package-level no-test events and unmatched packages inside targeted selections remain separate from behavioral counts.

## Evidence limits and retained failures

The seven [platform exclusions](observation-integration-evidence.md#platform-limits) remain unchanged. Both explicit full integration censuses reported those same seven unique skipped tests. Acceptance makes no claim that the Darwin/PTY paths passed.

The [UNT-08 historical process limits](observation-integration-evidence.md#historical-process-limits) remain part of the acceptance record. They grant no exception for later commands.

During an earlier UNT-09 matrix attempt, a helper ran setup, storage diagnostics, temporary build-directory cleanup and audit commands without advance log/result reservation. Its `rm -rf /tmp/go-build*` cleanup had no deletion manifest. The deleted contents are unknown. Later custody checks establish the retained command records and currently readable earlier artifacts; they cannot reconstruct the deleted contents or retrospective reservation compliance. Final verification used a controlled recorder and performed no shared temporary cleanup. Main accepted the result with this historical limitation disclosed and granted no future exception.

Earlier failed attempts remain separate from final-source results:

- Intentional owner loss exposed an incorrect clean-shutdown expectation in the new lifetime test. The test now checks either clean shutdown or the acquired child cancellation with restricted transport causes, while retaining cleanup joins and rejection of unrelated errors.
- A corroborated inactive-target response exposed an incorrect empty-message expectation for pre-commit cancellation. The test retains the canceled terminal requirement and checks the cause and settlement diagnostic in every nonempty message.
- The missing-original-terminal test exposed premature client EOF publication. A passing isolated rerun did not resolve that failure. Controlled regressions and the corrected client boundaries precede the final broader repeated passes.
- Parallel fixture execution exposed two `text file busy` failures. The guarded writer correction precedes the final shared-package pass.
- Compile, setup, lint and audit-method failures retain their original outcomes. Later GREEN results do not turn those attempts into successful checks or valid behavioral RED evidence.

## Open questions

None for UNT-09 acceptance. The next implementation unit is UNT-10; the phase remains subject to its remaining migration and cleanup work.
