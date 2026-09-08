# Ticket TKT-01: Architecture audit and correction

PHS-07.1 audits the architecture of the whole implemented product and corrects violations before PHS-07 resumes.

## Key definitions and abbreviations

Controller, usecase, domain, infrastructure adapter, and composition root identify architectural responsibilities. A directory name alone does not establish a component's responsibility.

## Problem statement

Compilation, tests, and an acyclic import graph do not establish correct architectural boundaries. Local fixes can leave misplaced responsibilities or conceal invalid dependencies through callbacks, forwarding code, and shared types. Further feature work must not build on those defects.

## Target picture

Each behavior and mutable state has an explicit owner. Package dependencies follow those responsibilities. Interfaces and their types belong to their consumers, and implementations satisfy those contracts without hidden reverse dependencies.

## Scenarios

### SCN-01: Audit and correct an end-to-end responsibility boundary

- Actor: Engineer performing the architecture audit.
- Pre-condition: Product behavior is implemented across several packages.
- Trigger: Architecture review before further feature work.
- Required behavior: Trace ownership, data flow, and dependencies through the complete behavior. Identify root causes, agree the correction plan, and correct every affected boundary.
- Example input and expected output: A behavior crosses input handling, application policy, state ownership, and external I/O. The audit produces source-based findings and corrections that preserve approved behavior and establish the required dependency direction.

### SCN-02: Keep TUI state consistent across a session operation

- Actor: User of the standard TUI.
- Pre-condition: The TUI has an editor draft, a displayed transcript, and an open session or tree selector.
- Trigger: Host rejects resume, publishes committed navigation progress, or completes session replacement.
- Required behavior: One application owner applies the corresponding display and interaction transitions. Rejected resume preserves the draft and preceding transcript. Committed navigation progress replaces the transcript; terminal navigation metadata supplies exact next input and closes the interaction. Session replacement clears the preceding draft only after Host confirmation.
- Example input and expected output: A rejected resume leaves the user's draft available. A later successful navigation displays the committed branch before applying the terminal next-input value, without submitting that value.

## Scope

In scope:
- All production modules and public contracts, including completed functionality and unfinished implementation.
- Responsibilities, state ownership, import dependencies, runtime calls, interface ownership, and command and response types.
- Corrections to the causes of findings across affected layers, with corresponding tests and architecture documentation.

Out of scope:
- New product capabilities, speculative infrastructure, and unrelated cosmetic cleanup.
- Manual changes to generated code instead of its source definitions.

## Dependencies and preconditions

- PHS-05.2 and PHS-04.1 are complete.
- The audit includes the partial PHS-07 implementation. Completion of PHS-07 is not a prerequisite.
- The [delivery plan](../../delivery-plan.md) places this audit before the remaining PHS-07 work.

## Requirements

### Goals

Remove architectural causes rather than make individual imports, assertions, or tests pass through local workarounds.

### Functional requirements

- FRQ-01: Establish the actual responsibilities and dependency graph across the whole audit scope. Inspect runtime calls and data flow as well as imports. Assess components by their behavior, not their names or presumed layer.
- FRQ-02: Controllers shall own the interfaces through which they invoke usecases. Usecases shall implement those interfaces. Controllers shall not implement usecase interfaces or call concrete usecase implementations.
- FRQ-03: Usecases shall own application policy and orchestration. Their outgoing dependencies shall use consumer-owned interfaces implemented by components responsible for external I/O or presentation. Input controllers and composition code shall not conceal those outgoing responsibilities.
- FRQ-04: Commands and responses used by an interface shall be defined beside that interface. Shared domain structures may be used only when they represent common domain concepts. Domain packages shall not become repositories for transport DTOs or interface-specific response aggregates.
- FRQ-05: Domain behavior shall remain independent of controllers, usecases, transport protocols, persistence implementations, provider SDKs, and presentation frameworks.
- FRQ-06: The composition root shall construct and connect concrete owners. It shall not own application policy, business-state transitions, or transformations that belong to another layer.
- FRQ-07: Every cross-package interface implementation shall contain its compile-time assertion in the implementation package. Before adding the consumer import, inspect the consumer's transitive dependencies. An assertion that exposes a cycle requires correction of the invalid dependency direction, not removal or relocation of the assertion.
- FRQ-08: Findings shall identify source locations, actual responsibilities, the violated principle, the dependency path, and the effect on behavior or maintenance. Distinguish an existing dependency from one introduced by a proposed change. Group symptoms that share an architectural cause.
- FRQ-09: Derive the correction plan from the complete audit before choosing package moves or new components. Do not hide invalid responsibilities through forwarding functions, aliases, adapters, copied types, shared contract packages, or replacement of interfaces with callbacks. Ordinary boundary adapters are acceptable only when their responsibility and dependency direction are correct.
- FRQ-10: Correct every confirmed violation across its affected paths. Update architecture documentation to describe the resulting ownership and dependencies. Do not leave known violations behind smaller local fixes.
- FRQ-11: The standard TUI shall have one application owner for presentation state transitions, editor drafts, selectors, tree interaction, pending commands, and foreground cancellation policy. Controllers and output implementations shall not mutate that private state directly. TUI state remains a projection; Host retains authoritative session and agent state.
- FRQ-12: TUI input controllers shall decode Host notifications and terminal input through their own usecase contracts. Event and response aggregates shall not be treated as domain concepts merely because their transformation is pure. Outgoing Host commands and coherent display snapshots shall belong beside the ports through which the application owner sends them.
- FRQ-13: The TUI application owner shall keep tree filtering, folding, visible-entry order, and selection reconciliation together. Terminal infrastructure shall own Bubble Tea, terminal resources, row geometry, wrapping, and rendering. SDK infrastructure shall own the initialized SDK connection, notification reads, and outbound encoding and dispatch, not pending-command or foreground-selection policy.
- FRQ-14: TUI input mapping shall preserve the source error category and complete text. The application state owner shall apply the approved persistence-cause cleanup. Neither rendering nor diagnostic wording shall select that cleanup behavior.

### Non-functional requirements

- NFQ-01: Preserve approved public behavior during structural changes. A required behavior change needs separate approval.
- NFQ-02: Preserve asynchronous operation lifecycles, cancellation, event ordering, state consistency, UI responsiveness, and complete error causes across changed boundaries.
- NFQ-03: Use existing behavioral tests for structure-only changes. Behavioral corrections require a failing executable test before implementation. Run the required project verification defined in [AGENTS.md](../../../../../../AGENTS.md) and verify repeatable generation for changed generated contracts or mocks.
- NFQ-04: Apply KISS and YAGNI. A proposed abstraction must resolve an observed responsibility or dependency problem, not a hypothetical future need.
- NFQ-05: During TUI interaction, the Bubble Tea event loop shall serialize application state transitions. Notification reads and command I/O shall not mutate that state from background work. The correction shall add no application event queue and shall preserve the progress-before-terminal transitions in SCN-02, asynchronous command dispatch, initialized sender context, and cancellation/cleanup order.

## Acceptance criteria

- ACC-01: The audit accounts for every production module and its cross-package boundaries, not only the latest diff or known examples.
- ACC-02: Findings meet FRQ-08 and have agreed dispositions. Every confirmed violation is corrected; no unresolved architectural violation remains in the audited scope.
- ACC-03: The resulting code satisfies FRQ-02 through FRQ-07. No assertion, adapter, alias, callback, or relocated type hides an invalid dependency.
- ACC-04: Required verification passes, approved behavior is retained, and architecture documents match the corrected implementation. Compilation and passing tests do not replace review of dependency direction.
- ACC-05: TUI source traces for resume rejection, navigation progress/completion, fork/clone, command-send failure, and foreground cancellation show one owner of state transitions and no direct controller/rendering mutation. Behavioral tests preserve SCN-02 and NFQ-05. Implementation-package assertions introduce no reverse dependency, and every blocker in the separately recorded TUI ownership gap is closed by source and verification evidence.

## Overengineering and overspecification considerations

This ticket selects audit principles, not a target package layout or a preferred local refactoring. The audit must justify structural changes through actual responsibilities and dependency paths.

## Constraints and risks

- Eliminating an import cycle can preserve the underlying responsibility error. Architectural acceptance requires both correct ownership and correct dependency direction.
- Existing code and documentation may contain the same mistaken boundary. Agreement between them is not sufficient evidence of correctness.

## Assumptions

None.

## Open questions

No unresolved ownership or behavior question remains for the completed units. The [TUI ownership record](tui-ownership-gap.md) closes BLK-01 through BLK-05 with implementation and independent verification evidence. [U8 implementation evidence](u8-evidence.md) includes completed main-agent inspection and repeated checks. Review of `b03095a` found [FND-12 through FND-14](audit.md#fnd-12-failed-work-is-discarded-before-terminal-delivery). The O72-1 [retention correction](solution.md#u6-retention-correction) is committed as `ded1549`. Independent review found FND-15 through FND-17. The user approved [D1 and D2](solution.md#qst-05-runtime-completion-meaning-and-tui-submission-causality). [D2 implementation and main-agent verification](solution.md#u5-causality-verification) are complete. [D1 implementation and main-agent verification](solution.md#u6-runtime-completion-evidence) are complete. The phase remains blocked; fresh independent whole-product review and explicit final acceptance remain mandatory.

## References

- [Project rules](../../../../../../AGENTS.md)
- [Target architecture](../../architecture.md)
- [Product requirements](../../prd.md)
- [Delivery plan](../../delivery-plan.md)
- [TUI ownership gap](tui-ownership-gap.md)

## Technical supplement

### Known example: a controller implements a usecase interface

At the audit baseline, `host/internal/controller/cli/headless/renderer.go` placed the outgoing `Renderer` implementation in the input-controller package and declared:

```go
var _ startup.Reporter = (*Renderer)(nil)
```

`Reporter` is defined in [host/internal/usecase/host/startup/interfaces.go](../../../../../../host/internal/usecase/host/startup/interfaces.go). An implementation of a usecase-owned reporting port is therefore placed in the controller layer and imports that usecase package. That baseline placement conflicts with the responsibility boundaries in FRQ-02 and FRQ-03. The assertion exposes the relationship; the assertion itself is not the defect. Correction 1 moved this implementation to [headless output](../../../../../../host/internal/infra/headless/renderer.go).
