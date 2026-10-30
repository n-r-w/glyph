# PHS-08 requirements

## Definitions

Use the [domain glossary](../../../../../terms.md). The [ticket](ticket.md) remains the authoritative phase scope and acceptance checklist. Identifiers below group its requirements without changing ticket identifiers.

## Context and problem

See [problem.md](problem.md) and the [current-behavior facts](facts.md).

## Goal

Allow ordinary extensions to customize model-facing behavior in every supported execution mode while Host retains orchestration and durable session ownership and Agent Core retains provider-neutral execution.

## Scenarios

Use [target-picture.md](target-picture.md), A4 through A7, and ticket SCN-01. For example, when A appends marker `A` and B replaces the current context from the immutable original plus `B`, the outbound context contains `B` without `A`, and persisted history contains neither marker.

## Scope and non-scope

Scope is the entire PHS-08 ticket and confirmed target picture, including public contracts, SDK support, reference extensions, tests and documentation. Tool middleware, extension commands, resource discovery, provider installation, and TUI-specific contributions remain outside this phase.

## Requirements

### Functional requirements

- [ ] FRQ-01: When a model request is prepared, extensions can change its effective system instructions and request-local context without changing persisted entries through that transformation.
  - Origin: Source. Ticket FRQ-01, FRQ-01.4, ACC-01 and ACC-02; PRD Extension Capabilities.
  - Goal: Project-specific instructions and context projection through ordinary extensions.
  - Goal achievement: Partial. Provides the model-facing customization described in problem evidence FCT-03 and FCT-04.
  - Feasibility: Confirmed. Existing model requests contain instructions and detached history, and extensions already support typed handler invocations.
- [ ] FRQ-02: Before agent processing, extensions can observe, transform or fully handle user text and image input. Host validates the final unhandled input against the selected model's declared modalities before starting Agent Core or dispatching a provider request.
  - Origin: Source. Ticket FRQ-01.2, FRQ-01.3, ACC-03 and ACC-03.1.
  - Goal: Extension-controlled user input with final model compatibility checks.
  - Goal achievement: Partial. Provides the missing user-input behavior described in FCT-01 and FCT-02.
  - Feasibility: Confirmed. The domain and existing configured-model contract already represent ordered text and image blocks; each selected model declares accepted modalities.
- [ ] FRQ-03: Before a finalized message is committed for subsequent use, extensions can replace it sequentially while preserving its role.
  - Origin: Source. Ticket FRQ-01; PRD Extension Capabilities finalized-message replacement.
  - Goal: Extension-controlled final message content, with role identity preserved.
  - Goal achievement: Partial. Supplies the replacement behavior absent from the current message-end observer.
  - Feasibility: Confirmed. Host owns history append and receives complete provider-neutral user, model and tool-result entries before persistence.
- [ ] FRQ-04: For provider execution, extensions can change request headers, replace the serialized request and observe response status and headers through public extension operations.
  - Origin: Source. Ticket FRQ-01, ACC-06.1; PRD Extension Capabilities provider customization.
  - Goal: Provider-request customization and response observation independent of the concrete provider implementation.
  - Goal achievement: Partial. Supplies the wire customization described in FCT-07.
  - Feasibility: Confirmed. Both built-in provider adapters execute through the shared raw-attempt boundary and configurable HTTP transports; serialized requests and raw response metadata exist at those transport calls.
- [ ] FRQ-05: Each transforming operation invokes handlers sequentially in deterministic registration order. Every handler receives immutable original input and the current value from preceding handlers and can preserve or replace the current value using either source.
  - Origin: Source. Ticket FRQ-02 and ACC-04.
  - Goal: Ordered composition and deliberate replacement across all transforming operations.
  - Goal achievement: Partial. Allows independently authored extensions to compose predictably.
  - Feasibility: Confirmed. Registration preserves deterministic extension and descriptor order; existing selection and session-tree owners use original/current composition.
- [ ] FRQ-06: A handler error, invalid action or runtime unavailability stops the invoking operation and the failed handler chain. Clients receive the closed Glyph category and complete error text. Host does not retry the extension, restart the chain or repeat a provider request to recover from that extension failure. A pre-dispatch failure prevents dispatch; a response-observer failure stops that model execution.
  - Origin: Source. Ticket FRQ-03, FRQ-03.1 and ACC-05; PRD Extension failure rule.
  - Goal: Complete failure visibility and terminal extension-failure behavior.
  - Goal achievement: Partial. Keeps middleware failures consistent with accepted extension semantics.
  - Feasibility: Confirmed. Current operations retain source errors, public clients expose coded failure results, and Host owns retry decisions.
- [ ] FRQ-07: The shared model-facing extension behavior applies to supported application modes and to the applicable shared model execution used by configured-model callers. Reference extensions demonstrate project instructions, context projection and provider-request changes.
  - Origin: Source. Ticket DLV-02, ACC-01 and ACC-06.1; target picture A4 and A7.
  - Goal: Usable extension behavior through public boundaries rather than mode-specific implementation.
  - Goal achievement: Partial. Makes the customization available to actual extension authors and clients.
  - Feasibility: Confirmed. All modes already share Host run coordination and model execution; public SDK/process fixtures exercise separately built extensions.

### Non-functional requirements

- [ ] NRQ-01: Host owns middleware dispatch, ordering, transport mapping, ordinary failure mapping and final validation. Agent Core obtains effective provider-neutral context through its minimal consumer-owned interface in `host/internal/usecase/agent/run`. Provider middleware coordination uses the existing shared model/provider execution boundaries and imports no concrete provider, provider SDK or credential package.
  - Origin: Source. Ticket FRQ-01.1, FRQ-01.4 through FRQ-01.6, NFQ-02, ACC-06 and ACC-06.1; target picture D2 and D3.
  - Goal: Preserve the confirmed Host/Core/provider ownership boundaries.
  - Goal achievement: Partial. Makes customization independent of plugin transport and provider implementation.
  - Feasibility: Confirmed. Current Core and raw-provider ports are consumer-owned; capability owners and transport mapping already reside outside Core.
- [ ] NRQ-02: Public non-provider middleware data preserves the existing credential and provider-reasoning confidentiality boundaries. Ordinary extension operations expose neither resolved credentials nor opaque provider reasoning payloads.
  - Origin: Source. Target architecture CNS-06, ENT-08 and RSK-02; PRD provider reasoning behavior.
  - Goal: Preserve public data ownership while adding request customization.
  - Goal achievement: Partial. Prevents the new operations from exposing private provider state through their public payloads.
  - Feasibility: Confirmed. Concrete adapters already own credentials and replay conversion; public configured-model response mapping already excludes opaque replay data.
- [ ] NRQ-03: Acceptance evidence covers every ticket criterion through public behavior where existing infrastructure permits it, includes focused RED/GREEN observations, and records all mandatory project checks and actual verification limits.
  - Origin: Source. Ticket NFQ-01; target-picture verification policy; project coding and testing rules.
  - Goal: Demonstrate the completed behavior and preserve existing behavior.
  - Goal achievement: Partial. Makes implementation completion independently checkable.
  - Feasibility: Confirmed. Baseline checks pass, public Programmatic/UI/extension fixtures exist, and built-in providers have controllable HTTP integration fixtures.

## Open questions

None. Contract shapes and placement are selected in the technical solution within confirmed authority. Checkboxes remain unset because this autonomous stage does not request separate requirement-by-requirement approval.

## Technical supplement

No technical solution is selected here. Existing ticket architecture constraints remain mandatory.

## References

- [Ticket](ticket.md).
- [Target picture](target-picture.md).
- [Problem](problem.md).
- [Facts](facts.md).
- [Target architecture](../../architecture.md).
- [PRD](../../prd.md).
