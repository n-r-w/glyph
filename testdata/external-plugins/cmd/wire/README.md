# Public provider-wire reference extension

This command imports only Glyph's public protobuf contracts and Extension SDK. It registers two header transformers, two safe-body transformers and two response observers. The second transformer replaces the first transformer's marker from immutable original input. Provider requests receive header `X-Glyph-Wire: B` and JSON field `glyph_wire_marker: "B"`. Session entries retain their original content.

From `testdata/external-plugins`, build the ordinary reference into a new directory that you name in the shell variable `EXTENSION_DIR`:

```sh
go build -o "$EXTENSION_DIR/wire" ./cmd/wire
```

See [Reference commands](../../../../docs/guidelines/model-middleware.md#reference-commands) for how to install and start it, and the [author guide](../../../../docs/guidelines/provider-wire-middleware.md) for public payloads, private replay and failures.

The executable name selects the scenario. Build or copy the same binary under one of these names:

| Executable | Behavior |
| --- | --- |
| `wire` | Compose headers and safe JSON, then acknowledge response metadata |
| `wire-preserve` | Transform headers and preserve exact serialized body bytes |
| `wire-fail-headers` | Return an ordinary error before body handlers or HTTP dispatch |
| `wire-fail-body` | Return an ordinary error before HTTP dispatch |
| `wire-invalid` | Return an invalid body action |
| `wire-bad-reference` | Return an unknown replay reference |
| `wire-fail-response` | Fail the first observer after one provider dispatch |

The failure scenarios return error text that contains `unexpected EOF`. Host still treats it as an ordinary extension failure and starts no provider recovery. The reference contributes no tools or provider registrations.
