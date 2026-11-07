# Idea: PHS-10.1 - Extension-defined slash commands

## Definitions

The [domain glossary](../../../../../../terms.md) defines `slash command`, `client command`, `extension`, and `Glyph client`.

## Context and problem

See the agreed [problem statement](slash-commands_problem.md).

## Goal

Let TUI users and programmatic controllers find and run commands supplied by extensions.

## Affected scenarios

- An extension author distributes a user-invokable action with an extension.
- A user finds and invokes the action through the standard TUI.
- A programmatic controller discovers and invokes the same extension-owned action.

## Scope and non-scope

This work covers command contribution, discovery, and explicit invocation through the standard TUI and Programmatic Control. The [problem boundary](slash-commands_problem.md#problem-boundary) and [parent phase exclusions](../ticket.md#scope-boundary) apply.

Command requests remain subject to the [shared operation requirements](../../../../../issues/blocking-contract-operation-processing/prd.md), including outcome reporting and cancellation.

## Requirements

### Functional requirements

- [ ] FRQ.01: An extension author shall be able to add commands to a separately delivered extension using the public Extension Contract and SDK, without changing or rebuilding Glyph.
  - Origin: source. The problem statement's affected audience and desired state, together with the [extension distribution requirements](../../../prd.md#extensions-and-glyph-clients).
  - Goal: Let authors supply commands as part of their extensions.
  - Goal achievement: Full. Authors can supply their own commands independently of changes to Glyph.
  - Feasibility: Confirmed. Host already loads separately delivered extension processes, and the public SDK already supports their startup registration. See `host/internal/usecase/host/extensionruntime/service.go`, `Service.LoadPending`, and `sdk/plugins/extension/v1/service.go`, `Service`.

- [ ] FRQ.02: A standard TUI user shall be able to find the commands provided by extensions and see what each command does. A programmatic controller shall be able to obtain the same information.
  - Origin: source. The problem statement's observed problem and desired state.
  - Goal: Help users choose the command they need.
  - Goal achievement: Full. Users of either client kind can find a command and learn what it does before choosing it.
  - Feasibility: Confirmed. Host already receives each active extension's registration and exposes Host-owned information through both client contracts. See `api/plugins/extension/v1/extension.proto`, `RegisterResponse`; `api/plugins/ui/v1/ui.proto`, `Initialization`; and `api/programmatic/v1/programmatic.proto`, `ControllerRequest`.

- [ ] FRQ.03: A standard TUI user shall be able to execute the action implemented for a chosen extension command by entering `/` followed by its name and optional arguments. A programmatic controller shall be able to execute the same action with its arguments through Programmatic Control. Host shall invoke the extension that provides the chosen command and pass the supplied arguments to that extension. For example, if an author implements `/review` to review a file and save the findings in the active session, `/review changes.go` shall perform that review for `changes.go` and save the findings in that session.
  - Origin: source. The problem statement's explicit-invocation boundary and the glossary definition of `slash command`.
  - Goal: Run the command the caller chose with the arguments the caller supplied.
  - Goal achievement: Full. Both client kinds can execute the selected extension action with the caller's arguments.
  - Feasibility: Confirmed. Both client contracts already support correlated operations, and Host already invokes selected extension implementations with request-specific input. See `host/internal/controller/ui/operations.go`, `handleOperationResponse`; `host/internal/controller/programmatic/service.go`, `Service.receive`; and `host/internal/usecase/host/extensionruntime/service.go`, `Service.HandleHandler`.

Without FRQ.01, adding an author's command can require changes to Glyph. Without FRQ.02, callers can be required to learn command names outside Glyph before invoking them. Without FRQ.03, Glyph can list commands without offering a way to run them. FRQ.02 applies to the commands supplied through FRQ.01. FRQ.03 lets users execute the action implemented for a command they found through FRQ.02, through either client kind. Therefore, fulfilling all three requirements gives authors a way to supply commands and gives users a way to find and execute those same actions in Glyph. These are all the outcomes in the problem's desired state. Calling extension code while preventing it from carrying out the implemented action does not fulfill FRQ.03.

### Non-functional requirements

No additional feature-specific non-functional requirements. The platform's [error semantics](../../../prd.md#error-semantics), [extension lifecycle requirements](../../../prd.md#environment-reload), and [architectural constraints](../../../architecture.md#non-functional-considerations) apply.

## Critical scenarios

Not defined at the requirements stage.

## Open questions

No unresolved requirement-level questions.

## Technical supplement

None.

## References

- [Problem statement](slash-commands_problem.md).
- [Parent phase](../ticket.md).
- [Platform requirements](../../../prd.md).
- [Standard TUI requirements](../../../standard-tui.md), especially command discovery and completion.
