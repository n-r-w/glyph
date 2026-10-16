# PHS-06.3 migration residue closure

## Scope

This phase checks the implementation after PHS-06.2 A1-A4 and prepares it for independent acceptance. The starting revision is `80493a7dbc50846f9c4fba448c5e7183eec35e93`. The changes described here are uncommitted.

The [migration plan](migration-plan.md#phase-phs-063---close-migration-residue-and-documentation) defines the scope and exit criteria. PHS-06.4 remains responsible for independent acceptance of the complete migration against checkpoint `690827f`.

## Cleanup and retained boundaries

The source inspection covered manual entry adapters, conversation preparation and recovery, policy composition, runtime transport, session mutation, configuration, and public compaction contracts.

| Boundary | Inspected implementation and result |
| --- | --- |
| Manual entry points | `host/internal/usecase/host/modelexecution/manual.go` builds model input and maps results for UI, Programmatic Control, and Extension callers. These adapters retain their consumer-owned contracts. |
| Conversation preparation and overflow | `modelexecution.Service.Stream` uses its captured conversation owner. `extensioncontext.capturedConversation.prepare` invokes that owner and obtains session projections. `RecoverOverflow` uses the same preparation path. These Host components contain no token threshold or retained-history selection. |
| Policy and composition | `plugins/extension/compaction/internal/usecase/policy/service.go`, `Service.Compact` and `Service.complete`, contain trigger decisions, ordered participant composition, generator selection, candidate acceptance, and commit requests. |
| Sizing and configuration | Token estimators reside under `plugins/extension/compaction/internal/domain/compaction`. Host's `CountCompactionPrivateBytes` counts private payload bytes only. Host's compaction settings contain `ParticipantTimeout`; the bundled extension owns `GLYPH_COMPACTION_RETAINED_TOKENS`. |
| Session mutation | `sessions.Service.CaptureCompaction`, `ProjectReplacement`, and `CommitReplacement` provide captured input, structural projection, and persistence through the existing session commit boundary. Their consumer contract remains in `extensioncontext`. |
| Removed Host policy | Repository search found no Go reference to `contextcompaction`. The prior removal and retained accounting contracts are documented in the [PHS-06.1 cleanup record](system-integration-evidence.md#cleanup-and-preservation). |

No superseded Host policy, compaction compatibility alias, or duplicate Host token estimator was found in these paths. The remaining controller, runtime, and session contracts serve the extension-owned implementation.

The cleanup removes `mapCompactionTrigger` from `host/internal/infra/plugins/extension/runtime/compaction.go`. Its sole caller, `mapCompactionRequest`, now performs the same enum conversion directly. The conversion and wire values are unchanged.

The `SessionTreeEntry` comment in `api/plugins/extension/v1/session_entry.proto` now identifies its use by branch-summary and compaction operations. Its content comment distinguishes public content from metadata for model-hidden extension entries. Regeneration updates the corresponding public Go comments; the message fields and wire format are unchanged. Mock generation produces no changes.

The parent technical solution separates the implemented integration contract from historical checkpoint and design evidence. The PHS-06.2 solution labels its design dry run separately from later implementation evidence. Phase status remains owned by the migration plan.

No behavior change or new test was needed. The enum conversion cleanup uses the behavior-preserving refactoring exception to RED/GREEN; contract-comment and documentation edits use the documentation exception.

## Verification

All commands below passed on 2026-10-16 after the last executable-source change. The environment was Linux amd64 with Go 1.27.2. Documentation changes followed the executable checks; the final diff check ran after documentation closure.

| Command | Result |
| --- | --- |
| `task generate`, twice | Passed; identical working-tree diffs after both runs |
| `task fmt` | Passed; no changes |
| `task fix_dry_run` | Passed; empty proposal, so no fixes were needed |
| `task lint` | Passed |
| `task test` | Passed, including external-plugin fixtures |
| `task itest` | Passed, including external-plugin fixtures |
| `task test-coverage` | Passed; 84.0%, required 80.0% |
| `task build` | Passed |
| `go list ./...` | Passed |
| `go list -tags=integration ./...` | Passed |
| `go build -tags=integration ./...` | Passed |
| `bash -c 'cd testdata/external-plugins && go list ./... && go build ./...'` | Passed |
| `go test -json -race -count=1 -tags=integration -p 1 -parallel 1 ./...` | Passed; 1,454 test pass events, zero test failures, seven skips |
| `git diff --check` | Passed |

The JSON test count includes parent tests and subtests; it is not a leaf-test census. The separate uncached integration run exposes skips hidden by the ordinary task output. Coverage also runs uncached unit and integration tests.

The integration run skipped these platform-specific tests:

| Test | Required platform capability |
| --- | --- |
| `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Darwin permission enforcement |
| `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Darwin `chflags` for immutable-file recovery injection |
| `TestLifecycleObserverThroughStandardTUI` | Darwin arm64 PTY execution |
| `TestStandardTUIHostSmoke` | Darwin arm64 PTY execution |
| `TestStandardTUIHostRuntimePersistenceFailures` | Darwin arm64 PTY execution |
| `TestEarlyFailureCleanup` | Darwin arm64 PTY execution |
| `TestStandardTUIPTY` | Darwin arm64 PTY execution |

## Remaining limits

PHS-06.3 has no open cleanup finding. The working changes are ready for independent review. This phase does not perform PHS-06.4 independent acceptance. Live-provider behavior and tokenizer accuracy remain outside the local verification evidence.

The [existing parent-context debt and separate production panic issue](system-integration-evidence.md#remaining-limits-and-historical-failures) remain outside this cleanup. No new workaround is introduced.
