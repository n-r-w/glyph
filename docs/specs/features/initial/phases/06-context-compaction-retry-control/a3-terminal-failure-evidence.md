# Completion-plan unit A3: Terminal-failure implementation evidence

Status: Completion-plan unit A3 is accepted after code-quality, text-quality, and independent unit review. The required project checks passed on Linux after the code-review assertion correction. Independent phase acceptance and PHS-06 completion remain pending.

## Scope

Completion-plan unit A3 implements [D3](solution.md#d3-terminal-refusal-and-subsequent-stream-failure), ticket FRQ-04.8.1, and the terminal-category scenarios in [ACC-04.5](ticket.md#acceptance-criteria). PHS-06.2 slice A3 covers automatic compaction; its separate scope appears in the [PHS-06.2 progress record](migration-plan.md#phase-phs-062---implement-the-standard-bundled-compaction-extension).

In `host/internal/infra/providers/openai/compatible/responses.go`, `Driver.streamResponses` returns the captured refusal response and joins its typed error with the subsequent read error and any content-delivery error. In `host/internal/infra/providers/openai/compatible/service.go`, `Driver.completeProviderFailure` preserves the acquired causes. When delivery succeeds, `classifyProviderFailure` classifies the failure from the typed refusal. Independent delivery errors remain outside provider-failure classification. Streams that fail before a received refusal continue through transport-failure classification.

The production edit is confined to that adapter branch. Regression tests and fixtures cover public-client reporting, retry-handler actions, and configured-provider selection. Host retry coordination, client mappings, compaction contracts, cancellation actions, generator limits, dependencies, and generated inputs retain their baseline implementation.

## Behavioral evidence

In `host/internal/infra/providers/openai/compatible/terminal_read_failure_test.go`, `TestResponsesTerminalRefusalBeforeReadFailure` exercises HTTP truncation after context-overflow and non-retryable refusals. For each refusal, it covers accepted delivery, ContentEnd rejection, and final error-event rejection: six subtests in total. The assertions check the typed refusal, `io.ErrUnexpectedEOF`, one provider request, and provider-neutral classification when delivery succeeds. On delivery rejection, the assertions check the independent delivery cause and the absence of provider-failure classification. Terminal diagnostic assertions also run when the final error-event callback rejects delivery; ContentEnd rejection returns before terminal delivery.

In `host/internal/app/terminal_failure_modes_integration_test.go`, `TestTerminalRefusalAcrossPublicClients` exercises Programmatic Control, UI Submit, headless execution, and configured-model Extension SDK operations through production application assembly. A controlled HTTP endpoint supplies six scenarios across four client paths: twenty-four subtests.

In `testdata/external-plugins/cmd/extension/configured_request.go`, `requestConfiguredModelFailures` calls both configured-model wait APIs inside the external extension process. Their SDK declarations are `ConfiguredModelOperation.Wait` and `ConfiguredModelOperation.WaitWithProgress` in `sdk/plugins/extension/v1/context.go`.

| Received failure and retry decision | Public error category | Provider requests per logical request |
|---|---|---:|
| Context overflow with no compaction policy owner or retry override | `CONTEXT_LIMIT` | 1 |
| Non-retryable refusal | `MODEL_FAILED` | 1 |
| Transient refusal, allowance exhausted | `RETRY_EXHAUSTED` | 2 |
| Retry handler returns Cancel after a transient refusal | `RETRY_CANCELED` | 1 |
| Retry handler fails after a transient refusal | `EXTENSION_FAILED` | 1 |
| Read failure before any terminal response | `RETRY_EXHAUSTED` | 2 |

Each public-client subtest checks the error category, `unexpected EOF`, and provider request count. Every refusal subtest also checks the received provider message. Exhaustion assertions check distinct messages from both attempts. Handler-failure assertions also check the full handler cause. Each configured-model scenario performs two independent logical requests, one for each wait API, and checks both results and the combined request count. Headless assertions inspect the returned application error and its category. UI assertions inspect the SDK failure received by the UI process.

The public-client matrix injects provider failures. Independent delivery failures are exercised at the adapter boundary. A failed delivery channel can prevent a client from receiving a terminal event, so the adapter cases alone establish no public-client delivery-failure outcome.

## Test-first evidence

The following observations were recorded on 2026-10-20 against baseline `3bcf90182f4519d219d371454c2ec99c47e7fbc9`, before the production edit. The compiling RED runs exited 1 because behavioral assertions failed.

A separate public Programmatic Control probe received a context-limit refusal followed by HTTP truncation. Glyph made two requests and returned `RETRY_EXHAUSTED` with two copies of `OpenAI-compatible request failed: unexpected EOF`; the provider refusal text was absent. The probe used a temporary build overlay. The repository test sources named below provide the durable regression scenarios.

```sh
go test -tags=integration ./host/internal/app -run '^TestTerminalRefusalAcrossPublicClients$' -count=1 -v -timeout=90s
go test -tags=integration ./host/internal/app ./host/internal/infra/providers/openai/compatible -run 'TestTerminalRefusalAcrossPublicClients|TestResponsesTerminalRefusalBeforeReadFailure' -count=1 -v
```

The public run failed all twenty refusal/client combinations on missing source text. Context-overflow and non-retryable cases also had the wrong category and request count. All four transport-only client cases passed before the production edit. The adapter run failed all six refusal/delivery combinations because the error chain omitted `providerResponseError` and its message.

After the production edit, the focused GREEN command exited 0. All twenty-four public-client subtests and all six adapter subtests passed:

```sh
go test -tags=integration ./host/internal/app ./host/internal/infra/providers/openai/compatible -run 'TestTerminalRefusalAcrossPublicClients|TestResponsesTerminalRefusalBeforeReadFailure' -count=1 -v -timeout=120s
```

Earlier fixture runs failed settings admission or timed out after 240 seconds at the command boundary and 40 seconds at the Go test boundary. Those setup failures are excluded from behavioral RED evidence.

Code review then added assertions that delivery failures remain outside provider-failure classification and that rejected terminal callbacks receive both refusal and EOF diagnostics. A temporary negative control classified delivery errors as provider failures while retaining their causes. With the original assertions, all six adapter subtests passed. With the corrected assertions, all four delivery-rejection subtests failed and the two accepted-delivery subtests passed. This observation establishes regression sensitivity for the assertion correction; the original RED/GREEN runs establish the production change's test-first evidence.

## Project verification

After the code-review assertion correction and subsequent comment corrections on 2026-10-20, these commands all exited 0 on Linux:

| Command | Observed result |
|---|---|
| `task fmt` | No output |
| `task fix_dry_run` | No proposed changes |
| `task lint` | `0 issues.` in both modules; `ifaceguard: no errors found`; `No vulnerabilities found.` |
| `task test` | Root and external-module unit checks completed; output included cached results and packages without tests |
| `task itest` | Root and external-module integration checks completed; the application package ran and the adapter package reused cached results |
| `task test-coverage` | `Coverage: 84.0%, required: 80.0%` for the combined root-module unit and integration suites |
| `task build` | No output |
| `git diff --check` | No output |

The earlier implementation run reported 84.1% coverage before the assertion correction. The table records the later corrected-source run. The coverage task runs both root-module suites with `-count=1`; the full-suite race tasks can reuse Go test cache results.

After the assertion correction, before the comment corrections, the focused race command exited 0. All twenty-four public-client subtests and all six adapter subtests passed without skips:

```sh
go test -race -tags=integration ./host/internal/app ./host/internal/infra/providers/openai/compatible -run 'TestTerminalRefusalAcrossPublicClients|TestResponsesTerminalRefusalBeforeReadFailure' -count=1 -v -timeout=180s
```

Before the assertion correction, an uncached skip census ran `go test -json -count=1 ./...` and `go test -json -count=1 -tags=integration -p 1 -parallel 1 ./...` in both modules. It recorded zero failure events and seven integration-test skips. The root runs contained 2,066 unit pass events and 1,524 integration pass events; the external integration run contained six pass events. These are event counts, including package and parent-test events, rather than counts of independent scenarios.

| Test source and symbol | Skip reason |
|---|---|
| `host/internal/app/headless_runtime_failure_test.go`, `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Directory permission injection requires Darwin permission enforcement |
| `host/internal/app/lifecycle_observer_modes_integration_test.go`, `TestLifecycleObserverThroughStandardTUI` | Real pseudoterminal (PTY) acceptance requires Darwin arm64 |
| `host/internal/app/programmatic_persistence_test.go`, `ProgrammaticAppSuite.TestResumeRecoveryPersistenceFailureProcessPath` | Immutable-file recovery injection requires Darwin `chflags` |
| `host/internal/app/standard_tui_app_test.go`, `TestStandardTUIHostSmoke` | Real PTY acceptance requires Darwin arm64 |
| `host/internal/app/runtime_failure_tui_test.go`, `TestStandardTUIHostRuntimePersistenceFailures` | Real PTY acceptance requires Darwin arm64 |
| `internal/testsupport/tui/tui_test.go`, `TestEarlyFailureCleanup` | Real PTY cleanup requires Darwin arm64 |
| `plugins/ui/tui/internal/app/pty_test.go`, `TestStandardTUIPTY` | Real PTY acceptance requires Darwin arm64 |

The census recorded no A3 test skips. Package events for packages without applicable tests were counted separately: root unit 35, root integration 55, external unit three, external integration two. The seven platform-specific scenarios remain unobserved on Linux.

## Independent unit review

Code-quality review added assertions for delivery-error classification and terminal diagnostics. Text-quality review corrected evidence chronology, durable source references, and the distinction from PHS-06.2 slice A3. Independent unit review accepted the complete A3 change set without remaining findings.

The independent reviewer repeated all thirty focused cases with the final production source and observed zero failures or skips. With the baseline adapter substituted through a temporary build overlay, twenty refusal/client cases and six adapter cases failed while the four transport-only client cases passed. These observations confirm regression sensitivity; the original recorded runs establish test-first order.

## Remaining boundaries

The history-source correction and independent PHS-06.4 acceptance remain pending. Controlled HTTP and SDK tests provide no live-provider or physical standard TUI presentation evidence. PHS-06 remains pending.

## Open questions

None for the terminal-refusal implementation.
