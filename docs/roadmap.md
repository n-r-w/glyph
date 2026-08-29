# Roadmap

Glyph is a local, extensible coding agent with a thin provider-neutral Agent Core and Host-managed plugins. The [PRD](specs/features/initial/prd.md) defines product behavior, the [target architecture](specs/features/initial/architecture.md) defines ownership boundaries, and the [delivery plan](specs/features/initial/delivery-plan.md) defines the complete phase order and dependencies.

## PHS-00: Prototype baseline

Status: Completed

Established the executable Host, Agent Core, bundled tools extension, and standard TUI baseline.

Documents:
- [Ticket](specs/features/initial/phases/00-prototype-baseline/ticket.md)
- [Technical Solution](specs/features/initial/phases/00-prototype-baseline/solution.md)

## PHS-01: Complete standard tools

Status: Completed

Completed bounded production behavior for the bundled coding tools and Host tool runtime.

Documents:
- [Ticket](specs/features/initial/phases/01-complete-standard-tools/ticket.md)

## PHS-02: Programmatic Control foundation

Status: Completed

Added the long-lived headless client contract independently of the standard TUI.

Documents:
- [Ticket](specs/features/initial/phases/02-programmatic-control/ticket.md)
- [Technical Solution](specs/features/initial/phases/02-programmatic-control/solution.md)

## PHS-03: Providers, models, and runtime selection

Status: Completed

Added the required built-in providers, reasoning behavior, and runtime model selection.

Documents:
- [Ticket](specs/features/initial/phases/03-providers-models-runtime-selection/ticket.md)
- [Technical Solution](specs/features/initial/phases/03-providers-models-runtime-selection/solution.md)

## PHS-04: Persistent linear sessions

Status: Completed

Added persistent conversations, session accounting, and resume after process restart.

Documents:
- [Ticket](specs/features/initial/phases/04-persistent-linear-sessions/ticket.md)
- [Technical Solution](specs/features/initial/phases/04-persistent-linear-sessions/solution.md)

## PHS-04.1: Model execution capabilities

Status: Current

The requirements are ready. The next work is the Technical Solution, implementation plan, implementation, and live Ornith verification.

Documents:
- [Ticket](specs/features/initial/phases/04.1-model-execution-capabilities/ticket.md)

## PHS-05: Session tree

Status: Planned

Add branch-preserving session navigation and extensible branch summarization.

Documents:
- [Ticket](specs/features/initial/phases/05-session-tree/ticket.md)

## PHS-07: Extension context and lifecycle

Status: Planned

Add session-bound extension contexts, configured-model requests, active-selection control, and lifecycle events.

Documents:
- [Ticket](specs/features/initial/phases/07-extension-context-lifecycle/ticket.md)

## PHS-06: Context compaction and retry control

Status: Planned

Add extensible context compaction and Host-owned retry decision coordination.

Documents:
- [Ticket](specs/features/initial/phases/06-context-compaction-retry-control/ticket.md)

## PHS-08: Prompt, context, input, and provider middleware

Status: Planned

Add ordered model-facing middleware and final input validation against model modalities.

Documents:
- [Ticket](specs/features/initial/phases/08-prompt-context-input-provider-middleware/ticket.md)

## PHS-09: Tool middleware and run control

Status: Planned

Add extension control over tool policy and agent-run continuation.

Documents:
- [Ticket](specs/features/initial/phases/09-tool-middleware-run-control/ticket.md)

## PHS-10: Commands, interaction, notifications, and extension events

Status: Planned

Add extension-defined user actions, interactions, notifications, and extension events.

Documents:
- [Ticket](specs/features/initial/phases/10-commands-interaction-notifications-events/ticket.md)

## PHS-11: Resource contributions

Status: Planned

Add extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.

Documents:
- [Ticket](specs/features/initial/phases/11-resource-contributions/ticket.md)

## PHS-12: Extension-defined providers

Status: Planned

Allow extensions to register complete provider implementations through provider-neutral contracts.

Documents:
- [Ticket](specs/features/initial/phases/12-extension-defined-providers/ticket.md)

## PHS-12.1: Standard TUI transcript rendering and layout

Status: Planned

Add semantic transcript rendering and a stable interaction dock.

Documents:
- [Ticket](specs/features/initial/phases/12.1-standard-tui-rendering-layout/ticket.md)

## PHS-12.2: Standard TUI viewport navigation

Status: Planned

Keep the complete transcript reachable during streaming, search, selection, and resize.

Documents:
- [Ticket](specs/features/initial/phases/12.2-standard-tui-viewport-navigation/ticket.md)

## PHS-12.3: Standard TUI editor and terminal interaction

Status: Planned

Add the complete editor and TUI-owned terminal lifecycle while removing Host terminal recovery and the obsolete UI startup-capability path.

Documents:
- [Ticket](specs/features/initial/phases/12.3-standard-tui-editor-terminal-interaction/ticket.md)

## PHS-13: Standard TUI presentation extensions

Status: Planned

Allow extensions to add passive presentation while the standard TUI retains terminal ownership.

Documents:
- [Ticket](specs/features/initial/phases/13-tui-presentation-extensions/ticket.md)

## PHS-14: Interactive standard TUI extensions

Status: Planned

Add focused extension interaction and editor integration inside the standard TUI.

Documents:
- [Ticket](specs/features/initial/phases/14-interactive-tui-extensions/ticket.md)

## PHS-15: Extension installation and state management

Status: Planned

Add installation, enablement, disablement, update, and removal of compatible extensions without rebuilding Glyph.

Documents:
- [Ticket](specs/features/initial/phases/15-extension-installation-state/ticket.md)

## PHS-16: Environment reload

Status: Planned

Apply environment changes without ending the active session.

Documents:
- [Ticket](specs/features/initial/phases/16-environment-reload/ticket.md)

## PHS-17: Glyph public-behavior traceability

Status: Planned

Provide public-contract evidence for every Glyph-owned behavior group in the PRD.

Documents:
- [Ticket](specs/features/initial/phases/17-reference-scenario-closure/ticket.md)

## PHS-18: Cleanup

Status: Planned

Remove prototype-only restrictions and implementation residue superseded by target behavior.

Documents:
- [Ticket](specs/features/initial/phases/18-cleanup/ticket.md)

## PHS-19: Independent final verification

Status: Planned

Verify the complete PRD through public behavior after cleanup.

Documents:
- [Ticket](specs/features/initial/phases/19-independent-final-verification/ticket.md)
