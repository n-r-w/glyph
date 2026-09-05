# Ticket PHS-05.2: Branch-summary extension control

Complete extension-controlled branch summarization and its error reporting before PHS-07 implementation starts.

## Key definitions and abbreviations

- Result source. The extension or model that produced a branch-summary result, separate from the configured model selected for built-in summarization.

## Problem statement

A ready extension summary bypasses the built-in model request, but `sessiontree.Service.validateFinalState` still checks the selected model's credentials and assigns that model to the result. An extension cannot supply a summary produced without a model independently of the unused model configuration.

The session-tree request, result, and observer chains replace received error text with fixed messages. The user cannot diagnose why an extension transformation failed.

## Target picture

A client can navigate with an extension-produced summary without invoking or authenticating an unused model. Persisted and client-visible result metadata identifies the result source. Ordinary handler failures retain their causes while navigation follows its defined continuation rules.

## Scenarios

### SCN-01: Extension summary without a model request

- Actor: Extension author.
- Pre-condition: A session has an abandoned branch. A request handler supplies a summary produced without a model.
- Trigger: A Glyph client requests navigation with summarization.
- Required behavior: Host commits the supplied summary and navigation without requiring the unused built-in summary model to be available.
- Example input and expected output: The selected summary model has unavailable credentials. The extension supplies a summary with no model usage. Navigation succeeds, no model request or credential check occurs, and the result identifies the extension rather than the unused model.

### SCN-02: Diagnosable handler failure

- Actor: Glyph user.
- Pre-condition: Two request handlers are registered.
- Trigger: The first handler returns an ordinary error containing `load summary rules: open rules.json: permission denied`.
- Required behavior: The second handler receives the preceding current state. The client receives the original cause in the operation issue.
- Example input and expected output: Navigation succeeds after the second handler supplies a valid result. The returned issue identifies the first extension and handler and retains the complete error text.

## Scope

In scope:
- Branch-summary replacement, result-source metadata, persistence, accounting, and client projection.
- Error text from session-tree request handlers, result handlers, and post-commit observers through the Extension Contract, Host, UI Plugin Contract, and Programmatic Control.

Out of scope:
- Extension context, session-state recovery, command registration, context compaction, and provider extension migration.
- A generic summary framework or new Agent Core policy.

## Dependencies and preconditions

- PHS-05.1 is complete. The [delivery plan](../../delivery-plan.md) places this phase before PHS-07.
- PHS-07 implementation shall not start until this ticket meets every acceptance criterion.

## Requirements

### Goals

- Let an extension replace branch summarization without retaining dependencies of the replaced computation.
- Preserve the diagnostic text needed to understand failed extension participation.

### Functional requirements

- FRQ-01: A ready extension result shall bypass built-in model execution. An unused built-in summary model's absence, reasoning configuration, or unavailable credentials shall not reject that result. Host shall check model availability and credentials when it dispatches a built-in summary request.
- FRQ-02: The result source shall remain separate from the selection used for built-in summarization. A summary produced without a model shall identify its producing extension and shall claim no model execution or token usage. A model-generated summary shall identify the provider, model, and reasoning choice that produced it. Result replacement shall preserve or replace source and accounting data consistently with the returned result.
- FRQ-03: Host shall persist the result source and expose it through UI Plugin Contract and Programmatic Control. Estimated cost shall use the actual model source and reported usage. Missing usage or applicable pricing shall produce absent estimated cost, not a cost derived from the unused built-in model.
- FRQ-04: Final summary validation shall retain nonempty-content, navigation-target, branch-boundary, and usage checks. Navigation and summary persistence shall commit atomically. Cancellation or invalid final state before commit shall change neither the active leaf nor persisted entries.
- FRQ-05: Session-tree request-handler, result-handler, and observer issues shall preserve the received error text and every added context message under the shared [error semantics](../../prd.md#error-semantics). A fixed message shall not replace the cause. Machine-readable issue categories shall supplement the text.
- FRQ-06: An ordinary request or result handler error shall preserve the current state received by that handler, continue later handlers, and leave the extension active. An observer error after commit shall report an issue without undoing navigation. Error-text preservation shall not change these outcomes.

### Non-functional requirements

- NFQ-01: The change shall remain within Host session-tree behavior and its public contracts. Agent Core shall acquire no summary-source, extension, persistence, or client dependency.
- NFQ-02: Behavioral changes shall follow RED, GREEN, and REFACTOR. Verification shall run `task generate` twice with no diff on the second run, `task fmt`, `task fix_dry_run` with accepted corrections, `task lint`, `task test`, `task itest`, and `task test-coverage`.

## Deliverables

- Branch-summary result-source and accounting contracts with Host, persistence, and client implementations.
- Public-contract tests for complete extension replacement and all three handler-error paths.

## Acceptance criteria

- ACC-01: A real extension supplies a complete summary while the unused built-in model has unavailable credentials. Navigation commits with zero built-in model requests and zero credential checks. Repeat with a missing unused model selection.
- ACC-02: A summary produced without a model survives restart with its extension source and absent model usage and estimated cost. Both client contracts expose that source without attributing the result to a configured model.
- ACC-03: A model-generated replacement from a model different from the built-in selection persists its actual model source. Available usage and pricing produce cost for that source, not the built-in selection.
- ACC-04: Clearing an extension result still invokes built-in summarization. Unavailable credentials then fail the request before navigation commits. Successful built-in summarization retains its actual model source and accounting.
- ACC-05: Request-handler, result-handler, and observer failures each return a distinct non-secret cause through real Extension Contract invocation and both client contracts. The received issue preserves the cause, extension ID, handler ID, and issue category.
- ACC-06: Request and result handler failures continue the chain with the preceding state. Observer failures retain committed navigation. Explicit cancellation and invalid final summary state produce no navigation commit.

## Overengineering and overspecification considerations

The phase completes one extension-controlled navigation path across contracts, Host, persistence, and clients. It adds no second summarizer service, compatibility layer, or state store. Result-source fields and transport shapes remain technical-solution decisions.

## Constraints and risks

- Result transformation can make source or usage metadata inaccurate. The final result must identify its producing source rather than inherit the built-in selection automatically.
- Tests that expect generic handler messages can preserve the defect. Public-contract assertions must check the received cause and continuation behavior together.

## Assumptions

None.

## Open questions

None. The [technical solution](solution.md) records the implemented contracts and verification evidence.

## Technical supplement

The affected implementation includes `Service.validateFinalState` in `host/internal/usecase/host/sessiontree/validation.go`, the handler chains in `host/internal/usecase/host/sessiontree/handlers.go`, provider availability checks, branch-summary persistence, and the public result mappings. This ticket does not select Go types, protobuf fields, or a persistence representation.

## References

- [Technical solution and verification evidence](solution.md) records the implementation and acceptance checks.
- [Target PRD](../../prd.md) defines extension replacement and complete error semantics.
- [Target architecture](../../architecture.md) defines Host ownership and Agent Core independence.
- [PHS-05](../05-session-tree/ticket.md) defines branch-preserving navigation and the implemented summary behavior.
- [PHS-05.1](../05.1-extension-boundary-cleanup/ticket.md) separates runtime mechanics from capability policy.
- [PHS-07](../07-extension-context-lifecycle/ticket.md) consumes the completed branch-summary contract.
