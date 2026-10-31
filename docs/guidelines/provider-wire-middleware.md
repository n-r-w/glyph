# Provider-wire middleware

Ordinary extensions can transform public provider request headers, replace safe serialized request JSON, and observe response status and public headers. These operations apply to ordinary agent requests and configured-model requests. The [overview](model-middleware.md) describes the handler contract, the actions, registration order, invocation order and the failure categories that all extension points share.

## Payloads

A header invocation contains `WireHeaders`: a map from a case-insensitive header name to the ordered values of that header. A header replacement is a complete `WireHeaders` value. A body invocation contains the safe serialized request JSON as bytes, and a body replacement is complete safe JSON. A response invocation contains the integer `status` and the public response `headers`.

## Safe headers and serialized JSON

Public headers exclude authentication, cookies, proxy authentication and provider account identity. Host redacts known authentication secrets echoed in other headers. Replacements may add ordinary custom headers, but attempts to write protected headers fail before dispatch. URLs, HTTP clients and credentials are outside the payload.

A body handler receives safe JSON, not an authenticated raw request. The built-in adapters replace private Responses reasoning items and OpenRouter Chat replay collections with replay references of this form:

```json
{"$glyph_replay":"opaque invocation-local value"}
```

References are valid only in their provider replay slots: Responses input-array items or an assistant Chat message's `reasoning_details` field. Removing preceding visible array items does not invalidate a retained reference. Reference identifiers contain no provider reasoning identifier or opaque reasoning bytes. The [overview](model-middleware.md#replay-references) states how to keep, remove and restore a reference.

When every body handler preserves, the adapter sends the original serialized bytes unchanged, including private replay. When a handler replaces the safe JSON, the adapter restores each retained fragment with its exact original private bytes. Wire changes affect only that provider attempt; Host does not append request-local markers to session history.

## Failure and retry behavior

The error text of a failed wire handler contains the complete acquired non-secret error and ordinary account identifiers. Host retains acquired middleware causes independently of SDK error reduction, so an SDK error that reports only cancellation does not hide them.

If a response observer fails, the adapter closes the response body and retains any close error. Codex also retains rejection status and body acquired by its diagnostic transport. Compatible adapters preserve observer and cleanup causes; observer failure occurs before SDK rejection decoding, so the returned error does not automatically include rejection status or body. The observer receives status through its invocation.

Successful response observers leave ordinary provider classification and retry policy intact. Each built-in adapter retains acquired authentication secrets through stream decoding, cleanup, cancellation and final diagnostic delivery. The cookie collector extracts credential values instead of complete headers or Set-Cookie attributes; diagnostic masking replaces those strings wherever they occur. Provider stream failures remain provider failures.

Wire handlers run again for each genuine raw provider attempt admitted by the ordinary provider retry policy. They receive fresh original/current wire values for that attempt.

## Provider independence

Host attaches wire handlers through a provider-neutral callback protocol, so they apply to any raw provider implementation. Installing a provider through an extension belongs to [PHS-12](../specs/features/initial/phases/12-extension-defined-providers/ticket.md) and is not available yet.
