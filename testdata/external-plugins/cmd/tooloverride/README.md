# Public tool-replacement reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. Whatever its executable name, it registers the tool `read` with `replaces_tool` set, so the tool replaces the tool `read` of another extension, for example of the bundled tools extension. The replacing tool accepts the object `{"value": "<text>"}` and returns the text `replacement read ran with <value>`. The extension also registers one tool-policy handler, `tool-policy`, that allows every call.

Build the same binary under several names to declare several replacements of the same tool. From `testdata/external-plugins`, build into a new directory that you name in the shell variable `EXTENSION_DIR`, next to the bundled `glyph-tools` executable:

```sh
go build -o "$EXTENSION_DIR/tooloverride-read" ./cmd/tooloverride
```

When one extension replaces `read`, the model sees and calls the replacing tool, and the other tools and handlers of both extensions stay available. When several extensions replace the same tool, Host accepts none of the replacements, keeps the original tool, and reports the conflict at startup as a startup issue.

When the environment variable `GLYPH_TOOLOVERRIDE_TRACE` names a file, every process appends one line per event: the executable name, a tab, the word `handler` or `tool`, a tab and the name of the called tool. `handler` marks an invocation of the tool-policy handler, and `tool` marks a run of the replacing tool.
