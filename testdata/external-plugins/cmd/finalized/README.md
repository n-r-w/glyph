# Public finalized-message reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. It registers one tool and two finalized-message handlers in this order:

| Declaration | Kind | Action |
| --- | --- | --- |
| `finalized_echo` | Tool | Returns the text `echo output` |
| `finalized-mark` | `HANDLER_KIND_FINALIZED_MESSAGE` | Replaces the current message with a copy in which every text block ends with ` [final user]`, ` [final model]` or ` [final tool]`, by role |
| `finalized-later` | `HANDLER_KIND_FINALIZED_MESSAGE` | Preserves the current message |

`finalized-mark` keeps tool calls, images and replay references unchanged. For example, the request `finalize me` is stored and sent as `finalize me [final user]`. A model response `Calling tool.` with a call of `finalized_echo` becomes `Calling tool. [final model]`, and the tool result becomes `echo output [final tool]`. The session, the completion events and the next model request contain these values.

From `testdata/external-plugins`, build the ordinary reference:

```sh
go build -o /tmp/glyph-finalized-extensions/finalized ./cmd/finalized
```

Point Glyph's `--extension-dir` at `/tmp/glyph-finalized-extensions`. See the [author guide](../../../../docs/guidelines/finalized-message-middleware.md) for payloads, actions, replacement rules and failures.

The executable name selects acceptance scenarios. Build or copy the same binary under one of these names:

| Executable | Behavior of `finalized-mark` |
| --- | --- |
| `finalized` | Marks every message as described above |
| `finalized-fail-tool` | Returns the ordinary error `reference finalized handler failed` for the first tool result, then behaves as `finalized` |
| `finalized-fail-model` | Returns the same error for the first model response, then behaves as `finalized` |
| `finalized-preserve` | Preserves every message |
| `finalized-redact` | Replaces the text of every model text block with `[redacted]` instead of appending ` [final model]` |
| `finalized-arguments` | Behaves as `finalized` and replaces the arguments of every model tool call with `{"value":"final"}` |
| `finalized-role` | Replaces the first model response with the user message `role change` |
| `finalized-bad-reference` | Sets the replay reference `unknown` on the first block of the first model response |
| `finalized-invalid` | Returns an empty action for the first model response |
| `finalized-image` | Appends a PNG image block to the first user message |
| `finalized-block` | Blocks the first model-response invocation until Host cancels it |

When the environment variable `GLYPH_FINALIZED_FAILING_MODEL` holds a number N, `finalized-mark` returns the ordinary error `reference finalized handler failed` for the Nth model response in any scenario.

When the environment variable `GLYPH_FINALIZED_SIGNALS` names a directory, the blocked `finalized-block` invocation writes the empty file `finalized-blocked` there before it waits.

When the environment variable `GLYPH_FINALIZED_TRACE` names a file, every handler invocation appends one line with the handler identifier, a tab and the received `FinalizedMessageInvocation` as protobuf JSON. Every call of `finalized_echo` appends a line with the tool name and its JSON arguments.

Install only the selected scenario in the extension directory. The Host integration fixtures build this command independently and invoke it through real public client operations and extension processes.
