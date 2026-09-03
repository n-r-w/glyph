# Ticket: PHS-05.1 - Extension boundary cleanup

Remove prototype coupling and separate implemented extension responsibilities before the Extension Contract gains more capabilities.

## Key definitions and abbreviations

- DEF-01: Extension runtime management. Discovery, process startup, operation invocation, runtime availability, monitoring, cancellation, and shutdown for extension processes.
- DEF-02: Capability orchestration. Host policy, ordering, validation, and state for one extension capability such as tools or session handlers.
- DEF-03: Contract source split. Moving declarations between protobuf source files without changing their protobuf package, fully qualified names, field numbers, enum values, or service behavior.

## Problem Statement

- PRB-01: Agent Core and the Codex provider execute through the prototype `host/internal/hooks` path even though every production composition configures no handlers and PHS-08 requires Host-owned extension middleware through consumer-owned interfaces.
- PRB-02: `host/internal/usecase/host/extensions.Service` combines extension runtime management with tool and session-handler capability behavior. Adding PHS-07 capabilities to this service would make unrelated Host behavior depend on one central service.
- PRB-03: The Extension Contract, UI Plugin Contract, and Programmatic Control each use one protobuf source file that already exceeds 500 lines. Planned contract additions would continue to increase those files.

## Target Picture

- SOL-01: Agent Core and the bundled providers contain no prototype hook dependency. Extension runtime management is separate from implemented capability orchestration. Public protobuf declarations are grouped by responsibility while their behavior and Go package usability remain unchanged.

## Scenarios

### SCN-01: Existing external extension behavior after cleanup

- Actor: external extension author.
- Pre-condition: PHS-05 and Blocking contract operation processing meet their acceptance criteria.
- Trigger: Glyph starts an external extension that registers a tool and session-tree handlers.
- Required behavior: registration, tool execution, handler invocation, cancellation, failure propagation, and shutdown retain their public contract behavior while Agent Core has no prototype hook dependency and Host capability policy is not owned by extension runtime management.
- Example input and expected output: Input: run the existing separate-module extension fixture and invoke its tool and session-tree handlers. Expected output: the fixture receives the same typed operations and lifecycle results defined by the public contracts, and static dependency checks find no production import of `host/internal/hooks`.

## Scope

In scope:

- ISP-01: Remove the prototype hook packages and every production dependency on them.
- ISP-02: Separate implemented extension runtime management from implemented tool and session-handler capability orchestration.
- ISP-03: Split the three public protobuf source files by responsibility without changing public behavior.
- ISP-04: Update source references and generation inputs affected by the contract source split.

Out of scope:

- OSP-01: No extension context, lifecycle handler, compaction, retry, middleware, command, resource, provider registration, installation, reload, or TUI extension capability.
- OSP-02: No new generic extension framework, future capability package, dynamic payload codec, `google.protobuf.Any`, or shared business-command model.
- OSP-03: No provider migration from Host to extension processes.
- OSP-04: No event-model decision from `docs/artefacts/ui-event-model-analysis.md`.

## Dependencies and Preconditions

- DEP-01: [PHS-05](../05-session-tree/ticket.md) is completed.
- DEP-02: [Blocking contract operation processing](../../../../issues/blocking-contract-operation-processing/solution.md) is completed.

## Requirements

### Goals

- GOL-01: Establish the implemented ownership boundaries required for PHS-07 without adding future extension behavior.

### Functional Requirements

- FRQ-01: Remove `host/internal/hooks`, `host/internal/hooks/runner`, the Agent Core `hooks.ContextRunner` dependency, empty production hook-runner construction, and the Codex hook transport.
- FRQ-02: Preserve provider-visible context, provider request serialization, provider response handling, Agent Core lifecycle delivery, and public error text after prototype hooks are removed.
- FRQ-03: Extension runtime management shall own only extension process discovery, startup, registration state, low-level operation invocation, runtime generation, availability, monitoring, cancellation, and shutdown.
- FRQ-04: The Host use case that consumes an implemented extension capability shall own that capability's policy, deterministic handler ordering, action validation, state transition, and public error meaning through an interface declared at the consumption site.
- FRQ-05: The cleanup shall separate only implemented tool and session-tree responsibilities. It shall add no placeholder service or package for a capability owned by a later phase.
- FRQ-06: Split `api/plugins/extension/v1/tool.proto`, `api/plugins/ui/v1/ui.proto`, and `api/programmatic/v1/programmatic.proto` into responsibility-focused source files.
- FRQ-07: The contract source split shall preserve each declaration's protobuf package, fully qualified name, field number, enum value, and owning `Open` service behavior.
- FRQ-08: The generated public Go packages shall remain `pkg/plugins/extension/v1`, `pkg/plugins/ui/v1`, and `pkg/programmatic/v1`. External plugin code shall continue to use the same exported message and service names.
- FRQ-09: Documentation shall reference the owning contract directory or the replacement responsibility-focused source file after the split.

### Non-Functional Requirements

- NFQ-01: The implementation shall follow RED, GREEN, and REFACTOR for each behavioral change.
- NFQ-02: `task generate` shall produce no second-run diff.
- NFQ-03: Final verification shall pass `task fmt`, `task fix_dry_run`, the accepted fixes from that report, `task lint`, `task test`, `task itest`, and `task test-coverage`.
- NFQ-04: Agent Core shall import no Host hook, extension runtime, plugin contract, plugin SDK, protobuf, gRPC, provider SDK, persistence adapter, settings, credential, or UI package.

## Deliverables

- DLV-01: Agent Core and provider execution without prototype hooks.
- DLV-02: Separate implemented runtime-management and capability-orchestration boundaries.
- DLV-03: Responsibility-focused protobuf source files with regenerated public Go contracts.

## Acceptance Criteria

- ACC-01: Production source contains no import or reference to `host/internal/hooks` or `host/internal/hooks/runner`.
- ACC-02: Headless, UI, and Programmatic Control compositions construct Agent Core and providers without a hook runner.
- ACC-03: Existing model context, provider request, provider response, tool execution, session-tree handler, cancellation, shutdown, and error-preservation tests pass without replacement hook behavior.
- ACC-04: Extension runtime management contains no tool conflict policy, model-visible unavailable-tool result construction, session-tree transformation policy, or final session-tree action validation.
- ACC-05: Tool and session-tree capability behavior depends on extension runtime management through consumer-owned interfaces.
- ACC-06: No empty package or service exists only for a capability delivered by PHS-07 or a later phase.
- ACC-07: The three original protobuf source files no longer combine all declarations for their contracts, and no replacement protobuf source file exceeds 500 lines.
- ACC-08: Every moved protobuf declaration preserves its package, fully qualified name, field number, enum value, and generated Go package.
- ACC-09: The existing external extension and external UI plugin fixtures compile and pass against the regenerated public SDK and contract packages.
- ACC-10: `task generate` produces no second-run diff, and every command in NFQ-03 passes.

## Overengineering and Overspecification Considerations

The phase removes unused prototype behavior and separates only responsibilities present before PHS-07. It keeps the existing processes, protobuf packages, SDKs, operation streams, and public behavior. It adds no abstraction or package for an unimplemented capability.

## Constraints and Risks

- RSK-01: Moving capability policy without preserving registration and runtime-failure semantics can change public tool or session-tree behavior. Public-contract and separate-module fixtures must cover the moved paths.
- RSK-02: Moving protobuf declarations changes generated filenames and file-descriptor symbols. No production or public SDK behavior may depend on those generated file-descriptor variables.
- RSK-03: A broad runtime abstraction can recreate the central service under another name. Each consumer interface must contain only the operations required by that consumer.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

A phase-specific technical solution must select package placement and the exact protobuf file split before implementation.

## References

- REF-01: [Target architecture](../../architecture.md) - Agent Core, Host, extension runtime, and consumer-owned interface boundaries.
- REF-02: [Delivery plan](../../delivery-plan.md) - phase order and dependencies.
- REF-03: [PHS-07 ticket](../07-extension-context-lifecycle/ticket.md) - first consumer of the cleaned extension boundary.
- REF-04: [PHS-08 ticket](../08-prompt-context-input-provider-middleware/ticket.md) - owner of replacement public middleware.
- REF-05: [UI event-model analysis](../../../../../artefacts/ui-event-model-analysis.md) - unresolved event-model questions excluded from this phase.
