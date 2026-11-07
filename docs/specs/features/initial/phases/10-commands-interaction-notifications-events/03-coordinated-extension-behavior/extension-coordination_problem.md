# Problem statement: PHS-10.3 - Coordinated behavior of an extension set

## Key definitions and abbreviations

The [domain glossary](../../../../../../terms.md) defines `extension`, `extension contract`, and `extension lifecycle event`.

## Context

An author can provide several extensions that participate in one user workflow. The next action of one extension can depend on a work result or state change owned by another extension.

## Observed problem

An author cannot connect specialized extension behavior outside the capability-specific interactions already defined by Glyph. The public operations do not cover an extension-owned work result or state change that another extension needs for their shared scenario.

For example, one extension manages work stages and another performs a review. The stage owner needs the review outcome to determine how the user's work can continue. These are application-specific facts of the extension set.

## Affected audience

- Authors of extension sets whose components depend on each other's work.
- Users who expect the set to operate as one coherent workflow.

## Evidence

- `api/plugins/extension/v1/extension.proto`, `RegisterResponse`, `HostRequest`, and `ExtensionRequest`, declare tools, typed handlers, and extension-to-Host operations. They include capability-specific invocation for compaction participants, but no general operation for conveying an extension-owned result or state change for another scenario.
- `api/plugins/extension/v1/lifecycle.proto` describes Glyph lifecycle observations. Those observations do not carry the extension-specific review outcome in the example above.
- `host/internal/usecase/host/events/service.go`, `Dispatcher.Deliver`, delivers Agent Core events to the client and then invokes lifecycle observers. That path observes Glyph activity.

## Impact

HIGH for extension-set composition. Authors cannot connect dependent extension behavior through the public contract, so users cannot receive the complete coordinated scenario from the set.

## Current state

Extensions can react to defined Glyph lifecycle events and participate in capability-specific interactions, such as compaction. The review outcome needed by the stage owner in the example is outside those defined interactions.

## Desired state

Authors can provide extension sets whose components work coherently toward one user goal. Users experience the intended continuation of the shared workflow when one component finishes or changes state.

## Problem boundary

The problem concerns extension sets whose components need each other's results or state to continue a shared user scenario.

Built-in workflow or child-agent semantics are outside this problem. Direct interaction with users and extension-command invocation have separate problem statements in the [parent phase](../ticket.md).

## Assumptions

None.

## Open questions

None.
