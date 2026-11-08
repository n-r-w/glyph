# Problem statement: PHS-10.1 - Extension-defined slash commands

## Key definitions and abbreviations

The [domain glossary](../../../../../../terms.md) defines `slash command`, `client command`, `extension`, and `Glyph client`.

## Context

Glyph users invoke built-in actions through commands such as `/model` and `/new` in the standard TUI. Extensions already contribute tools and handlers, make configured-model requests, and store session-bound data.

## Observed problem

- PRM.01: Extension authors cannot expose their actions through the public Extension Contract as commands that Glyph clients offer for explicit invocation.
  - PRB.01: An extension author wants to distribute an action as a command that users can find and run in Glyph.
    - Scenario:
      1. The author implements an action in a separately delivered extension.
      2. The author tries to declare that action as a command available to Glyph clients.
      3. The author distributes the extension without changing Glyph.
    - Current break: Step 2. The Extension Contract registers tools and typed handlers and has no command declaration. The author cannot expose the action as a discoverable, explicitly invokable command.
    - Example: An author implements file-review logic and wants to offer it as `/review`.
- PRM.02: Callers cannot use Glyph to find an extension-owned action and explicitly address that action with their arguments. This prevents TUI users and programmatic-controller developers from accessing extension actions through Glyph's command facilities.
  - PRB.02: A standard TUI user wants to see which extension commands are available and what each command does.
    - Scenario:
      1. The user starts the standard TUI with an extension loaded.
      2. The user looks for the extension's commands and their descriptions in Glyph.
      3. The user identifies the command needed for the task.
    - Current break: Step 2. The TUI receives no extension-command catalogue. The user cannot discover the extension's actions through Glyph's command facilities.
    - Example: A user wants to find the loaded extension's file-review action and learn its purpose.
  - PRB.03: A programmatic-controller developer wants the controller to obtain the available extension commands and their descriptions without loading the standard TUI.
    - Scenario:
      1. The controller connects to a Glyph host with an extension loaded.
      2. The controller requests information about the extension's available commands through Programmatic Control.
      3. The controller uses that information to identify an action for the task.
    - Current break: Step 2. Programmatic Control has no operation for obtaining extension commands. The controller cannot obtain this information through its Glyph connection.
    - Example: A controller needs to identify a loaded extension's file-review action before requesting a review.
  - PRB.04: A caller who already knows an extension action wants to invoke that action with arguments through either Glyph client kind.
    - Scenario:
      1. The caller chooses a known action and supplies its arguments.
      2. The caller addresses that action through the Glyph client.
      3. Host invokes the extension that provides the chosen action with the supplied arguments.
    - Current break: Step 2. Neither client contract has an extension-command invocation. The idle TUI submits an unmatched slash string as ordinary user input. Host sends ordinary user input through a handler chain in which an earlier handler can replace or fully handle the message. The caller therefore has no command-level guarantee that Host invokes the extension providing the chosen action.
    - Example: A TUI user enters `/review changes.go` intending to invoke the review extension for `changes.go`. A programmatic controller wants to request that same action with the same argument.

## Affected audience

- Extension authors who provide actions that users explicitly choose to run.
- Standard TUI users who want to discover and explicitly invoke extension-owned actions.
- Programmatic-controller developers who need the same access through a headless Glyph connection.

## Evidence

- `api/plugins/extension/v1/extension.proto`, `RegisterResponse`, declares tools and handlers. It contains no command registration.
- `api/plugins/ui/v1/ui.proto`, `Initialization` and `UIRequest`, and `api/plugins/ui/v1/agent.proto`, `ExtensionAvailability`, expose startup state, extension tool names, and built-in operations. They contain no extension-command catalogue.
- `api/programmatic/v1/programmatic.proto`, `ControllerRequest`, contains built-in operations and ordinary user input, with no extension-command discovery or invocation operation.
- `plugins/ui/tui/internal/usecase/presentation/update.go`, `interaction.updateEnter`, handles built-in slash commands and submits other nonempty text while idle. The [standard command list](../../../tui-defaults.md#command-names) records the built-in names.
- `host/internal/usecase/host/middleware/context.go`, `compose`, invokes matching user-input handlers in order. A handler can replace the current input or fully handle it and end the chain. Host selects these handlers by handler kind, independently of any command name in the text.
- `host/internal/usecase/host/runcontrol/coordinator.go`, `Coordinator.RunPrepared`, completes fully handled user input without executing Agent Core.

## Impact

HIGH. Authors cannot offer their extension actions through Glyph's command facilities. Callers cannot discover those actions through Glyph or address a chosen action with a command-level guarantee of invocation. Knowing an extension-specific text convention leaves the caller dependent on the ordinary input-handler chain.

## Current state

The standard TUI recognizes its built-in command names. Host registers extension tools and typed handlers. Both client contracts expose ordinary user input and built-in operations, with no extension-command catalogue or invocation.

While idle, the TUI submits an unmatched slash string as ordinary user input. Host invokes user-input handlers in order, and an extension can interpret that text and fully handle it without starting Agent Core. An earlier handler can replace the text or end the chain before the intended extension receives it. Successful text handling therefore depends on the participating handlers and their actions.

## Desired state

Authors can deliver actions that callers discover and explicitly invoke through either Glyph client kind. Callers can choose an extension-owned action and supply its arguments with an unambiguous connection between their choice and the extension Host invokes.

## Problem boundary

This problem concerns discovering and explicitly invoking extension-owned actions. `/review` illustrates a user-invoked action in the TUI.

Questions asked during an action, notifications, coordination with other extensions, and provider authentication have separate problem statements in the [parent phase](../ticket.md).

Built-in session actions and the [rejected session-control problem](../ticket.md#rejected-problem-extension-initiated-main-session-control) are outside this subphase.

## Assumptions

None.

## Open questions

None.
