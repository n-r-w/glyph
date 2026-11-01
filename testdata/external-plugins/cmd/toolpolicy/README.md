# Public tool-policy reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. The last dash-separated part of the executable name selects its behavior. Every policy executable registers one `HANDLER_KIND_TOOL_POLICY` handler, `tool-policy`. The executable that ends with `-tool` registers the tool `policy_echo` and no handler. The tool accepts the object `{"value": "<text>"}`, and its result is `policy tool ran with <value>`.

| Executable ends with | Behavior of the `tool-policy` handler |
| --- | --- |
| `-tool` | Registers the tool and no handler |
| `-allow` | Allows every call |
| `-replace` | Replaces the input of every call with `{"value":"replaced"}` |
| `-reject` | Rejects a call whose value is `forbidden` with the reason `tool policy forbids the value forbidden; use another value`, and allows every other call |
| `-fail` | Returns the ordinary error `reference tool policy failed` for every call |
| `-invalid` | Replaces the input of every call with `{"value":1}`, which violates the tool schema |
| `-empty` | Returns an empty action for every call |

Host registers the extensions in the order of their executable names. To run two policies in order, build the same binary under names such as `toolpolicy-1-replace` and `toolpolicy-2-allow`, and add `toolpolicy-tool` for the tool. From `testdata/external-plugins`, build into a new directory that you name in the shell variable `EXTENSION_DIR`:

```sh
for name in toolpolicy-1-replace toolpolicy-2-allow toolpolicy-tool; do
  go build -o "$EXTENSION_DIR/$name" ./cmd/toolpolicy
done
```

See [Reference commands](../../../../docs/guidelines/model-middleware.md#reference-commands) for how to start it, and [Tool calls](../../../../docs/guidelines/model-middleware.md#tool-calls) for the actions and failures.

When the environment variable `GLYPH_TOOLPOLICY_TRACE` names a file, every process appends one line per event: for a handler invocation the executable name, a tab, the JSON arguments of the original call, a tab and the JSON arguments of the current call; for a run of the tool the word `tool`, a tab and its JSON arguments.
