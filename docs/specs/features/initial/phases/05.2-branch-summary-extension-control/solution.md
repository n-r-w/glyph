# Technical solution: PHS-05.2 Branch-summary extension control

## Problem statement

The [ticket](ticket.md) defines the required behavior and acceptance criteria. This document describes the implemented solution.

## Proposed solution

### Ownership and scope

- Host `sessiontree` owns handler composition, built-in summary dispatch, and final navigation validation. Host `sessions` owns atomic persistence and estimated cost.
- The session domain owns the stored result source. Extension, UI, and Programmatic Control adapters map that source through their public contracts.
- Agent Core receives no new type, interface, or policy. PHS-07 remains outside this implementation.

### Result-source representation

Add `session.BranchSummarySource` with two fields:

| Field | Go type | Meaning |
| --- | --- | --- |
| `ExtensionID` | `mo.Option[string]` | Extension that produced a summary without model execution. |
| `Model` | `mo.Option[session.BranchSummaryModelSource]` | Actual model selection and its optional reported usage. |

Add `session.BranchSummaryModelSource` with `Selection model.Selection` and `Usage mo.Option[session.TokenUsage]`. Usage belongs only to this model alternative; the extension alternative has no usage field.

Exactly one field must be present. An extension ID must contain non-whitespace text. A model source must contain nonempty provider and model IDs and a reasoning choice accepted by `model.ReasoningChoice.Valid`. Source validation checks the metadata shape, not catalogue membership, credentials, or the configured model's reasoning capabilities.

- Add required `Source` to `HandlerBranchSummaryResult`. Replace `BranchSummaryDraft.Selection` and the model-only attribution fields of `session.BranchSummaryEntry` with `Source`. Remove standalone `Usage` from all three types.
- A model source permits absent usage or usage accepted by `session.TokenUsage.Valid`. Selecting an extension source cannot carry model usage.
- Keep persisted `EstimatedCost` beside `Source`. Extension-source summaries require absent cost. Model-source estimated cost remains Host-calculated; extensions cannot supply a cost value in `BranchSummaryResult`.
- A handler that replaces a result supplies the complete text and source, including optional usage inside the model alternative. Host does not merge omitted fields with the preceding result or infer source from `SummaryModel`.
- Preserve actions retain text, source, and usage together. A replacement may retain model attribution for a transformation of that model output. A replacement produced independently must report its own source and accounting.
- Extension IDs are explicit public metadata. Host does not require the source ID to equal the last handler's ID or refer to a currently loaded extension. Trusted, cooperating extensions can pass results between handlers, and stored summaries remain readable after extension removal.
- Host validates source consistency but does not attempt to prove how a trusted extension produced text. No source-history ledger or provenance attestation is added.

### Public contracts

In `api/plugins/extension/v1/session_tree.proto`, add `BranchSummarySource` with `oneof source` containing `string extension_id` and `BranchSummaryModelSource model`. Add `BranchSummaryModelSource` with required-in-semantics `ModelSelection selection` and optional `TokenUsage usage`.

- `BranchSummaryResult` gains required-in-semantics `source` and removes standalone `usage`. Its `summary` remains the result text.
- `CommittedBranchSummary` replaces `summary_model` with `source` and removes standalone `usage`. Its boundary and optional cost remain visible to observers. Reported usage is available inside the model alternative.
- `SessionTreeNavigationRequest.summary_model` remains the input selection for built-in dispatch only. Missing or unusable selection does not invalidate a ready extension result.
- Request and result invocations expose original and current sources with their corresponding results. Missing source reaches final result validation as invalid metadata, rather than acquiring the selected model implicitly.

In each client `session.proto`, add a contract-local `BranchSummarySource` with the same two alternatives. Its `BranchSummaryModelSource` carries provider ID, model ID, the client contract's reasoning enum, and optional `TokenUsage usage`. Replace the flat provider, model, reasoning, and usage fields of `BranchSummary` with `source`. Keep optional `estimated_cost` beside `source`.

Update Host UI and Programmatic Control projection types, their mappings, and the standard TUI's wire decoding wherever these types are consumed. An extension source must not pass through model-reasoning conversion. Both client contracts expose the same semantic source, usage presence, and cost presence in navigation results and resumed session trees.

Regenerate protobuf Go packages and update SDK consumers and external-plugin fixtures. Retain edition 2023 and `int64` token fields. Remove obsolete fields without reserved tags, deprecated aliases, or compatibility readers, as required by the project.

### Navigation and built-in dispatch

1. `NavigateTree` copies the active selection into the original request and runs request handlers. Reading the selection does not check model availability or credentials.
2. `generateMissingSummary` dispatches only when summarization is requested, the abandoned path is nonempty, and the current result is absent.
3. `Catalog.Request` remains the dispatch boundary for catalogue lookup, configured reasoning support, and credential checks. Remove the availability call from `validateFinalState`, `CheckAvailability` from the `sessiontree.ModelRequester` interface, and the resulting unused methods on `modelRequesterBinding` and `Catalog`. Preserve credential and selection rejection coverage through `Catalog.Request`; do not add another preflight check.
4. Built-in summarization attaches the exact selection passed to `Request` as the model source. Provider usage belongs to that result.
5. Result handlers preserve or replace the complete result. Request-handler clearing re-enables built-in dispatch; result-handler clearing remains an invalid action under the established action contract.
6. Final validation checks navigation mode and target, nonempty summary text, source and usage consistency, and the recomputed abandoned-branch boundary. A ready result never consults the unused selection.
7. The existing pre-commit cancellation check and `sessions.CommitNavigation` retain the expected-active-leaf check and one atomic tree mutation. Source metadata enters the same mutation as summary text and the active leaf.
8. Observers receive the committed entry, including its persisted source and accounting.

The session commit validator also validates source and usage before writing. A malformed final result, cancellation before commit, invalid boundary, or failed persistence changes neither the active leaf nor the stored entries. No fallback summary is generated to repair an invalid ready result.

### Persistence and accounting

Replace the flat model attribution in `branchSummaryRecord` with a required `source` object. The object has `extension_id` and `model` fields represented with `mo.Option`, with exactly one non-null value. The model object contains provider, model, reasoning choice, and optional usage. Move the usage record into that object, retaining its optional encoding. Keep optional estimated cost beside `source`. Remove standalone usage from `branchSummaryRecord`.

- Encoding and decoding reject absent source, both source alternatives, invalid source fields, extension-source cost, invalid token usage, and invalid estimated cost. Model cost requires reported usage.
- Decode stored model source as historical metadata without catalogue or credential access. Do not convert records with obsolete flat attribution into the new shape.
- `buildBranchSummaryEntry` calls `estimatedUsageCost` only for a model source, using the selection and usage contained in that model source.
- Missing usage or pricing returns absent estimated cost. A different configured model's prices never substitute for source pricing. Keep the existing pricing tiers and cost formula.
- Restart, snapshot creation, fork, and clone retain stored source and accounting without recomputing cost under new prices.
- `statisticsFromEntries` includes model-source summaries in token totals and provider-model cost groups. Missing accounting on a model-source summary retains the existing incomplete-total rules.
- Extension-source summaries contribute no model usage or model cost group. They do not make otherwise complete model totals unavailable. The individual summary still exposes absent usage and absent cost, not fabricated zero-valued accounting.
- Message and tool counts retain their existing entry-kind rules. Summary text projection into model context does not change.

### Handler errors

The ordinary error already reaches Host through `HandlerError.message` and `mapHandleResponse`. Change only the loss of that text and the affected contract comments.

| Path | Issue category | Message and state outcome |
| --- | --- | --- |
| Request handler returns an ordinary error | `OperationIssueHandlerError` | Store the received `err.Error()` without truncation or replacement. Keep the handler's incoming request and result, then invoke later handlers. |
| Result handler returns an ordinary error | `OperationIssueHandlerError` | Store the received `err.Error()` without truncation or replacement. Keep the handler's incoming result, then invoke later handlers. |
| Observer returns an error after commit | `OperationIssueObserverError` | Store the received `err.Error()` without truncation or replacement. Keep the committed navigation and invoke later observers. |

- Each issue retains the registered extension ID and handler ID as separate fields. Existing context already present in the received error remains in the message.
- Do not add a new generic prefix merely to replace the removed fixed messages. Protocol and runtime layers retain any context they already add.
- Keep `OperationIssueInvalidHandlerAction` for Host-detected action-shape errors. That path has no received ordinary error cause to preserve.
- Caller cancellation before commit remains terminal, rather than becoming an ordinary handler issue. Post-commit observer invocation retains `context.WithoutCancel` behavior.
- Ordinary handler errors leave the extension active. Runtime unavailability remains owned by the runtime manager and is not reclassified as an ordinary extension failure.
- UI and Programmatic Control mappings copy issue text and identity fields without rewriting them. Only actual secrets may be redacted under the [shared error semantics](../../prd.md#error-semantics); no speculative text sanitizer is added.

### Verification evidence

Verification completed on 2026-09-05. Focused tests ran with `-count=1`. RED runs exposed replaced handler causes, the unused availability check, extension-summary commit rejection, incomplete aggregate accounting, missing persisted source, and cost without reported model usage. The corresponding focused suites passed after implementation.

| Acceptance criterion | Executable evidence | Observed result |
| --- | --- | --- |
| ACC-01 | `TestRealExtensionChecksCredentialsOnlyAfterClearing` in `host/internal/app/summary_control_credentials_integration_test.go`; `TestReadySummaryUsesActualSource` in `host/internal/usecase/host/sessiontree/source_test.go` | A real extension supplies a result with unavailable credentials or missing selection. Credential checks and provider stream calls are zero. The unit case also covers unused unsupported reasoning. |
| ACC-02 | `TestProgrammaticBranchSummaryControl` and `TestUIBranchSummaryControl` in `host/internal/app/summary_control_public_integration_test.go` and `summary_control_ui_integration_test.go` | Both clients read the explicit extension source without model usage or cost. The entire committed tree equals the tree read after Host restart. |
| ACC-03 | The model-source cases in both client tests above | A replacement reports `local/priced` with 1,000,000 input tokens. Stored and client-visible cost is USD 3, using that model's price rather than the active model's USD 1 rate. |
| ACC-04 | `TestRealExtensionChecksCredentialsOnlyAfterClearing`; `TestNavigateClearedReadyResultRunsBuiltInAndResultHandlers`; `TestNavigateSummarizesOnlyAbandonedPath`; `TestCommitNavigationBuildsAndPersistsOneValidatedSummaryMutation` | Clearing performs one credential check and commits nothing on credential failure. Successful built-in dispatch retains its model source and reported usage; summary commit calculates and persists source-based cost. |
| ACC-05 | Both client tests above and `host/internal/app/summary_control_fixture_integration_test.go` | Real request, result, and observer errors return distinct complete causes, extension ID, handler ID, and issue category through each client contract. |
| ACC-06 | Both client tests above; `TestSummaryControlPreCommitFailureKeepsStoredTree` in `host/internal/app/summary_control_atomicity_integration_test.go`; session-tree composition and failure suites | Later handlers receive preceding state, repeated navigation succeeds through the same extension, and observer errors retain the committed leaf. Cancellation and missing final source preserve the whole tree before and after restart. |

Additional focused coverage includes `TestBranchSummarySourceValidation`, `TestSummaryCostRequiresModelUsage`, `TestBranchSummarySourceRoundTrip`, `TestCommitExtensionSummaryPreservesSource`, and `TestExtensionSummaryKeepsModelTotalsComplete`. These tests cover exclusive source alternatives, malformed identity and usage, optional usage round-trip, model-only cost, and complete model totals in sessions containing extension summaries.

The repository verification sequence passed:

- Two `task generate` runs produced identical diffs.
- `task fmt` completed; `task fix_dry_run` proposed no changes.
- `task lint` reported no issues, no interface-direction errors, and no vulnerabilities.
- `task test` and `task itest` passed, including the external-plugin module.
- `task test-coverage` reported 83.6% against the 80.0% minimum.

## Overengineering and overspecification considerations

The solution adds one two-alternative source value and maps it through existing records and contracts. It retains the summary service, store, pricing calculation, action protocol, and transaction boundary. It adds no generic result framework, independent accounting service, provenance ledger, compatibility path, or Agent Core dependency.

An extension ID plus optional model fields without an exclusive source rule was rejected because it permits ambiguous attribution. Inferring source from the last handler was rejected because a handler can preserve or forward another producer's result. Authenticating the reported source was rejected because source metadata records completed work rather than requesting a new model execution.

## Open questions

None.

## References

- [Phase ticket](ticket.md) defines requirements and acceptance criteria.
- [PHS-05.1 solution](../05.1-extension-boundary-cleanup/solution.md) defines capability and runtime ownership.
- [Delivery plan](../../delivery-plan.md) defines phase dependencies and the PHS-07 completion gate.
- [Session requirements](../../prd.md#context-and-sessions) define persistence and accounting semantics.
