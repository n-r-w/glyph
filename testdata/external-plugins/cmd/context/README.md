# Public instruction and context reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. It registers two system-instruction transformers and two model-context transformers:

| Handler | Kind | Action |
| --- | --- | --- |
| `instructions-a` | `HANDLER_KIND_SYSTEM_INSTRUCTIONS` | Appends `glyph-instructions-A` to the current instructions |
| `instructions-b` | `HANDLER_KIND_SYSTEM_INSTRUCTIONS` | Replaces them with the original instructions plus [project-instructions.md](project-instructions.md) |
| `context-a` | `HANDLER_KIND_MODEL_CONTEXT` | Appends a user message `glyph-context-A` to the current history |
| `context-b` | `HANDLER_KIND_MODEL_CONTEXT` | Replaces it with the original history plus a user message `glyph-context-B` |

The command embeds `project-instructions.md` at build time. Provider requests therefore receive the original instructions followed by the project instructions, and the original history followed by `glyph-context-B`. They contain neither `A` value. Session entries keep their original content. Because `context-b` copies the original entries, reasoning blocks keep their replay references and the provider receives their private replay.

From `testdata/external-plugins`, build the ordinary reference into a new directory that you name in the shell variable `EXTENSION_DIR`:

```sh
go build -o "$EXTENSION_DIR/context" ./cmd/context
```

See [Reference commands](../../../../docs/guidelines/model-middleware.md#reference-commands) for how to install and start it, and the [author guide](../../../../docs/guidelines/context-middleware.md) for public payloads, replay references and failures.

The executable name selects the scenario. Build or copy the same binary under one of these names:

| Executable | Behavior |
| --- | --- |
| `context` | Compose instructions and history as described above |
| `context-counted` | Append the number of `context-b` invocations in this process, for example `glyph-context-B-1` |
| `context-fail` | Return an ordinary error from the first `instructions-a` invocation, then behave as `context` |
| `context-invalid` | Return an empty history action from the first `context-a` invocation, then behave as `context` |
| `context-fail-second` | Return an ordinary error from the second `instructions-a` invocation, then behave as `context` |
| `context-bad-reference` | Append a model response with the unissued replay reference `unknown` in the first `context-a` invocation, then behave as `context` |
| `context-drop-replay` | Behave as `context`, but remove every replay reference from the `context-b` replacement |

When the environment variable `GLYPH_CONTEXT_TRACE` names a file, every invocation appends one line with the handler identifier, a tab and the received payload as protobuf JSON. The trace shows which handlers ran and that history payloads contain replay references instead of private replay.

The failure scenarios return error text that contains `unexpected EOF`. Host still treats it as an ordinary extension failure and starts no provider recovery. The reference contributes no tools or provider registrations.
