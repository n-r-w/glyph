# Public user-input reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. It registers three user-input handlers in this order:

| Handler | Kind | Action |
| --- | --- | --- |
| `input-handle` | `HANDLER_KIND_USER_INPUT` | Fully handles input whose text contains `glyph-handle-input`; preserves any other input |
| `input-text` | `HANDLER_KIND_USER_INPUT` | Replaces the current input with a copy in which every text block ends with ` glyph-input` |
| `input-images` | `HANDLER_KIND_USER_INPUT` | Replaces the current input with a copy in which every image block becomes the text block `[image removed]` |

For example, the submitted blocks `describe this`, an image and `after image` reach the model as the three text blocks `describe this glyph-input`, `[image removed]` and `after image glyph-input`. A text-only model therefore accepts this input. Core appends the final value to the session.

A request with the text `glyph-handle-input please` completes successfully. Host starts no agent run for it: it sends no model request and appends nothing to the session. The two later handlers do not run.

From `testdata/external-plugins`, build the ordinary reference:

```sh
go build -o /tmp/glyph-input-extensions/input ./cmd/input
```

Point Glyph's `--extension-dir` at `/tmp/glyph-input-extensions`. See the [author guide](../../../../docs/guidelines/input-middleware.md) for payloads, actions and failures.

The executable name selects acceptance scenarios. Build or copy the same binary under one of these names:

| Executable | Behavior |
| --- | --- |
| `input` | Handle, transform text and remove images as described above |
| `input-keep-images` | Behave as `input`, but `input-images` preserves the current input, so images reach the final validation |
| `input-fail` | Return the ordinary error `reference input handler failed` from the first `input-text` invocation, then behave as `input` |
| `input-invalid` | Return an empty action from the first `input-text` invocation, then behave as `input` |
| `input-add-image` | Behave as `input`, but `input-images` appends a PNG image block instead of removing images |
| `input-original` | `input-text` appends the text block `glyph-input-A` to the current input; `input-images` replaces the input with the original plus the text block `glyph-input-B` |
| `input-block` | Block the first `input-text` invocation until Host cancels it, then behave as `input` |
| `input-exit` | Terminate the extension process with status 17 during the first `input-text` invocation |

When the environment variable `GLYPH_INPUT_SIGNALS` names a directory, the blocked `input-block` invocation writes the empty file `input-blocked` there before it waits.

When the environment variable `GLYPH_INPUT_TRACE` names a file, every invocation appends one line with the handler identifier, a tab and the received `UserInputInvocation` as protobuf JSON. The trace shows which handlers ran and the original and current input of each invocation.

Install only the selected scenario in the extension directory. The Host integration fixtures build this command independently and invoke it through real public client operations and extension processes. The reference contributes no tools or provider registrations.
