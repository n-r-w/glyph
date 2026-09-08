# U8 implementation evidence

U8 implementation and main-agent verification are complete. Independent integrated review subsequently found [FND-10 and FND-11](audit.md#fnd-10-codex-streaming-failures-truncate-source-text). Their [U6 correction](solution.md#u6-follow-up-complete-source-and-delivery-causes) is committed as `b03095a`. Review of that commit found FND-12 through FND-14. The O72-1 [retention correction](solution.md#u6-retention-correction) is committed as `ded1549`. Its independent review found FND-15 through FND-17. The user approved the [D1/D2 corrections](solution.md#qst-05-runtime-completion-meaning-and-tui-submission-causality). [D2 implementation and main-agent checks](solution.md#u5-causality-verification) are complete. [D1 implementation and main-agent checks](solution.md#u6-runtime-completion-evidence) are complete. Review of `cb3a8f6` found FND-18 through FND-20. O74-1 includes FND-21. The [producer correction](solution.md#u6-producer-preservation) has implementation and main-agent verification evidence. The separate [FND-22 process correction](solution.md#process-cancellation-evidence) has implementation and main-agent verification evidence. The [FND-23 dispatch-confirmation correction](solution.md#host-command-dispatch-confirmation-correction) has implementation and main-agent verification evidence under O78-1. The separate U8 smoke-fixture correction and fresh independent review remain pending. PHS-07.1 is blocked and explicit user acceptance remains pending. PHS-07 remains paused. This record supplements the [solution](solution.md#8-remove-residue-and-verify-the-whole-product), not the historical [audit baseline](audit.md#package-and-contract-coverage).

## Scoped changes

- Implementation assertions now follow their implementing structs in provider selection, operation admission, tool registration, all three mode outputs, and terminal device I/O. Duplicate Codex provider and session-state assertions are removed. No consumer interface, package dependency, runtime binding, or production behavior changes.
- [The Programmatic closure fixture](../../../../../../host/internal/controller/programmatic/service_terminal_test.go), `TestHostClosurePreservesWriterFailure`, waits for `Recv` entry before application cancellation. After the writer fails, it releases the receive callback and waits for its completion signal before mock cleanup. The exact receive expectation, `Unavailable` status, and complete writer-error assertion remain. Production `Service.open` and `Service.receive` are unchanged.
- The two startup authentication classification tests now share a table. Each case retains its source classification, ordered error/availability output, and explicit-retry requirement. Formatting corrects six phase-introduced long lines in UI tests and one in the Extension SDK test.

These changes need no artificial executable RED. Production changes only move or remove duplicate declarations. Test changes repair fixture synchronization or preserve existing assertions. The approved behavior corrections retain their executable RED/GREEN evidence under [U2, U6, and U7](solution.md#implementation-evidence).

## Finding dispositions

Each row states the implementation disposition. It does not claim independent whole-product acceptance.

| Finding | Resulting owner and source evidence |
| --- | --- |
| FND-01 | [App assembly](../../../../../../host/internal/app/sessions.go) initializes storage and constructs concrete owners. Provider catalogue binding goes directly to sessions and sessiontree. [UI startup output](../../../../../../host/internal/infra/plugins/ui/runtime/startup.go) owns report projection and warning delivery. No pricing/model forwarding owner remains. |
| FND-02 | [Run control](../../../../../../host/internal/usecase/host/runcontrol/coordinator.go) owns reservations, Core invocation, and owner-reported settlement. [Dispatcher](../../../../../../host/internal/usecase/host/events/service.go) delivers to clients before observers and joins both causes. [UI input](../../../../../../host/internal/controller/ui/operations.go) owns command operations. [Programmatic output](../../../../../../host/internal/infra/programmatic/output/service.go) owns active-output correlation and its attached writer. Sessions owns entry publication. The removed forwarding packages are accounted for below. |
| FND-03 | [UI consumer ports](../../../../../../host/internal/usecase/host/ui/interfaces.go), [Programmatic consumer ports](../../../../../../host/internal/usecase/host/programmatic/interfaces.go), and their navigation contracts own client-specific inputs/results. [Runtime ports](../../../../../../host/internal/usecase/host/extensionruntime/interfaces.go) consume process representations, not borrowed capability aggregates. [TUI input contracts](../../../../../../plugins/ui/tui/internal/controller/plugin/interfaces.go) and [outgoing ports](../../../../../../plugins/ui/tui/internal/usecase/presentation/interfaces.go) have separate consumers. |
| FND-04 | [Repository `formatVersion`](../../../../../../host/internal/infra/persistence/sessions/service.go) and replay own version 2. Domain headers and session construction have no storage version selector. |
| FND-05 | [U6 source traces and regressions](solution.md#u6-complete-error-text) cover SDK diagnostics, Codex HTTP 401, navigation validation, and unknown labels. U8 changes no error behavior. The [follow-up correction](solution.md#u6-follow-up-complete-source-and-delivery-causes) addresses FND-10/FND-11. FND-12 through FND-14 have [retention implementation and local check evidence](solution.md#u6-retention-correction) at `ded1549`. Review of `cb3a8f6` closes FND-15 at its D1 scope but finds new producer losses. [FND-18 through FND-21](solution.md#u6-producer-preservation) have main-agent corrective evidence and still require integrated verification. [FND-22 process verification](solution.md#process-cancellation-evidence) is complete locally; main review remains pending. |
| FND-06 | [U7 source traces and regressions](solution.md#u7-run-persistence-cause-and-tui-cleanup) cover the shared history-persistence identity, both public run failures, and semantic TUI cleanup. No text-prefix policy remains. |
| FND-07 | [Extension acceptance](../../../../../../host/internal/usecase/host/extensionruntime/discovery.go) and [UI acceptance](../../../../../../host/internal/usecase/host/ui/discovery.go) interpret filesystem observations. [Bash execution context](../../../../../../plugins/extension/tools/internal/usecase/tools/bash/timeout.go) owns timer creation, cause, and cleanup. |
| FND-08 | [UI ports](../../../../../../host/internal/usecase/host/ui/interfaces.go) omit unused direct `Run` and output `Close`. Named selection, preparation, and navigation failures remain at consumers. [Provider errors](../../../../../../host/internal/usecase/host/providers/catalog.go) assert those contracts beneath `SelectionError`. SDK, writer, and rune-reader assertions remain at their implementations. U8 completes declaration placement and removes duplicate assertions. |
| FND-09 | [Presentation `Service`](../../../../../../plugins/ui/tui/internal/usecase/presentation/service.go) owns private interaction state. [Commands](../../../../../../plugins/ui/tui/internal/usecase/presentation/commands.go) own pending/foreground decisions and notification transitions. [Terminal `Model.Update`](../../../../../../plugins/ui/tui/internal/infra/terminal/model.go) dispatches input on the Bubble Tea loop and runs prepared I/O asynchronously. The former presentation-domain package is removed. |

[BLK-01 through BLK-05](tui-ownership-gap.md#blockers) retain their completed U5/U7 source traces. Resume rejection, navigation progress before terminal input, confirmation before replacement, command-send failure, and foreground cancellation remain with the application owner. U8 changes no TUI transition.

## Package coverage accounting

The comparison uses production baseline `86be985c47cb3719cd98c7e611af6173c5692cfc` and the U8 working tree above `405338e0b00f7c8764d5310377e8a17369fdaf9e`. It excludes test files, generated mocks, experiments, fixtures, and test support from handwritten production counts.

All 72 baseline production packages are accounted for. There are 39 retained packages with changed source, 28 with byte-identical production source, and five removed packages. Five added owners leave 72 production packages. Root `go list` also contains three test-support packages. Handwritten production files increase from 273 to 324. The 16 public protobuf sources and four generated production packages retain their baseline contents.

### Retained packages with changed source

Paths and symbols identify the resulting owners. Changes include the preceding U1 through U7 units, not only U8.

| Package | Resulting ownership and source |
| --- | --- |
| `host/cmd/glyph` | `main.go` invokes CLI and the concrete headless error recipient; no application state. |
| `host/internal/app` | `sessions.go`, `headless.go`, `programmatic.go`, and `ui.go` construct/bind actual owners and control startup/shutdown. |
| `host/internal/controller/cli` | `command.go` retains command syntax and mode arguments. The outgoing `output.go` forwarding function is removed. |
| `host/internal/controller/cli/headless` | `interfaces.go` and `service.go` consume `AgentRunner`; renderer state moved to infrastructure. |
| `host/internal/controller/programmatic` | `service.go`, `cancellation.go`, and `delivery.go` own input, operation IDs, and the public response allowlist. Unsolicited output moved out. |
| `host/internal/controller/ui` | `interfaces.go`, `operations.go`, and `command_mapping.go` own actual input contracts, validation, dispatch, and operation lifetime. |
| `host/internal/domain/agent` | `event.go` and `errors.go` own provider-neutral lifecycle facts and the single history-persistence identity. |
| `host/internal/domain/session` | `session.go` retains metadata and tree values, without storage version or client query aggregates. |
| `host/internal/infra/browser` | `service.go` launches the browser through the UI output consumer's port. |
| `host/internal/infra/persistence/sessions` | `service.go` and `recovery.go` own JSONL encoding, version validation, replay, and durability. |
| `host/internal/infra/plugins/extension/catalog` | `service.go` returns filesystem observations and original I/O causes, not acceptance decisions. |
| `host/internal/infra/plugins/extension/runtime` | `runtime.go`, `handler_mapping.go`, and `lifecycle_event.go` own process/SDK transport and encode runtime-owned payloads. |
| `host/internal/infra/plugins/ui/catalog` | `service.go` returns filesystem observations to Host UI acceptance. |
| `host/internal/infra/plugins/ui/runtime` | `runtime.go`, `startup.go`, `operations.go`, and `publication.go` own the selected process, one writer, startup output, and publication. |
| `host/internal/infra/providers/openai/codex` | `service.go`, `auth.go`, and `provider.go` own OAuth, wire mapping, and complete classified provider causes. |
| `host/internal/usecase/agent/run` | `service.go` owns run state, append failure identity, and the actual settlement transition; implements Host contracts directly. |
| `host/internal/usecase/host/events` | `service.go` owns client-first dispatch and observation, not run reservation or execution. |
| `host/internal/usecase/host/extensioncontext` | `service.go` owns issued bindings and stale-context orchestration, not stored client publication. |
| `host/internal/usecase/host/extensionruntime` | `service.go`, `discovery.go`, and process payload files own runtime state, trusted identity, filtering, and acceptance. |
| `host/internal/usecase/host/lifecycle` | `service.go` retains observer registration/order and consumes concrete mode delivery through its port. |
| `host/internal/usecase/host/operationgate` | `service.go` owns one occupied reservation and idempotent release, directly implementing admission consumers. |
| `host/internal/usecase/host/programmatic` | `service.go`, `prepared.go`, and `interfaces.go` own admission and prepared work, not output correlation or Core state. |
| `host/internal/usecase/host/providers` | `catalog.go` owns configured entries, active selection, pricing, and typed source failures, with direct consumer assertions. |
| `host/internal/usecase/host/sessions` | `service.go`, `navigation.go`, `publication.go`, and `entry_history.go` own active state, durable commits, ordered publication, and history projection. |
| `host/internal/usecase/host/sessiontree` | `service.go`, `handlers.go`, and `navigation_error.go` own navigation/summary policy and consumer-specific failure classification. |
| `host/internal/usecase/host/startup` | `service.go` retains registration acceptance and reporting through actual mode output. |
| `host/internal/usecase/host/tools` | `service.go` retains tool state and validation; U8 only moves its assertions directly beneath `Service`. |
| `host/internal/usecase/host/ui` | `session.go`, `interfaces.go`, and `prepared_operations.go` own readiness, admission, authentication, selection, and prepared work. |
| `plugins/extension/tools/internal/controller/extension` | `bash.go` validates timeout intent and dispatches the consumer-owned command; it does not start the timer. |
| `plugins/extension/tools/internal/infra/filesystem/project` | `grep.go` retains bounded reads and adds the implementation's `io.RuneReader` assertion. |
| `plugins/extension/tools/internal/infra/process/bash` | `service.go` retains process/output ownership and asserts `io.Writer` for `streamWriter`. |
| `plugins/extension/tools/internal/usecase/tools/bash` | `service.go` and `timeout.go` own execution orchestration and timer lifetime. |
| `plugins/ui/tui/internal/app` | `app.go` constructs and connects input, application, SDK, terminal, and device owners. |
| `plugins/ui/tui/internal/controller/plugin` | `controller.go`, `interfaces.go`, and `request_mapping.go` decode SDK input and preserve categories/text without private application state. |
| `plugins/ui/tui/internal/controller/tui` | `controller.go` and `interfaces.go` decode keys and consume the application interaction contract. |
| `plugins/ui/tui/internal/infra/terminal` | `model.go`, `program.go`, `tree_geometry.go`, and rendering files own framework execution and display geometry, not application transitions. |
| `plugins/ui/tui/internal/usecase/presentation` | `service.go`, `commands.go`, `tree.go`, and `snapshot.go` own interaction, semantic tree visibility, and detached display data. |
| `sdk/plugins/extension/v1` | `host.go`, `server.go`, and `status_error.go` retain operation transport and complete diagnostics; the lossy external-error helper is removed. |
| `sdk/plugins/ui/v1` | `sdk.go` declares external plugin assertions; SDK connection and operation behavior remains unchanged. |

### Retained packages with identical production source

Each package below has the same production file set and bytes as the audit baseline. Its [baseline responsibility and contract disposition](audit.md#package-and-contract-coverage) therefore remains applicable. The resulting import graph was also checked; no production dependency reaches test support, fixtures, or experiments.

| Package | Production Go files |
| --- | ---: |
| `host/internal/config/codingagent` | 1 |
| `host/internal/controller/extension` | 5 |
| `host/internal/domain/extension` | 2 |
| `host/internal/domain/model` | 2 |
| `host/internal/domain/pluginid` | 1 |
| `host/internal/domain/tool` | 1 |
| `host/internal/infra/logging` | 1 |
| `host/internal/infra/persistence` | 1 |
| `host/internal/infra/persistence/credentials` | 2 |
| `host/internal/infra/persistence/sessionfilesystem` | 1 |
| `host/internal/infra/persistence/settings` | 5 |
| `host/internal/infra/programmatic/socket` | 1 |
| `host/internal/infra/providers` | 1 |
| `host/internal/infra/providers/openai/compatible` | 9 |
| `host/internal/infra/sessionruntime` | 1 |
| `internal/operation` | 3 |
| `pkg/operation/v1` | 1 generated |
| `pkg/plugins/extension/v1` | 8 generated |
| `pkg/plugins/ui/v1` | 5 generated |
| `pkg/programmatic/v1` | 5 generated |
| `plugins/extension/tools/cmd/glyph-tools` | 1 |
| `plugins/extension/tools/internal/app` | 1 |
| `plugins/extension/tools/internal/core/textbudget` | 1 |
| `plugins/extension/tools/internal/usecase/tools/edit` | 2 |
| `plugins/extension/tools/internal/usecase/tools/read` | 2 |
| `plugins/extension/tools/internal/usecase/tools/search` | 2 |
| `plugins/extension/tools/internal/usecase/tools/write` | 2 |
| `plugins/ui/tui/cmd/glyph-tui` | 1 |

### Removed baseline packages

| Removed package | Actual replacement owners |
| --- | --- |
| `host/internal/domain/ui` | UI controller input/results, Host UI outgoing contracts, and private UI output projections. |
| `host/internal/usecase/host/interactions` | UI output authorization presentation and browser I/O; Codex retains OAuth policy. |
| `host/internal/usecase/host/sessioncontrol` | Direct session, sessiontree, and operationgate implementations of client consumer ports. |
| `host/internal/usecase/host/sessionnavigation` | UI/Programmatic navigation contracts and sessiontree commit contracts at their consumers. |
| `plugins/ui/tui/internal/domain/presentation` | Private application state/tree policy, input contracts, outgoing snapshots, and terminal geometry at their actual owners. |

### Added production owners

| Package | Source and responsibility |
| --- | --- |
| `host/internal/infra/headless` | `renderer.go` owns model/tool line state, startup output, and diagnostics. |
| `host/internal/infra/programmatic/output` | `service.go` owns active run/output correlation; `publication.go` uses the attached ordered writer. |
| `host/internal/usecase/host/runcontrol` | `coordinator.go` owns reservation transfer, execution, settlement ordering, and release. |
| `plugins/ui/tui/internal/infra/host` | `service.go` owns initialized SDK binding, active context, outbound dispatch, and notification reads. |
| `plugins/ui/tui/internal/infra/terminal/device` | `terminal.go` owns controlling-terminal files and implements runtime-owned `Device`/`Files`. |

Generated mocks are regenerated from consumer interfaces, not counted as production owners. The external-plugin fixture remains a separate SDK consumer. The two experiment modules and three root test-support packages retain the audit's non-product dispositions. No future PHS-06 or PHS-12 capability is implemented.

## Assertion and lint accounting

An AST inspection found 154 handwritten production interface assertion declarations. Each declaration is directly after its implementing struct, with assertions for that struct grouped together. The resulting `go list -json ./...` graph contains 105 distinct project-local implementation/consumer package pairs from those declarations. No consumer transitively imports its implementation. `ifaceguard` also passes without a suppression. These checks support declaration/dependency closure; the finding source traces above address actual responsibilities.

Full unit-tag lint used `go tool golangci-lint run --config .golangci.yml` at both the original audit source and the U8 baseline. The original source reports 205 findings. The U8 baseline reports 175. Per-file diagnostics and source-line comparison identify nine phase-introduced findings corrected by U8: two authentication duplication findings, six UI line-length findings, and one SDK line-length finding.

The remaining 166 diagnostics have original-baseline counterparts. Nine need explicit source mapping rather than exact line matching: eight UI output literals changed the qualifier from domain UI to controller UI, and one session header literal lost its storage version field. Their missing-field diagnostics already exist in the original source. The recorded 28-findings U4 subset and 15-findings U7 subset are not whole-phase baselines.

Full supplemental unit-tag lint still exits 1 on those 166 baseline findings: 69 incomplete literals, 30 long lines, five spelling findings, 33 assertion-style findings, one redundant conversion, and 28 unit-tag-unused fixture declarations. They are not marked fixed or silently suppressed. Required `task lint` uses integration tags and exits 0. No unrelated baseline cleanup, dependency change, or cache clearing is included.

## Verification

All required checks pass on U8: `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, `task build`, and `git diff --check`. The fix dry run has no proposals. Combined coverage is 83.6%, above the 80.0% threshold.

Additional uncached checks pass:

- `go test -race -count=1 ./host/... ./internal/operation ./plugins/... ./sdk/...`.
- `go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/... ./internal/operation ./plugins/... ./sdk/...`.
- `go test -race -cover -count=1000 ./host/internal/controller/programmatic -run '^TestHostClosurePreservesWriterFailure$'`.

Two `task generate` runs pass and produce identical SHA-256 maps with no generated-source diff. There are 71 generated Go files outside experiments. Including the two unchanged experiment protocol files gives the 73-file scope used by the preceding U7 record. No generated body is edited manually.

Main-agent inspection confirmed every U8 production change is declaration placement or duplicate removal, with unchanged production behavior. The main agent reviewed the receive-entry/completion synchronization and preserved authentication assertions, then reran the complete required sequence, both uncached suites, and 1,000 race/coverage fixture repetitions. All passed at 83.6% coverage. Two further generation runs retained all 73 hashes, and all 24 handoff files were unchanged by verification.

Independent main-agent accounting reproduced 72 baseline and 72 current production packages, 273 baseline and 324 current handwritten files, and unchanged contents for all 16 public protobuf sources. The declaration check reproduced 154 adjacent assertions and 105 acyclic project-local implementation/consumer pairs. The supplemental unit-tag check reproduced the 166 original-baseline findings described above; it is not reported as passing.

## Remaining gates

The verified scope forms one separate local U8 commit. Fresh independent integrated checks, whole-product architecture review, and explicit user acceptance remain pending. No unresolved implementation question is identified. PHS-07 remains paused until those gates pass.
