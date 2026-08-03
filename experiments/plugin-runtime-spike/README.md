# Plugin Runtime Spike

This experiment exercises the selected Glyph plugin process architecture with real child processes and a real controlling terminal.

It covers:

- independent extension and UI protocol version `1` handshakes through `go-plugin v1.8.0`;
- fixed UI capability discovery without opening the UI stream or terminal;
- multiple extension processes and Host-owned tool routing;
- streamed progress and terminal tool results;
- context cancellation;
- extension crash isolation and late unavailable-tool results;
- global tool-name conflict cleanup;
- one persistent bidirectional UI gRPC stream;
- Bubble Tea v2 terminal ownership through `tea.OpenTTY()`;
- terminal resize delivery;
- Host terminal recovery after normal UI completion and deliberate `os.Exit(23)` failure.

## Automated Checks

```bash
go test -race -count=1 ./...
```

These checks build the spike executable and launch real extension and UI plugin processes. They do not require a terminal.

## Full Check on macOS

The terminal check must run inside a controlling pseudo-terminal. From this directory:

```bash
go build -o /tmp/glyph-plugin-runtime-spike .
/usr/bin/script -q /dev/null /tmp/glyph-plugin-runtime-spike check-all
```

The hard-failure scenario changes the pseudo-terminal size, delivers `SIGWINCH`, and terminates the UI process through `os.Exit(23)` while Bubble Tea owns raw mode. Glyph Host then emits terminal-mode cleanup through `github.com/charmbracelet/x/ansi` and restores the saved state through `github.com/charmbracelet/x/term`.

The harness independently restores its initial terminal state and dimensions before returning, including when an assertion fails.

A successful run ends with:

```text
PASS: plugin SDK, multiple extensions, bidirectional UI, cancellation, crash isolation, and terminal restoration
```
