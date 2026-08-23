# Tickets Index: Glyph Initial Product

## Tickets List

### PHS-00. Prototype baseline
- Owner: Glyph Host, Agent Core, bundled tools extension, and standard TUI
- Result: Preserve an executable baseline before target behavior changes.
- Dependencies: None
- Blockers: None
- File: [00-prototype-baseline.md](00-prototype-baseline.md)

### PHS-01. Complete standard tools
- Owner: Bundled tools extension and Host tool runtime
- Result: Deliver bounded, production-usable `read`, `write`, `edit`, `grep`, `find`, `ls`, and `bash` behavior instead of only completing the tool-name catalogue.
- Dependencies: PHS-00
- Blockers: None
- File: [01-complete-standard-tools.md](01-complete-standard-tools.md)

### PHS-02. Programmatic Control foundation
- Owner: Glyph Host Programmatic Control
- Result: Provide a long-lived headless client contract independent of the standard TUI.
- Dependencies: PHS-01
- Blockers: None
- File: [02-programmatic-control-foundation.md](02-programmatic-control-foundation.md)

### PHS-03. Providers, models, and runtime selection
- Owner: Host provider catalogue, provider adapters, and Glyph clients
- Result: Support the required built-in providers and model selection without ending the session.
- Dependencies: PHS-02
- Blockers: None
- File: [03-providers-models-runtime-selection.md](03-providers-models-runtime-selection.md)

### PHS-04. Persistent linear sessions
- Owner: Session domain, session persistence, and Glyph clients
- Result: Persist conversations and resume them after process restart.
- Dependencies: PHS-03
- Blockers: None
- File: [04-persistent-linear-sessions.md](04-persistent-linear-sessions.md)

### PHS-05. Session tree
- Owner: Session use cases and standard TUI
- Result: Support branch-preserving session navigation.
- Dependencies: PHS-04
- Blockers: None
- File: [05-session-tree.md](05-session-tree.md)

### PHS-06. Context compaction and retry control
- Owner: Agent Core compaction and Host control use cases
- Result: Keep long sessions usable within model context limits.
- Dependencies: PHS-05
- Blockers: None
- File: [06-context-compaction-retry-control.md](06-context-compaction-retry-control.md)

### PHS-07. Extension context and lifecycle
- Owner: Host extension runtime and session use cases
- Result: Give extension processes session-bound access and lifecycle events without terminal dependencies.
- Dependencies: PHS-06
- Blockers: None
- File: [07-extension-context-lifecycle.md](07-extension-context-lifecycle.md)

### PHS-08. Prompt, context, input, and provider middleware
- Owner: Agent Core extension-point dispatcher and Host extension runtime
- Result: Allow extensions to change model-facing input through ordered generic extension points.
- Dependencies: PHS-07
- Blockers: None
- File: [08-prompt-context-input-provider-middleware.md](08-prompt-context-input-provider-middleware.md)

### PHS-09. Tool middleware and run control
- Owner: Agent Core tool loop and Host extension runtime
- Result: Allow extensions to control tool policy and agent-run continuation.
- Dependencies: PHS-08
- Blockers: None
- File: [09-tool-middleware-run-control.md](09-tool-middleware-run-control.md)

### PHS-10. Commands, interaction, notifications, and extension events
- Owner: Host command, interaction, notification, and model-access use cases
- Result: Let extensions expose user actions and request Host or client behavior.
- Dependencies: PHS-09
- Blockers: None
- File: [10-commands-interaction-notifications-events.md](10-commands-interaction-notifications-events.md)

### PHS-11. Resource contributions
- Owner: Host resource registry and bundled resource extension
- Result: Collect extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.
- Dependencies: PHS-10
- Blockers: None
- File: [11-resource-contributions.md](11-resource-contributions.md)

### PHS-12. Extension-defined providers
- Owner: Host provider registry and extension provider runtime
- Result: Allow an installed extension to add and remove complete model provider implementations.
- Dependencies: PHS-11
- Blockers: None
- File: [12-extension-defined-providers.md](12-extension-defined-providers.md)

### PHS-13. Standard TUI presentation extensions
- Owner: Standard TUI extension presentation
- Result: Let extensions add passive presentation to the standard TUI while the TUI retains terminal ownership.
- Dependencies: PHS-12
- Blockers: None
- File: [13-tui-presentation-extensions.md](13-tui-presentation-extensions.md)

### PHS-14. Interactive standard TUI extensions
- Owner: Standard TUI extension interaction
- Result: Support focused extension interaction and editor integration inside the standard TUI.
- Dependencies: PHS-13
- Blockers: None
- File: [14-interactive-tui-extensions.md](14-interactive-tui-extensions.md)

### PHS-15. Extension installation and state management
- Owner: Host extension catalogue and package lifecycle
- Result: Manage compatible extensions without rebuilding Glyph.
- Dependencies: PHS-14
- Blockers: None
- File: [15-extension-installation-state.md](15-extension-installation-state.md)

### PHS-16. Environment reload
- Owner: Host environment orchestration
- Result: Apply environment changes without ending the active session.
- Dependencies: PHS-15
- Blockers: None
- File: [16-environment-reload.md](16-environment-reload.md)

### PHS-17. Reference scenario closure
- Owner: Cross-component public contract verification
- Result: Demonstrate that generic Glyph contracts cover all 20 reference entry points listed in [`prd.md`](../prd.md).
- Dependencies: PHS-16
- Blockers: None
- File: [17-reference-scenario-closure.md](17-reference-scenario-closure.md)

### PHS-18. Cleanup
- Owner: All component owners
- Result: Remove prototype-only restrictions and implementation residue superseded by target behavior.
- Dependencies: PHS-17
- Blockers: None
- File: [18-cleanup.md](18-cleanup.md)

### PHS-19. Independent final verification
- Owner: Independent product verification
- Result: Verify the complete PRD through public behavior after cleanup.
- Dependencies: PHS-18
- Blockers: None
- File: [19-independent-final-verification.md](19-independent-final-verification.md)

## Execution Order

1. PHS-00 - Prototype baseline
2. PHS-01 - Complete standard tools
3. PHS-02 - Programmatic Control foundation
4. PHS-03 - Providers, models, and runtime selection
5. PHS-04 - Persistent linear sessions
6. PHS-05 - Session tree
7. PHS-06 - Context compaction and retry control
8. PHS-07 - Extension context and lifecycle
9. PHS-08 - Prompt, context, input, and provider middleware
10. PHS-09 - Tool middleware and run control
11. PHS-10 - Commands, interaction, notifications, and extension events
12. PHS-11 - Resource contributions
13. PHS-12 - Extension-defined providers
14. PHS-13 - Standard TUI presentation extensions
15. PHS-14 - Interactive standard TUI extensions
16. PHS-15 - Extension installation and state management
17. PHS-16 - Environment reload
18. PHS-17 - Reference scenario closure
19. PHS-18 - Cleanup
20. PHS-19 - Independent final verification

## References

- [`prd.md`](../prd.md) - target product requirements and reference scenario coverage.
- [`prototype-prd.md`](../prototype-prd.md) - implemented prototype scope.
- [`prototype-technical-solution.md`](../prototype-technical-solution.md) - prototype architecture and implementation baseline.
- [`pi-extension-surface.md`](../../../../artefacts/pi-extension-surface.md) - researched extension capability surface.
