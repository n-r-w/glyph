# Roadmap

Glyph is a local, extensible coding agent with a thin provider-neutral Agent Core and Host-managed plugins. The [PRD](specs/features/initial/prd.md) defines product behavior, the [target architecture](specs/features/initial/architecture.md) defines ownership boundaries, and the [delivery plan](specs/features/initial/delivery-plan/index.md) defines the complete phase order and dependencies.

## PHS-00: Prototype baseline

Status: Completed

Established the executable Host, Agent Core, bundled tools extension, and standard TUI baseline.

Documents:
- [Requirements](specs/features/initial/delivery-plan/00-prototype-baseline.md)
- [Technical Solution](specs/features/initial/prototype-technical-solution.md)

## PHS-01: Complete standard tools

Status: Completed

Completed bounded production behavior for the bundled coding tools and Host tool runtime.

Documents:
- [Requirements](specs/features/initial/delivery-plan/01-complete-standard-tools.md)

## PHS-02: Programmatic Control foundation

Status: Completed

Added the long-lived headless client contract independently of the standard TUI.

Documents:
- [Requirements](specs/features/initial/delivery-plan/02-programmatic-control-foundation.md)
- [Technical Solution](specs/features/initial/phs-02-programmatic-control_solution.md)

## PHS-03: Providers, models, and runtime selection

Status: Completed

Added the required built-in providers, reasoning behavior, and runtime model selection.

Documents:
- [Requirements](specs/features/initial/delivery-plan/03-providers-models-runtime-selection.md)
- [Technical Solution](specs/features/initial/phs-03-providers-models-runtime-selection_solution.md)

## PHS-04: Persistent linear sessions

Status: Completed

Added persistent conversations, session accounting, and resume after process restart.

Documents:
- [Requirements](specs/features/initial/delivery-plan/04-persistent-linear-sessions.md)
- [Technical Solution](specs/features/initial/phs-04-persistent-linear-sessions_solution.md)

## PHS-04.1: Model execution capabilities

Status: Current

The requirements are ready. The next work is the Technical Solution, implementation plan, implementation, and live Ornith verification.

Documents:
- [Requirements](specs/features/initial/delivery-plan/04.1-model-execution-capabilities.md)

## PHS-05: Session tree

Status: Planned

Add branch-preserving session navigation and extensible branch summarization.

Documents:
- [Requirements](specs/features/initial/delivery-plan/05-session-tree.md)

## PHS-07: Extension context and lifecycle

Status: Planned

Add session-bound extension contexts, configured-model requests, active-selection control, and lifecycle events.

Documents:
- [Requirements](specs/features/initial/delivery-plan/07-extension-context-lifecycle.md)

## PHS-06: Context compaction and retry control

Status: Planned

Add extensible context compaction and Host-owned retry decision coordination.

Documents:
- [Requirements](specs/features/initial/delivery-plan/06-context-compaction-retry-control.md)

## PHS-08: Prompt, context, input, and provider middleware

Status: Planned

Add ordered model-facing middleware and final input validation against model modalities.

Documents:
- [Requirements](specs/features/initial/delivery-plan/08-prompt-context-input-provider-middleware.md)

## PHS-09: Tool middleware and run control

Status: Planned

Add extension control over tool policy and agent-run continuation.

Documents:
- [Requirements](specs/features/initial/delivery-plan/09-tool-middleware-run-control.md)

## PHS-10: Commands, interaction, notifications, and extension events

Status: Planned

Add extension-defined user actions, interactions, notifications, and extension events.

Documents:
- [Requirements](specs/features/initial/delivery-plan/10-commands-interaction-notifications-events.md)

## PHS-11: Resource contributions

Status: Planned

Add extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.

Documents:
- [Requirements](specs/features/initial/delivery-plan/11-resource-contributions.md)

## PHS-12: Extension-defined providers

Status: Planned

Allow extensions to register complete provider implementations through provider-neutral contracts.

Documents:
- [Requirements](specs/features/initial/delivery-plan/12-extension-defined-providers.md)

## PHS-12.1: Standard TUI transcript rendering and layout

Status: Planned

Add semantic transcript rendering and a stable interaction dock.

Documents:
- [Requirements](specs/features/initial/delivery-plan/12.1-standard-tui-rendering-layout.md)

## PHS-12.2: Standard TUI viewport navigation

Status: Planned

Keep the complete transcript reachable during streaming, search, selection, and resize.

Documents:
- [Requirements](specs/features/initial/delivery-plan/12.2-standard-tui-viewport-navigation.md)

## PHS-12.3: Standard TUI editor and terminal interaction

Status: Planned

Add the complete editor and TUI-owned terminal lifecycle while removing Host terminal recovery and the obsolete UI startup-capability path.

Documents:
- [Requirements](specs/features/initial/delivery-plan/12.3-standard-tui-editor-terminal-interaction.md)

## PHS-13: Standard TUI presentation extensions

Status: Planned

Allow extensions to add passive presentation while the standard TUI retains terminal ownership.

Documents:
- [Requirements](specs/features/initial/delivery-plan/13-tui-presentation-extensions.md)

## PHS-14: Interactive standard TUI extensions

Status: Planned

Add focused extension interaction and editor integration inside the standard TUI.

Documents:
- [Requirements](specs/features/initial/delivery-plan/14-interactive-tui-extensions.md)

## PHS-15: Extension installation and state management

Status: Planned

Add installation, enablement, disablement, update, and removal of compatible extensions without rebuilding Glyph.

Documents:
- [Requirements](specs/features/initial/delivery-plan/15-extension-installation-state.md)

## PHS-16: Environment reload

Status: Planned

Apply environment changes without ending the active session.

Documents:
- [Requirements](specs/features/initial/delivery-plan/16-environment-reload.md)

## PHS-17: Glyph public-behavior traceability

Status: Planned

Provide public-contract evidence for every Glyph-owned behavior group in the PRD.

Documents:
- [Requirements](specs/features/initial/delivery-plan/17-reference-scenario-closure.md)

## PHS-18: Cleanup

Status: Planned

Remove prototype-only restrictions and implementation residue superseded by target behavior.

Documents:
- [Requirements](specs/features/initial/delivery-plan/18-cleanup.md)

## PHS-19: Independent final verification

Status: Planned

Verify the complete PRD through public behavior after cleanup.

Documents:
- [Requirements](specs/features/initial/delivery-plan/19-independent-final-verification.md)
