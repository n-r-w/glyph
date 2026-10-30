# Provider-wire middleware

Ordinary extensions can transform public provider request headers, replace safe serialized request JSON, and observe response status and public headers. These operations apply to ordinary agent requests and configured-model requests in headless, Programmatic and UI modes.

## Register and handle operations

Register ordered `HandlerDescriptor` values using these `HandlerKind` values:

| Kind | `HandleRequest` payload | `HandleResponse` outcome |
| --- | --- | --- |
| `HANDLER_KIND_PROVIDER_HEADERS` | `provider_headers` | `provider_headers` |
| `HANDLER_KIND_PROVIDER_BODY` | `provider_body` | `provider_body` |
| `HANDLER_KIND_PROVIDER_RESPONSE` | `provider_response` | `provider_response` |

The SDK checks handler identity and the declared payload before admitting work. Implement the ordinary `PrepareHandle` and `HandleOperation.Run` API. Each invocation has the ordinary issued extension context. Host invokes accepted extensions in registration order and retains each extension's descriptor order within an operation kind. All header handlers finish before body handlers begin. Response observers run before SDK error decoding or stream consumption.

Header and body invocations contain an immutable `original` value and the `current` value from preceding handlers. Return `preserve: true` to retain the current value or select `replace` to replace the complete value. For example, handler A can append `A`, then handler B can discard A's result by replacing from `original` plus `B`. Replacement validation completes before the next handler runs. A response observer returns `acknowledge: true`; it cannot replace the response.

The public schema is [`middleware.proto`](../../api/plugins/extension/v1/middleware.proto). Generated Go messages use the opaque protobuf API.

## Safe headers and serialized JSON

Public headers exclude authentication, cookies, proxy authentication and provider account identity. Host redacts known authentication secrets echoed in other headers. Replacements may add ordinary custom headers, but attempts to write protected headers fail before dispatch. URLs, HTTP clients and credentials are outside the payload.

A body handler receives safe JSON, not an authenticated raw request. The built-in adapters replace private Responses reasoning items and OpenRouter Chat replay collections with replay references of this form:

```json
{"$glyph_replay":"opaque invocation-local value"}
```

Keep the complete reference object to retain its private fragment. Remove the reference to discard that fragment. A later handler may restore a reference from `original` even if an earlier handler removed it. References are valid only in their provider replay slots: Responses input-array items or an assistant Chat message's `reasoning_details` field. Removing preceding visible array items does not invalidate a retained reference. Unknown, altered, duplicated or misplaced references fail before dispatch. Reference identifiers contain no provider reasoning identifier or opaque reasoning bytes.

When every body handler preserves, the adapter sends the original serialized bytes unchanged, including private replay. When a handler replaces the safe JSON, the adapter restores each retained fragment with its exact original private bytes. Wire changes affect only that provider attempt; Host does not append request-local markers to session history.

## Failure and retry behavior

An ordinary handler error, runtime loss, invalid action or invalid replacement stops the chain. Host reports `EXTENSION_FAILED` with complete acquired non-secret error text and ordinary account identifiers. No later handler runs, and Host does not retry the handler or start provider retry or overflow recovery to recover from that failure. A pre-dispatch failure sends no provider request.

If a response observer fails, the adapter closes the response body and retains any close error. Codex also retains rejection status and body acquired by its diagnostic transport. Compatible adapters preserve observer and cleanup causes; observer failure occurs before SDK rejection decoding, so the returned error does not automatically include rejection status or body. The observer receives status through its invocation. Host retains acquired middleware causes independently of SDK error reduction, including concurrent cancellation. Pure owning cancellation keeps cancellation semantics.

Successful response observers leave ordinary provider classification and retry policy intact. Each built-in adapter retains acquired authentication secrets through stream decoding, cleanup, cancellation and final diagnostic delivery. The cookie collector extracts credential values instead of complete headers or Set-Cookie attributes; diagnostic masking replaces those strings wherever they occur. Provider stream failures remain provider failures.

Wire handlers run again for each genuine raw provider attempt admitted by the ordinary provider retry policy. They receive fresh original/current wire values for that attempt.

## Public-only reference

[`testdata/external-plugins/cmd/wire`](../../testdata/external-plugins/cmd/wire/README.md) builds a reference extension using only public contracts and the SDK. The reference registers two header transformers, two safe-body transformers and two response observers.

The [UNT-01 evidence](../specs/features/initial/phases/08-prompt-context-input-provider-middleware/unt-01-evidence.md) records focused behavioral checks. Extension-defined provider installation remains a PHS-12 responsibility; neutral raw-attempt substitutes verify provider independence without claiming a production process-provider registration implementation.
