# Idea: PHS-10.1 - Extension-defined slash commands

## Definitions

The [domain glossary](../../../../../../terms.md) defines `slash command`, `client command`, `extension`, and `Glyph client`.

## Context and problem

See the agreed [problem statement](slash-commands_problem.md).

## Goal

Let TUI users and programmatic controllers find and run commands supplied by extensions.

## Affected scenarios

- An extension author distributes a user-invokable action with an extension.
- A standard TUI user discovers the available extension commands and their purposes.
- A programmatic controller obtains the same command information without loading the standard TUI.
- A caller explicitly invokes a known extension command with arguments through either Glyph client kind.

## Scope and non-scope

This work covers command contribution, discovery, and explicit invocation through the standard TUI and Programmatic Control. The [problem boundary](slash-commands_problem.md#problem-boundary) and [parent phase exclusions](../ticket.md#scope-boundary) apply.

Command requests remain subject to the [shared operation requirements](../../../../../issues/blocking-contract-operation-processing/prd.md), including outcome reporting and cancellation.

## Requirements

### Functional requirements

- [X] FRQ.01: An extension author shall be able to declare commands and their action implementations in a separately delivered extension using the public Extension Contract and SDK, without changing or rebuilding Glyph.
  - Origin: source. PRB.01 in the [problem statement](slash-commands_problem.md#observed-problem) and the [extension distribution requirements](../../../prd.md#extensions-and-glyph-clients).
  - Goal: Let authors supply user-invokable actions as commands with their extensions.
  - Feasibility: Confirmed. Glyph already loads separately delivered extensions through its public contract and SDK. Extension authors can supply executable behavior independently of the Glyph application.
- [X] FRQ.02: A standard TUI user shall be able to discover the available commands supplied through FRQ.01 and see each command's name and purpose. A programmatic controller shall be able to obtain the same command information through Programmatic Control without loading the standard TUI.
  - Origin: source. PRB.02 and PRB.03 in the [problem statement](slash-commands_problem.md#observed-problem).
  - Goal: Let callers identify the extension command needed for their task through either Glyph client kind.
  - Feasibility: Confirmed. Host already provides model and session information to both client kinds through public contracts. Host can provide command information through these same client boundaries without requiring terminal behavior from a controller.
- [X] FRQ.03: A standard TUI user shall be able to execute the action implemented for a chosen extension command by entering `/` followed by its name and optional arguments. A programmatic controller shall be able to execute the same action with its arguments through Programmatic Control. Host shall invoke the chosen command's action in the extension that provides it and pass the supplied arguments unchanged, independently of ordinary user-input handler behavior. For example, if an extension implements `/review` to review a file and save findings in the active session, `/review changes.go` shall execute that action for `changes.go` and save its findings in that session.
  - Origin: source. PRB.04 in the [problem statement](slash-commands_problem.md#observed-problem) and the domain glossary definition of `slash command`.
  - Goal: Let callers run the action they explicitly chose with the arguments they supplied.
  - Feasibility: Confirmed. Host already invokes selected extension behavior with request-specific input. Extensions already have configured-model access and session-bound writes for actions such as the review example. Both client kinds support correlated, cancelable operations.

### Non-functional requirements

No additional feature-specific non-functional requirements. The platform's [error semantics](../../../prd.md#error-semantics), [extension lifecycle requirements](../../../prd.md#environment-reload), and [architectural constraints](../../../architecture.md#non-functional-considerations) apply.

## Justification of requirements

| What does not work now | Requirements that fix it |
|---|---|
| PRB.01: An author cannot declare an extension action as a command available to Glyph clients. | FRQ.01: Command declarations and action implementations in a separately delivered extension through the public contract and SDK. |
| PRB.02: A TUI user cannot find the loaded extensions' commands and their purposes in Glyph. | FRQ.02: Command discovery with names and purposes in the standard TUI. |
| PRB.03: A controller cannot obtain the loaded extensions' commands and their purposes through its Glyph connection. | FRQ.02: The same command information through Programmatic Control without the standard TUI. |
| PRB.04: A caller cannot explicitly address a known extension action with arguments and a command-level guarantee of invocation. | FRQ.03: TUI slash invocation and Programmatic invocation of the chosen extension action with unchanged arguments, independently of ordinary user-input handlers. |

Sufficiency: Each problem part has a requirement that removes its failing step. Together, FRQ.01-FRQ.03 let an author supply commands and let callers discover and explicitly execute those commands through either client kind.

Necessity: Each requirement and each client-kind variant appears in a row. Without FRQ.01, authors cannot supply commands through their extensions. Without either discovery variant of FRQ.02, callers using that client kind still cannot obtain command information. Without either invocation variant of FRQ.03, callers using that client kind still cannot execute the chosen action. Without preservation of the chosen action and its arguments independently of user-input handlers, PRB.04 remains unresolved.

## Critical scenarios

- SCN.01: Separately delivered commands become discoverable in the standard TUI.
  - Requirements: FRQ.01; FRQ.02, standard TUI discovery.
  - Level: external.
  - Area: Extension project, public Extension Contract and SDK, Host extension loading, standard TUI.
  - Flow: An author declares commands and implements their actions in a separate extension project using the public Extension Contract and SDK. The author delivers the extension without changing or rebuilding Glyph. Host loads the extension, and a TUI user discovers its commands with their names and purposes and identifies the action needed for the task.
- SCN.02: A programmatic controller discovers extension commands without the standard TUI.
  - Requirements: FRQ.02, Programmatic Control discovery.
  - Level: external.
  - Area: Loaded extension, Host, Programmatic Control, programmatic controller.
  - Flow: A controller connects to a headless Glyph host with the extension loaded. Through Programmatic Control, the controller obtains the available extension commands with the same names and purposes available to a TUI user. The controller identifies the action needed for the task while the standard TUI remains unloaded.
- SCN.03: Explicit invocation preserves the chosen action and optional arguments despite ordinary input handlers.
  - Requirements: FRQ.03, standard TUI and Programmatic Control invocation, with and without arguments, independently of ordinary user-input handlers.
  - Level: external.
  - Area: Standard TUI or programmatic controller, Host, command-owning extension, extensions with ordinary user-input handlers.
  - Flow: Extensions with handlers that replace or fully handle ordinary user input are active. A caller explicitly invokes a known extension command through the standard TUI or Programmatic Control. Host invokes the chosen action in its providing extension with the supplied arguments unchanged. When the caller omits arguments, the invocation reaches that action without arguments. The caller observes the chosen action's outcome, and ordinary input-handler behavior does not substitute another action or arguments or consume the command invocation.
- SCN.04: An extension command completes its implemented review and saves findings in the active session.
  - Requirements: FRQ.03, complete extension-owned action through the standard TUI and Programmatic Control.
  - Level: external. The extension observes saved findings through its public session access.
  - Area: Standard TUI or programmatic controller, Host, review extension, public Extension Contract, active session.
  - Flow: An extension implements `/review` to review a file and save findings in the active session. A TUI user invokes `/review changes.go`, or a controller invokes the same command with the same argument through Programmatic Control. The extension reviews `changes.go`, saves its findings in that session, and can recover the saved findings through its public session access.

Every requirement and each of its client-kind variants has a scenario.

## Open questions

No unresolved requirement-level questions.

## Technical supplement

None.

## References

- [Problem statement](slash-commands_problem.md).
- [Parent phase](../ticket.md).
- [Platform requirements](../../../prd.md).
- [Standard TUI requirements](../../../standard-tui.md), especially command discovery and completion.
