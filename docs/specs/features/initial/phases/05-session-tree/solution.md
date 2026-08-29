# Technical solution: PHS-05 session tree

## Problem statement

[The PHS-05 ticket](ticket.md) defines the problem, approved behavior, scope, and acceptance criteria.

## Proposed solution

### Solution overview

- SOL-01: The Host owns one persistent session-tree aggregate, its active leaf, tree navigation, branch summarization, fork, clone, labels, and client-neutral results.
- SOL-02: Agent Core continues to consume only provider-neutral active-branch history. It receives no session-tree, extension, storage, protobuf, gRPC, or UI types.
- SOL-03: The UI Plugin Contract and Programmatic Control expose the same tree semantics. Each client decides how to present a tree and next-input text.

### Domain model

- ENT-01: `session.Tree` contains entries in persistence order, an index by entry ID, an optional active-leaf ID, and labels keyed by entry ID.
- ENT-02: `session.Entry` gains an optional `ParentID`. An absent parent identifies a root entry under the session's implicit root.
- ENT-03: `session.Entry` variants are user message, model response, tool result, opaque extension entry, and `BranchSummaryEntry`. Exactly one variant is present.
- ENT-04: `BranchSummaryEntry` contains summary text, the first and last abandoned-path entry IDs, provider ID, model ID, reasoning choice, optional normalized token usage, and optional persisted estimated cost.
- ENT-05: Session name changes, label changes, and active-leaf changes are persistence mutations rather than tree entries. They do not change a conversation branch unless the mutation is an explicit navigation.

The tree aggregate provides these operations:

- APC-01: `ActiveBranch` walks from the active leaf through parent IDs and returns entries in root-first order.
- APC-02: `NavigationPreparation` validates the target and returns the navigation destination, optional next-input text, last common ancestor, and abandoned path.
- APC-03: `Labels` applies label mutations in persistence order and returns the latest value for each target entry.

### Persistence format

- DEC-01: Session JSONL changes to format version 2. Format version 1 is rejected without migration because the project has no backward-compatibility requirement.
- EVC-01: An `entry` record stores one complete tree entry. A successful append makes that entry the active leaf.
- EVC-02: A `navigation` record stores the navigation destination and an optional embedded `BranchSummaryEntry`. With a summary, the embedded entry is a child of the destination and becomes the active leaf. Without a summary, the destination becomes the active leaf.
- EVC-03: A `label` record stores a target entry ID and the new label state. An empty label clears the label.
- EVC-04: A `session_info` record stores the normalized session name.

`host/internal/infra/persistence/sessions` performs these validations while replaying JSONL:

- FLR-01: A duplicate entry ID, unknown parent, or parent that appears after its child makes the session unavailable.
- FLR-02: A navigation destination or label target that does not exist makes the session unavailable. An absent navigation destination selects the implicit root.
- FLR-03: A `BranchSummaryEntry` with an empty summary, empty boundary ID, empty provider or model ID, unknown reasoning-choice value, invalid usage shape, or invalid cost shape makes the session unavailable. When both boundary IDs resolve inside the loaded session, they must identify one connected ancestor-to-descendant path. A missing boundary entry is accepted as provenance from a forked or cloned source session.
- FLR-04: A record with more than one entry payload or an unknown record type makes the session unavailable.

The sessions repository exposes two mutation forms:

- APC-04: `Apply` appends and synchronizes one entry, navigation, label, or session-information record.
- APC-05: `CreateSnapshot` writes a new header, retained tree entries, retained labels, and final active leaf in one repository call.

`fork` and `clone` preserve copied entry IDs because parent IDs, label targets, branch boundaries, and opaque extension data can refer to those IDs. IDs remain unique within each session. `fork` retains the path through the selected user message's parent. `clone` retains the complete active branch. Neither operation copies abandoned entries referenced only by a retained `BranchSummaryEntry`. The boundary IDs remain unchanged provenance and need not resolve in the replacement session. A new session header supplies a new session ID and creation time.

### Active-session service

`host/internal/usecase/host/sessions` owns mutable in-process state under its existing mutex.

- CMP-01: `sessions.Service.Tree` returns a defensive tree snapshot.
- CMP-02: `sessions.Service.CommitNavigation` compares the expected active leaf with the stored active leaf, builds any `BranchSummaryEntry`, calculates optional estimated cost from configured pricing, persists one navigation record, and publishes the new state only after repository synchronization.
- CMP-03: `sessions.Service.ForkActive` and `CloneActive` create and persist a replacement session before changing active-session ownership.
- CMP-04: `sessions.Service.SetLabel` validates the target, persists the mutation, and publishes the label only after repository synchronization.
- CMP-05: Normal user, model, tool-result, and extension appends use the active leaf as `ParentID` and advance the active leaf after persistence succeeds.

The active history projection changes from persistence order to `ActiveBranch` order. User, model, and tool-result entries retain their existing provider-neutral projections. A `BranchSummaryEntry` becomes a synthetic user-context message with a stable branch-summary marker. Session name, labels, and model-hidden extension entries do not enter Agent Core history.

Session statistics continue to cover all stored branches. A `BranchSummaryEntry` contributes its available usage and cost to totals and provider-model cost breakdown. It does not increment user-message, model-response, tool-call, or tool-result counts. A missing summary usage or cost makes the corresponding complete session total unavailable under the existing PHS-04 accounting rules.

### Navigation orchestration

A new `host/internal/usecase/host/sessiontree` package owns the navigation use case. `sessioncontrol.Service` remains the facade used by Host client orchestration.

- ALG-01: `sessioncontrol.Service.Navigate` acquires the existing nonblocking operation gate. Failure to acquire returns `session.ErrBusy` without invoking handlers or storage.
- STP-01: `sessiontree.Service` reads one immutable tree snapshot and computes the original target, navigation destination, next-input text, common ancestor, and abandoned path.
- STP-02: The service snapshots the active provider, model, and reasoning choice into equal original and current requests.
- STP-03: Request handlers run in registration order. Each receives the immutable original request, the current request, and the current result state.
- STP-04: When the final request requires summarization and has no current result, the built-in summarizer runs with the final configured-model selection. An empty abandoned path skips the model call and produces no `BranchSummaryEntry`.
- STP-05: When a result exists, result handlers run in registration order with the original request, final current request, immutable original result, and current result.
- STP-06: Final validation recomputes the destination and abandoned path from the final target. It also validates mode-result consistency, nonempty summary text, configured-model selection, normalized usage, and branch boundary.
- STP-07: `CommitNavigation` writes the navigation. `session_tree` observers run only after that commit.
- STP-08: The operation gate is released on every terminal path.

A request action can preserve or replace the current request, preserve, set, replace, or clear the current result, or cancel. A result action can preserve, replace, or cancel the current result. Cancellation stops the chain. An invalid action or ordinary handler error preserves the state received by that handler, records an `OperationIssue`, and continues with the next handler.

`NavigationRequest` contains target entry ID, summary mode, custom focus, and summary model selection. `TreePreparation` contains session ID, optional preceding active leaf, optional navigation destination, optional common ancestor, and projected abandoned entries. The projection contains user messages, model responses, tool results, and prior branch summaries. It exposes only extension ID and entry type for model-hidden extension entries and never exposes their payload data.

A request-handler invocation contains immutable original request and preparation, current request and preparation, and the optional current result. When a handler replaces the current target, Host recomputes the current preparation from the immutable tree snapshot before invoking the next handler. A branch-summary result contains summary text and optional normalized usage. Host owns its boundary, model selection, and estimated-cost calculation.

A result-handler invocation contains the immutable original request and preparation, final current request and preparation, immutable original result, and current result. Handler responses never supply credentials, estimated cost, active-leaf state, or persistence records.

A client cancellation, extension cancellation, model failure, final validation failure, or persistence failure before STP-07 changes no session state and emits no `session_tree`. An observer failure after STP-07 does not roll back committed state.

Navigation uses this closed terminal mapping:

| Source condition | Terminal form | Public code | State |
|---|---|---|---|
| Invalid command fields | Failure | `INVALID_ARGUMENT` | Unchanged |
| Unknown target entry | Failure | `NOT_FOUND` | Unchanged |
| Occupied operation gate | Failure | `BUSY` | Unchanged |
| Client context cancellation while a response channel remains available | Canceled result | None | Unchanged |
| Extension cancellation | Canceled result | None | Unchanged |
| Missing model or unsupported reasoning choice | Failure | `MODEL_UNAVAILABLE` | Unchanged |
| Missing model credentials | Failure | `CREDENTIAL_UNAVAILABLE` | Unchanged |
| Failed, aborted without context cancellation, empty, or tool-calling summary response | Failure | `MODEL_FAILED` | Unchanged |
| Invalid final handler-produced state | Failure | `EXTENSION_INVALID_RESULT` | Unchanged |
| Extension transport or protocol failure before commit | Failure | `EXTENSION_UNAVAILABLE` | Unchanged |
| Repository failure | Failure | `PERSISTENCE_UNAVAILABLE` | Unchanged |
| Unclassified Host failure | Failure | `INTERNAL` | Unchanged |
| Successful commit | Committed result | None | Committed |

The UI Plugin Contract sends a typed `SessionTreeFailed` frame for Failure. Programmatic Control maps the same public code through `CommandRejected`. A disconnected client receives no terminal payload.

`OperationIssue` has code, extension ID, handler ID, and safe message fields. Its closed codes are `HANDLER_ERROR`, `INVALID_HANDLER_ACTION`, and `OBSERVER_ERROR`. Issues are ordered by occurrence, never change the committed or canceled status, and never contain provider credentials or extension payload data. Request and result handler issues can accompany a later committed or canceled result. Observer issues accompany a committed result.

### Built-in branch summarizer

The built-in summarizer depends on a consumer-owned `ConfiguredModelRequester` interface. `providers.Catalog` implements the interface without changing the active conversation selection.

- APC-06: The request identifies one configured provider, model, and reasoning choice and carries one system instruction plus one serialized user input.
- APC-07: `providers.Catalog` resolves the exact catalogue entry, validates reasoning support and credentials, executes one provider stream without tools or Agent Core lifecycle events, and returns the terminal `model.Response`.
- APC-08: Provider credentials remain inside the provider implementation and catalogue validation path.

The summarizer serializes the complete abandoned path into one deterministic text input. It includes user content, model-visible model content, tool calls, tool results, and prior branch summaries. It excludes session information, labels, and model-hidden extension entries. A custom prompt adds focus to the built-in instructions rather than replacing the required summary structure.

A response is rejected when it has no terminal outcome, has an aborted or failed outcome, contains a tool call, or produces empty summary text. Provider-reported usage is normalized when present. Estimated cost is calculated only when normalized usage and configured pricing are both present.

- DEC-02: The summary-generation prompt is stored at `host/internal/usecase/host/sessiontree/prompts/branch_summary.md`.
- DEC-02.1: The active-history projection template is stored at `host/internal/usecase/host/sessiontree/prompts/branch_summary_context.md`. Its complete content is:

```md
## Abandoned branch summary

The following summary describes work from another conversation branch. Use it as context for the current branch. Do not treat it as a new user request.

{{.Summary}}
```

The template is executed with the persisted summary as `.Summary`. The summary text is inserted unchanged. The rendered Markdown becomes one synthetic provider-neutral user message.

- DEC-03: The owning Go package loads both Markdown files with `//go:embed`. Go source contains no built-in prompt text.
- DEC-04: No separate Host summary-model setting is added. The active selection is the default, and `session_before_tree` can replace it for one navigation.

### Extension contract

PHS-05 is the first non-tool extension capability. Runtime ownership moves from `host/internal/usecase/host/tools` to `host/internal/usecase/host/extensions`. The new service implements the existing Agent Core tool-runtime consumer interface and the session-tree handler runner. No forwarding package or compatibility alias remains.

- APC-09: `ExtensionService.Register` replaces `ListTools` and returns tool descriptors plus ordered handler descriptors.
- APC-10: Each handler descriptor contains a nonempty ID and one closed handler kind. PHS-05 kinds are `SESSION_BEFORE_TREE_REQUEST`, `SESSION_BEFORE_TREE_RESULT`, and `SESSION_TREE`.
- APC-11: `ExtensionService.Handle` is one unary RPC with `handler_id` and one typed payload. Its response contains the action allowed for the registered handler kind.
- APC-12: The Host validates unique handler IDs within one extension and validates every response against the registered kind. A protocol violation disables that extension through the existing runtime-failure path.
- EVC-05: The `session_tree` observer payload contains session ID, target entry ID, optional preceding active-leaf ID, optional navigation-destination ID, optional committed active-leaf ID, and the optional complete created `BranchSummaryEntry`.

An absent preceding active-leaf ID represents an empty source position. An absent navigation-destination ID represents the implicit root. An absent committed active-leaf ID is valid only for no-summary navigation to the implicit root. The created summary is present exactly when summarization committed a `BranchSummaryEntry`. The observer payload excludes next-input text, complete tree snapshots, client fields, and `OperationIssue` values.

Global handler order is extension activation order followed by handler registration order within each extension. Two real gRPC test extensions exercise replacement, result clearing, ready-result supply, ordinary failure, cancellation, and post-commit observation. No production extension is added only to demonstrate the contract.

### Client contracts

Both public client contracts add these semantic commands:

- APC-13: `GetSessionTree` returns every tree entry, parent relation, label state, and optional active leaf.
- APC-14: `NavigateSessionTree` accepts target entry ID, summary mode, and custom focus. It returns committed or canceled status, the complete committed tree snapshot, committed active leaf, optional next-input text, active-branch transcript snapshot, and operation issues.
- APC-15: `ForkSession` accepts one user-message entry ID and returns the replacement session plus next-input text.
- APC-16: `CloneSession` returns the replacement session created from the active branch.
- APC-17: `SetEntryLabel` accepts target entry ID and label state.
- APC-18: `SessionTreeFailed` and Programmatic Control `CommandRejected` use the navigation failure codes defined by the terminal mapping. Neither failure payload returns speculative active-leaf or next-input values.

The summary-mode enum has `NO_SUMMARY`, `SUMMARIZE`, and `SUMMARIZE_WITH_CUSTOM_PROMPT`. The custom-focus field must be nonempty only for `SUMMARIZE_WITH_CUSTOM_PROMPT`.

`api/plugins/ui/v1/ui.proto` adds Host frames and UI commands for these operations. `api/programmatic/v1/programmatic.proto` adds correlated commands and command results with the same semantic fields. Neither contract contains editor, terminal, widget, keybinding, or rendering fields.

Existing session-replacement frames carry only active-branch transcript entries. Full-tree data is returned by `GetSessionTree` and the committed navigation result. Extension payload bytes remain private in PHS-05. Clients receive only extension ID and entry type for opaque extension entries.

### Standard TUI

The standard TUI adds `/tree`, `/fork`, and `/clone` actions.

- CMP-06: `/tree` requests a tree snapshot and opens a tree selector.
- CMP-07: `/fork` opens the selector in user-only mode. `/clone` immediately requests an active-branch clone.
- CMP-08: The selector implements search, branch folding, label editing, active-path indication, and the `default`, `no-tools`, `user-only`, `labeled-only`, and `all` filters from `docs/specs/features/initial/tui-defaults.md`.
- CMP-09: Target confirmation opens a summary selector whose first and default choice is `No summary`. The custom-prompt choice opens a separate input state.
- CMP-10: A committed navigation replaces the transcript with the returned active branch and places next-input text in the editor. It never emits a submit command for that text.

Filtering, search, folding, and selection remain client-local and do not mutate Host state. Label editing sends `SetEntryLabel` because labels are persistent session state.

### TDD and verification

Each behavioral change follows RED, GREEN, and REFACTOR. A RED test must compile and fail through the expected assertion with `go test -count=1`.

- TSK-01: Add format-version-2 codec and replay tests. Inputs cover branched entries, navigation records, labels, and both accounting presence states. Expected output is the exact restarted tree. Edge cases are unknown parents, duplicate IDs, invalid active leaves, connected local boundaries, and unresolved replacement-session boundary provenance. Tests depend only on the sessions repository contract.
- TSK-02: Add active-branch projection tests. Inputs contain two branches and a `BranchSummaryEntry`. Expected history contains only the active path and synthetic summary context. Edge cases are an absent active leaf and model-hidden extension entries. Tests depend on the session domain model.
- TSK-03: Add navigation orchestration tests. Inputs cover user targets, non-user targets, and all three summary modes. Expected results cover destination, next-input text, final active leaf, and event ordering. Edge cases are an empty abandoned path, `busy`, cancellation, model failure, validation failure, and storage failure. Tests use generated mocks for handlers, model requests, and storage.
- TSK-04: Add handler-composition tests. Inputs contain two request handlers and two result handlers. Expected outputs preserve original request and preparation, pass each current value to the next handler, and recompute preparation after target replacement. Edge cases are result clearing, invalid actions, ordinary errors, alternate model selection, model-hidden extension payload exclusion, and cancellation. Tests depend on the extension handler runner contract.
- TSK-05: Add configured-model request tests. An alternate configured selection must reach the selected provider without changing active conversation selection or exposing credentials. Edge cases are a missing model, unsupported reasoning, unavailable credentials, missing usage, and missing pricing. Tests depend on provider catalogue mocks.
- TSK-06: Add fork, clone, and label tests. Expected snapshots retain only the required path, preserve copied IDs and unresolved summary-boundary provenance, restart successfully, and leave the source session unchanged. Edge cases are the root user message, an empty session, an off-path summary boundary, and repository failure. Tests depend on the sessions repository contract.
- TSK-07: Add real gRPC extension contract tests. Two test processes register ordered handlers and return composed actions. Expected outputs cover ready-result replacement and post-commit `session_tree`. Edge cases are transport failure, kind mismatch, and duplicate handler IDs.
- TSK-08: Add UI and Programmatic Control contract tests. Equivalent commands must return the same active leaf, next-input text, active transcript, cancellation status, and classified failures.
- TSK-09: Add TUI tests for search, filters, folding, labels, summary-choice default, custom-focus input, and editor placement without submission.
- TSK-10: Add summarizer behavior tests that capture the configured-model request and active-history projection. The tests prove that both embedded Markdown files are used, the summary is inserted unchanged, and resumed history renders the same context message. The generation test checks behavior rather than mutable prompt wording. The projection test checks the exact approved context layout.

Final verification runs `go fix -diff ./...`, reviews the proposed fixes, runs `go fix ./...`, then runs `task lint` and `task test`.

### Affected areas

- CMP-11: `host/internal/domain/session` gains the tree and branch-summary model.
- CMP-12: `host/internal/usecase/host/sessions`, `sessioncontrol`, and new `sessiontree` implement tree state and orchestration.
- CMP-13: `host/internal/infra/persistence/sessions` implements JSONL format version 2.
- CMP-14: `host/internal/usecase/host/providers` adds configured-model completion without active-selection mutation.
- CMP-15: `host/internal/usecase/host/extensions`, extension runtime infrastructure, extension SDK, and extension protobuf implement handler registration and dispatch.
- CMP-16: UI and Programmatic Control protobuf, controllers, mappings, generated packages, and tests add tree operations.
- CMP-17: The standard TUI presentation model, controller, selector, rendering, and tests add tree interaction.

## Overengineering and overspecification considerations

- DEC-05: Navigation uses one append-only mutation record instead of a database, snapshot rewrite, or transaction framework.
- DEC-06: The tree builds an in-memory ID index and uses linear path walks. A local session does not need a separate graph store or incremental client patch protocol.
- DEC-07: PHS-05 preserves opaque extension entries but does not add model-visible extension-message creation or projection. PHS-07 owns that behavior.
- DEC-08: PHS-05 serializes the complete abandoned path and does not duplicate PHS-06 compaction. A provider context-limit failure follows the ordinary no-commit failure path.
- DEC-09: Unary extension-handler calls fit ordered request and result processing. A multiplexed lifecycle stream is not required for PHS-05.

## Open questions

None.

## References

- REF-01: [PHS-05 ticket](ticket.md) - approved requirements and acceptance criteria.
- REF-02: [Target architecture](../../architecture.md) - Host, Agent Core, extension, session, and client ownership.
- REF-03: [Target product requirements](../../prd.md) - client-neutral session and extension behavior.
- REF-04: [Delivery plan](../../delivery-plan.md) - phase order and dependencies.
- REF-05: [Domain glossary](../../../../../terms.md) - project terminology.
- REF-06: [Standard TUI defaults](../../tui-defaults.md) - tree commands, filters, and keybinding baseline.
