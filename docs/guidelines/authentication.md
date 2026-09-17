# OpenAI Codex sign-in

Configure the provider and model as described in [configuration](configuration.md). Glyph reuses saved credentials when they remain usable.

When the TUI reports that sign-in is required:

1. Press `Ctrl+R` to open the sign-in selector.
2. Use the arrow keys to choose a method, then press `Enter`.
   - `Browser (on this computer)` opens the browser flow with a local callback. Use it when Glyph and the browser run on the same computer.
   - `Device code (SSH / remote)` displays a provider link and a one-time code. Open the link on the browser computer, sign in, and enter the code there. Glyph on the server waits for approval without a local callback or SSH port forwarding.
3. Wait for the TUI to return to `Idle`.

`Esc` closes the method selector without starting sign-in. `Ctrl+C` cancels a running attempt. After failure, cancellation, or expiration, use `Ctrl+R` to choose a method again. A failed code-based attempt does not automatically start browser sign-in.

Device-code attempts expire after 15 minutes. Provider failures appear in the TUI with their original error details. Successful sign-in uses Glyph's existing credential store and refresh behavior.
