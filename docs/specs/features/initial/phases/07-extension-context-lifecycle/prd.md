# Idea: PHS-07 Extension Context and Lifecycle

## Definitions

The [phase terminology](terms.md) identifies the terms used by this phase. The [Domain Glossary](../../../../../terms.md) defines their meanings.

## Context and Problem

The [Problem Statement](problem.md) defines the partial PHS-07 baseline and the missing public selection capabilities.

## Goal

Give isolated extension processes UI-neutral access to the active session, configured models, lifecycle events, active model selection, and persisted extension entries through protobuf contracts.

## Scenarios

- SCN-01: An extension receives `agent_start` with the current extension context, makes a configured-model request, and persists the result in the active session.
- SCN-02: A Glyph client or extension requests a new model selection. Multiple extensions transform or reject the selection in order before Host commits it atomically.
- SCN-03: A Glyph client receives extension messages and their client visibility through a protobuf contract independently of the selected UI.
- SCN-04: After restart or session replacement, an extension reconstructs its state from persisted entries on the active branch through the public Extension Contract.

## Scope and Non-Scope

In scope:

- Extension context, configured model and provider catalogues, configured-model requests, lifecycle events, model selection, extension entries, and public recovery of persisted extension state.

Out of scope:

- Prompt, context, input, provider, and tool middleware.
- Context compaction and retry control.
- Extension commands, command-initiated session control, interactions, notifications, and provider implementations.
- UI-specific presentation.

## Requirements

- FRQ-01: An extension context shall be bound to one extension runtime instance and one active session. Replacement of either binding shall invalidate the preceding context. An operation through an invalidated context shall fail without committing session or selection changes.
  - Origin: `source`, [ticket](ticket.md) ACC-02.
  - Goal: Prevent an operation from applying to another active session or extension runtime.
  - Goal achievement: Full. Every context operation checks both bindings.
- FRQ-02: An extension context shall provide the extension ID, the active session ID, the bound extension runtime instance identifier, cancellation, cwd, the configured model catalogue, and the configured provider catalogue. The catalogues shall contain no credentials.
  - Origin: `source`, [ticket](ticket.md) ACC-03 and [product requirements](../../prd.md#extension-capabilities).
  - Goal: Give an extension the current environment and available provider-neutral model data.
  - Goal achievement: Full. The extension receives the required data without secret values.
- FRQ-03: An extension shall be able to make a configured-model request through an explicitly selected provider, model, and reasoning choice. The result shall contain the final response and all visible reasoning content. The request shall not change the active model selection.
  - Origin: `source`, [ticket](ticket.md) ACC-04.
  - Goal: Support model-assisted extension behavior without changing the user's conversation selection.
  - Goal achievement: Full. The request uses an independent selection and returns the final response and visible reasoning content.
- FRQ-04: Extensions shall receive agent, turn, message, tool-execution, model-selection, and reasoning-selection lifecycle events with an extension context bound to the extension runtime and active session at event delivery. Glyph client delivery of each Agent Core event shall precede observer delivery in registration order. An observer error shall stop later observers and the operation invoking them under the [extension failure rule](../../prd.md#extension-failure-rule). Glyph shall report the complete error text without retrying the observer or rolling back committed state.
  - Origin: `source`, [product requirements](../../prd.md#extension-capabilities) and [ticket](ticket.md) ACC-05.
  - Goal: Support lifecycle-aware extension behavior independently of the connected Glyph client.
  - Goal achievement: Full. The required lifecycle groups are available through the Extension Contract.
- FRQ-05: A Glyph client or extension shall be able to request a model or reasoning-choice change. Selection handlers shall receive the immutable original target selection and the current target selection in registration order. Each handler shall preserve, replace, or reject the current target selection.
  - Origin: `source`, [product requirements](../../prd.md#extension-capabilities) and [ticket](ticket.md) ACC-06.
  - Goal: Make multiple selection handlers compose predictably.
  - Goal achievement: Full. Handler order, input state, and allowed actions are defined.
- FRQ-06: Host shall validate the final provider, model, reasoning choice, and credentials before one atomic model-selection commit. Rejection, cancellation, a stale context, or final validation failure before commit shall preserve the active selection and emit no selection event. Observer or delivery errors after commit shall not roll back the selection. Host shall emit events only for changed values after commit, with reasoning selection before model selection when both change.
  - Origin: `source`, [ticket](ticket.md) ACC-07 and [selection semantics](solution.md#active-model-selection).
  - Goal: Prevent a partially applied model selection.
  - Goal achievement: Full. Final validation precedes one state commit and its event.
- FRQ-07: A handler error, invalid handler action, or runtime crash before commit shall fail the selection operation under the [extension failure rule](../../prd.md#extension-failure-rule). Host shall preserve the active selection, report the complete error text, invoke no later handler, and perform no retry. An explicit rejection shall stop the handler chain.
  - Origin: `source`, [product handler semantics](../../prd.md#extension-failure-rule) and [runtime failure semantics](../../prd.md#environment-reload).
  - Goal: Stop selection when an extension on its execution path fails.
  - Goal achievement: Full. Failed handler processing cannot commit a selection or continue silently.
- FRQ-08: An extension shall be able to append a model-hidden extension entry or model-visible extension message at the active position, including the implicit root. Both entry types shall survive application restart.
  - Origin: `source`, [product session requirements](../../prd.md#context-and-sessions) and [ticket](ticket.md) ACC-08.
  - Goal: Support durable extension state and durable model context.
  - Goal achievement: Full. Both entry types persist on the active session branch.
- FRQ-08.1: An extension shall be able to obtain its persisted model-hidden entries and model-visible messages for the bound session's active branch through the public Extension Contract. The recovered data shall preserve exact payloads, entry IDs, extension IDs, entry types, parent relationships, and branch order.
  - Origin: `source`, [product requirements](../../prd.md#extension-capabilities) and [ticket](ticket.md) ACC-08.1.
  - Goal: Reconstruct session-backed extension state without depending on Host storage internals.
  - Goal achievement: Full. Recovery exposes the stored data and branch identity required by extension-owned state logic.
- FRQ-08.2: Recovery shall work after application restart, active-session replacement, and branch navigation. A stale extension context shall fail recovery under FRQ-01. Recovery shall neither mutate stored entries nor present entries from an abandoned branch as active-branch state.
  - Origin: `source`, [product requirements](../../prd.md#extension-capabilities) and [ticket](ticket.md) ACC-08.2.
  - Goal: Resume extension behavior from the selected conversation rather than obsolete in-memory state.
  - Goal achievement: Full. Public recovery follows the bound session and selected branch. Environment reload reuses this capability in PHS-16.
- FRQ-09: A model-hidden extension entry shall not enter model context. A model-visible extension message shall enter model context and shall have client visibility set to `visible` or `hidden`.
  - Origin: `source`, [ticket](ticket.md) ACC-08 and ACC-09.
  - Goal: Separate model visibility from ordinary conversation presentation.
  - Goal achievement: Full. Each entry type has defined model-context behavior.
- FRQ-10: Every Glyph client shall receive the content and client visibility of each model-visible extension message through its protobuf contract. A message with `hidden` client visibility shall remain available in the session tree and Programmatic Control but shall be excluded from the ordinary conversation transcript.
  - Origin: `source`, [ticket](ticket.md) ACC-09.
  - Goal: Give every isolated Glyph client the same extension-message semantics.
  - Goal achievement: Full. Host sends one content value and one visibility state through each client contract.
- FRQ-11: When a Glyph client selects a model-visible extension message, its parent shall be the navigation destination and Host shall return the exact message text as next input without starting an agent run. An absent parent shall select the implicit root. Without a branch summary, the destination shall become the active position, with no active leaf at the implicit root. A created branch summary shall become the active leaf under the PHS-05 branch-summarization rules.
  - Origin: `source`, [product session requirements](../../prd.md#context-and-sessions) and [ticket](ticket.md) ACC-11.
  - Goal: Support the same message-resubmission and branch-summarization behavior through every Glyph client.
  - Goal achievement: Full. Host defines the navigation result without depending on a client-specific editor and preserves the existing branch-summarization commit.
- FRQ-12: New operations shall satisfy the shared [Error Semantics](../../prd.md#error-semantics), including closed error-category sets and complete error text through the Extension Contract, UI Plugin Contract, and Programmatic Control.
  - Origin: `source`, [product error semantics](../../prd.md#error-semantics).
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
