# Public provider-wire reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. It registers two header transformers, two safe-body transformers and two response observers. The second transformer replaces the first transformer's marker from immutable original input. Provider requests receive header `X-Glyph-Wire: B` and JSON field `glyph_wire_marker: "B"`. Session entries retain their original content.

From `testdata/external-plugins`, build the ordinary reference:

```sh
go build -o /tmp/glyph-wire-extensions/wire ./cmd/wire
```

Point Glyph's `--extension-dir` at `/tmp/glyph-wire-extensions`. See the [author guide](../../../../docs/guidelines/provider-wire-middleware.md) for public payloads, action semantics, private replay and failures.

The executable name selects acceptance scenarios. Build or copy the same binary under one of these names:

| Executable | Behavior |
| --- | --- |
| `wire` | Compose headers and safe JSON, then acknowledge response metadata |
| `wire-preserve` | Transform headers and preserve exact serialized body bytes |
| `wire-fail-headers` | Return an ordinary error before body handlers or HTTP dispatch |
| `wire-fail-body` | Return an ordinary error before HTTP dispatch |
| `wire-invalid` | Return an invalid body action |
| `wire-bad-reference` | Return an unknown replay reference |
| `wire-fail-response` | Fail the first observer after one provider dispatch |

Install only the selected scenario in the extension directory. The failure text includes `unexpected EOF` to demonstrate that retry-like text does not turn an ordinary extension failure into permission for provider recovery.

The Host integration fixtures build this command independently and invoke it through real public client operations and extension processes. The reference contributes no tools or provider registrations.
