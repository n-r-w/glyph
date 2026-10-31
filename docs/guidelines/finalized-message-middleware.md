# Finalized-message middleware

Ordinary extensions can preserve or replace the content of a user message, a model response or a tool result before Host records it. The replacement becomes the message itself: Host stores it in the session, shows it in the client events that complete the message and sends it in later model requests. [Model-context middleware](context-middleware.md) differs: its changes apply to one model request, and the session keeps the original entries. Finalized-message handlers apply in headless, Programmatic and UI modes, and to configured-model requests.

## Register and handle operations

Register ordered `HandlerDescriptor` values with `HandlerKind` `HANDLER_KIND_FINALIZED_MESSAGE`. Its `HandleRequest` payload and its `HandleResponse` outcome are both `finalized_message`. One handler receives messages of all three roles; return `preserve: true` for a role that the handler does not change.

SDK admission, invocation order and the original/current semantics are the same as for [provider-wire handlers](provider-wire-middleware.md#register-and-handle-operations). The `FinalizedMessageInvocation` contains the immutable `original` message and the `current` message from preceding handlers. Both are `ModelContextEntry` values, the same entries as in [model-context history](context-middleware.md#history-entries-and-replay-references): a user message, a model response with replay references, or a tool result.

Return one `FinalizedMessageAction`:

| Action | Effect |
| --- | --- |
| `preserve: true` | Keeps the current message |
| `replace` | Replaces the current message with a complete new value of the same role, derived from `original`, `current` or neither |

A missing action or `preserve: false` is an invalid action. Each role has its own replacement rules:

| Role | A replacement must | Host keeps |
| --- | --- | --- |
| User message | Contain at least one block; each text block is nonempty, and each image has a media type and data | Nothing else; the replacement is the complete message |
| Model response | Contain the tool calls of the original in the same order, with the same call identifiers and names; only their arguments can change | Outcome, error message, provider, configured model, reported model, response identifier, usage and diagnostics of the original |
| Tool result | Keep the call identifier and the tool name of the original; each image has a media type and data | Nothing else; the content and the error flag come from the replacement |

A replacement of another role, a changed tool call, or an invalid reference is an invalid action. Replay references follow the [model-context rules](context-middleware.md#history-entries-and-replay-references): keep a reference to keep the private provider replay of its block, and remove it to discard that replay.

For example, a handler can append ` [reviewed]` to each text block of a model response. The session then stores the marked text, the client events `message_end` and `turn_end` show it, and the next model request contains it. The provider identity and the token usage of the response stay unchanged.

The public schema is [`middleware.proto`](../../api/plugins/extension/v1/middleware.proto).

## When handlers run

Host runs the chain once for each message, immediately before it records the message:

| Message | Moment |
| --- | --- |
| User message | After the [user-input handlers](input-middleware.md) and before the final check against the selected model. Fully handled input reaches no finalized-message handler. |
| Model response | After the provider completes the response and before `message_end`. A response retained after a provider failure is finalized too; a response aborted by cancellation is recorded without finalization. |
| Tool result | After the tool completes and before the client events `tool_execution_end` and `tool_result`. Results that Host generates for tool calls of a length-limited response are finalized too. |
| Configured-model response | After the provider completes the response and before Host returns it to the extension or to the branch summary that requested it. Host stores no session entry for it. |

Agent Core executes the tool calls of the final model response, so a handler can change the arguments of a call before the tool runs.

Programmatic and UI clients receive the streamed events of a model message as the provider sends them, before finalization: content start, text delta and content end, and tool-call start, delta and end, including the arguments of `tool_call_end`. Treat these values as provisional. The client events `message_end`, `tool_execution_start`, `tool_execution_end`, `tool_result` and `turn_end`, the session and later model requests carry the final value.

When an extension registers a finalized-message handler, one-shot mode writes no streamed text; it writes the final text of each model message when the message ends. Without such a handler, one-shot mode streams model text as it arrives.

Finalization of a model response or a tool result starts when Host admits the first finalized-message handler. When the run is canceled before that admission, Host starts no handler and records the value unchanged. When cancellation arrives after a handler was admitted, the chain fails, Host records nothing for that message, and the operation is canceled. When cancellation arrives after the chain succeeded, Host records the final value. A user message or a configured-model response whose chain is canceled before or during its handlers fails with the cancellation.

After the user message chain, Host checks the final user message against the input types of the selected model. An image that a handler adds to a message for a model without image input fails with `INPUT_INVALID`, for example `process user input: model gpt-active does not accept image input at content 1`.

## Failures

A handler error, runtime loss or invalid action stops the chain. No later handler runs, and Host does not run the chain again for that message. Host reports `EXTENSION_FAILED` with the complete error text, which names the handler; one-shot mode shows the category before the text. The request ends without recording the message, and Host emits no client event that completes it.

Earlier session entries remain, and a tool that already ran is not undone. After a failed tool-result chain, the session keeps the model response and its tool call but no result for the failed call. A later user request starts new chains.

When the provider failed and finalization of the retained response fails too, the request reports `EXTENSION_FAILED`, and its text contains the complete handler failure and the complete provider failure. A failed configured-model finalization fails the configured request with `EXTENSION_FAILED`.

## Public-only reference

[`testdata/external-plugins/cmd/finalized`](../../testdata/external-plugins/cmd/finalized/README.md) builds a reference extension using only public contracts and the SDK. It marks the text of every user message, model response and tool result and provides one tool whose result it marks.
