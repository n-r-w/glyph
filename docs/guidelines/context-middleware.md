# System-instruction and model-context middleware

Ordinary extensions can replace the system instructions and the history of a model request. Each change applies only to that model request; session entries keep their original content. These operations apply to ordinary agent requests and configured-model requests. To change the stored content of a message, use [finalized-message middleware](finalized-message-middleware.md). The [overview](model-middleware.md) describes the handler contract, the actions, registration order, invocation order and the failure categories that all extension points share.

## History entries and replay references

A `ModelContextHistory` lists `ModelContextEntry` values in request order. Each entry contains a user message, a model response or a tool result. User messages and tool results use the session-tree messages `SessionTreeUserMessage` and `SessionTreeToolResult`. A model response is a `ReplayableModelResponse`: each block wraps one `SessionTreeModelContent` and can carry a `replay_reference`.

Host gives a replay reference to every model block that owns private provider replay, such as encrypted reasoning. The same block has a different reference in `original` and in `current`. The [overview](model-middleware.md#replay-references) states how to keep, remove and restore a reference.

## Replacement values

An instruction replacement can be any string, including an empty one, and a history replacement can contain any number of entries. The history contains only model-visible content. A replaced model response keeps its content and restored replay; Host sends it with outcome `stop` and without provider, model, usage or response identifiers.

For example, handler A can append a user message `A` to `current`. Handler B can then replace the history with `original` plus a user message `B`. The provider receives the original history and `B`, and the session keeps neither message.

## When handlers run

Host runs both chains once for each prepared request revision, after conversation preparation, including automatic compaction. Handlers therefore receive the prepared history as `original`. The compaction policy observes the prepared request, not the transformed one.

Provider retries after a transient failure send the same transformed values; instruction and history handlers do not run again. After a context overflow, the compaction policy can replace the session history. Host then runs both chains again with the same registrations, the recovered history and the original instructions as `original`.

A configured-model request runs both chains on the instructions and history that the caller supplied; see the [overview](model-middleware.md#invocation-order-in-a-model-request). It starts no automatic compaction.

## Final history check

After both chains succeed, Host checks the final history against the input types of the selected model. A handler may add an image that a later handler removes; only the final history counts. If the final history contains an image that the model does not accept and a history handler replaced the history, Host reports `EXTENSION_FAILED`. If no handler replaced it, Host reports the plain validation error, for example `model gpt-active does not accept image input at message 0 content 1`. In both cases the request is not sent. The check also applies without context handlers: a conversation whose history holds user images fails before dispatch, with the plain validation error, when the selected model declares no image input.
