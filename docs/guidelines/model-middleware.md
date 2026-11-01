# Model-facing middleware

Ordinary extensions change what a model request contains, what Host sends to the provider, which messages Agent Core appends to the session and whether a tool call runs. Each change goes through an extension point, a documented boundary at which a handler can change an operation. The extension registers a handler for the point, and Host invokes it. Eight extension points exist. They work in one-shot (headless), Programmatic and UI modes. This guide holds what they share. Each detailed guide holds what is specific to its extension points.

## Extension points

| Extension point | Handler kind | Payload and outcome field | Allowed actions | Guide | Reference command |
| --- | --- | --- | --- | --- | --- |
| System instructions | `HANDLER_KIND_SYSTEM_INSTRUCTIONS` | `system_instructions` | preserve, replace | [Context](context-middleware.md) | [`context`](../../testdata/external-plugins/cmd/context/README.md) |
| Request-local context | `HANDLER_KIND_MODEL_CONTEXT` | `model_context` | preserve, replace | [Context](context-middleware.md) | [`context`](../../testdata/external-plugins/cmd/context/README.md) |
| User input | `HANDLER_KIND_USER_INPUT` | `user_input` | preserve, replace, handle | [User input](input-middleware.md) | [`input`](../../testdata/external-plugins/cmd/input/README.md) |
| Finalized messages | `HANDLER_KIND_FINALIZED_MESSAGE` | `finalized_message` | preserve, replace | [Finalized messages](finalized-message-middleware.md) | [`finalized`](../../testdata/external-plugins/cmd/finalized/README.md) |
| Tool calls | `HANDLER_KIND_TOOL_POLICY` | `tool_policy` | allow, replace the input, reject | [Tool calls](#tool-calls) | [`toolpolicy`](../../testdata/external-plugins/cmd/toolpolicy/README.md) |
| Provider headers | `HANDLER_KIND_PROVIDER_HEADERS` | `provider_headers` | preserve, replace | [Provider wire](provider-wire-middleware.md) | [`wire`](../../testdata/external-plugins/cmd/wire/README.md) |
| Serialized provider request | `HANDLER_KIND_PROVIDER_BODY` | `provider_body` | preserve, replace | [Provider wire](provider-wire-middleware.md) | [`wire`](../../testdata/external-plugins/cmd/wire/README.md) |
| Provider response | `HANDLER_KIND_PROVIDER_RESPONSE` | `provider_response` | acknowledge | [Provider wire](provider-wire-middleware.md) | [`wire`](../../testdata/external-plugins/cmd/wire/README.md) |

Generated Go messages use the opaque protobuf API. The public schema is [`middleware.proto`](../../api/plugins/extension/v1/middleware.proto) for the invocation and action messages, and [`session_tree.proto`](../../api/plugins/extension/v1/session_tree.proto) for the `HandleRequest` and `HandleResponse` envelopes. A `HandleRequest` carries the payload field named in the table, and the `HandleResponse` of the handler carries the outcome field of the same name.

## Handler contract

An extension lists one `HandlerDescriptor` with an identifier and a kind for each handler in its registration response. The SDK admits an invocation only when its `HandleRequest` carries the payload that the registered kind declares. The extension implements the ordinary `PrepareHandle` method and the `HandleOperation.Run` method of the SDK `Service`. Each invocation has the ordinary issued extension context.

A handler returns one action:

| Action | Field | Effect |
| --- | --- | --- |
| Preserve | `preserve: true` | Keeps the current value |
| Replace | `replace` | Replaces the current value with a complete new value, derived from the original value, the current value or neither |
| Handle | `handle: true` | Completes a user request successfully and skips all later user-input handlers |
| Acknowledge | `acknowledge: true` | Reports that the handler observed the provider response, which it cannot change |
| Allow, reject | `allow: true`, `reject_reason` | Tool-call handlers only: allow keeps the current call, and reject refuses it with a reason; see [Tool calls](#tool-calls) |

A missing action or a `false` value is an invalid action.

## Original and current values

Every invocation of a transforming extension point contains two values. `original` is the immutable value that the operation started with. `current` is the value that the preceding handlers produced. A handler can build its replacement from either, so a later handler can discard the changes of earlier handlers. Host validates the structure of each replacement before it invokes the next handler.

The [context guide](context-middleware.md#replacement-values) shows an example: handler A appends a message to `current`, and handler B replaces the history with `original` plus its own message.

## Registration order

Host treats every regular executable file in the extension directory as an extension, as the [configuration guide](configuration.md#extensions-and-tools) describes. It registers the extensions in ascending order of their identifiers. The identifier is the executable file name in lower case, with each run of spaces, underscores and hyphens inside the name replaced by one hyphen. A chain contains the handlers of one kind. Host invokes them in registration order across extensions, and in the order of the `HandlerDescriptor` list inside one extension.

## Invocation order in a model request

1. Host runs the [user-input handlers](input-middleware.md), then the [finalized-message handlers](finalized-message-middleware.md#when-handlers-run) for the user message, and then checks the final input against the input types of the selected model. A fully handled input ends here.
2. Agent Core appends the user message and asks Host for the effective context: the final instructions and history of the request. Host prepares the conversation, including automatic compaction. It then runs the system-instruction handlers, the model-context handlers, and checks the final history against the input types of the selected model.
3. For each provider attempt, Host runs the provider-header handlers, then the serialized-request handlers. After the provider returns the response status and headers, it runs the provider-response handlers before the SDK decodes an error or consumes the stream.
4. After the provider completes the response, Host runs the finalized-message handlers for the model response. Agent Core then appends the final response and sends the client events that complete it.
5. For each tool call of the final response, Host runs the [tool-call handlers](#tool-calls) before the tool runs. Agent Core runs the tool, and Host runs the finalized-message handlers for its result. The next model request repeats step 2 from the request for the effective context.

A configured-model request, which an extension or a branch summary starts, runs the instruction, context, provider and finalized-message handlers of steps 2 to 4 on the instructions and history that the caller supplies. Host starts no conversation preparation and records no session entry for the response. The [context guide](context-middleware.md#when-handlers-run) states when instruction and context handlers run again, and the [provider-wire guide](provider-wire-middleware.md#failure-and-retry-behavior) states the same for wire handlers.

## Tool calls

A tool-call handler checks one tool call before the tool runs; [D1](../specs/features/initial/phases/09-tool-middleware-run-control/solution.md#d1-tool-policy-through-a-tool-call-handler-chain) describes the design. Host finds the tool and checks that its extension runs, then invokes the handlers one by one in registration order, and only then validates the final arguments against the tool schema and calls the extension that owns the tool. A `ToolPolicyInvocation` carries the immutable `original` call and the `current` call that preceding handlers left, both as `SessionTreeToolCall`. A handler returns one `ToolPolicyAction`:

| Action | Field | Effect |
| --- | --- | --- |
| Allow | `allow: true` | Keeps the current call |
| Replace the input | `replace_arguments_json` | Sets the complete JSON arguments of the call; the identifier and the tool name stay |
| Reject | `reject_reason` | Ends the chain and the call with a nonempty reason |

The tool runs with the final arguments. After a rejection the tool does not run, and no later handler runs. The model and the client receive an error result with the category `policy_rejection`, and its text is `tool call rejected by extension "<extension>" handler "<handler>": <reason>` with the complete reason. The agent run continues, and the extension stays active.

Host validates each replacement against the tool schema before it invokes the next handler. A replacement that violates the schema, an empty reason, a missing action or a handler error is a failure as in [Failures](#failures): no later handler runs, the tool does not run, and the agent run fails with `EXTENSION_FAILED` and the complete text. When no handler replaced the arguments, Host validates the original arguments after the chain, and the model receives the invalid-arguments result when they violate the schema.

## Replay references

Private provider data, such as encrypted reasoning, never reaches an extension. Host replaces it with a replay reference: a value that is valid for one invocation and contains no provider data. Keep a reference to keep the private data of its block or JSON item, or remove it to discard that data. A later handler can restore a reference from `original` even when an earlier handler removed it. An unknown, altered, duplicated or misplaced reference is an invalid action. Typed history and messages carry the reference in the field `replay_reference`, as the [context guide](context-middleware.md#history-entries-and-replay-references) describes. Serialized provider requests carry it as a JSON object, as the [provider-wire guide](provider-wire-middleware.md#safe-headers-and-serialized-json) describes.

## Failures

A handler error, runtime loss, invalid action or invalid replacement stops the chain and the operation that invoked it. Host runs no later handler. It does not repeat the handler or the chain, and it starts no provider retry or recovery to recover from the failure. The client receives the category `EXTENSION_FAILED` with the complete error text, which names the failed handler. When the handler fails before the provider dispatch, Host sends no provider request.

The category `INPUT_INVALID` reports a final user input that the selected model cannot accept; see [final validation](input-middleware.md#final-validation).

One-shot mode prints the category before the text, for example `[error] EXTENSION_FAILED: run headless agent: process user input: middleware handler "input-text": reference input handler failed`.

When the client cancels the operation while a handler runs, Host reports the cancellation without the category. When a handler had already failed, Host keeps `EXTENSION_FAILED` and retains the cancellation as a contributing cause. The next request runs the chains again.

## Reference commands

The commands in `testdata/external-plugins/cmd` are separate extensions. Each README lists the scenarios of its command and the environment variables that the command reads.

The `testdata/external-plugins` directory is a separate Go module. Set `EXTENSION_DIR` to the path of a new directory, build a command into it, and start Glyph with that directory as its extension directory. Run these commands from the repository root:

```sh
EXTENSION_DIR="$PWD/bin/reference-extensions"
mkdir -p "$EXTENSION_DIR"
(cd testdata/external-plugins && go build -o "$EXTENSION_DIR/context" ./cmd/context)
glyph run --extension-dir "$EXTENSION_DIR" "Describe this project."
```

`--extension-dir` replaces the whole extension catalog, so the directory holds only what you install. A command selects its scenario by the name of its executable. Build or copy the same binary under a scenario name from the README, and install only the selected scenario. Host rejects two executables whose identifiers are equal after normalization, such as `Wire_preserve` and `wire-preserve`, with the error `extension candidate ID is duplicated`. Glyph needs a configured provider and model, as the [configuration guide](configuration.md) describes.
