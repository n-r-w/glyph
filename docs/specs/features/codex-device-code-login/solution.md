# Technical solution: Codex device-code sign-in

## Problem statement

See [problem statement](problem.md) and [requirements](prd.md).

## Proposed solution

### Ownership and contracts

`host/internal/domain/authentication` defines the provider-neutral method and displayed challenge. `RetryAuthenticationCommand.method` requires an explicit browser or device-code choice. Host rejects missing and unknown methods instead of selecting a default on behalf of the user.

The Codex adapter owns both protocol flows. Host runs the selected flow inside the existing authentication operation. `AuthorizationRequest` carries its URL and an optional `user_code`; the standard TUI decodes and displays those values without owning provider HTTP calls.

The TUI interaction owner opens the method selector before emitting the authentication command. Authentication occupies the foreground operation slot so the ordinary stop command targets it. Availability transitions clear expired or completed challenge data. Codes stay out of the conversation transcript and persistent session history.

### Provider protocol

The protocol follows OpenAI's [Codex device-code implementation](https://github.com/openai/codex/blob/96aca987f72e29a639ecabe5b76ccd6e5693fc88/codex-rs/login/src/device_code_auth.rs):

1. Request a code from `/api/accounts/deviceauth/usercode` using the registered client ID.
2. Display the provider's `/codex/device` page and returned user code.
3. Poll `/api/accounts/deviceauth/token` with the device authorization ID and user code. HTTP 403 and 404 represent pending approval. Subsequent polls use the returned interval.
4. Exchange the approved authorization code and PKCE verifier at the existing OAuth token endpoint with `/deviceauth/callback` as the redirect identifier.
5. Validate and save credentials through the same token-persistence path used by browser sign-in.

The device flow creates no callback listener and does not launch a browser on the server. Its context bounds the attempt to 15 minutes. Cancellation interrupts requests and pending delays. Non-pending provider failures retain status and body text; transport, decoding, presentation, exchange, and persistence failures retain their original causes.

### Verification

- `device_auth_test.go` covers code requests, pending statuses and intervals, token exchange, credential persistence, cancellation, expiration, incomplete approval, and provider/UI failures. The first run failed against a compile-only device-flow stub before implementation.
- `device_auth_integration_test.go` exercises the provider against real local HTTP endpoints and checks that saved credentials pass the normal credential check. Existing browser-flow integration tests continue to cover PKCE, callback validation, cleanup, and token failures.
- Host command and progress tests cover explicit method selection and code preservation. TUI tests cover the selector, code projection, stale-challenge cleanup, terminal display, and foreground cancellation.
- `TestRuntimeSelectsDeviceCodeThroughTerminal` drives `Ctrl+R`, Down, and Enter through the real Bubble Tea decoder and runtime. Ten uncached race runs passed.
- The Darwin arm64 PTY acceptance scenario now confirms the browser row before awaiting authentication completion. This platform-specific scenario cannot execute on the Linux verification host.
- Final Linux checks passed: `task fmt`, `task fix_dry_run` with no proposals, `task lint`, `task test`, `task itest`, `task test-coverage`, and `task build`. Combined coverage is 84.5%, above the 80.0% threshold. Two complete `task generate` runs produced identical generated-file hashes.
- Supplemental unit-tag lint on the affected packages retains 27 findings in pre-existing test code. All 27 diagnostic source snippets occur in `HEAD`; none concerns the new authentication implementation. This supplemental check is not reported as passing.

Live authentication against an OpenAI account is not performed by the automated tests. The user completes the provider confirmation in their browser.

## Overengineering and overspecification considerations

The implementation reuses the asynchronous UI operation lifecycle, OAuth client, credential store, refresh logic, and terminal selector pattern. It adds no dependencies or remote callback workaround.

## Open questions

None for the implementation. Live account authorization and the Darwin arm64 PTY scenario remain environment-specific checks.

## References

- [Sign-in instructions](../../../guidelines/authentication.md)
- [Public UI authentication contract](../../../../api/plugins/ui/v1/model.proto)
- [Codex provider implementation](../../../../host/internal/infra/providers/openai/codex/device_auth.go)
