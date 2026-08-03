# Codex OAuth Spike

This experiment verifies that the selected Glyph prototype stack works end to end against the live ChatGPT Codex backend with `gpt-5.6-luna`:

- browser PKCE through `golang.org/x/oauth2`;
- forced JSON token refresh;
- OpenAI Responses SSE through `openai-go/v3`;
- a strict `read` function schema compiled with `jsonschema/v6`;
- function-call argument validation;
- tool-result continuation with encrypted reasoning replay.

## Run

```bash
go run .
```

On the first run, the program displays the authorization URL and attempts to open it in the macOS browser. Complete the login in that browser. The program then creates `~/.glyph/credentials.json` with mode `0600` inside an owner-only `~/.glyph` directory and stores the `openai-codex` provider payload. Later runs load and refresh that credential without browser login. Delete the credential file to force a new login.

The experiment creates a separate temporary directory outside the repository for its generated sample file and removes that directory before exit. It does not read Pi credentials and never prints tokens or authorization headers.

A successful run prints a checkpoint for each verified contract and ends with:

```text
PASS: Codex OAuth, refresh, SSE, strict tool call, and encrypted reasoning replay
```
