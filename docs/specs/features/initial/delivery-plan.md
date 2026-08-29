# Delivery Plan: Glyph Initial Product

## Phases

### PHS-00. Prototype baseline
- Owner: Glyph Host, Agent Core, bundled tools extension, and standard TUI
- Result: Preserve an executable baseline before target behavior changes.
- Dependencies: None
- Blockers: None
- Documents: [Ticket](phases/00-prototype-baseline/ticket.md), [baseline PRD](phases/00-prototype-baseline/baseline-prd.md), [Technical Solution](phases/00-prototype-baseline/technical-solution.md)

### PHS-01. Complete standard tools
- Owner: Bundled tools extension and Host tool runtime
- Result: Deliver bounded, production-usable `read`, `write`, `edit`, `grep`, `find`, `ls`, and `bash` behavior instead of only completing the tool-name catalogue.
- Dependencies: PHS-00
- Blockers: None
- Documents: [Ticket](phases/01-complete-standard-tools/ticket.md)

### PHS-02. Programmatic Control foundation
- Owner: Glyph Host Programmatic Control
- Result: Provide a long-lived headless client contract independent of the standard TUI.
- Dependencies: PHS-01
- Blockers: None
- Documents: [Ticket](phases/02-programmatic-control/ticket.md), [Technical Solution](phases/02-programmatic-control/technical-solution.md)

### PHS-03. Providers, models, and runtime selection
- Owner: Host provider catalogue, provider adapters, and Glyph clients
- Result: Support the required built-in providers and model selection without ending the session.
- Dependencies: PHS-02
- Blockers: None
- Documents: [Ticket](phases/03-providers-models-runtime-selection/ticket.md), [Technical Solution](phases/03-providers-models-runtime-selection/technical-solution.md)

### PHS-04. Persistent linear sessions
- Owner: Session domain, session persistence, and Glyph clients
- Result: Persist conversations and resume them after process restart.
- Dependencies: PHS-03
- Blockers: None
- Documents: [Ticket](phases/04-persistent-linear-sessions/ticket.md), [Technical Solution](phases/04-persistent-linear-sessions/technical-solution.md)

### PHS-04.1. Model execution capabilities
- Owner: Settings, provider-neutral model descriptor, Host model catalogue, and Programmatic Control
- Result: Expose strict model input modalities, context window, and maximum output tokens.
- Dependencies: PHS-04
- Blockers: None
- Documents: [Ticket](phases/04.1-model-execution-capabilities/ticket.md)

### PHS-05. Session tree
- Owner: Host session-tree and extension orchestration, session persistence, and standard TUI
- Result: Support branch-preserving session navigation with extensible branch summarization.
- Dependencies: PHS-04
- Blockers: None
- Documents: [Ticket](phases/05-session-tree/ticket.md)

### PHS-07. Extension context and lifecycle
- Owner: Host extension runtime, model access, active-selection orchestration, and session use cases
- Result: Give extension processes session-bound access, configured-model requests, active-selection control, and lifecycle events without terminal dependencies.
- Dependencies: PHS-05, PHS-04.1
- Blockers: None
- Documents: [Ticket](phases/07-extension-context-lifecycle/ticket.md)

### PHS-06. Context compaction and retry control
- Owner: Host compaction, model-execution, retry, extension, and session orchestration
- Result: Keep long sessions usable within model context limits while extensions compose with or replace compaction and retry decisions.
- Dependencies: PHS-07, PHS-04.1
- Blockers: None
- Documents: [Ticket](phases/06-context-compaction-retry-control/ticket.md)

### PHS-08. Prompt, context, input, and provider middleware
- Owner: Host middleware orchestration and Agent Core consumer-owned ports
- Result: Allow extensions to change model-facing input through ordered generic extension points and validate final input against model modalities.
- Dependencies: PHS-06, PHS-04.1
- Blockers: None
- Documents: [Ticket](phases/08-prompt-context-input-provider-middleware/ticket.md)

### PHS-09. Tool middleware and run control
- Owner: Agent Core tool loop and Host extension runtime
- Result: Allow extensions to control tool policy and agent-run continuation.
- Dependencies: PHS-08
- Blockers: None
- Documents: [Ticket](phases/09-tool-middleware-run-control/ticket.md)

### PHS-10. Commands, interaction, notifications, and extension events
- Owner: Host command, interaction, notification, and extension-event use cases
- Result: Let extensions expose user actions and request Host or client behavior.
- Dependencies: PHS-09
- Blockers: None
- Documents: [Ticket](phases/10-commands-interaction-notifications-events/ticket.md)

### PHS-11. Resource contributions
- Owner: Host resource registry and bundled resource extension
- Result: Collect extension-owned skills, prompt templates, and context files without adding resource concepts to Agent Core.
- Dependencies: PHS-10
- Blockers: None
- Documents: [Ticket](phases/11-resource-contributions/ticket.md)

### PHS-12. Extension-defined providers
- Owner: Host provider registry and extension provider runtime
- Result: Allow an installed extension to add and remove complete model provider implementations with provider-neutral execution capabilities.
- Dependencies: PHS-11, PHS-04.1
- Blockers: None
- Documents: [Ticket](phases/12-extension-defined-providers/ticket.md)

### PHS-12.1. Standard TUI transcript rendering and layout
- Owner: Standard TUI presentation
- Result: Render semantic transcript blocks and keep a stable interaction dock visible.
- Dependencies: PHS-12
- Blockers: None
- Documents: [Ticket](phases/12.1-standard-tui-rendering-layout/ticket.md)

### PHS-12.2. Standard TUI viewport navigation
- Owner: Standard TUI viewport and input routing
- Result: Keep the complete transcript reachable during streaming, search, selection, and resize.
- Dependencies: PHS-12.1
- Blockers: None
- Documents: [Ticket](phases/12.2-standard-tui-viewport-navigation/ticket.md)

### PHS-12.3. Standard TUI editor and terminal interaction
- Owner: Standard TUI editor and terminal lifecycle
- Result: Provide multiline editing, completion, attachments, queued input, selectors, and TUI-owned terminal lifecycle while removing Host recovery and the obsolete UI startup-capability path.
- Dependencies: PHS-12.2
- Blockers: None
- Documents: [Ticket](phases/12.3-standard-tui-editor-terminal-interaction/ticket.md)

### PHS-13. Standard TUI presentation extensions
- Owner: Standard TUI extension presentation
- Result: Let extensions add passive presentation to the standard TUI while the TUI retains terminal ownership.
- Dependencies: PHS-12.3
- Blockers: None
- Documents: [Ticket](phases/13-tui-presentation-extensions/ticket.md)

### PHS-14. Interactive standard TUI extensions
- Owner: Standard TUI extension interaction
- Result: Support focused extension interaction and editor integration inside the standard TUI.
- Dependencies: PHS-13
- Blockers: None
- Documents: [Ticket](phases/14-interactive-tui-extensions/ticket.md)

### PHS-15. Extension installation and state management
- Owner: Host extension catalogue and package lifecycle
- Result: Manage compatible extensions without rebuilding Glyph.
- Dependencies: PHS-14
- Blockers: None
- Documents: [Ticket](phases/15-extension-installation-state/ticket.md)

### PHS-16. Environment reload
- Owner: Host environment orchestration
- Result: Apply environment changes without ending the active session.
- Dependencies: PHS-15
- Blockers: None
- Documents: [Ticket](phases/16-environment-reload/ticket.md)

### PHS-17. Glyph public-behavior traceability
- Owner: Cross-component public contract verification
- Result: Demonstrate that every Glyph-owned public behavior group in [`prd.md`](prd.md) has traceable public-contract evidence.
- Dependencies: PHS-16
- Blockers: None
- Documents: [Ticket](phases/17-reference-scenario-closure/ticket.md)

### PHS-18. Cleanup
- Owner: All component owners
- Result: Remove prototype-only restrictions and implementation residue superseded by target behavior.
- Dependencies: PHS-17
- Blockers: None
- Documents: [Ticket](phases/18-cleanup/ticket.md)

### PHS-19. Independent final verification
- Owner: Independent product verification
- Result: Verify the complete PRD through public behavior after cleanup.
- Dependencies: PHS-18
- Blockers: None
- Documents: [Ticket](phases/19-independent-final-verification/ticket.md)

## Execution Order

1. PHS-00 - Prototype baseline
2. PHS-01 - Complete standard tools
3. PHS-02 - Programmatic Control foundation
4. PHS-03 - Providers, models, and runtime selection
5. PHS-04 - Persistent linear sessions
6. PHS-04.1 - Model execution capabilities
7. PHS-05 - Session tree
8. PHS-07 - Extension context and lifecycle
9. PHS-06 - Context compaction and retry control
10. PHS-08 - Prompt, context, input, and provider middleware
11. PHS-09 - Tool middleware and run control
12. PHS-10 - Commands, interaction, notifications, and extension events
13. PHS-11 - Resource contributions
14. PHS-12 - Extension-defined providers
15. PHS-12.1 - Standard TUI transcript rendering and layout
16. PHS-12.2 - Standard TUI viewport navigation
17. PHS-12.3 - Standard TUI editor and terminal interaction
18. PHS-13 - Standard TUI presentation extensions
19. PHS-14 - Interactive standard TUI extensions
20. PHS-15 - Extension installation and state management
21. PHS-16 - Environment reload
22. PHS-17 - Glyph public-behavior traceability
23. PHS-18 - Cleanup
24. PHS-19 - Independent final verification

## References

- [`prd.md`](prd.md) - target product requirements and Glyph public-behavior traceability.
- [`architecture.md`](architecture.md) - normative target component, dependency, contract, and package boundaries.
- [Prototype baseline PRD](phases/00-prototype-baseline/baseline-prd.md) - implemented prototype scope.
- [Prototype Technical Solution](phases/00-prototype-baseline/technical-solution.md) - prototype architecture and implementation baseline.
- [`standard-tui.md`](standard-tui.md) - standard terminal-agent interaction requirements.
- [`pi-extension-surface.md`](../../../artefacts/pi-extension-surface.md) - researched extension capability surface.
