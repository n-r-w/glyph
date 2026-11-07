# Problem statement: PHS-10.2 - Direct extension interaction with users

## Key definitions and abbreviations

The [domain glossary](../../../../../../terms.md) defines `extension`, `Glyph client`, `interaction request`, and `notification`.

## Context

An extension-owned action can need information that only the user can provide. An extension can also have information to show the user while performing its work. These needs arise independently of whether a language model participates in the action.

## Observed problem

Extension authors cannot directly ask a user a question and obtain the answer through the active Glyph client. They also cannot send a user notification through the public Extension Contract.

For example, a review extension needs to know whether the user wants a general review or a review focused on error handling. The extension cannot present that choice and receive the selected scope as its own input. It also cannot send a completion notification through the client notification path.

## Affected audience

- Extension authors whose actions need a user's choice, text, confirmation, or cancellation.
- Glyph users who need to provide that input or receive information from an extension through their chosen client.

## Evidence

- `api/plugins/extension/v1/extension.proto`, `ExtensionRequest` and `HostCompleted`, contain no general user-interaction or notification operation.
- `api/plugins/ui/v1/ui.proto`, `HostRequest` and `UICompleted`, carry initialization and cancellation but no general question-and-answer exchange.
- `api/programmatic/v1/programmatic.proto` contains no equivalent general interaction exchange or extension notification payload.
- `api/plugins/ui/v1/model.proto`, `AuthorizationRequest`, describes a provider authorization challenge. That dedicated authentication output does not provide general extension question-and-answer behavior.

## Impact

HIGH for interactive extension scenarios. Users cannot provide input directly to extension logic through their client, and authors cannot complete actions that depend on such input. Users also lack a direct extension notification channel through that contract.

## Current state

The TUI can show Host informational output and dedicated authorization progress. Extension authors have no public operation for asking a general question, obtaining its answer, or delivering a notification through either client kind.

## Desired state

Users can participate directly in extension-owned actions and receive their information through the client they use to control Glyph. Extension authors can complete those actions with the user's actual input.

## Problem boundary

The problem covers direct interaction between extension logic and a user. A list of choices, free-text input, and confirmation illustrate the input need. The extension itself can need the answer before it continues its work.

Terminal-specific presentation is outside this problem. Command discovery, coordination between extensions, and provider authentication have separate problem statements in the [parent phase](../ticket.md).

## Assumptions

None.

## Open questions

None.
