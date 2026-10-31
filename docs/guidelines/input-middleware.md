# User-input middleware

Ordinary extensions can preserve, replace or fully handle the user input of an agent run before Host starts the run. The handlers apply to user requests in headless, Programmatic and UI modes. Host appends the final input to the session, and [system-instruction and model-context middleware](context-middleware.md) later sees it as part of the history.

## Register and handle operations

Register ordered `HandlerDescriptor` values with `HandlerKind` `HANDLER_KIND_USER_INPUT`. Its `HandleRequest` payload and its `HandleResponse` outcome are both `user_input`.

SDK admission, invocation order and the original/current semantics are the same as for [provider-wire handlers](provider-wire-middleware.md#register-and-handle-operations). The `UserInputInvocation` contains the immutable `original` input that the client submitted and the `current` input from preceding handlers. Both are `SessionTreeUserMessage` values: ordered text and image blocks, as in session history.

Return one `UserInputAction`:

| Action | Effect |
| --- | --- |
| `preserve: true` | Keeps the current input |
| `replace` | Replaces the current input with a complete new value, derived from `original`, `current` or neither |
| `handle: true` | Completes the request successfully and skips all later input handlers |

A missing action, `preserve: false` or `handle: false` is an invalid action. A replacement must contain at least one block. Each text block must be nonempty, and each image must have a media type and data.

For example, handler A can append ` A` to each text block of `current`. Handler B then receives the submitted input as `original` and A's result as `current`. If B replaces each image block with a text block, a model without image input accepts the final input.

The public schema is [`middleware.proto`](../../api/plugins/extension/v1/middleware.proto).

## When handlers run

Host runs the chain once for each accepted user request. The chain runs after the request is admitted and before the agent run starts, while Host holds the run reservation of this request, so no other run is active. Handlers run before any agent event, session append or model request of that run.

When a handler fully handles the input, the client operation completes successfully. Host starts no agent run, so it appends no session entry, sends no model request and emits no agent events. Host adds no message to the session or the client output. A Programmatic or UI client can submit its next request at once, and a one-shot invocation exits successfully.

## Final validation

After all handlers succeed without handling the input, Host checks the final input against the input types of the selected model. If the final input contains an image and the model declares no image input, Host reports `INPUT_INVALID` with the complete text, for example `process user input: model gpt-active does not accept image input at content 1`. Content indexes start at 0, so `content 1` is the second block. Only the final input counts: an image that a handler removes does not fail the request.

## Failures

A handler error, runtime loss, invalid action or invalid replacement stops the chain. Host reports `EXTENSION_FAILED` with the complete error text, which names the handler. One-shot mode shows the category before the text, for example `[error] EXTENSION_FAILED: run headless agent: process user input: middleware handler "input-text": reference input handler failed`. No later handler runs, the agent run does not start, and no model request is sent. The next request runs the chain again. When the client cancels the operation while a handler runs, the operation is canceled.

## Public-only reference

[`testdata/external-plugins/cmd/input`](../../testdata/external-plugins/cmd/input/README.md) builds a reference extension using only public contracts and the SDK. It transforms text, removes images and fully handles a request whose text contains `glyph-handle-input`.
