# Presence-aware option extraction evidence

## Scope and status

UNT05 removes all 15 production `Option.MustGet` calls at starting revision `ee9774ef838d8995ae4d994db5b6b6412096bd30`. The production scan covers non-test Go files under `host`, `internal`, `plugins`, and `sdk`. No panic extractor or recovery wrapper replaces them. The issue remains in progress while UNT06 replacement of panic-based regular-expression initialization is pending. Extension failure semantics and transport changes remain outside this unit.

## Implementation and source evidence

| File and symbol | Behavior retained or diagnosed |
| --- | --- |
| `host/internal/controller/programmatic/delivery.go`, `streamDelivery.Progress` | Four checked `Get` results select agent, navigation, retry, then compaction. The first present payload still wins. Missing payload and mapping failures retain their delivery-failure paths. Present zero retry values and an empty compaction stage remain present. |
| `host/internal/infra/persistence/sessions/codec.go`, `encodeEntry` | The existing ID and exactly-one-variant checks remain. The final extension variant uses checked extraction and retains the invalid-entry error if absent. Encoding functions and their cause propagation are unchanged. |
| `host/internal/usecase/host/programmatic/prepared.go`, `commandPrepared.runSelection` | Present rejection retains code mapping and cause. Missing producer rejection returns an explicitly diagnosed INTERNAL outcome wrapping the original selection source. |
| `host/internal/usecase/host/sessions/history.go`, `terminalContinuationEntry` | Missing selected user, model, or tool-result payload returns its specific missing-payload error. Present payloads retain cloning. Nonterminal or invalid-outcome model responses remain process-local. |
| `host/internal/usecase/host/sessions/entry_history.go`, `compactedHistoryFromEntries` | The backward scan retains the selected marker value and index together. No-marker history, latest-marker selection, source metadata, retained suffix, and missing-boundary nil results remain unchanged. |
| `host/internal/usecase/host/ui/prepared_operations.go`, `Session.runSessionOperation` | Resume, name, navigation, fork, and label commands check required presence locally. Missing fields return the admission validator's command-specific diagnosis. A present empty label reaches `SetLabel` as deletion. |

`host/internal/usecase/host/sessions/service.go`, `Service.Append`, still calls `HistoryEntry.ValidatedClone` once before projection. The local extraction changes add no second whole-history validation or clone. `host/internal/usecase/host/ui/prepared_operations.go`, `Session.prepareSessionOperation`, still performs readiness, admission validation, and gate acquisition before execution; execution does not repeat full command validation.

`host/internal/usecase/host/programmatic/service.go`, `Service.selectionRejected` and `Service.rejection`, always construct `Some(Rejection)` on the traced producer paths. The new missing-rejection branch therefore has source-supported cause retention, not a fabricated behavioral RED. Similarly, codec union validation establishes the final extension variant before extraction, and compaction scanning establishes marker presence. Progress branches previously guarded each extraction with presence checks.

## Behavioral tests and RED/GREEN bounds

| Test file and symbol | Observations |
| --- | --- |
| `host/internal/usecase/host/ui/option_extraction_test.go`, `TestSessionOperationMissingValues` | Six direct execution cases cover absent session ID, name, navigation target, fork target, label target, and label value. Strict generated mocks allow no invalid downstream call. After each returned error, an admitted present-empty-label command succeeds. |
| `host/internal/usecase/host/sessions/option_extraction_test.go`, `TestTerminalContinuationMissingPayload` | Three direct projection cases diagnose selected absence, then project present zero user/tool payloads or a present nonterminal zero model response successfully in the same process. |
| `host/internal/usecase/host/sessions/option_extraction_test.go`, `TestCompactedHistoryAbsentMarkerAndBoundary` | Ordinary empty user content survives no-marker projection; a missing retained boundary still returns nil. |
| `host/internal/controller/programmatic/option_progress_integration_test.go`, `TestProgressPresencePrecedence` | A real writer exercises missing-payload reporting followed by valid empty compaction, zero retry, navigation precedence, and first-priority agent mapping failure without fallthrough. |
| `host/internal/usecase/host/sessions/empty_label_integration_test.go`, `TestEmptyLabelDeletesPersistedLabel` | A real session service and tree with a generated repository mock persist an empty label mutation and remove the label from both active state and the returned snapshot. |

The UI and history missing-value tests call existing execution/projection functions directly. Public preparation and `Append` prevent those absences on their traced paths. Their RED evidence establishes locally callable panic removal, not a claim that a normal admitted Host request previously reached those states. No production seam was added.

Each of these nine baseline commands exited 1 with a runtime `panic: no such element`:

```sh
go test ./host/internal/usecase/host/sessions -run TestTerminalContinuationMissingPayload/user -count=1
go test ./host/internal/usecase/host/sessions -run TestTerminalContinuationMissingPayload/model -count=1
go test ./host/internal/usecase/host/sessions -run TestTerminalContinuationMissingPayload/tool -count=1
go test ./host/internal/usecase/host/ui -run TestSessionOperationMissingValues/resume -count=1
go test ./host/internal/usecase/host/ui -run TestSessionOperationMissingValues/name -count=1
go test ./host/internal/usecase/host/ui -run TestSessionOperationMissingValues/navigation -count=1
go test ./host/internal/usecase/host/ui -run TestSessionOperationMissingValues/fork -count=1
go test ./host/internal/usecase/host/ui -run TestSessionOperationMissingValues/label_target -count=1
go test ./host/internal/usecase/host/ui -run TestSessionOperationMissingValues/label_value -count=1
```

The focused corrected-code GREEN command exited 0:

```sh
go test -race ./host/internal/usecase/host/sessions ./host/internal/usecase/host/ui -run 'TestTerminalContinuationMissingPayload|TestCompactedHistory|TestSessionOperationMissingValues' -count=1
```

The first GREEN attempt failed compilation because the new selection error wrapper lacked its `fmt` import. The successful run followed the import correction. An intermediate lint run rejected shadowed presence names and nested branching in progress mapping. Checked extractions now feed the original flat switch; the subsequent lint run passed. The first final lint attempt then reported one overlong comment in the added empty-label test. The comment was shortened before repeating final checks.

The following preservation tests each passed both on a baseline source copy and on corrected code. They are successful-path equivalence evidence, not RED evidence:

```sh
go test -race -tags=integration ./host/internal/controller/programmatic -run TestProgressPresencePrecedence -count=1
go test -race ./host/internal/usecase/host/sessions -run TestCompactedHistoryAbsentMarkerAndBoundary -count=1
go test -race -tags=integration ./host/internal/usecase/host/sessions -run TestEmptyLabelDeletesPersistedLabel -count=1
```

Existing behavioral coverage also includes `TestCompactedHistoryUsesLatestSummaryAndExactRetainedSuffix` and `TestCompactedHistoryKeepsCompleteToolGroupAtBoundary` in `host/internal/usecase/host/sessions/compaction_history_test.go`; `TestExtensionPayloadBytesRoundTrip` in `host/internal/infra/persistence/sessions/codec_ownership_test.go`; and `ServiceSuite.TestSelectionErrorsPreservePublicCodesAndCauses` in `host/internal/usecase/host/programmatic/selection_test.go`. Full unit and integration tasks execute these tests.

## Verification

Final commands after the scoped code and documentation changes:

| Command | Result |
| --- | --- |
| `task fmt` | Exit 0 |
| `task fix_dry_run` | Exit 0; empty proposal, no fix required after analysis |
| `task lint` | Exit 0; zero issues, ifaceguard clean, no vulnerabilities |
| `task test` | Exit 0, including external-plugin module |
| `task itest` | Exit 0, including external-plugin module |
| `task test-coverage` | Exit 0; 84.3%, required 80.0% |
| `task build` | Exit 0 |
| `git diff --check` | Exit 0 |

## Outside-task deviation and limits

The initial full coverage task exited 201 during its integration run: `serviceSuite.TestTransportFailuresExposeTransientClassification/connection_reset` in `host/internal/infra/providers/openai/compatible/stream_failure_test.go` expected transient classification and received non-retryable classification. Other baseline tasks passed. The initial coverage run is the only reported failing run; the assertion did not print the original underlying transport error text.

Investigation read that test, `classifyProviderFailure` in `host/internal/infra/providers/openai/compatible/service.go`, and `IsTransientTransportFailure` in `host/internal/infra/providers/openai/failure.go`. The test closes a hijacked HTTP connection; classification depends on the error returned by the transport. Tagged reruns passed 30 times alone, 30 times with race detection, and 10 full-package repetitions. A baseline-copy diagnostic assertion including complete error text also passed 100 race-enabled repetitions. Earlier untagged probes ran no relevant tests and provide no reproduction evidence.

Consequences are a possible intermittent check failure; no runtime-impact conclusion is established. The underlying intermittent cause remains unconfirmed. This is outside the extraction task, with the provider owner responsible for a separate investigation or acceptance of the check limitation. No provider change or weakened expectation was made. The final full coverage run passed, so this deviation does not block the scoped extraction result.

## Open questions

None for UNT05. The provider deviation retains the investigation limit above. The ticket remains open for the remaining approved scope.
