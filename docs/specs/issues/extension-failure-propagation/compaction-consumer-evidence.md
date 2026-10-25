# Compaction consumer correction evidence

## Scope and source states

O9-1 authorizes the bounded UNT11 TUI receipt correction and replacement-caller regression. O10-1 authorizes the subsequent local session-association and membership-only corrections, followed by the separate text corrections. UNT12 final verification and both issue closures remain pending.

The Git comparison baseline remains `a2bbf27e93b61e22371ad3763b790627995d6c3e`. The first consumer correction started from the preserved 1,501-file producer delivery. The local ownership correction started from the preserved 1,504-file consumer delivery and finished with 1,506 files. These are uncommitted source captures, not intermediate Git revisions. The code part changed eight TUI paths and preserved the other 1,498 incoming paths byte for byte. Complete captures include both modules' dependency files. The [producer evidence](compaction-evidence.md) retains historical producer results, including its 84.7% coverage; those runs did not establish TUI receipt application.

## Receipt decoding and application

`DecodeCompleted` and `mapCompactionCompletion` in `plugins/ui/tui/internal/controller/plugin/request_mapping.go` validate and map the actual saved entry through the entry mapper. Previously the decoder discarded `CompactionResult.Committed`, although `Controller.Notify` already forwarded failed completion and complete failure. `Payload.CommittedCompaction` in `plugins/ui/tui/internal/controller/plugin/payload.go` now carries the local entry projection beside diagnostic or settlement input. The wire contract remains unchanged.

For a confirmed displayed owner, `Service.applyCompactionInput` in `plugins/ui/tui/internal/usecase/presentation/compaction_session.go` forwards the receipt to `Service.applyInput` in `input.go`. That method applies the entry before its payload diagnostics. `Service.Notify` in `commands.go` performs terminal bookkeeping and independent failure display. D3 requires saved-result application and error display; it imposes no global receipt-before-error ordering obligation.

`interaction.applyCompactionEntry` in `plugins/ui/tui/internal/usecase/presentation/compaction.go` records ID membership in `interaction.compactionEntries`, a `map[string]struct{}` declared in `model.go`. It renders the supplied summary through `LineCompaction`, including with the tree panel closed. For an identity already applied in the displayed session, the method returns immediately. Different IDs with identical text remain distinct. On entry publication, `interaction.applyTreeEvent` in `tree_interaction.go` invokes the same projection. When Host confirms replacement, `interaction.applyEvent` in `update.go` and `interaction.applyTreeReplacement` in `tree_interaction.go` clear receipt membership. Closing the panel preserves membership.

The local terminal projection contains the saved ID, timestamp, kind, and summary. The terminal entry supplies no parent or label. The decoder does not insert a temporary parentless mapping into the tree. Actual publication and tree queries supply topology. Earlier publication's parent, label, and active leaf survive receipt application. This evidence establishes identity-based application in the checked receipt/publication paths, not universal cardinality of restored or navigation-replaced transcripts.

## Local operation and session association

`Service.prepareCommand` in `plugins/ui/tui/internal/usecase/presentation/commands.go` associates each command with preceding replacement requests through `compactionSession`. This private implementation type represents a displayed-transcript lifetime, not a stored session ID or Host authority. `Command.replacesSession` in `compaction_session.go` recognizes create, resume, fork, and clone. A replacement starts a tentative association with its predecessor. `Service.resolveCompactionSession` confirms replacement only from actual SessionChanged, TreeForked, or TreeCloned input, independently of Completed versus Failed status. Without replacement data, it rejects the tentative association. `Service.Complete` also rejects an undelivered replacement; a late local result cannot undo confirmation already established by correlated Host output.

`compactionSession.owner` follows rejected requests to their predecessor and leaves pending ownership unresolved. Confirmation releases the predecessor link. Before a subsequent dispatch, `Service.prepareCommand` collapses a resolved rejected latest association. Pending correlations and unresolved receipt deliveries retain only associations they still need.

The request-order inference follows existing production boundaries: `interaction.emitCommand` in `presentation/session_view.go` and acknowledgement handling enable later dispatch only after earlier enqueue; infra Host `Service.Send` in `plugins/ui/tui/internal/infra/host/service.go` calls SDK `Host.Start` in `sdk/plugins/ui/v1/host.go` synchronously. `Writer.enqueue` and `Writer.Run` in `internal/operation/writer.go` preserve request insertion order. Host UI `receiveOperations` and `startPreparedHostOperation` in `host/internal/controller/ui/operations.go` prepare requests serially. `Owner.Start` in `internal/operation/operation.go` prepares before launching its worker. UI `Session.prepareCompaction` and `Session.prepareSessionOperation` in `host/internal/usecase/host/ui/prepared_operations.go` acquire the same gate; `isUISessionMutation` includes all four replacements. A later receipt-bearing compaction therefore runs after accepted replacement or in the unchanged session after rejection.

`Owner.run` releases prepared ownership before terminal delivery. Accepted asynchronous workers can therefore enqueue terminals out of request order. This is source-permitted reordering, not a complete transport observation or frequency measurement. FIFO preserves insertion order, not mutation order for terminals not yet enqueued.

The consumer handles these cases:

- After replacement B confirms, a delayed terminal from compaction A retains A's association. `Service.applyCompactionInput` omits A's receipt from B while `Service.Notify` preserves full diagnostics and normal correlation settlement.
- A compaction dispatched after replacement B's request captures B's tentative association. If B confirms first, the consumer applies the receipt directly. If the compaction terminal arrives first, the consumer displays full diagnostics and settles correlation immediately, retaining the receipt until confirmation.
- If replacement is rejected, `compactionSession.owner` resolves to the predecessor. A valid subsequent compaction receipt applies to that unchanged displayed session.

For example, supply a `/new` request, then a correlated Failed compaction terminal containing saved entry E and a two-line publication error before the replacement confirmation. The TUI shows the complete error and consumes the compaction terminal while withholding E's summary. On successful confirmation, `Service.applyDeferredCompactions` applies E to the new transcript and restores diagnostics removed by replacement. A duplicate terminal remains rejected after settlement. This is a consumer-input example, not evidence that default policy compacts an empty new session.

`Service.applyDeferredCompactions` retains only unresolved terminal deliveries. When ownership resolves, it applies a matching receipt or omits a stale receipt. If replacement cleared the earlier diagnostics, it restores that terminal's text and complete independent failure through `Service.applyOperationFailure` in `commands.go`. It repeats neither acknowledgement nor correlation settlement and does not replay healthy PayloadSettled. Thus receipt replay preserves a newer retry indication.

Create/resume share SessionChanged decoding and presentation. Fork/clone share `interaction.applyTreeReplacement`. All four share local classification, Host gate admission, and ownership resolution. New/clone crossing is executed; resume/fork coverage here consists of shared-path source tracing and existing suites, not separate new crossing executions. Resuming the same stored session still replaces the displayed transcript lifetime.

Uncorrelated connection publication continues through its separate existing `Service.Notify` path. It can project later-session content before delayed replacement confirmation. No new publication-owner guarantee, lifecycle, or requirement is added. Displaying a diagnostic only once remains optional (SUG-01). The implementation adds no general text-deduplication mechanism or one-line diagnostic contract. Full multiline failures remain visible.

## Caller-visible regressions and assertion reach

`TestCompactionReceiptNotification` and `TestCompactionReceiptIdentity` in `plugins/ui/tui/internal/usecase/presentation/compaction_receipt_integration_test.go` combine production decoding and presentation with generated Host/Display boundary mocks. They exercise public Key, Execute, Complete, and Notify paths and inspect published snapshots and returned errors. The notification cases preserve failed receipt application with/without publication and with closed panels, owning-cancellation diagnostics, healthy completion, prior-publication idempotence, real metadata, and absence of invented topology. The identity test covers distinct IDs with equal text and reuse after replacement. Receipt application precedes diagnostic snapshots in these confirmed-owner cases only.

`TestCompactionCompletionRejectsInvalidReceipt` in `plugins/ui/tui/internal/controller/plugin/request_mapping_test.go` rejects an incomplete saved entry. It supplies no diagnostic and proves no presentation ordering. Existing diagnostic and explicit Cancel decoder tests are preservation checks.

`TestCompactionReceiptSessionCrossing`, `TestCompactionReceiptPendingReplacement`, `TestCompactionReceiptRejectedReplacement`, and `TestCompactionReceiptReplayPreservesLaterRetry` in `plugins/ui/tui/internal/usecase/presentation/compaction_session_integration_test.go` add seven leaf cases. Generated Host mocks verify actual dispatched command kinds. The shared helper clears the retained editor draft through public Home/Delete keys before typing the next command.

| Source and selection | Actual assertion reach |
| --- | --- |
| Incoming local-ownership source, new/clone crossing | Both leaves stop at an unwanted summary: expected zero, actual one. Later full-error and duplicate-terminal assertions are GREEN-only observations. |
| Incoming source, terminal before replacement confirmation/local acknowledgement | Stops at missing final summary: expected one, actual zero. Immediate diagnostics and local acknowledgement execute first; later final-error/duplicate assertions do not. |
| Incoming source, replacement before terminal and both rejection-input orders | Already pass. These leaves are preservation evidence. |
| Incoming source, healthy replay | Stops at missing summary before the retry assertion. |
| Healthy replay draft with final-identical test inputs | Applies the receipt, then fails because newer retry text is cleared. Matching final GREEN preserves the retry text. |
| Exact Git-original replacement caller mutation | `TestPolicyReplacementRetainsCommittedFailure` stops at missing completion. Final GREEN reaches actual receipt, complete result/error text, and original-cause assertions. |
| Final source, matching selections | All seven ownership leaves and preserved receipt cases pass, reaching later full-error and settlement assertions. |

The replacement test in `host/internal/controller/extension/service_test.go` calls actual `contextOperation.runPolicyCandidate` with the generated policy dependency. The correct caller in `compaction_candidate.go` and this test remain byte-identical to the incoming ownership source. Its original mutation restores only that caller to exact Git-original bytes.

The earlier consumer correction's exact-input incoming runs separately failed six notification leaves at missing summaries and the identity leaf at zero versus two, before replacement reuse. Invalid-receipt decoding failed at the missing error. The incoming replacement caller already passed. Its original-caller mutation failed at missing completion. The original public/component/owner producer replay remains historical evidence in the producer record. Later passing assertions are not credited to these fatal RED boundaries.

Terminal-before-synchronous-BUSY rejection is a defensive component permutation. `operationDelivery.Reject` in `host/internal/infra/plugins/ui/runtime/operations.go` enqueues rejection synchronously before the serial receiver prepares later compaction. Healthy FIFO transport therefore cannot deliver that later terminal first. Accepted asynchronous replacement failure can be overtaken, but has a different source/category. Immediate `/new` plus injected receipt verifies consumer handling, not default-policy empty-history compaction. The tests do not execute the full Host/SDK interleaving or a PTY.

Setup/compiler/no-matching-test failures and retained-editor-draft failures are excluded from behavioral RED. Earlier consumer draft observations have limited source identity: the original three-case draft's complete source identity is unconfirmed, and a later open-tree capture does not establish it. Revised NewTextLine compilation failures are setup failures. The ownership replay draft, incoming regression overlay, and exact-original caller mutation have complete source captures and final-identical regression inputs.

## Current commands, checks, and preservation

The final code ledger records these exact command bodies. Each focused command uses `-race -count=1`; integration commands serialize packages/tests.

```sh
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./plugins/ui/tui/internal/usecase/presentation -run '^TestCompactionReceipt(SessionCrossing|PendingReplacement|RejectedReplacement|ReplayPreservesLaterRetry)$' -v
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./plugins/ui/tui/internal/usecase/presentation -run '^TestCompactionReceiptReplayPreservesLaterRetry$' -v
go test -race -count=1 -tags=integration -p 1 -parallel 1 ./plugins/ui/tui/internal/usecase/presentation -run '^TestCompactionReceipt(Notification|Identity)$' -v
go test -race -count=1 ./host/internal/controller/extension -run '^TestPolicyReplacementRetainsCommittedFailure$' -v
```

The ownership incoming and replay-draft selections exit 1; matching final selections exit 0. Receipt preservation exits 0. The original-caller mutation exits 1 and final replacement selection exits 0. The external execution ledger records literal CWD, command-local TMPDIR, exits, and complete source verification before/after every run. Temporary execution locations are kept outside repository prose.

The same final ledger records `task generate`, `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build`, each exiting 0 after the last code edit. The dry-run proposes no changes. Current combined coverage is 84.8%, required 80.0%. Task-defined external subcommands use `testdata/external-plugins` beneath the source root and inherit the command-local TMPDIR. Full tasks retain Taskfile caching defaults; some unchanged package results are cached. Focused runs disable result caching explicitly.

The text part changes only this record, the linked solution's consumer summary, and the invalid-receipt test's descriptive comment. Complete source captures and executable Go token comparison support reuse of the accepted code checks; prose editing requires no new full code-task run. Root and external dependency files, directives, fixtures, test logic, and executable behavior remain unchanged. Host ownership, SDK settlement, protocol schema, mutation authority, and dependencies remain unchanged. No commit, push, UNT12 work, or issue closure was performed.

The active symbolic index omits integration-tagged references; source reading and compiled evidence supplement it. No exhaustive index claim is made. No unresolved question blocks this text correction. UNT12 and both issue closures remain pending.
