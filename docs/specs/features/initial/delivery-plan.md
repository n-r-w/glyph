# Delivery Plan: Glyph Initial Product

## Phases

### PHS-00. Prototype baseline
- Dependencies: None
- Ticket: [Ticket](phases/00-prototype-baseline/ticket.md)

### PHS-01. Complete standard tools
- Dependencies: PHS-00
- Ticket: [Ticket](phases/01-complete-standard-tools/ticket.md)

### PHS-02. Programmatic Control foundation
- Dependencies: PHS-01
- Ticket: [Ticket](phases/02-programmatic-control/ticket.md)

### PHS-03. Providers, models, and runtime selection
- Dependencies: PHS-02
- Ticket: [Ticket](phases/03-providers-models-runtime-selection/ticket.md)

### PHS-04. Persistent linear sessions
- Dependencies: PHS-03
- Ticket: [Ticket](phases/04-persistent-linear-sessions/ticket.md)

### PHS-04.1. Model execution capabilities
- Dependencies: PHS-04
- Ticket: [Ticket](phases/04.1-model-execution-capabilities/ticket.md)

### PHS-05. Session tree
- Dependencies: PHS-04, PHS-04.1
- Ticket: [Ticket](phases/05-session-tree/ticket.md)

### Cross-cutting issue. Blocking contract operation processing
- Dependencies: PHS-05
- PRD: [PRD](../../issues/blocking-contract-operation-processing/prd.md)
- Technical Solution: [Technical Solution](../../issues/blocking-contract-operation-processing/solution.md)

### PHS-05.1. Extension boundary cleanup
- Dependencies: PHS-05, Blocking contract operation processing
- Ticket: [Ticket](phases/05.1-extension-boundary-cleanup/ticket.md)

### PHS-05.2. Branch-summary extension control
- Dependencies: PHS-05.1
- Ticket: [Ticket](phases/05.2-branch-summary-extension-control/ticket.md)
- Completion gate: PHS-07 implementation cannot start until every PHS-05.2 acceptance criterion passes.

### PHS-07.1. Architecture audit and correction
- Dependencies: PHS-05.2, PHS-04.1
- Ticket: [Ticket](phases/07.1-architecture-audit-and-correction/ticket.md)
- Execution point: Audit the implemented product, including partial PHS-07 work, before PHS-07 resumes. This phase does not depend on PHS-07 completion.
- Completion gate: [BLK-01 through BLK-05](phases/07.1-architecture-audit-and-correction/tui-ownership-gap.md#blockers) have U5/U7 closure evidence. Review of `b03095a` found FND-12 through FND-14. The O72-1 [retention correction](phases/07.1-architecture-audit-and-correction/solution.md#u6-retention-correction) is committed as `ded1549`. Its independent review found FND-15 through FND-17. The [approved TUI causality correction](phases/07.1-architecture-audit-and-correction/solution.md#u5-causality-verification) has implementation and main-agent verification evidence. [D1 runtime completion](phases/07.1-architecture-audit-and-correction/solution.md#u6-runtime-completion-evidence) has implementation and main-agent verification evidence. The [FND-18 through FND-21 producer correction](phases/07.1-architecture-audit-and-correction/solution.md#u6-producer-preservation), including O74-1 bash scope, has implementation and main-agent verification evidence. The separate [FND-22 process correction](phases/07.1-architecture-audit-and-correction/solution.md#process-cancellation-evidence) has implementation and main-agent verification evidence. The [FND-23 dispatch-confirmation correction](phases/07.1-architecture-audit-and-correction/solution.md#host-command-dispatch-confirmation-correction) has implementation and main-agent verification evidence. The separate U8 smoke-fixture correction, fresh independent review and explicit final acceptance remain pending. PHS-07.1 remains blocked and PHS-07 remains paused.

### PHS-07. Extension context and lifecycle
- Dependencies: PHS-05.2, PHS-04.1, PHS-07.1
- Ticket: [Ticket](phases/07-extension-context-lifecycle/ticket.md)
- Completion gate: Implementation is paused. Remaining work can resume only after every PHS-07.1 acceptance criterion passes.

### PHS-06. Context compaction and retry control
- Dependencies: PHS-07, PHS-04.1
- Ticket: [Ticket](phases/06-context-compaction-retry-control/ticket.md)

### PHS-08. Prompt, context, input, and provider middleware
- Dependencies: PHS-06, PHS-04.1
- Ticket: [Ticket](phases/08-prompt-context-input-provider-middleware/ticket.md)

### PHS-09. Tool middleware and run control
- Dependencies: PHS-08
- Ticket: [Ticket](phases/09-tool-middleware-run-control/ticket.md)

### PHS-10. Commands, interaction, notifications, and extension events
- Dependencies: PHS-09
- Ticket: [Ticket](phases/10-commands-interaction-notifications-events/ticket.md)

### PHS-11. Resource contributions
- Dependencies: PHS-10
- Ticket: [Ticket](phases/11-resource-contributions/ticket.md)

### PHS-12. Bundled and extension-defined providers
- Dependencies: PHS-11, PHS-04.1
- Ticket: [Ticket](phases/12-extension-defined-providers/ticket.md)

### PHS-12.1. Standard TUI transcript rendering and layout
- Dependencies: PHS-12
- Ticket: [Ticket](phases/12.1-standard-tui-rendering-layout/ticket.md)

### PHS-12.2. Standard TUI viewport navigation
- Dependencies: PHS-12.1
- Ticket: [Ticket](phases/12.2-standard-tui-viewport-navigation/ticket.md)

### PHS-12.3. Standard TUI editor and terminal interaction
- Dependencies: PHS-12.2, Blocking contract operation processing
- Ticket: [Ticket](phases/12.3-standard-tui-editor-terminal-interaction/ticket.md)

### PHS-13. Standard TUI presentation extensions
- Dependencies: PHS-12.3
- Ticket: [Ticket](phases/13-tui-presentation-extensions/ticket.md)

### PHS-14. Interactive standard TUI extensions
- Dependencies: PHS-13
- Ticket: [Ticket](phases/14-interactive-tui-extensions/ticket.md)

### PHS-15. Extension installation and state management
- Dependencies: PHS-14
- Ticket: [Ticket](phases/15-extension-installation-state/ticket.md)

### PHS-16. Environment reload
- Dependencies: PHS-15
- Ticket: [Ticket](phases/16-environment-reload/ticket.md)

### PHS-17. Glyph public-behavior traceability
- Dependencies: PHS-16
- Ticket: [Ticket](phases/17-reference-scenario-closure/ticket.md)

### PHS-18. Cleanup
- Dependencies: PHS-17
- Ticket: [Ticket](phases/18-cleanup/ticket.md)

### PHS-19. Independent final verification
- Dependencies: PHS-18
- Ticket: [Ticket](phases/19-independent-final-verification/ticket.md)
