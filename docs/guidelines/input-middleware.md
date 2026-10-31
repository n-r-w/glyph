# User-input middleware

Ordinary extensions can preserve, replace or fully handle the user input of an agent run before Host starts the run. The handlers apply to user requests. Agent Core appends the final input to the session, and [system-instruction and model-context middleware](context-middleware.md) later sees it as part of the history. The [overview](model-middleware.md) describes the handler contract, the actions, registration order, invocation order and the failure categories that all extension points share.

## Input values

The `UserInputInvocation` contains the immutable `original` input that the client submitted and the `current` input from preceding handlers. Both are `SessionTreeUserMessage` values: ordered text and image blocks, as in session history. A replacement must contain at least one block. Each text block must be nonempty, and each image must have a media type and data.

For example, handler A can append ` A` to each text block of `current`. Handler B then receives the submitted input as `original` and A's result as `current`. If B replaces each image block with a text block, a model without image input accepts the final input.

## When handlers run

Host runs the chain once for each accepted user request. The chain runs after the request is admitted and before the agent run starts, while Host holds the run reservation of this request, so no other run is active. Handlers run before any agent event, session append or model request of that run. When a handler fails, Host starts no agent run and sends no model request.

When a handler fully handles the input, the client operation completes successfully. Host starts no agent run, so it appends no session entry, sends no model request and emits no agent events. Host adds no message to the session or the client output. A Programmatic or UI client can submit its next request at once, and a one-shot invocation exits successfully.

## Final validation

After all handlers succeed without handling the input, Host checks the final input against the input types of the selected model. If the final input contains an image and the model declares no image input, Host reports `INPUT_INVALID` with the complete text, for example `process user input: model gpt-active does not accept image input at content 1`. Content indexes start at 0, so `content 1` is the second block. Only the final input counts: an image that a handler removes does not fail the request. The same rule applies to an image that a finalized-message handler adds to the user message.
