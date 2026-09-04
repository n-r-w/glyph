# Idea: PHS-07 Extension Context and Lifecycle

## Definitions

The [phase terminology](terms.md) identifies the terms used by this phase. The [Domain Glossary](../../../../../terms.md) defines their meanings.

## Context and Problem

The [Problem Statement](problem.md) defines the missing public extension access to session-bound Host capabilities.

## Goal

Give isolated extension processes UI-neutral access to the active session, configured models, lifecycle events, active model selection, and persisted extension entries through protobuf contracts.

## Scenarios

- SCN-01: An extension receives `agent_start` with the current extension context, makes a configured-model request, and persists the result in the active session.
- SCN-02: A Glyph client or extension requests a new model selection. Multiple extensions transform or reject the selection in order before Host commits it atomically.
- SCN-03: A Glyph client receives extension messages and their client visibility through a protobuf contract independently of the selected UI.

## Scope and Non-Scope

In scope:

- Extension context, configured model and provider catalogues, configured-model requests, lifecycle events, model selection, and extension entries.

Out of scope:

- Prompt, context, input, provider, and tool middleware.
- Context compaction and retry control.
- Extension commands, interactions, notifications, and provider implementations.
- UI-specific presentation.

## Requirements

- FRQ-01: An extension context shall be bound to one extension runtime instance and one active session. After replacement of either binding, every operation through the preceding context shall fail.
  - Goal: Prevent an operation from applying to another active session or extension runtime.
  - Goal achievement: Full. Every context operation checks both bindings.
- FRQ-02: An extension context shall provide the extension ID, the active session ID, the bound extension runtime instance identifier, cancellation, cwd, the configured model catalogue, and the configured provider catalogue. The catalogues shall contain no credentials.
  - Goal: Give an extension the current environment and available provider-neutral model data.
  - Goal achievement: Full. The extension receives the required data without secret values.
- FRQ-03: An extension shall be able to make a configured-model request through an explicitly selected provider, model, and reasoning choice. The result shall contain the final response and all visible reasoning content. The request shall not change the active model selection.
  - Goal: Support model-assisted extension behavior without changing the user's conversation selection.
  - Goal achievement: Full. The request uses an independent selection and returns the final response and visible reasoning content.
- FRQ-04: Extensions shall receive agent, turn, message, tool-execution, model-selection, and reasoning-selection lifecycle events with an extension context bound to the extension runtime and active session at event delivery.
  - Goal: Support lifecycle-aware extension behavior independently of the connected Glyph client.
  - Goal achievement: Full. The required lifecycle groups are available through the Extension Contract.
- FRQ-05: A Glyph client or extension shall be able to request a model selection. Selection handlers shall receive the immutable original target selection and the current target selection in registration order. Each handler shall preserve, replace, or reject the current target selection.
  - Goal: Make multiple selection handlers compose predictably.
  - Goal achievement: Full. Handler order, input state, and allowed actions are defined.
- FRQ-06: Host shall validate the final provider, model, reasoning choice, and credentials before one atomic model-selection commit. An error or rejection shall preserve the active model selection. Host shall emit the corresponding model-selection or reasoning-selection lifecycle event only after commit.
  - Goal: Prevent a partially applied model selection.
  - Goal achievement: Full. Final validation precedes one state commit and its event.
- FRQ-07: An ordinary handler error or invalid handler action shall preserve the current target selection received by that handler, shall not stop later handlers, and shall not deactivate the extension. An explicit rejection shall stop the handler chain.
  - Goal: Isolate one extension error from other extensions and the active model selection.
  - Goal achievement: Full. Ordinary errors and explicit rejection have separate outcomes.
- FRQ-08: An extension shall be able to append a model-hidden extension entry or model-visible extension message at the active leaf. Both entry types shall survive application restart.
  - Goal: Support durable extension state and durable model context.
  - Goal achievement: Full. Both entry types persist on the active session branch.
- FRQ-09: A model-hidden extension entry shall not enter model context. A model-visible extension message shall enter model context and shall have client visibility set to `visible` or `hidden`.
  - Goal: Separate model visibility from ordinary conversation presentation.
  - Goal achievement: Full. Each entry type has defined model-context behavior.
- FRQ-10: Every Glyph client shall receive the content and client visibility of each model-visible extension message through its protobuf contract. A message with `hidden` client visibility shall remain available in the session tree and Programmatic Control but shall be excluded from the ordinary conversation transcript.
  - Goal: Give every isolated Glyph client the same extension-message semantics.
  - Goal achievement: Full. Host sends one content value and one visibility state through each client contract.
- FRQ-11: When a Glyph client selects a model-visible extension message, its parent shall be the navigation destination and Host shall return the exact message text as next input. Without a branch summary, the navigation destination shall become the active leaf. With a branch summary, the PHS-05 branch-summarization rules shall determine the committed active leaf. Host shall not start an agent run automatically.
  - Goal: Support the same message-resubmission and branch-summarization behavior through every Glyph client.
  - Goal achievement: Full. Host defines the navigation result without depending on a client-specific editor and preserves the existing branch-summarization commit.
- FRQ-12: New operations shall satisfy the shared [Error Semantics](../../prd.md#error-semantics), including closed error-category sets and complete error text through the Extension Contract, UI Plugin Contract, and Programmatic Control.
  - Goal: Preserve diagnosable and equivalent public failures.
  - Goal achievement: Full. Every affected protobuf contract uses the shared Glyph error semantics.

## Open Questions

None.

## Technical Supplement

No technical design is selected by this PRD.

## References

- [Problem Statement](problem.md)
- [Phase terminology](terms.md)
- [Domain Glossary](../../../../../terms.md)
- [Delivery plan](../../delivery-plan.md)
- [Target architecture](../../architecture.md)
