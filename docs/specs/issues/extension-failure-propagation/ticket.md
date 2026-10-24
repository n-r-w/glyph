# Ticket: Stop operations on extension failure

Status: In progress. UNT07 ownership is verified. UNT08 selection failure propagation and typed Failed transport are implemented and verified in the [selection evidence](selection-evidence.md). For remaining issue scope, see [the technical solution](solution.md#limits-and-remaining-scope).

Align implemented extension invocation paths with the existing [extension failure rule](../../features/initial/prd.md#extension-failure-rule) before PHS-08 starts.

## Key definitions and abbreviations

Use the [domain glossary](../../../terms.md) for extension, parent operation, complete error text, and original cause.

## Problem Statement

At the recorded baseline, Host continued some implemented extension chains after a handler error or invalid action. Model selection and session-tree navigation could invoke later handlers and commit a change after an earlier handler failed. Lifecycle observer failures could remain diagnostic issues while Host continued the chain and the invoking operation.

The baseline tests included expectations for this continuation. Passing those tests did not establish compliance with the extension failure rule.

## Target Picture

When an extension fails on an operation's execution path, Host stops that operation and the remaining handlers in the failed chain. The client receives the complete error text and any outcome already committed before the failure.

## Scenarios

### SCN-01: Handler failure before commit

- Actor: Glyph Host.
- Pre-condition: An operation has multiple registered extension handlers and has not committed its change.
- Trigger: A handler returns an error, returns an invalid action, or becomes unavailable.
- Required behavior: Host follows the extension failure rule and prevents the commit and later handler invocation.
- Example input and expected output: Selection handlers A, B, and C are registered in that order. B returns `extension failed: permission denied`. Host invokes neither C nor the selection commit and reports the complete error text.

### SCN-02: Observer failure after commit

- Actor: Glyph Host.
- Pre-condition: Host has committed a selection or session-tree navigation change.
- Trigger: A post-commit observer returns an error.
- Required behavior: Host stops further processing and reports both the error and the committed outcome. The persisted change remains available.
- Example input and expected output: Navigation to entry `target` commits, then observer A fails. Host does not invoke observer B. The client receives A's error and the committed active entry `target`.

### SCN-03: Failure during an agent run

- Actor: Glyph Host.
- Pre-condition: An agent run invokes an extension handler or lifecycle observer.
- Trigger: The extension invocation fails.
- Required behavior: Host stops the run under the extension failure rule without repeating the failed extension invocation, restarting the chain, or scheduling a provider retry to recover from that extension failure.
- Example input and expected output: An `agent_start` observer returns `observer failed: exact cause`. Host invokes no later `agent_start` observer and does not dispatch the run's model request. The client receives the complete failure text.

## Scope

In scope:

- Correct failure propagation in implemented model and reasoning selection, session-tree request and result handlers, and lifecycle and post-commit observers.
- Correct callers and public completion mapping so an extension failure stops the invoking operation or agent run, including nested operations where the parent consumes the failure.
- Verify existing compaction and retry invocation paths against the same rule and change them only where a concrete mismatch remains.
- Replace existing test expectations that require continuation after extension failure with regression coverage for the required behavior.
- Preserve successful execution, committed outcomes, complete error causes, and the distinction between extension failures and ordinary provider or tool results.

Out of scope:

- New extension points or middleware from PHS-08 and later phases.
- A general codebase or architecture audit.
- Production panic elimination, which has its own [ticket](../production-panic-elimination/ticket.md).
- Reopening unrelated accepted behavior from completed phases.

## Dependencies and Preconditions

- PHS-06 and PHS-07 provide the implemented invocation paths being corrected.
- This correction and production panic elimination must be completed and verified before PHS-08 starts. The [delivery plan](../../features/initial/delivery-plan.md#cross-cutting-corrections-before-phs-08) records their scheduling.

## Requirements

### Goals

- Make implemented extension invocation paths comply with the accepted failure behavior before adding PHS-08 middleware.

### Functional Requirements

The [extension failure rule](../../features/initial/prd.md#extension-failure-rule) is the normative source for operation and run termination, handler ordering after failure, complete error reporting, commit boundaries, retry prohibition, extension availability, and provider and tool result exceptions. This ticket adds no alternative failure policy.

### Non-Functional Requirements

- Focused regressions must demonstrate the current mismatch before the correction and the required behavior after it.
- Public-contract tests must verify error text and operation outcomes through Programmatic Control and the UI Plugin Contract for the corrected behavior.
- The implementation must pass the project checks listed in [AGENTS.md](../../../../AGENTS.md).

### Acceptance Criteria

- ACC-01: Handler errors, invalid actions, and unavailable handlers stop the affected chain and operation before any pending commit or provider dispatch.
- ACC-02: An extension failure during an agent run stops the run and reaches the client with its complete error text.
- ACC-03: A post-commit observer failure stops later observers and further processing while the client can identify the committed outcome.
- ACC-04: No correction path retries the failed extension, restarts the chain, or schedules provider retry to recover from that failure. Ordinary handler errors do not deactivate the extension.
- ACC-05: Existing compaction and retry failure-stop behavior remains covered, and successful operations and the PRD's provider and tool result exceptions retain their behavior.
- ACC-06: Focused and public-contract regressions pass, and implementation evidence records the required project-check results.

## Overengineering and Overspecification Considerations

Use the existing operation, run, extension-runtime, and public error contracts. This task requires corrections to known failure paths and their callers, without a new extension framework, global recovery layer, or unrelated refactoring.

## Constraints and Risks

- Some current tests require continuation after an extension failure. Retaining those expectations would preserve the defect.
- A failure after commit must retain the committed outcome. Reporting an unqualified rollback or undoing completed actions would violate the existing failure rule.

## Assumptions

None.

## Open Questions

None for the task scope and required behavior. Package-level changes remain implementation work.

## Technical Supplement

### Confirmed baseline source locations

- `host/internal/usecase/host/modelselection/handlers.go`, `Service.compose`: handler errors and invalid actions can be reported before composition continues.
- `host/internal/usecase/host/modelselection/service.go`, `Service.run` and `Service.commitSelection`: a composed selection can subsequently be committed.
- `host/internal/usecase/host/sessiontree/handlers.go`, `Service.runRequestHandlers`, `Service.runResultHandlers`, and `Service.runObservers`: errors can be collected while later handlers continue.
- `host/internal/usecase/host/sessiontree/service.go`, `Service.navigate`: navigation can proceed to commit after collected handler issues.
- `host/internal/usecase/host/lifecycle/service.go`, `Service.observe` and `Service.ObserveSelection`: ordinary errors can remain diagnostics while later observers continue.

### Existing regression evidence

During the read-only verification, the following existing tests passed while requiring continuation:

- `TestServiceContinuesAfterOrdinaryAndInvalidHandlerResults` in `host/internal/usecase/host/modelselection/handlers_test.go`.
- `TestNavigatePreservesStateForInvalidHandlerAction` in `host/internal/usecase/host/sessiontree/handler_composition_test.go`.
- `TestServiceContinuesAfterOrdinaryObserverError` in `host/internal/usecase/host/lifecycle/service_test.go`.
- `TestPublicSelectionObserverErrorCompletesWithDiagnostics` in `host/internal/app/extension_selection_modes_integration_test.go`. This public integration test uses a real extension process and verifies later observer invocation and continued execution after the first observer fails.

Verification commands:

```sh
go test -count=1 -race ./host/internal/usecase/host/lifecycle ./host/internal/usecase/host/modelselection ./host/internal/usecase/host/sessiontree ./host/internal/usecase/host/modelexecution ./plugins/extension/compaction/internal/usecase/policy
go test -count=1 -race -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestPublicSelectionObserverErrorCompletesWithDiagnostics$' -v
```

These passing results describe the pre-correction baseline. They are not acceptance evidence for this ticket.

## References

- [Extension failure rule](../../features/initial/prd.md#extension-failure-rule).
- [Delivery plan](../../features/initial/delivery-plan.md#cross-cutting-corrections-before-phs-08).
- [Roadmap](../../../roadmap.md#extension-failures-stop-the-invoking-operation).
