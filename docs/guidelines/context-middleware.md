# System-instruction and model-context middleware

Ordinary extensions can replace the system instructions and the history of a model request. Each change applies only to that model request; session entries keep their original content. These operations apply to ordinary agent requests and configured-model requests in headless, Programmatic and UI modes. To change the stored content of a message, use [finalized-message middleware](finalized-message-middleware.md).

## Register and handle operations

Register ordered `HandlerDescriptor` values using these `HandlerKind` values:

| Kind | `HandleRequest` payload | `HandleResponse` outcome |
| --- | --- | --- |
| `HANDLER_KIND_SYSTEM_INSTRUCTIONS` | `system_instructions` | `system_instructions` |
| `HANDLER_KIND_MODEL_CONTEXT` | `model_context` | `model_context` |

SDK admission, invocation order and the original/current semantics are the same as for [provider-wire handlers](provider-wire-middleware.md#register-and-handle-operations). All instruction handlers finish before history handlers begin. Return `preserve: true` to keep the current value, or `replace` with the complete new value. A missing action or `preserve: false` is an invalid action. An instruction replacement can be any string, including an empty one, and a history replacement can contain any number of entries.

The public schema is [`middleware.proto`](../../api/plugins/extension/v1/middleware.proto).

## History entries and replay references

A `ModelContextHistory` lists `ModelContextEntry` values in request order. Each entry contains a user message, a model response or a tool result. User messages and tool results use the session-tree messages `SessionTreeUserMessage` and `SessionTreeToolResult`. A model response is a `ReplayableModelResponse`: each block wraps one `SessionTreeModelContent` and can carry a `replay_reference`.

Host gives a replay reference to every model block that owns private provider replay, such as encrypted reasoning. The reference is valid only for the current invocation and contains no provider data. The same block has a different reference in `original` and in `current`. Keep the reference on its block to keep the private replay, and remove it to send the block without replay. A handler can restore a reference from `original` even when an earlier handler removed it. A replacement fails as an invalid action when a reference is unknown or altered, appears twice, or is placed on a block of another content kind.

The history contains only model-visible content. A replaced model response keeps its content and restored replay; Host sends it with outcome `stop` and without provider, model, usage or response identifiers.

For example, handler A can append a user message `A` to `current`. Handler B can then replace the history with `original` plus a user message `B`. The provider receives the original history and `B`, and the session keeps neither message.

## When handlers run

Host runs both chains once for each prepared request revision, after conversation preparation, including automatic compaction. Handlers therefore receive the prepared history as `original`. The compaction policy observes the prepared request, not the transformed one.

Provider retries after a transient failure send the same transformed values; instruction and history handlers do not run again. Provider-wire handlers still run for each attempt. After a context overflow, the compaction policy can replace the session history. Host then runs both chains again with the same registrations, the recovered history and the original instructions as `original`.

Configured-model requests run both chains on the instructions and history that the caller supplied. They do not start conversation preparation or automatic compaction.

## Failures

A handler error, runtime loss or invalid action stops the chain before the provider request of that revision. Host reports `EXTENSION_FAILED` with the complete error text. No later handler runs, and Host does not repeat the chain or send the request. The next model request runs both chains again.

After both chains succeed, Host checks the final history against the input types of the selected model. A handler may add an image that a later handler removes; only the final history counts. If the final history contains an image that the model does not accept and a history handler replaced the history, Host reports `EXTENSION_FAILED`. If no handler replaced it, Host reports the plain validation error, for example `model gpt-active does not accept image input at message 0 content 1`. In both cases the request is not sent. The check also applies without context handlers: a conversation whose history holds user images fails before dispatch, with the plain validation error, when the selected model declares no image input.

## Public-only reference

[`testdata/external-plugins/cmd/context`](../../testdata/external-plugins/cmd/context/README.md) builds a reference extension using only public contracts and the SDK. It appends embedded project instructions to the system instructions and a marker message to the history.
