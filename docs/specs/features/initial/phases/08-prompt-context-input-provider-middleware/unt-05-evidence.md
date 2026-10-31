# UNT-05 implementation evidence

## Status

UNT-05 is documentation only and is implemented on top of commit `c2cf46a`. A text review and a check of the documentation against the code ran on it; their accepted findings are corrected. The phase result is in the [acceptance report](acceptance-report.md).

## Choices

**Canonical solution name.** Other phases name their technical solution `solution.md`, and the project rules name it the same way. The smallest change was to rename `technical-solution.md` to `solution.md` in this directory. The links in `plan.md`, `decisions.md`, `design-review.md` and `unt-01-evidence.md` follow the new name. A search for `technical-solution.md` finds no remaining reference to this phase. The PHS-06 acceptance-corrections directory has its own file of that name and is unchanged.

**Author guide.** The four detailed guides each described one group of extension points and repeated the same contract. [`model-middleware.md`](../../../../../guidelines/model-middleware.md) is now the entry point. It holds the facts that all extension points share: the seven extension points, handler kinds and payload fields, actions, original and current values, registration order, invocation order in a model request, replay references, failure categories and the build instructions of the reference commands. Each detailed guide keeps only what is specific to its extension points and links to the overview for the rest. The reference READMEs keep their scenario tables and link to the overview for installation.

**Output directory.** The READMEs built into fixed directories of a system temporary location. They now build into a new directory that the reader names in the shell variable `EXTENSION_DIR`. The overview sets it to `bin/reference-extensions` below the repository root, which `.gitignore` excludes.

**Architecture tree.** The entries for `middleware`, `modelexecution`, `runcontrol`, the Agent Core ports and the provider adapters matched the code; the [architecture check](#architecture-check) records how. The tree had no entry for the reference extensions, so it now lists `testdata/external-plugins` and the four commands.

## Reference command builds

Each command ran from `testdata/external-plugins`, with `EXTENSION_DIR` set to an empty directory.

| Command | Result |
| --- | --- |
| `go build -o "$EXTENSION_DIR/wire" ./cmd/wire` | Exit 0 |
| `go build -o "$EXTENSION_DIR/context" ./cmd/context` | Exit 0 |
| `go build -o "$EXTENSION_DIR/input" ./cmd/input` | Exit 0 |
| `go build -o "$EXTENSION_DIR/finalized" ./cmd/finalized` | Exit 0 |
| The build block of the overview from the repository root: `EXTENSION_DIR="$PWD/bin/reference-extensions"`, `mkdir -p "$EXTENSION_DIR"`, `(cd testdata/external-plugins && go build -o "$EXTENSION_DIR/context" ./cmd/context)` | Exit 0; the directory was removed afterwards |
| `cp "$EXTENSION_DIR/wire" "$EXTENSION_DIR/wire-preserve"` | Exit 0; the copy under a scenario name is the documented way to select a scenario |
| `go test -race ./...` in `testdata/external-plugins` | Exit 0; no package of the module has a test file without the `integration` tag |
| `go test -race -tags=integration -p 1 -parallel 1 ./...` in `testdata/external-plugins` | Exit 0; the root package passed in 5.464 s and `compaction-policy` passed from the test cache |

The `glyph run --extension-dir "$EXTENSION_DIR" ...` command of the overview needs a configured provider and model. No run against a live provider took place. The tests below run the production executable and the assembled Host against a local HTTP provider.

## Tests run for the documented behavior

The commands ran from the repository root. No `task itest` and no `task test-coverage` ran.

| Command | Result |
| --- | --- |
| `go test -race -count=1 -v -tags=integration -p 1 -parallel 1 -run '^(TestPublicWireMiddleware\|TestPublicContextMiddleware\|TestPublicInputMiddlewareProgrammatic\|TestPublicFinalizedMessages)$' ./host/internal/app` | Exit 0; all four passed, including the six Programmatic input cases |
| `go test -race -count=1 -v -tags=integration -p 1 -parallel 1 -run '^(TestPublicInputMiddlewareOneShot\|TestPublicFinalizedOneShot\|TestPublicContextMiddlewareAcrossModes\|TestPublicUserInputAcrossClients)$' ./host/internal/app` | Exit 0; all four passed, including the headless and UI cases |
| `go build -o /dev/null ./host/cmd/glyph` | Exit 0 |

## Package boundary check

The command `go list -f '{{join .Imports "\n"}}' ./host/internal/usecase/<package>` listed the direct imports of `agent/run`, `host/middleware` and `host/modelexecution`. A filter for `protobuf`, `grpc`, `github.com/n-r-w/glyph/pkg/`, `github.com/n-r-w/glyph/sdk/`, `infra/`, `hooks`, `openai`, `plugins/` and `tui` matched no import in any of the three packages. The directory `host/internal/hooks` does not exist.

`go list -deps` of `agent/run` lists protobuf and gRPC packages. The path leads through its import of `host/internal/usecase/host/programmatic`, which imports `host/internal/controller/programmatic`. Both imports exist at commit `b332e05`, before PHS-08. This is a limit of the direct-import check; see [acceptance report](acceptance-report.md#verification-limits).

The reference commands import only `github.com/n-r-w/glyph/pkg/plugins/extension/v1` and `github.com/n-r-w/glyph/sdk/plugins/extension/v1` from this repository (`go list -f '{{join .Imports "\n"}}' ./cmd/wire ./cmd/context ./cmd/input ./cmd/finalized`).

## Architecture check

The text of CMP-03, CMP-04 and APC-01 to APC-04 was compared with `InputProcessor` in `host/internal/usecase/host/runcontrol/interfaces.go`, with `EffectiveContextPort` and `MessageFinalizer` in `host/internal/usecase/agent/run/interfaces.go`, and with the compile-time assertions in `modelexecution/service.go`, `modelexecution/prepared_provider_binding.go` and `middleware/service.go`. The tree entries for `middleware/`, `modelexecution/` and the provider adapters were compared with directory listings of `host/internal/usecase/host` and `host/internal/infra/providers/openai`. The tree has no `runcontrol` entry; CMP-03 describes that package. No entry needed a correction.

## Documentation checks

**Public names.** Each name in the guides and READMEs was compared with its source.

| Names | Source |
| --- | --- |
| `HANDLER_KIND_PROVIDER_HEADERS`, `_PROVIDER_BODY`, `_PROVIDER_RESPONSE`, `_SYSTEM_INSTRUCTIONS`, `_MODEL_CONTEXT`, `_USER_INPUT`, `_FINALIZED_MESSAGE` | `HandlerKind` in `api/plugins/extension/v1/extension.proto` |
| Payload and outcome fields `provider_headers`, `provider_body`, `provider_response`, `system_instructions`, `model_context`, `user_input`, `finalized_message` | `HandleRequest` and `HandleResponse` in `api/plugins/extension/v1/session_tree.proto` |
| Actions `preserve`, `replace`, `handle`, `acknowledge`, and the invocation fields `original`, `current`, `status`, `headers` | `api/plugins/extension/v1/middleware.proto` |
| `ModelContextHistory`, `ModelContextEntry`, `ReplayableModelResponse`, `replay_reference`, `SessionTreeUserMessage`, `SessionTreeToolResult`, `SessionTreeModelContent` | `middleware.proto`, `session_entry.proto` |
| `HandlerDescriptor`, `PrepareHandle`, `HandleOperation.Run` | `extension.proto`, `sdk/plugins/extension/v1/service.go` |
| `EXTENSION_FAILED`, `INPUT_INVALID` | `host/internal/usecase/host/middleware/failure.go`, the failure codes of the Programmatic and UI controllers |
| The replay key `$glyph_replay` and the slot `reasoning_details` | `host/internal/infra/providers/openai/wire_body.go` |
| Extension order | `Discover` in `host/internal/infra/plugins/extension/catalog/service.go` keeps regular executable files; `acceptDiscovery` in `host/internal/usecase/host/extensionruntime/discovery.go` sorts candidates by identifier and rejects duplicated identifiers; `pluginid.Normalize` derives the identifier |
| Failure, cancellation, invocation order and one-shot output rules | D8-13 to D8-18 in [decisions.md](decisions.md); `categorizeChainFailure` in `middleware/failure.go`; `ProcessInput` in `middleware/input.go`; `TestProviderFailureCanceledDuringFinalization`, `TestPublicInputMiddlewareOneShot` and `TestPublicFinalizedOneShot` |
| Client events | `AgentEventType` in `api/programmatic/v1/agent.proto` and `LifecycleType` in `api/plugins/ui/v1/agent.proto` |
| Scenario names, handler identifiers, trace variables and behavior of the four reference commands | `main.go`, `scenarios.go`, `transformation.go` and `trace.go` of each command |

The first check found one wrong name. The finalized-message guide named client events in lower case (`message_end`, `turn_end`, `tool_execution_end`, `tool_result` and others). The protocols define `AGENT_EVENT_TYPE_*` and `LIFECYCLE_TYPE_*` values, so the guide now uses their suffixes and says so.

**Review round 1.** The first review found two wrong statements in the finalized-message guide. It stated cancellation and finalization-failure outcomes without the rule that pure owning cancellation is uncategorized and a handler failure is `EXTENSION_FAILED`. It also used event names before explaining them. The guide now qualifies both passages and links to the overview. The review also asked for the removal of repeated statements across the guides, which the overview and the guides now reflect.

**Links.** A script resolved every relative Markdown link and every anchor in the 23 changed or new Markdown files (this record included) against the file system and the headings of the target files: 344 links in the last run after the review corrections, no missing file and no missing anchor.

**Temporary paths.** A search of the added lines and the new files for system temporary directories, scratchpad paths and review message identifiers found none.

**Duplication.** The contract that all four guides repeated (SDK admission, original and current values, preserve and replace, the failure rule, cancellation, replay-reference rules, the list of modes and the reference-command paragraph) is now stated once in the overview. The guides link to it.
