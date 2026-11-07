# Problem statement: PHS-10.1 - Extension-defined slash commands

## Key definitions and abbreviations

The [domain glossary](../../../../../../terms.md) defines `slash command`, `client command`, `extension`, and `Glyph client`.

## Context

Glyph users invoke built-in actions through commands such as `/model` and `/new` in the standard TUI. Extensions already contribute tools and handlers, make configured-model requests, and store session-bound data.

## Observed problem

Extension authors cannot add their own discoverable, explicitly invokable slash commands. A user cannot find and invoke an extension-owned action through the client command catalogue.

For example, an author can implement review logic in an extension, but cannot expose that logic as a registered `/review` command that the user can discover and invoke.

## Problem parts

- PRB.01: An extension author wants to distribute an action, such as code review, as a command that users can find and run in Glyph.
  - Scenario:
    1. The author implements the action in an extension.
    2. The author tries to expose the action to Glyph clients as a discoverable command.
    3. The author distributes the extension without changing Glyph.
  - Current break: Step 2. The Extension Contract registers tools and typed handlers but has no registration for discoverable user commands. The author cannot expose the action as such a command through the public contract.

- PRB.02: A standard TUI user wants to find an action supplied by a loaded extension and run it for their task.
  - Scenario:
    1. The user starts the standard TUI with the extension loaded.
    2. The user tries to find the extension's commands and their purpose through Glyph's command catalogue.
    3. The user chooses a command and runs its action with the arguments needed for the task.
  - Current break: Step 2. The TUI receives no extension-command catalogue and recognizes its fixed built-in slash names. The user needs extension-specific instructions to learn how to invoke the action.

- PRB.03: A programmatic controller wants to find an action supplied by a loaded extension and run it without the standard TUI.
  - Scenario:
    1. The controller connects to a Glyph host with the extension loaded.
    2. The controller tries to obtain the extension's available commands and their purpose through Programmatic Control.
    3. The controller chooses a command and requests its action with the arguments needed for the task.
  - Current break: Step 2. Programmatic Control has no operation for obtaining extension commands. The controller needs extension-specific instructions to identify and invoke the action.

## Affected audience

- Extension authors who provide actions that users explicitly choose to run.
- Glyph users who want to invoke those actions through the standard TUI or Programmatic Control.

## Evidence

- `api/plugins/extension/v1/extension.proto`, `RegisterResponse`, declares tools and handlers. It contains no command registration.
- `api/plugins/ui/v1/ui.proto`, `Initialization` and `UIRequest`, and `api/plugins/ui/v1/agent.proto`, `ExtensionAvailability`, expose startup state, extension tool names, and built-in operations. They contain no extension-command catalogue.
- `api/programmatic/v1/programmatic.proto`, `ControllerRequest`, contains built-in operations but no extension-command discovery operation.
- `plugins/ui/tui/internal/usecase/presentation/update.go`, `interaction.updateEnter`, handles built-in slash commands and submits other nonempty text while idle. The [standard command list](../../../tui-defaults.md#command-names) records the built-in names.
- `host/internal/usecase/host/runcontrol/coordinator.go`, `Coordinator.RunPrepared`, completes fully handled user input without executing Agent Core. This is an existing way for an extension to run its logic in response to user text.

## Impact

HIGH for extensibility. Authors cannot supply discoverable commands through the public Extension Contract. Users and controller developers need extension-specific instructions to learn which actions are available and how to invoke them.

## Current state

The standard TUI recognizes its built-in command names. Host registers extension tools and typed handlers. While idle, the TUI submits an unmatched slash string as ordinary user input. An extension can interpret that text in a user-input handler and fully handle it without starting Agent Core. The public contracts provide no catalogue through which clients can discover extension commands.

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
