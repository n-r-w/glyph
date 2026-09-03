# Idea: PHS-05.1 Extension Boundary Cleanup

## Definitions

The [Domain Glossary](../../../../../terms.md) defines extension runtime management, capability orchestration, and contract source split.

## Context and Problem

The [Problem Statement](problem.md) defines the prototype coupling, combined Host responsibilities, and oversized public protobuf sources addressed by this phase.

## Goal

Prepare the implemented extension boundary for PHS-07 without changing public behavior or adding capabilities owned by later phases.

## Scenarios

- A production composition runs Agent Core and a configured provider without constructing or invoking prototype hooks.
- An external extension registers tools and session-tree handlers. Tool execution, handler invocation, cancellation, runtime failure, and shutdown retain their public results.
- External extension, UI plugin, and Programmatic Control consumers compile and run against regenerated public Go packages after protobuf declarations move between source files.

## Scope and Non-Scope

In scope:

- Remove the implemented prototype hook path.
- Separate extension runtime management from capability orchestration for implemented tools and session-tree handlers.
- Split the Extension Contract, UI Plugin Contract, and Programmatic Control protobuf sources by responsibility.
- Update generation inputs and documentation affected by those changes.

Out of scope:

- Extension context, lifecycle handlers, compaction, retry, middleware, commands, resources, provider registration, installation, environment reload, and TUI extension capabilities.
- A generic extension framework or packages for capabilities owned by later phases.
- Migration of provider implementations into extension processes.
- Changes to public behavior.

## Requirements

- Production source shall contain no `host/internal/hooks`, `host/internal/hooks/runner`, Agent Core `hooks.ContextRunner`, provider `hooks.ProviderRunner`, Codex hook transport, or production hook-runner construction.
  Justification: The empty production hook path couples Agent Core and providers to a prototype Host mechanism without configured production behavior.
- Removing hooks shall preserve model-visible context, provider request serialization, provider response handling, Agent Core lifecycle events, complete error text, and original error causes.
  Justification: PHS-05.1 changes structure rather than public or model-execution behavior.
- Extension runtime management shall own only extension discovery, process startup, runtime registration state, low-level operation invocation, availability, monitoring, cancellation, and shutdown.
  Justification: Process management must not determine capability-specific behavior.
- The Host use case that consumes tools or session-tree handlers shall own conflict policy, handler ordering, action validation, state changes, and public error meaning through an interface declared by that use case.
  Justification: One component must own each behavior, and Go interfaces must belong to their consumers.
- PHS-05.1 shall separate only implemented tool and session-tree behavior. It shall add no package, service, or stub for PHS-07 or a later phase.
  Justification: Future capabilities must not determine the structure of this cleanup.
- `api/plugins/extension/v1/tool.proto`, `api/plugins/ui/v1/ui.proto`, and `api/programmatic/v1/programmatic.proto` shall be split by responsibility. No resulting protobuf source file shall exceed 500 lines.
  Justification: Responsibility-focused files satisfy the project file-size guideline and limit each contract change to its owning declarations.
- Every moved protobuf declaration shall preserve its protobuf package, fully qualified name, field number, enum value, owning `Open` service behavior, generated Go package, and exported message or service name.
  Justification: Source organization must not change public contract behavior or generated package use.
- A second `task generate` run shall produce no diff. External extension and UI plugin fixtures, tool and session-tree tests, cancellation and shutdown tests, and public error-preservation tests shall pass. Final verification shall pass `task fmt`, `task fix_dry_run`, accepted fixes, `task lint`, `task test`, `task itest`, and `task test-coverage`.
  Justification: These checks observe generation stability, public contract usability, behavior preservation, and repository quality.

## Open Questions

None.

## References

- [PHS-05.1 ticket](ticket.md)
- [Delivery plan](../../delivery-plan.md)
- [Target architecture](../../architecture.md)
- [PHS-07 ticket](../07-extension-context-lifecycle/ticket.md)
