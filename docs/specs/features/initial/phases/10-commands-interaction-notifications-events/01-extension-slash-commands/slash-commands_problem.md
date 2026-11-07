# Problem statement: PHS-10.1 - Extension-defined slash commands

## Key definitions and abbreviations

The [domain glossary](../../../../../../terms.md) defines `slash command`, `client command`, `extension`, and `Glyph client`.

## Context

Glyph users invoke built-in actions through commands such as `/model` and `/new` in the standard TUI. Extensions already contribute tools and handlers, make configured-model requests, and store session-bound data.

## Observed problem

Extension authors cannot add their own discoverable, explicitly invokable slash commands. A user cannot find and invoke an extension-owned action through the client command catalogue.

For example, an author can implement review logic in an extension, but cannot expose that logic as a registered `/review` command that the user can discover and invoke.

## Affected audience

- Extension authors who provide actions that users explicitly choose to run.
- Glyph users who want to invoke those actions through the standard TUI or Programmatic Control.

## Evidence

- `api/plugins/extension/v1/extension.proto`, `RegisterResponse`, declares tools and handlers. It contains no command registration.
- `api/plugins/ui/v1/ui.proto`, `UIRequest`, and `api/programmatic/v1/programmatic.proto`, `ControllerRequest`, contain built-in operations but no extension-command discovery or invocation.
- `plugins/ui/tui/internal/usecase/presentation/update.go`, `interaction.updateEnter`, handles built-in slash commands. The [standard command list](../../../tui-defaults.md#command-names) records their names.

## Impact

HIGH for extensibility. Authors cannot deliver a registered user action as part of an extension, and users remain limited to built-in commands for direct command invocation.

## Current state

The standard TUI recognizes its built-in command names. Host registers extension tools and typed handlers. User-input handlers can process user messages, but the public contracts contain no extension-command catalogue or invocation entry point.

## Desired state

Users have direct access to the actions supplied by their extensions, and authors can distribute those actions with the extensions. The same action is available to users of either Glyph client kind.

## Problem boundary

This problem concerns discovering and explicitly invoking extension-owned actions. `/review` illustrates a user-invoked action in the TUI.

Questions asked during an action, notifications, coordination with other extensions, and provider authentication have separate problem statements in the [parent phase](../ticket.md).

Existing built-in session commands remain available. The [rejected session-control problem](../ticket.md#rejected-problem-extension-initiated-main-session-control) is outside this subphase.

## Assumptions

None.

## Open questions

None.
