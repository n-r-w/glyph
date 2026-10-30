# Public instruction and context reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. It registers two system-instruction transformers and two model-context transformers:

| Handler | Kind | Action |
| --- | --- | --- |
| `instructions-a` | `HANDLER_KIND_SYSTEM_INSTRUCTIONS` | Appends `glyph-instructions-A` to the current instructions |
| `instructions-b` | `HANDLER_KIND_SYSTEM_INSTRUCTIONS` | Replaces them with the original instructions plus [project-instructions.md](project-instructions.md) |
| `context-a` | `HANDLER_KIND_MODEL_CONTEXT` | Appends a user message `glyph-context-A` to the current history |
| `context-b` | `HANDLER_KIND_MODEL_CONTEXT` | Replaces it with the original history plus a user message `glyph-context-B` |

The command embeds `project-instructions.md` at build time. Provider requests therefore receive the original instructions followed by the project instructions, and the original history followed by `glyph-context-B`. They contain neither `A` value. Session entries keep their original content. Because `context-b` copies the original entries, reasoning blocks keep their replay references and the provider receives their private replay.

From `testdata/external-plugins`, build the ordinary reference:

```sh
go build -o /tmp/glyph-context-extensions/context ./cmd/context
```

Point Glyph's `--extension-dir` at `/tmp/glyph-context-extensions`. See the [author guide](../../../../docs/guidelines/context-middleware.md) for public payloads, action semantics, replay references and failures.

The executable name selects acceptance scenarios. Build or copy the same binary under one of these names:

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

Install only the selected scenario in the extension directory. The failure text includes `unexpected EOF` to demonstrate that retry-like text does not turn an ordinary extension failure into permission for provider recovery.

The Host integration fixtures build this command independently and invoke it through real public client operations and extension processes. The reference contributes no tools or provider registrations.
