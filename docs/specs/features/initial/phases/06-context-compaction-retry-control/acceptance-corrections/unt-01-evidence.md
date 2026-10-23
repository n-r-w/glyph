# UNT-01 implementation and correction evidence

## Result and scope

UNT-01 implements the private Codex refusal continuation described in the [technical solution](technical-solution.md#codex-refusal-only-continuation). Passing driver and public application assertions cover [FRQ-01](requirements.md#functional-requirements) and [A1](target-picture.md#acceptance-scenarios). Phase acceptance and the remaining units retain their existing status.

Base HEAD is `7238582309a8abd2750898ac0c0d8bbc07e78a18`. The initial checkout contained the untracked acceptance-corrections documents and the plan's in-progress status. Those documents were preserved. No commit or push was made. The explicit uncommitted-diff instruction takes precedence over the plan's local-commit criterion.

### Changed paths

- `host/internal/infra/providers/openai/codex/provider.go`: `Driver.executeRequest` observes request-local delivery failure and continues SDK iteration only for a typed refusal with successful semantic delivery.
- `host/internal/infra/providers/openai/codex/refusal_continuation_integration_test.go`: continuation, immediate-exit, and cancellation assertions.
- `host/internal/app/terminal_failure_modes_integration_test.go`: focused ordinary and configured SDK Codex cases in the shared public fixture.
- `docs/specs/features/initial/phases/06-context-compaction-retry-control/acceptance-corrections/unt-01-evidence.md`: implementation and correction evidence.

No public contract, dependency, toolchain, persisted format, generated mock, or phase status changed. F17 and optional F18 work remained excluded.

## Accepted review corrections

The text review is desk message `2d31a5df-d0e6-4095-9f41-831028b65506`; the code-quality review is `b8febf92-669f-4afb-9d0a-87671f973e61`. All accepted findings were corrected within this unit.

| Finding | Correction |
| --- | --- |
| Text FND-01 | Historical implementation-session tool limits and delivery-session repository review status are distinguished in one review-status paragraph below. |
| Text FND-02 | The historical skip inventory quotes the actual preceding output reasons for all seven named skips. |
| Text FND-03 | Unsupported advisory outcome and concern-closure claims were removed. No advisory assessment is used as repository-review acceptance. |
| Text FND-04 | Final three-file fingerprints are separate from tracked generation manifests. Original RED chronology is attributed to the implementation account; the absent immutable RED snapshot is explicit. Recorder coverage is bounded to files actually present. |
| Text FND-05 | Material commands, output excerpts, source identities, and provenance limits are inline. Machine-local raw artifact locations are confined to the desk handoff. |
| Text FND-06 | Both HTTP cancellation comments describe cancellation after refusal delivery and the asserted causes, classification, and outcome. |
| Text FND-07 | The production comment says continued context-bound iteration acquires any later error, covering clean termination too. |
| Code-quality FND-01 | Added provider ID, overflow code, public category, and JWT claim-key literals use test-local named constants. Values and assertions are unchanged. |

These corrections change comments, constant representation, and this report. They request no production behavior change.

## Final source identity

Source set **S1** is the corrected code frozen for re-review. SHA-256 values were calculated directly from all three files, including the untracked test:

```text
86b8b72f8a16e92c6442a00e0143a0e681cb61198a599a44093b5f0eb99ea1a2  host/internal/infra/providers/openai/codex/provider.go
a937263d68879c96f7c83e401d4e1ac4a90281d165d3cb0b84d12601f03001bd  host/internal/infra/providers/openai/codex/refusal_continuation_integration_test.go
3d1b1d40242a3102c44a1e529e178abb123ad0321fe9dccc4fa3d40673645961  host/internal/app/terminal_failure_modes_integration_test.go
```

The complete readiness batch has identical before/after three-file fingerprints and the base HEAD above. Subsequent recorded formatting and fix-dry-run commands each have identical before/after S1 fingerprints. Generation also retained S1. Code remains frozen after those checks; the evidence document was rewritten afterward.

## Original RED/GREEN history and limits

The implementation agent's recorded execution account places both original compiling RED runs before the production edit. The raw RED output demonstrates diagnostic failures, but contains no run-specific source hash. An immutable RED source snapshot was not retained. The final test file includes later assertion, cancellation-evidence, formatting, constants, and comment refinements. Consequently, neither S1 nor the historical final fingerprint identifies the exact test source used for original RED. No later reproduction is presented as original RED chronology.

### Driver RED

Command:

```sh
TMPDIR=/var/tmp go test -count=1 -tags=integration ./host/internal/infra/providers/openai/codex -run '^TestRefusalContinuation$' -v
```

Recorded exit: **1**. Material original output:

```text
TestRefusalContinuation/read_failure
Target error should be in err chain:
expected: "unexpected EOF"
in chain: "OpenAI Codex request failed: refusal source\nrefusal cleanup source"
missing call(s) to *codex.MockIOReadCloser.Read(is anything)
--- FAIL: TestRefusalContinuation
FAIL github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex
```

The original output also contains missing later-read causes in conversion/merge cases and unmet later-read expectations in clean-end/cancellation cases. This is a compiled assertion failure, not a compile or environment failure.

### Public RED

Command:

```sh
TMPDIR=/var/tmp go test -count=1 -tags=integration ./host/internal/app -run '^TestTerminalRefusalAcrossPublicClients/Codex' -v
```

Recorded exit: **1**. Material original output:

```text
TestTerminalRefusalAcrossPublicClients/Codex_context_overflow/programmatic
"OpenAI Codex request failed: provider terminal rejection full unique text attempt 1" does not contain "unexpected EOF"
TestTerminalRefusalAcrossPublicClients/Codex_context_overflow/configured_SDK
"request configured model: extension context CONTEXT_LIMIT: request configured model: execute model request: OpenAI Codex request failed: provider terminal rejection full unique text attempt 1" does not contain "unexpected EOF"
"request configured model: extension context CONTEXT_LIMIT: request configured model: execute model request: OpenAI Codex request failed: provider terminal rejection full unique text attempt 2" does not contain "unexpected EOF"
--- FAIL: TestTerminalRefusalAcrossPublicClients
```

Both original commands subsequently exited **0** after the production correction, according to the recorded execution account and passing outputs. Strict immediate-exit assertions were added next and passed. Those assertions strengthen preservation evidence; they are not represented as newly changed behavior with original failing-before-fix output.

The first whole-project lint run after implementation exited **201** with 12 line-length findings, three assertion-form findings, and two unnecessary conversions in the tests. Direct corrections removed those issues without suppressions. The prior full verification then passed except for the accepted scanner exception. Current correction verification is recorded separately below.

Historical RED and early formatting/fix-dry-run outputs do not have complete adjacent command/exit/source recorder files. Their commands and exit values are retained here as the implementation execution account. This limitation does not apply to the current recorded checks listed below.

## Behavior observations

| Requirement or distinction | Passing assertion at S1 |
| --- | --- |
| A1 ordinary request | `TestTerminalRefusalAcrossPublicClients/Codex_context_overflow/programmatic` exercises real application assembly and RPC failure mapping. It asserts refusal text plus unexpected EOF, `CONTEXT_LIMIT`, and one provider attempt. |
| A1 configured request | The `configured_SDK` case exercises both `Wait` and `WaitWithProgress` through the external extension and Host assembly. Each result retains its refusal and EOF with `CONTEXT_LIMIT`. Two configured invocations produce two attempts total, one per logical request; outer tool/report requests are counted separately. |
| Refusal plus later read/close causes | `TestRefusalContinuation/read_failure` requires separate later reads and asserts cause identities, overflow classification, one terminal event, and one occurrence of the close cause. |
| Joined conversion/merge causes | `conversion_failure` and `merge_failure` require later reads and retain refusal, conversion/merge diagnostic, unexpected EOF, and close cause. |
| Later semantic isolation | Continuation cases supply a later text event and assert no delivered text delta and one terminal event. |
| Clean end | `clean_end` supplies EOF and successful Close, retains refusal/category, and excludes missing-terminal and unexpected-EOF diagnostics. |
| Success and assembler-only immediate exits | `TestTerminalImmediateExits/completed`, `length`, and `assembler_only` permit only one body read. Existing successful-outcome suites also run in the full checks. |
| Delivery immediate exits | `delta_delivery`, `refusal_content_end_delivery`, and `final_delivery` permit only one body read, retain delivery/close causes, and bypass provider classification. Refusal ContentEnd failure also retains the refusal. Earlier delivery failures emit no terminal callback; final delivery has one callback. |
| Cleanup ordering | Immediate-exit cases assert Close before terminal delivery. Continuation cases retain acquired cleanup causes. |
| Owning cancellation | The controlled mock case signals entry into a continued body read, then cancels the owning context and retains refusal/cancellation/close causes and aborted presentation. The real HTTP case cancels after refusal ContentEnd delivery and asserts cancellation identity, refusal text, overflow classification, and aborted presentation. |

The HTTP case does not acknowledge an already-blocked network read. The mock case separately establishes continued-read entry. Neither case supplies the retry-timer evidence assigned to UNT-05. Public additions are limited to ordinary programmatic and configured SDK Codex overflow; the existing compatible-provider client matrix is unchanged.

Source inspection is distinct from these executions: `Driver.executeRequest` supplies the owning context to SDK `NewStreaming` with SDK retries disabled; its existing defer joins Close and cancellation causes. `Driver.Stream` retains exact delivery-error ownership. `classifyCodexFailure` searches joined typed refusal causes before transport classification. SDK v3.49.0 iteration obtains decoder/scanner errors; Close alone closes the body. SDK iteration stops at its first error, so this mechanism makes no promise to acquire failures beyond SDK termination.

Symbolic references were requested before edits. `Driver.executeRequest` references identify `Driver.Stream`. The active LSP build configuration excludes both integration-tag test files; their reference requests returned that limitation, and their containing source was read directly.

## Current readiness checks

Every command below ran after the last constants/comment edit. All code checks refer to S1, rather than HEAD alone. UTC intervals are recorded execution metadata. The two focused commands were uncached and race-enabled. Full task logs also contain cached package results and packages with no test files.

| Command | UTC start to end, 2026-10-23 | Exit and material result |
| --- | --- | --- |
| `task lint` | 01:01:16Z to 01:01:26Z | **201**: code lint `0 issues.`; ifaceguard `no errors found`; govulncheck only GO-2026-6809, underlying exit 3. |
| `TMPDIR=/var/tmp task test` | 01:01:26Z to 01:01:29Z | **0**, main and external-module unit tests. |
| `TMPDIR=/var/tmp task itest` | 01:01:29Z to 01:07:02Z | **0**, main and external-module integration tests. |
| `TMPDIR=/var/tmp task test-coverage` | 01:07:02Z to 01:11:22Z | **0**: `Coverage: 84.2%, required: 80.0%`. |
| `TMPDIR=/var/tmp task build BUILD_ROOT=<temporary build directory>` | 01:11:22Z to 01:11:24Z | **0**, Host and standard plugin executables built outside the checkout. Only the machine-local BUILD_ROOT argument is abbreviated here; the exact command is in the desk handoff. |
| Focused driver command below | 01:11:24Z to 01:11:26Z | **0**, all three new test functions and table cases passed. |
| Focused public command below | 01:11:26Z to 01:11:29Z | **0**, ordinary and configured SDK Codex cases passed. |
| Root-resolved external lint command below | 01:11:29Z to 01:11:29Z | **0**, `0 issues.`. |
| `task fmt` | 01:12:00Z to 01:12:05Z | **0**, empty output and unchanged S1. |
| `task fix_dry_run` | 01:12:05Z to 01:12:05Z | **0**, empty output and unchanged S1. No proposal required `task fix`. |

```sh
TMPDIR=/var/tmp go test -race -count=1 -tags=integration ./host/internal/infra/providers/openai/codex -run 'Test(RefusalContinuation|TerminalImmediateExits)' -v
TMPDIR=/var/tmp go test -race -count=1 -tags=integration ./host/internal/app -run '^TestTerminalRefusalAcrossPublicClients/Codex' -v
(cd testdata/external-plugins && "$(cd ../.. && go tool -n golangci-lint)" run --config ../../.golangci.yml --build-tags=integration ./...)
```

The external lint binary was resolved in ROOT because main lint stopped before the nested-module step. Material current output excerpts:

```text
0 issues.
ifaceguard: no errors found
Vulnerability #1: GO-2026-6809
Distrust compromised intermediate certificates in crypto/x509
Found in: crypto/x509@go1.27.2
Fixed in: crypto/x509@go1.27.3
Your code is affected by 1 vulnerability from the Go standard library.
This scan found no other vulnerabilities in packages you import or modules you require.
task: Failed to run task "lint": exit status 3

--- PASS: TestTerminalImmediateExits
--- PASS: TestRefusalContinuationHTTPCancellation
--- PASS: TestRefusalContinuation
PASS
ok github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex 1.034s

--- PASS: TestTerminalRefusalAcrossPublicClients
--- PASS: TestTerminalRefusalAcrossPublicClients/Codex_context_overflow/programmatic
--- PASS: TestTerminalRefusalAcrossPublicClients/Codex_context_overflow/configured_SDK
PASS
ok github.com/n-r-w/glyph/host/internal/app 1.547s

ok example.com/glyph-external-plugins (cached)
ok example.com/glyph-external-plugins/cmd/compaction-policy (cached)
Coverage: 84.2%, required: 80.0%
```

Spacing and test duration suffixes are omitted in the grouped PASS excerpts. Scanner text is retained; the scanner was not suppressed. The baseline had the same accepted vulnerability-only lint exit **201**. Baseline unit/integration/coverage/build commands exited **0**, coverage was 84.2%, external lint exited **0**, and nonmodifying formatting inspection and fix-dry-run exited **0**. No current required check introduces an additional failure relative to that baseline.

Current readiness and focused commands have actual command, exit, start/end, and raw-output recorder files. Their before/after source fingerprints cover the whole frozen batch. The later recorded fmt/fix-dry-run commands additionally have individual before/after fingerprints. These records exist; no equivalent metadata is claimed for original RED or earlier unrecorded operations.

### Generation stability

After the readiness commands, `task generate` ran twice with recorded command/output/exit files. Both exits were **0**. Tracked-file manifests were generated with `git ls-files -z | xargs -0 sha256sum` before and after each run. Both `cmp` comparisons exited **0**. Each manifest contains 1,375 tracked paths and has SHA-256:

```text
e0a73b7915452145e189dd1948dafe68e303cb8a08e7ffc85fb84533efc4c681
```

These manifests exclude the untracked regression test. They establish tracked generation stability only. The separate S1 three-file check includes that test and passed again after generation. No generated path appeared in Git status. `git diff --check` exited **0**.

## Historical uncached skip inventory

The preceding implementation verification executed these commands before the current constants/comment corrections; both exited **0**:

```sh
TMPDIR=/var/tmp go test -json -count=1 -tags=integration -p 1 -parallel 1 ./...
(cd testdata/external-plugins && TMPDIR=/var/tmp go test -json -count=1 -tags=integration -p 1 -parallel 1 ./...)
```

Their captured events were reread for this correction. Each test-level skip was joined to its preceding output, which supplies the following exact reason. This is historical skip evidence, not a new uncached event inventory for S1. The current full integration task passed; these historical skipped scenarios are not claimed as executed successes.

| Package | Test | Exact preceding output reason |
| --- | --- | --- |
| `host/internal/app` | `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | `directory permission failure injection requires Darwin permission enforcement` |
| `host/internal/app` | `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | `immutable-file recovery injection requires Darwin chflags` |
| `host/internal/app` | `TestLifecycleObserverThroughStandardTUI` | `real PTY acceptance runs on Darwin arm64` |
| `host/internal/app` | `TestStandardTUIHostRuntimePersistenceFailures` | `real PTY acceptance runs on Darwin arm64` |
| `host/internal/app` | `TestStandardTUIHostSmoke` | `real PTY acceptance runs on Darwin arm64` |
| `plugins/ui/tui/internal/app` | `TestStandardTUIPTY` | `real PTY acceptance runs on Darwin arm64` |
| `internal/testsupport/tui` | `TestEarlyFailureCleanup` | `real PTY cleanup runs on Darwin arm64` |

The historical main stream contains seven test-level skips and 55 package-level no-test skips. The external stream contains zero test-level skips and two package-level no-test skips. Reasons are quoted from captured output only; the excluded subjects were not investigated or reproduced.

## Review status and remaining risks

The implementation agent lacked the named repository-review tools and reports historical advisory assessments of supplied source whose review records are not included here. The delivery owner is running repository reviews with the named review agents in the main delivery session. UNT-01 remains in progress until the delivery owner resolves those reviews, including re-review of this corrected frozen source and evidence. No advisory outcome or closure of an advisory concern is asserted by this record.

No unresolved implementation question requires a user decision. No unrelated defect, workaround, deprecation, or new technical debt was introduced. The accepted vulnerability remains visible with its actual failing scanner exit. The design's material risk remains continued context-bound SDK reading after a delivered refusal until SDK termination or owning cancellation. Strict immediate-exit assertions constrain the excluded terminals. No cancellation-free drain or additional timeout was introduced. This record makes no whole-phase completion claim.
