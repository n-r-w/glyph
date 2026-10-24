# Tree snapshot and empty replay evidence

## Source locations

Paths below are relative to the repository root; implementation and test references in this document use this mapping.

| Symbols | Repository file |
| --- | --- |
| `session.Tree.Clone`, `NewTree` | [host/internal/domain/session/tree.go](../../../../host/internal/domain/session/tree.go) |
| `LoadedSession.Clone` | [host/internal/usecase/host/sessions/clone.go](../../../../host/internal/usecase/host/sessions/clone.go) |
| `sessions.Service.Tree`, `sessions.Service.ResumeActive`, `Service.appendEntryLocked` | [host/internal/usecase/host/sessions/service.go](../../../../host/internal/usecase/host/sessions/service.go) |
| `Service.CommitNavigation` | [host/internal/usecase/host/sessions/navigation.go](../../../../host/internal/usecase/host/sessions/navigation.go) |
| `Service.SetLabel` | [host/internal/usecase/host/sessions/replacement_operations.go](../../../../host/internal/usecase/host/sessions/replacement_operations.go) |
| `newReplayState` | [host/internal/infra/persistence/sessions/mutations.go](../../../../host/internal/infra/persistence/sessions/mutations.go) |
| `session.TestCloneRejectsAliasedTreeState` | [host/internal/domain/session/clone_failure_test.go](../../../../host/internal/domain/session/clone_failure_test.go) |
| `sessions.TestSnapshotFailurePreservesActiveState`, `sessions.TestSessionSnapshotsReportInvalidLabels`, `sessions.TestNavigationRetainsCommitAfterSnapshotPublicationFailure` | [host/internal/usecase/host/sessions/snapshot_failure_integration_test.go](../../../../host/internal/usecase/host/sessions/snapshot_failure_integration_test.go) |
| `sessiontree.TestNavigatePropagatesSnapshotFailure` | [host/internal/usecase/host/sessiontree/service_test.go](../../../../host/internal/usecase/host/sessiontree/service_test.go) |
| `programmatic.TestSessionTreeQueryReportsSnapshotFailure` | [host/internal/usecase/host/programmatic/tree_test.go](../../../../host/internal/usecase/host/programmatic/tree_test.go) |
| `ui.TestGetSessionTreeOperationReportsSnapshotFailure` | [host/internal/usecase/host/ui/tree_test.go](../../../../host/internal/usecase/host/ui/tree_test.go) |
| `TestEmptyReplayRetainsMutationValidation` | [host/internal/infra/persistence/sessions/empty_replay_integration_test.go](../../../../host/internal/infra/persistence/sessions/empty_replay_integration_test.go) |

## Scope

UNT-04 retains `session.Tree.Clone` reconstruction through `NewTree` and returns validation errors. `LoadedSession.Clone`, `sessions.Service.Tree`, resume, append, navigation, label changes, and consumer-owned UI, Programmatic, and navigation interfaces propagate snapshot failures.

The whole issue remains in progress. Option extraction, regular-expression initialization, and the later failed-terminal transport changes are outside this unit.

## Ownership and commit boundaries

`sessions.Service.ResumeActive` reconstructs the loaded tree before replacing active state or restoring write access. Append, navigation, and label operations reconstruct candidates before persistence. Navigation and label operations also validate their independent caller snapshots before persistence, so these snapshot failures cannot follow their durable commits.

After navigation persistence succeeds, the service retains the committed receipt when publication fails. The receipt contains the committed tree and summary. The navigation consumer retains the complete publication error through its current issue/source mechanism. This unit preserves that mechanism; it does not introduce the later true-Failed terminal transport.

## Behavioral evidence

- `session.TestCloneRejectsAliasedTreeState` starts with an initialized empty tree, copies its value, and adds a valid root entry through the alias. It checks both an active leaf set through the shared lookup and a label set through the alias. Reconstruction returns the complete diagnosis, followed by a successful valid clone in the same process.
- `sessions.TestSnapshotFailurePreservesActiveState` exercises navigation, label, append, and resume with public Tree aliasing. Strict generated repository mocks permit no write. Active state remains unchanged and write access remains available.
- `sessions.TestSessionSnapshotsReportInvalidLabels` checks aggregate and active-session snapshot errors, followed by valid snapshots in the same process.
- `sessions.TestNavigationRetainsCommitAfterSnapshotPublicationFailure` commits navigation before a publisher attempts an independently invalid snapshot. The caller receives both the committed receipt and complete error. Active position, entries, and committed storage path remain available.
- `sessiontree.TestNavigatePropagatesSnapshotFailure` checks original cause identity and prevents handler and commit calls.
- `programmatic.TestSessionTreeQueryReportsSnapshotFailure` checks the public rejection retains the cause and contains no successful tree result.
- `ui.TestGetSessionTreeOperationReportsSnapshotFailure` checks the retained operation returns the complete cause without a successful tree frame.
- Persistence `TestEmptyReplayRetainsMutationValidation` checks empty replay and rejection of navigation and label records targeting missing entries. Existing replay, recovery, and restart tests cover valid mutations.

All new tests combining production components use the integration build tag. Consumer tests with generated mocks and the domain Tree tests use the unit build tag.

## RED/GREEN and limits

The starting revision is `354218d2f676a4fc2a9f03ae70c61eb6d00c7a1d`. Separate baseline runs reproduce `active leaf does not exist` and `label target "x" does not exist` panics through the public alias sequences. Baseline navigation, label, append, resume, and post-commit publication runs also panic. Updated signatures require the baseline test sources to use the original single-value snapshot calls. The corrected tests require errors and verify continued operation. The retained execution record identifies these RED commands, each with exit 1 and a runtime panic in the saved output:

| Baseline command | Observed panic |
| --- | --- |
| `go test ./host/internal/domain/session -run 'TestCloneRejectsAliasedTreeState/active_leaf' -count=1` | `active leaf does not exist` |
| `go test ./host/internal/domain/session -run 'TestCloneRejectsAliasedTreeState/label' -count=1` | `label target "x" does not exist` |
| `go test -tags=integration ./host/internal/usecase/host/sessions -run 'TestSnapshotFailurePreservesActiveState/navigation' -count=1` | `active leaf does not exist` |
| `go test -tags=integration ./host/internal/usecase/host/sessions -run 'TestSnapshotFailurePreservesActiveState/label' -count=1` | `active leaf does not exist` |
| `go test -tags=integration ./host/internal/usecase/host/sessions -run 'TestSnapshotFailurePreservesActiveState/append' -count=1` | `active leaf does not exist` |
| `go test -tags=integration ./host/internal/usecase/host/sessions -run 'TestSnapshotFailurePreservesActiveState/resume' -count=1` | `active leaf does not exist` |
| `go test -tags=integration ./host/internal/usecase/host/sessions -run TestNavigationRetainsCommitAfterSnapshotPublicationFailure -count=1` | `active leaf does not exist` after persistence |

These runs used adapted baseline signatures; they are runtime RED evidence, not unchanged-source executions of the new error-return tests.

Mock-backed consumer tests verify the newly representable error result and preserve cause identity. Baseline interfaces have no error result, so those tests cannot run unchanged against the starting revision. No current Host request executing the alias sequence has been identified; the sequence is a public Tree API capability.

Constant empty reconstruction cannot fail under `NewTree` validation. Empty replay therefore has source-level panic removal and successful-path regression evidence, rather than a claimed behavioral failure reproduction. Supplied replay mutations continue to use their validation paths.

## Verification

The starting revision passed `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build`. Combined baseline coverage was 84.2%, with an 80% requirement.

The retained implementation execution record reports the following final targeted GREEN commands. Saved outputs show all six packages passing; the execution record reports exit 0 for both commands. Command filters and baseline signature adaptation are recorded by the execution record, rather than inferred from bare package output.

```sh
go test -race ./host/internal/domain/session ./host/internal/usecase/host/sessiontree ./host/internal/usecase/host/programmatic ./host/internal/usecase/host/ui -run 'TestCloneRejectsAliasedTreeState|SnapshotFailure' -count=1
go test -race -tags=integration ./host/internal/usecase/host/sessions ./host/internal/infra/persistence/sessions -run 'TestSnapshotFailure|TestSessionSnapshotsReportInvalidLabels|TestNavigationRetainsCommitAfterSnapshotPublicationFailure|TestEmptyReplay' -count=1
```

The final implementation checks had these observed results:

| Command | Observed result |
| --- | --- |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0, empty proposal; no fix needed after analysis |
| `task lint` | Exit 0, zero issues, ifaceguard clean, no vulnerabilities |
| `task test` | Exit 0, including external-plugin module |
| `task itest` | Exit 0, including external-plugin module |
| `task test-coverage` | Exit 0, combined coverage 84.3%, required 80.0% |
| `task build` | Exit 0, Host and bundled plugins |
| `git diff --check` | Exit 0, no output |

After the documentation and six snapshot-contract comment corrections, both targeted GREEN commands above were rerun and exited 0. The full command table was also rerun: every command exited 0, the fixer proposal remained empty, lint reported zero issues and no vulnerabilities, and combined coverage remained 84.3% against the 80.0% requirement. A Go scanner comparison against the pre-correction source snapshot found identical non-comment tokens in all 1,248 Go files. Exactly eight files changed bytes: this document, the ticket, and the six contract-comment files.

These are observed corrected-code runs, distinct from the baseline RED runs and the source-only conclusion about constant empty replay.

## Open questions

None for this unit. The broader issue retains the remaining scope listed in the [ticket](ticket.md).
