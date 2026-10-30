# UNT-01 implementation evidence

## Status

The latest 2026-10-30 follow-up corrects behavior FND-03 in `6aae0713-3703-49e8-9b07-f2af7b9e9522` and text FND-02/FND-03 in `9d216f24-f539-4cf5-b69f-89de93f76e35`. The response-secret lifetime and cookie over-redaction defects are corrected in code; the guide now qualifies acquired rejection diagnostics. Prior code/conformance reviews passed the earlier correction, and prior token/URL-auth fixes were confirmed. The new changes require all affected reviews again. This record does not declare UNT-01 accepted. No full-project check, dependency change, commit or UNT-02 work occurred. Earlier correction records remain explicitly identified as history.

## Public wire admission

Schema generation: `go tool easyp generate` exited 0.

Behavioral RED command:

```text
go test ./sdk/plugins/extension/v1 -run '^TestWireHandlerAdmission$' -count=1
--- FAIL: TestWireHandlerAdmission (0.00s)
    wire_test.go:29:
        Error: Not equal:
            expected: true
            actual  : false
        Test: TestWireHandlerAdmission
        Messages: kind HANDLER_KIND_PROVIDER_HEADERS payload 0
FAIL
FAIL github.com/n-r-w/glyph/sdk/plugins/extension/v1 0.004s
FAIL
Command exited with code 1
```

The test compiled against the new public schema and failed because SDK admission did not accept a registered wire payload.

GREEN after adding the SDK payload predicates:

```text
go test ./sdk/plugins/extension/v1 -run '^TestWireHandlerAdmission$' -count=1
ok github.com/n-r-w/glyph/sdk/plugins/extension/v1 0.003s
```

The test checks all three declared payloads and rejection of the six mismatched kind/payload combinations.

## Implemented path and review entry points

The implementation adds the three provider-wire handler kinds, typed public payloads and generated bindings in `api/plugins/extension/v1` and `pkg/plugins/extension/v1`. SDK admission uses `newHandlerPayloadPredicates` in `sdk/plugins/extension/v1/server.go`.

Host registers handlers through `startup.Service`, invokes the exact runtime generation through `extensionruntime.Service`, and maps ordinary extension operations through `runtime.Runtime.HandleWire`. `middleware.Service` owns ordered composition, detached immutable original/current values and replacement validation. Headers finish before body handlers; response handlers acknowledge metadata in order.

`modelexecution.Service.execute` attaches neutral callbacks per raw attempt. `WireCallbacks.captureFailures` and `wireFailure` retain acquired middleware failures before the SDK can reduce them to cancellation. Host checks that failure before provider retry classification. Pure owning cancellation retains cancellation meaning.

`openai.WireDiagnostics.NewWireOption` uses the declared SDK middleware hook. Codex, compatible Chat and compatible Responses retain a fresh infrastructure-owned `WireDiagnostics` value through each raw attempt's final error delivery. `prepareWireRequest` retains original body bytes on preserve, validates each replacement, and restores retained private replay references. `wireProjection` keeps replay fragments invocation-local. Header projection excludes protected authentication, cookie and account state, validates public writes and redacts known authentication-secret echoes. Ordinary account identifiers remain in diagnostics. Observer failure closes the response and retains close errors.

Application assembly binds the owner in Programmatic, UI and headless execution. The separately built public reference extension is `testdata/external-plugins/cmd/wire`. See the [author guide](../../../../../guidelines/provider-wire-middleware.md) and [technical solution](technical-solution.md) for the contract and selected mechanisms.

This is an implementation inventory supported by focused tests and inspection, not an independent conformance review of every changed line.

## Follow-up correction handoff on 2026-10-30

### Root-cause/fix chains

1. Behavior FND-03: HTTP 200 metadata reached the shared hook, which collected cookie/authentication values and sanitized the successful observation. The local secret slice expired before later SSE errors. Codex's completion recovered only rejection headers, and compatible completion rediscovered headers only from SDK HTTP rejection errors. Neither path retained successful-response knowledge. Each concrete `Driver.Stream` now owns one `WireDiagnostics` value. Its `NewWireOption` records request and response authentication secrets before observation. The same value masks terminal messages after stream/cleanup/cancellation composition and masks the final returned error after terminal-consumer failures. Recording secret knowledge never invokes `Failed`.
2. Codex's existing rejection-capture fallback remains necessary: its transport can retain rejection metadata but return no response when reading or closing that rejection fails. `Driver.executeRequest` contributes those retained headers to the same diagnostic state before `completeWireDiagnostics`. The new fallback regression passed before correction and remains GREEN; it is a preserved invariant, not a newly established RED.
3. Compatible cleanup: both stream helpers acquired and discarded `stream.Close()` errors. Their named-return defers now join those errors before `completeProviderFailure` formats the terminal message. The latter preserves mixed cancellation text and all acquired source identities. The final `Driver.Stream` defer also masks errors returned by a terminal consumer. This is a direct correction under the assignment's acquired-cause requirement.
4. Text FND-03: `CollectWireSecrets` appended complete Cookie/Set-Cookie strings before extracting credential values. Exact header echoes therefore lost cookie names and attributes. Collection now extracts cookie values first and omits complete cookie header strings. `RedactWireText` documents its actual responsibility: replacement of supplied nonempty secret strings without interpreting surrounding diagnostic text.
5. Text FND-02: the guide promised rejection status/body in every observer failure. It now distinguishes Codex's acquired rejection diagnostics from compatible observer/cleanup errors returned before SDK rejection decoding. No generic HTTP body/status capture was added.

All corrections are direct and within UNT-01. Secret state remains in provider infrastructure; the neutral callback and public extension contracts are unchanged. Normal provider classification and retry logic still use original identities through `wireError.Unwrap`.

### Fresh pre-correction RED

The following commands ran before the production corrections and exited 1:

```sh
go test ./host/internal/infra/providers/openai -run '^TestWireCookieDiagnostics$' -count=1
go test -tags=integration -p 1 -parallel 1 ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app -run '^(TestWireStreamSecrets|TestPublicWireStreamSecrets)$' -count=1
go test -tags=integration ./host/internal/infra/providers/openai/codex -run '^TestWireStreamCleanupSecrets$' -count=1
go test -tags=integration ./host/internal/infra/providers/openai/compatible -run '^TestWireStreamCleanupSecrets$' -count=1
```

- Cookie diagnostics expected `Set-Cookie: session=[REDACTED]; Path=/; HttpOnly; SameSite=Lax`, but received `Set-Cookie: [REDACTED]`. The request-cookie case likewise lost `session=` and `refresh=`.
- All three raw adapters exposed `cookie-private` and `response-auth-private` in returned errors and terminal messages after successful sanitized HTTP 200 observation. Configured Codex also exposed them. Public Codex returned `MODEL_FAILED` after one request while exposing `cookie-private`. Metadata safety, ordinary identifiers and normal provider-category assertions passed; confidentiality assertions failed.
- Codex cleanup/cancellation retained its cleanup identity but exposed the cookie in provider and cleanup diagnostics. Compatible Chat/Responses additionally failed `errors.Is(err, cleanup)` because their close errors had been discarded.

After the first lifetime correction, the Codex cleanup test also returned a terminal-consumer cause containing the known cookie. The same Codex command exited 1: provider and cleanup text were already `[REDACTED]`, but `complete terminal cause account=failure-account session=cookie-private` remained raw. Moving the state lifetime to the outer `Driver.Stream` completion corrected that final join.

A separate preservation check ran on the entry implementation and exited 0:

```sh
go test -tags=integration ./host/internal/infra/providers/openai/codex -run '^TestWireRejectionCaptureSecrets$' -count=1
```

It checks response-less rejection capture with original read/close identities, HTTP 429 and safe diagnostic text, with no wire callbacks installed.

### Additional saved-entry-state baseline RED

The compatible terminal-consumer and public compatible regressions were added after implementation. Their RED runs used the actual Go files saved at assignment entry through a Go overlay, plus the new regression sources. The newly introduced diagnostic-state file was absent from that entry state and was represented by a package-only overlay file. No working source was rolled back, no assertions were relaxed, and these runs are not claimed as chronological pre-fix or recovered historical runs.

```sh
go test -overlay=/tmp/unt01-stream-correction/baseline-overlay.json -tags=integration ./host/internal/infra/providers/openai/compatible -run '^TestWireStreamTerminalSecrets$' -count=1
go test -overlay=/tmp/unt01-stream-correction/baseline-overlay.json -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicWireCompatibleStreamSecrets$' -count=1
```

Both exited 1 for Chat and Responses. The first retained the terminal-consumer identity but exposed the cookie in both the provider cause and the later terminal cause. The second returned normal public `MODEL_FAILED` after one request but exposed `Set-Cookie: session=cookie-private; Path=/; HttpOnly; SameSite=Lax`. The overlay path records an executed temporary baseline artifact; its contents are not a durable fixture.

### Final regression GREEN

After the last source/test edit:

```sh
go test ./host/internal/infra/providers/openai -run '^TestWireCookieDiagnostics$' -count=1
# ok github.com/n-r-w/glyph/host/internal/infra/providers/openai 0.004s

go test -tags=integration -p 1 -parallel 1 ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app -run '^(TestWireStreamSecrets|TestWireStreamCleanupSecrets|TestWireStreamTerminalSecrets|TestWireRejectionCaptureSecrets|TestPublicWireStreamSecrets|TestPublicWireCompatibleStreamSecrets)$' -count=1
# ok github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex 0.009s
# ok github.com/n-r-w/glyph/host/internal/infra/providers/openai/compatible 0.011s
# ok github.com/n-r-w/glyph/host/internal/app 0.921s
```

Raw and terminal tests cover all three adapters. Public Programmatic tests cover Codex and both compatible APIs. They assert one dispatch with retries disabled, ordinary account identifiers, complete non-secret provider text, and intact cookie names/attributes. Normal raw stream failures remain `ProviderFailureNonRetryable`; configured Codex uses `FailureModelFailed`, and public provider failures use `MODEL_FAILED`. Successful metadata observations exclude protected fields and mask known secret echoes. Cleanup and terminal-delivery regressions preserve original source identities with `errors.Is`; concurrent cancellation identity and non-secret text also remain. These combined credential-echo/cleanup regressions supersede the earlier record's missing combined-fixture limitation.

### Focused checks after the final edit

```sh
go test -race -count=1 ./host/internal/infra/providers/openai/...
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/infra/providers/openai/...
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/app -run 'Wire'
go test -race -count=1 ./host/internal/usecase/host/modelexecution -run 'Wire'
```

All exited 0. Provider unit packages passed in 1.013s, 1.030s and 1.013s. Provider integration packages passed in 1.021s, 1.263s and 1.252s. App wire integration passed in 7.970s; neutral model-execution wire tests passed in 1.014s. These are affected-package/behavior checks, not whole-project checks. They include prior token redaction, URL Basic authentication and explicit Authorization precedence, rejection/transport diagnostics, cleanup/cancellation, replay and ordinary provider retry.

```sh
go tool golangci-lint run --config .golangci.yml --build-tags=integration ./host/internal/infra/providers/openai/... ./host/internal/app
go tool golangci-lint run --config .golangci.yml --new-from-patch=/tmp/unt01-stream-correction/focused.patch ./host/internal/infra/providers/openai/... ./host/internal/app
```

Both exited 0 with `0 issues.` The second command checks no-tag changed lines against a patch from the saved assignment-entry sources; it does not certify unrelated unit-build lines. Focused formatting passed. Initial new-test lint findings were long fixture lines, require-style error assertions, and duplicated successful-observer setup; direct corrections split literals, used require assertions and shared test-only setup without suppressions. New mocks are mockgen-generated, integration-tagged test-only implementations of the existing `io.ReadCloser` and `http.RoundTripper` ports. No production abstraction was introduced for mocking.

### Exact follow-up paths

```text
docs/guidelines/provider-wire-middleware.md
docs/specs/features/initial/phases/08-prompt-context-input-provider-middleware/unt-01-evidence.md
host/internal/app/wire_compatible_integration_test.go
host/internal/app/wire_compatible_settings_integration_test.go
host/internal/app/wire_compatible_stream_integration_test.go
host/internal/app/wire_stream_integration_test.go
host/internal/infra/providers/openai/codex/provider.go
host/internal/infra/providers/openai/codex/wire.go
host/internal/infra/providers/openai/codex/wire_capture_integration_test.go
host/internal/infra/providers/openai/codex/wire_stream_cleanup_integration_test.go
host/internal/infra/providers/openai/codex/wire_stream_integration_test.go
host/internal/infra/providers/openai/compatible/chat.go
host/internal/infra/providers/openai/compatible/responses.go
host/internal/infra/providers/openai/compatible/service.go
host/internal/infra/providers/openai/compatible/wire_observer_integration_test.go
host/internal/infra/providers/openai/compatible/wire_readcloser_mock_test.go
host/internal/infra/providers/openai/compatible/wire_roundtripper_mock_test.go
host/internal/infra/providers/openai/compatible/wire_stream_cleanup_integration_test.go
host/internal/infra/providers/openai/compatible/wire_stream_integration_test.go
host/internal/infra/providers/openai/compatible/wire_stream_terminal_integration_test.go
host/internal/infra/providers/openai/wire.go
host/internal/infra/providers/openai/wire_close_integration_test.go
host/internal/infra/providers/openai/wire_cookie_diagnostics_test.go
host/internal/infra/providers/openai/wire_diagnostics.go
host/internal/infra/providers/openai/wire_integration_test.go
host/internal/infra/providers/openai/wire_preserve_integration_test.go
host/internal/infra/providers/openai/wire_retention_integration_test.go
```

### Review boundary and limits

The new changes require fresh code-quality, text-quality, behavior and technical-solution-conformance disposition. An independent advisory check identified the compatible late-terminal evidence gap; the added baseline RED/current GREEN closes that specific gap. This advisory work is not acceptance by the original reviewers.

All remote-provider fixtures are local and synthetic. No live-provider incident, credential or complete-project result is claimed. Successful-response secret tests install wire callbacks; the no-callback success collector path is source-inspected, and the no-callback rejection-capture path has an executed regression. No generic compatible rejection-body capture was added. Historical RED gaps documented below remain historical limits. The unchanged `Runtime.context` and unrelated no-tag lint findings remain outside this correction, with their earlier descriptions retained below. No new outside-task defect was established. No dependency, public contract, environment, commit or UNT-02 change occurred.

## Prior correction record on 2026-10-30: FND-01/FND-02

This subsection preserves the earlier implementation names and executed checks as history. The follow-up above supersedes its current-code references and its combined-cleanup-fixture limitation.

### Root causes and direct corrections

- Behavior FND-01: the shared bridge redacted its acquired observer envelope, but Codex appended its separately captured rejection body afterward. SDK cancellation could also replace the observer error before Codex built its terminal response. `codex.Driver.executeRequest` now retains the bridge-reported cause. `errorCaptureTransport.completeDiagnostics` joins cleanup/cancellation before redacting the completed error and terminal message using the attempt access token and acquired response authentication/cookie secrets. `wireError.Unwrap` preserves source identities and provider classification. The rejection-body/status branch remains intact.
- Behavior FND-02: `http.Client` derived Basic Authorization from URL userinfo after the bridge's secret snapshot. `acquireWireAuthentication` now materializes that same value before projection when Authorization is empty and acquires plain/percent-encoded passwords. Explicit Authorization takes precedence. The SDK retains its original request separately from the middleware clone; `RedactWireError` acquires known authentication values from that original request and response before compatible failure delivery. URLs remain private, and transport diagnostics retain their non-secret username.
- Diagnostic policy: the former collector treated every protected header value as a secret. `CollectWireSecrets` distinguishes authentication secrets from ordinary account, organization and project identifiers. Structured middleware payloads still exclude protected headers; error text retains ordinary identifiers.
- Code-quality FND-01 through FND-03: callback capture is `WireCallbacks.captureFailures`; the identified helpers have verb-led names and all callers are updated; the cited assertion declarations have purpose comments. No wrapper type or dependency was added.
- Protobuf: `ProviderBodyAction` now says it preserves the current safe JSON or replaces it. `go tool easyp generate` exited 0 and regenerated the Go comment.

These are direct, in-scope root corrections. Provider retries, error categories, supported authentication configuration and the rejection diagnostic branch remain supported.

### Fresh regression RED and GREEN

The following invocations preceded the security corrections and exited 1:

```sh
go test ./host/internal/infra/providers/openai -run '^TestWireErrorPreservesOrdinaryIdentifiers$' -count=1
go test -tags=integration ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app -run '^(TestCodexAcquiredDiagnosticRedaction|TestWireURLAuthentication|TestPublicWireAcquiredRedaction)$' -count=1
```

The identifier test expected `account=ordinary-account organization=ordinary-organization project=ordinary-project`, but each identifier became `[REDACTED]`. Codex configured/public errors and raw terminal messages contained the synthetic access token in the rejection body. Observer text hid the ordinary account identifier. The raw canceled scenario additionally failed `errors.Is(err, source)` because SDK cancellation replaced the observer error. URL Basic subtests observed the encoded credential in `X-Auth-Echo`; failing-observer errors exposed it. Successful-observer rejection errors contained the URL password, including with explicit bearer Authorization.

Additional RED invocations, each exiting 1 before its corresponding correction:

```sh
go test -tags=integration ./host/internal/infra/providers/openai/codex -run '^TestCodexAcquiredDiagnosticRedaction$' -count=1
go test -tags=integration ./host/internal/infra/providers/openai/compatible -run '^TestWireURLAuthentication$' -count=1
go test -tags=integration ./host/internal/infra/providers/openai/compatible -run '^TestWireURLTransportDiagnostics$' -count=1
```

The Codex fixture added `Set-Cookie: session=synthetic-session-secret` and a rejection-body echo; the error still contained `synthetic-session-secret`. The URL-auth fixture used a reserved-character password; SDK rejection text still contained `synthetic-user:synthetic%2Fp%40ssword`. The URL-transport fixture reproduced an intermediate correction regression in both APIs: removing all userinfo produced `Post "http://127.0.0.1:...": EOF` without `ordinary-user-identifier`. The final correction retains private userinfo and removes password representations from diagnostics. That transport RED is a correction regression, not historical initial-feature evidence.

Final GREEN after the last production edit:

```sh
go test ./host/internal/infra/providers/openai -run '^TestWireErrorPreservesOrdinaryIdentifiers$' -count=1
# ok github.com/n-r-w/glyph/host/internal/infra/providers/openai 0.004s

go test -tags=integration ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app -run '^(TestCodexAcquiredDiagnosticRedaction|TestWireURLAuthentication|TestWireURLTransportDiagnostics|TestPublicWireAcquiredRedaction)$' -count=1
# ok github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex 0.009s
# ok github.com/n-r-w/glyph/host/internal/infra/providers/openai/compatible 0.010s
# ok github.com/n-r-w/glyph/host/internal/app 0.443s
```

Codex covers raw terminal callbacks and configured execution, each with and without cancellation. Assertions retain complete non-secret observer/provider text, HTTP 429, account identifiers and source/cancellation identities. Configured failures remain `EXTENSION_FAILED` after one request. The public Programmatic fixture checks the same category, one request, observer source and safe rejection detail. Compatible coverage includes both APIs, explicit versus URL-derived Authorization, and successful versus failing observers. Assertions preserve non-secret headers, status, transient provider classification, observer identity and one request. The transport test preserves username and EOF after one request without response/failure callbacks. All credentials are synthetic.

### Focused checks after the last code edit

```sh
go test -race -count=1 ./host/internal/usecase/host/middleware ./host/internal/usecase/host/modelexecution ./host/internal/infra/providers/openai ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app ./sdk/plugins/extension/v1 ./pkg/plugins/extension/v1 -run 'Wire|Replay'
```

Exit 0. Middleware, model execution, shared OpenAI and SDK passed in 1.012s, 1.014s, 1.015s and 1.013s. Codex, compatible and app compiled with no matching unit tests; their wire tests are integration-tagged. Generated bindings reported no test files.

```sh
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./host/internal/infra/providers/openai ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app -run 'Wire|Received|Rejection|TransportFailure|DriverStreamCleanup|DriverStreamMixedCancellation|DriverStreamCancellation'
go tool golangci-lint run --config .golangci.yml --build-tags=integration ./host/internal/usecase/host/middleware ./host/internal/usecase/host/modelexecution ./host/internal/infra/providers/openai/... ./host/internal/app
```

Both exited 0. Integration packages passed in 1.020s, 1.052s, 1.052s and 8.186s. Coverage includes ordinary provider retry, acquired rejection/transport diagnostics, response-close retention, stream cleanup, cancellation, replay and the new security cases. Existing cleanup tests retain complete non-secret cleanup text and identity; a combined credential-echo-plus-cleanup-failure fixture was not added. Lint reported `0 issues.` Its initial `gocognit` and `golines` findings were corrected by placing final diagnostic completion on the capture transport and extracting a local test URL expression.

The same selected packages passed no-tag lint with `--new-from-patch` over tracked diffs and complete untracked additions for the correction's Go paths: exit 0, `0 issues.` This checks changed lines, not unrelated unit-build lines. The external reference passed focused integration-tagged lint (`0 issues.`) and `go test -race -count=1 ./cmd/wire` (`[no test files]`). Public app fixtures built and executed that command. Focused formatting passed. No full-project checks were run.

### Wording reconciliation and limits

D8-07 formerly said response observations redact `known private values`; the guide said `known private header values echoed in other headers`. The technical solution used `known private values` while also limiting error redaction to secrets. Treating these phrases as an error-text rule for all protected account values conflicts with AGENTS.md Architecture Decisions 3 and 4. The minimal clarification names authentication secrets and separates structured protected-header exclusion from diagnostic text. D8-07, the technical solution and the guide now retain ordinary account identifiers in errors. No broader filtering policy was implemented.

No live incident is asserted. No dependency/environment change, full-project check, commit or UNT-02 work occurred. The broader plan's full-check/reviewer sequence is not replaced by this explicitly scoped handoff. Earlier full-check results reported to reviewers were not independently recovered or rerun here. Correction review disposition and remaining historical RED gaps still require acceptance handling.

The code-quality review also identified unchanged `host/internal/infra/plugins/extension/runtime/runtime.go`, `Runtime.context`, which stores `context.Context` contrary to the Go guideline. Source inspection confirms the field; lifetime behavior and a runtime defect were not investigated. Consequence: MEDIUM maintenance/conformance risk, with no demonstrated runtime failure. This is outside UNT-01 corrections. Options are separate owner-approved investigation or an explicitly retained limitation; the maintainer owns that choice. This finding is reported without a code change.

### Exact correction paths

```text
api/plugins/extension/v1/middleware.proto
pkg/plugins/extension/v1/middleware.pb.go
host/internal/infra/providers/openai/wire.go
host/internal/infra/providers/openai/wire_diagnostics_test.go
host/internal/infra/providers/openai/wire_integration_test.go
host/internal/infra/providers/openai/wire_close_integration_test.go
host/internal/infra/providers/openai/wire_retention_integration_test.go
host/internal/infra/providers/openai/wire_preserve_integration_test.go
host/internal/infra/providers/openai/codex/provider.go
host/internal/infra/providers/openai/codex/wire.go
host/internal/infra/providers/openai/codex/wire_redaction_integration_test.go
host/internal/infra/providers/openai/compatible/chat.go
host/internal/infra/providers/openai/compatible/responses.go
host/internal/infra/providers/openai/compatible/service.go
host/internal/infra/providers/openai/compatible/wire_url_auth_integration_test.go
host/internal/infra/providers/openai/compatible/wire_url_transport_integration_test.go
host/internal/usecase/host/modelexecution/wire.go
host/internal/usecase/host/modelexecution/retry.go
host/internal/usecase/host/middleware/service.go
host/internal/app/wire_integration_test.go
host/internal/app/wire_retry_integration_test.go
host/internal/app/wire_redaction_integration_test.go
testdata/external-plugins/cmd/wire/main.go
docs/guidelines/provider-wire-middleware.md
docs/specs/features/initial/phases/08-prompt-context-input-provider-middleware/decisions.md
docs/specs/features/initial/phases/08-prompt-context-input-provider-middleware/technical-solution.md
docs/specs/features/initial/phases/08-prompt-context-input-provider-middleware/unt-01-evidence.md
```

## Historical failure evidence

The 2026-10-26 continuation read saved RED/GREEN outputs. The table below preserves its classifications for correction and fixture failures. The recovered historical transcript subsection adds genuine initial missing-behavior evidence supplied with the 2026-10-30 correction task. Recovery is from saved tool calls/outputs and preceding edits, not original temporary logs or fresh reruns.

| Evidence class | Test | Observed RED | Observed later GREEN |
| --- | --- | --- | --- |
| Original missing behavior | `TestWireHandlerAdmission` | New wire payload rejected by SDK admission; exact command above | SDK package passed |
| Correction | `TestReplayReferencesRestoreOrDiscard`, `TestReplayReferenceValidation` | Retained reference rejected as misplaced; expected a reference value but observed a private reasoning object | OpenAI package passed in 0.004s |
| Correction | `TestWireOwningCancellationKeepsCancellation` | Pure cancellation incorrectly categorized as `EXTENSION_FAILED` | Model-execution package passed in 0.004s |
| Correction | `TestWireRetainedFailureUsesSecretRedaction` | Retained failure still contained `token-private` alongside redacted causes | OpenAI package passed in 0.005s |
| Correction | `TestReplayReferenceValidation/object_instead_of_input_array` | Malformed replay slot accepted without error | OpenAI package passed in 0.004s |
| Correction | `TestWireHeaderValidationRejectsControls` | Header values containing bytes 0x01, 0x1f and 0x7f accepted | OpenAI package passed in 0.004s |
| Correction | `TestSDKWireProjection` | Public response header still contained `cookie-private` | OpenAI package passed in 0.004s |
| Fixture failure | `TestPublicWireCompatibleFamilies` | Both cases failed configuration: `providers must contain exactly one openai-codex instance` | App package passed in 0.435s after fixture correction |

The compatible-family configuration failure is not evidence of missing middleware behavior. Correction REDs establish defects in an existing implementation, not the original absence of the whole feature.

### Recovered historical transcript: initial missing behavior

These are recovered historical records, not fresh executions. Every listed invocation exited 1. Output excerpts and implementation states come from the saved transcript supplied with the correction task.

| Historical invocation | Exact failure excerpt | Implementation state before the next edit |
| --- | --- | --- |
| `go test ./host/internal/usecase/host/modelexecution -run '^TestWireFailureSurvivesSDKReduction$' -count=1` | `raw attempt must receive the neutral wire callbacks`; missing `MockWireMiddleware.NewWireCallbacks` | Callback types and Bind API existed; `Service.execute` had no wire binding. The next edit added binding. |
| `go test ./host/internal/usecase/host/middleware -run '^TestWire(Composition\|FailureStopsChain)$' -count=1` | Expected `originalB`, actual `original`; `invalid_action` and `handler_error`: expected error, got nil | `NewWireCallbacks` was an identity/no-op implementation. The next edit implemented composition. |
| `go test ./host/internal/usecase/host/middleware -run '^TestInvalidWireReplacementStopsLaterHandler$' -count=1` | Expected `bad`, actual `later` | Validation callback API existed; replacement did not invoke it. The next edit invoked validation. |
| `go test -tags=integration ./host/internal/infra/providers/openai -run '^TestSDKWireProjection$' -count=1` | `require.True(observed)` failed | The wire option returned `option.WithMiddleware()` without handlers. The next edit implemented the bridge. |
| `go test -tags=integration ./host/internal/infra/providers/openai/compatible -run '^TestWireCallbacksBothAPIs$' -count=1` | Both APIs: expected `wire-final`, actual empty; observer false | Adapters did not pass `request.Wire`. The next edit connected the bridge. |
| `go test -tags=integration ./host/internal/infra/providers/openai/codex -run '^TestCodexWireCallbacks$'` | Expected header/observer effects were absent | The wire option had not been inserted. The recovered task names this package and run filter; it does not establish additional command flags. |
| `go test -tags=integration ./host/internal/app -run '^TestPublicWireMiddleware$' -count=1` | `extension_count=0 issue_count=1`; expected `B`, actual empty header and nil body | The assemblies lacked `bindWireMiddleware`. The next edit added binding. |

Current focused GREEN covers these tests through the wire filters below. Initial RED pairs for response-close retention and some additional tests remain unavailable. This record does not claim complete historical RED coverage.

No deliberate negative control was executed in this continuation. The previously prepared disable script and negative-control script were reported as never run. They provide no executed evidence. No implementation was disabled to manufacture an original missing-behavior RED.

## Commands executed during focused completion on 2026-10-26

All commands below exited 0 unless explicitly marked otherwise. Tests used the declared toolchain and existing dependencies. No whole-project task was run.

### Unit tests after the last code edit

```sh
go test -race -count=1 ./sdk/plugins/extension/v1 ./host/internal/usecase/host/middleware ./host/internal/usecase/host/startup ./host/internal/usecase/host/extensionruntime ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/extensioncontext ./host/internal/infra/plugins/extension/runtime ./host/internal/infra/providers/openai ./host/internal/infra/providers/openai/codex ./host/internal/infra/providers/openai/compatible ./host/internal/app
```

All 11 packages passed. The last run reported durations of 1.012s to 1.082s. The unit suite includes ordered composition, stopping later handlers, invalid replacement, neutral provider substitution across ordinary/configured callers, retry preservation, SDK failure reduction, cancellation, replay and header validation.

### Integration tests

The complete integration suites of the affected packages passed:

```sh
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./sdk/plugins/extension/v1 ./host/internal/usecase/host/middleware ./host/internal/usecase/host/startup ./host/internal/usecase/host/extensionruntime ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/extensioncontext ./host/internal/infra/plugins/extension/runtime ./host/internal/infra/providers/openai/... ./host/internal/app
```

Results: SDK 5.061s; model execution 1.017s; extension context 1.030s; runtime 49.666s; shared OpenAI 1.019s; Codex 1.426s; compatible 1.244s; app 416.267s. Middleware, startup and extensionruntime have no integration-tagged test files. This run followed the three integration-test formatting corrections. Later edits formatted only three unit-test files excluded by the integration tag.

After the last code edit, the focused wire integration scenarios were rerun:

```sh
go test -race -tags=integration -p 1 -parallel 1 -count=1 ./sdk/plugins/extension/v1 ./host/internal/infra/plugins/extension/runtime ./host/internal/infra/providers/openai/... ./host/internal/app -run '^(TestPublicWire.*|TestSDKWire.*|TestWire.*|TestCodexWireCallbacks)$'
```

Shared OpenAI, Codex, compatible and app passed in 1.018s, 1.031s, 1.030s and 6.503s respectively. SDK and runtime reported no matching tests; their full integration suites passed in the preceding command. Public tests cover ordered original/current header/body transformation, persisted history separation, pre-dispatch failure, response failure, configured calls and supported modes, both compatible APIs and ordinary provider retry. Shared adapter tests cover real SDK projection, exact-byte preservation, invalid replacement before dispatch, cancellation reduction, redaction and response-close error retention.

### Focused lint and maintenance checks

```sh
go tool golangci-lint run --config .golangci.yml --build-tags=integration ./sdk/plugins/extension/v1 ./host/internal/usecase/host/middleware ./host/internal/usecase/host/startup ./host/internal/usecase/host/extensionruntime ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/extensioncontext ./host/internal/infra/plugins/extension/runtime ./host/internal/infra/providers/openai/... ./host/internal/app
```

Final result: `0 issues.` This uses the project lint task's integration build tag. The first fresh run found three `gofumpt` issues in `wire_compatible_integration_test.go`, `wire_close_integration_test.go` and `wire_preserve_integration_test.go`. The focused formatter corrected them:

```sh
go tool golangci-lint fmt --config .golangci.yml host/internal/app/wire_compatible_integration_test.go host/internal/infra/providers/openai/wire_close_integration_test.go host/internal/infra/providers/openai/wire_preserve_integration_test.go
```

An additional no-tag lint command used the same package list without `--build-tags=integration`. It exited 1 with 169 issues. After `go tool golangci-lint cache clean`, the same run reproduced 169 issues: dupl 9, errname 1, exhaustruct_v5 21, gofumpt 4, lll 80, misspell 2, paralleltest 2, testifylint 18, unconvert 4 and unused 28. This is not a passing unfiltered unit-build lint result.

Three new unit-test files had formatting findings, corrected with:

```sh
go tool golangci-lint fmt --config .golangci.yml host/internal/infra/providers/openai/wire_body_test.go host/internal/usecase/host/modelexecution/wire_cancellation_test.go host/internal/usecase/host/modelexecution/wire_protocol_test.go
```

A patch containing `git diff HEAD -- '*.go'` plus complete additions for every untracked Go file was supplied to no-tag lint through `--new-from-patch`. With the same focused package list, it reported `0 issues.` This checks diagnostics on changed lines, including the new unit files, and does not certify all pre-existing lines in the selected packages. The unfiltered no-tag check was not rerun after these formatting-only corrections.

```sh
go fix -diff ./sdk/plugins/extension/v1 ./host/internal/usecase/host/middleware ./host/internal/usecase/host/startup ./host/internal/usecase/host/extensionruntime ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/extensioncontext ./host/internal/infra/plugins/extension/runtime ./host/internal/infra/providers/openai/... ./host/internal/app
(cd testdata/external-plugins && go fix -diff ./cmd/wire)
go tool nilness ./sdk/plugins/extension/v1 ./host/internal/usecase/host/middleware ./host/internal/usecase/host/startup ./host/internal/usecase/host/extensionruntime ./host/internal/usecase/host/modelexecution ./host/internal/usecase/host/extensioncontext ./host/internal/infra/plugins/extension/runtime ./host/internal/infra/providers/openai/... ./host/internal/app
go tool ifaceguard -config ifaceguard.cfg ./host/internal/usecase/host/middleware
git diff --check
```

Both `go fix -diff` commands produced no proposals. Nilness produced no diagnostics. Ifaceguard reported `no errors found`. Whitespace checking passed. Ifaceguard was limited to the new middleware owner.

The external reference command was checked using the root module's declared linter binary:

```sh
lint=$(go tool -n golangci-lint)
cd testdata/external-plugins
"$lint" run --config ../../.golangci.yml --build-tags=integration ./cmd/wire
go test -race -count=1 ./cmd/wire
go test -race -tags=integration -count=1 ./cmd/wire
```

Lint reported `0 issues.` Both test commands compiled the command and reported `[no test files]`. Public app tests build and execute this extension as a separate process.

## Verification limits and handoff

- Whole-project formatting, fix analysis, lint, tests, integration tests, coverage and build were deliberately not run in this continuation. The pre-task baseline and its reported 84.8% coverage are historical context, not measurements of this implementation.
- The earlier completion awaited independent reviews. The correction task supplies later code-quality and behavior reports; the 2026-10-30 corrections still require reviewer/acceptor disposition. No full acceptance or complete UNT-01 plan exit is claimed.
- Recovered original missing-behavior evidence now includes the seven historical records above as well as SDK admission. Missing RED pairs for additional cases remain an acceptance-evidence limitation. Current GREEN does not reconstruct missing history.
- No-tag unfiltered lint remains a limitation. The project-configured integration-tag lint passes, and changed-line no-tag lint passes. The extra no-tag check also reports findings outside changed lines, including helpers used only by integration builds; these findings were not redesigned or suppressed.
- The generated raw-provider substitute tests the neutral callback boundary. No production PHS-12 extension-provider process is implemented or claimed.
- The LSP reference lookup for integration test symbols failed because its active build excludes integration-tagged files. Formatting changes did not rename symbols or alter behavior. Runtime tests and compiler checks supplied verification for those files.
- No dependency, environment, architecture or public-contract redesign was introduced during completion. All previous PHS-08 changes remain uncommitted.

### Additional no-tag lint finding

Scenario: a maintainer runs the selected packages through lint without the project's integration build tag. The check reproducibly fails, including diagnostics outside the task diff. Consequence: this optional check cannot serve as a clean package-wide unit-build lint gate; the configured integration lint and focused tests still pass. Severity: LOW for this handoff, with no runtime failure established by the lint output. The result was reproduced before and after cache clearing; the origin of every pre-existing-line finding was not investigated. Scope: outside changed-line corrections. Options are separate investigation or retaining the explicitly reported verification limit. The maintainer owns that choice. This assignment reports the finding and makes no unrelated cleanup or suppression.
