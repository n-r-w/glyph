# Finalized-message middleware

Ordinary extensions can preserve or replace the content of a user message, a model response or a tool result before Agent Core appends it to the session. The replacement becomes the message itself: Agent Core appends it, the client events that complete the message show it, and later model requests contain it. [Model-context middleware](context-middleware.md) differs: its changes apply to one model request, and the session keeps the original entries. Finalized-message handlers also apply to configured-model requests. Client event names in this guide are the suffixes of the `AGENT_EVENT_TYPE_` values of Programmatic Control and of the `LIFECYCLE_TYPE_` values of the UI Plugin Contract. The [overview](model-middleware.md) describes the handler contract, the actions, registration order, invocation order and the failure categories that all extension points share.

## Messages and replacements

One handler receives messages of all three roles; return `preserve: true` for a role that the handler does not change. The `FinalizedMessageInvocation` contains the immutable `original` message and the `current` message from preceding handlers. Both are `ModelContextEntry` values, the same entries as in [model-context history](context-middleware.md#history-entries-and-replay-references): a user message, a model response with replay references, or a tool result.

A replacement has the role of the original message. Each role has its own replacement rules:

| Role | A replacement must | Host keeps |
| --- | --- | --- |
| User message | Contain at least one block; each text block is nonempty, and each image has a media type and data | Nothing else; the replacement is the complete message |
| Model response | Contain the tool calls of the original in the same order, with the same call identifiers and names; only their arguments can change | Outcome, error message, provider, configured model, reported model, response identifier, usage and diagnostics of the original |
| Tool result | Keep the call identifier and the tool name of the original; each image has a media type and data | Nothing else; the content and the error flag come from the replacement |

A replacement of another role, a changed tool call, or an invalid reference is an invalid action.

For example, a handler can append ` [reviewed]` to each text block of a model response. The session then stores the marked text, the client events `MESSAGE_END` and `TURN_END` show it, and the next model request contains it. The provider identity and the token usage of the response stay unchanged.

## When handlers run

Host runs the chain once for each message, immediately before it records the message:

| Message | Moment |
| --- | --- |
| User message | After the [user-input handlers](input-middleware.md) and before the final check against the selected model. |
| Model response | After the provider completes the response and before `MESSAGE_END`. A response retained after a provider failure is finalized too; a response aborted by cancellation is recorded without finalization. |
| Tool result | After the tool completes and before the client events `TOOL_EXECUTION_END` and `TOOL_RESULT`. Results that Host generates for tool calls of a length-limited response are finalized too. |
| Configured-model response | After the provider completes the response and before Host returns it to the extension or to the branch summary that requested it. Host stores no session entry for it. |

Agent Core executes the tool calls of the final model response, so a handler can change the arguments of a call before the tool runs.

Programmatic and UI clients receive the streamed events of a model message as the provider sends them, before finalization: `MODEL_CONTENT_START`, `MODEL_TEXT_DELTA` and `MODEL_CONTENT_END`, and `TOOL_CALL_START`, `TOOL_CALL_DELTA` and `TOOL_CALL_END`, including the arguments of `TOOL_CALL_END`. Treat these values as provisional. The events `MESSAGE_END`, `TOOL_EXECUTION_START`, `TOOL_EXECUTION_END`, `TOOL_RESULT` and `TURN_END`, the session and later model requests carry the final value.

When an extension registers a finalized-message handler, one-shot mode writes no streamed text; it writes the final text of each model message when the message ends. Without such a handler, one-shot mode streams model text as it arrives.

Finalization of a model response or a tool result starts when Host admits the first finalized-message handler. When the run is canceled before that admission, Host starts no handler and records the value unchanged. When pure cancellation of the owning context interrupts the chain after a handler was admitted, the chain fails without the category `EXTENSION_FAILED`, Agent Core appends nothing for that message, and the operation is canceled. A handler failure that Host acquired keeps `EXTENSION_FAILED` even when cancellation arrives too, as the [overview](model-middleware.md#failures) states. When cancellation arrives after the chain succeeded, Host records the final value. A user message or a configured-model response whose chain is canceled before or during its handlers fails with the cancellation.

An image that a finalized-message handler adds to a user message is checked by the [final validation](input-middleware.md#final-validation) of user input.

## Failures

A failed chain ends the request without recording the message, and Host emits no client event that completes it.

Earlier session entries remain, and a tool that already ran is not undone. After a failed tool-result chain, the session keeps the model response and its tool call but no result for the failed call.

When the provider failed and a handler failure also fails the finalization of the retained response, the request reports `EXTENSION_FAILED`, and its text contains the complete handler failure and the complete provider failure. When pure cancellation interrupts that finalization instead, the run is aborted, Agent Core appends no response, and the error text still contains both causes. A failed configured-model finalization fails the configured request with `EXTENSION_FAILED`, except pure cancellation, which keeps no category.
