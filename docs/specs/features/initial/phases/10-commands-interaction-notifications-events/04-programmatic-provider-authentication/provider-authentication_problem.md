# Problem statement: PHS-10.4 - Provider authentication through Programmatic Control

## Key definitions and abbreviations

The [domain glossary](../../../../../../terms.md) defines `Glyph client`, `programmatic controller`, `provider instance`, and `Programmatic Control transport`.

## Context

Users control Glyph through the standard TUI or an external application using Programmatic Control. A configured provider can require interactive authentication before it can serve model requests.

## Observed problem

A user of Programmatic Control cannot perform interactive provider authentication through that client, although a TUI user can. The ability to regain access to the provider therefore depends on the chosen Glyph client.

For example, stored Codex credentials no longer work and the user needs to sign in again. The TUI user can start browser or device-code sign-in. An external application using Programmatic Control has no corresponding authentication action for its user.

## Affected audience

- Users who control Glyph through an external application and need to authenticate a provider.
- Authors of programmatic clients that provide Glyph management capabilities to those users.

## Evidence

- `api/plugins/ui/v1/model.proto` declares `RetryAuthenticationCommand`, `AuthenticationMethod`, and `AuthorizationRequest` for the existing UI authentication path.
- `host/internal/usecase/host/ui/prepared_operations.go`, `Session.prepareAuthentication`, accepts a retry after authentication failure and invokes sign-in.
- `host/internal/infra/providers/openai/codex/auth.go`, `Driver.SignIn`, selects browser or device-code authentication.
- `api/programmatic/v1/programmatic.proto`, `ControllerRequest`, `HostProgress`, and `HostCompleted`, contain no corresponding authentication operation.

## Impact

HIGH for programmatic use. A user cannot restore provider access through the application they use to control Glyph, even though the interactive client provides that capability.

## Current state

The UI authentication retry applies to the active provider and reports authorization progress through the UI Plugin Contract. Programmatic Control exposes model and session management but no interactive authentication action. Stored valid credentials can still support ordinary model use; the problem arises when authentication needs user participation.

## Desired state

Users can obtain or restore provider access through their chosen Glyph client. Authors of programmatic clients can offer the same provider-authentication capability available to interactive users.

## Problem boundary

The problem covers users who need to authenticate a configured provider through their chosen Glyph client, including the existing Codex case.

Terminal rendering and editor behavior are outside management-capability parity. General extension interaction with users has a separate problem statement in the [parent phase](../ticket.md).

## Assumptions

None.

## Open questions

None.
