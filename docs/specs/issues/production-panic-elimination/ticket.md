# Ticket: Eliminate panic paths from production code

Remove intentional and helper-induced panic paths from Glyph production code. Errors must be returned through existing contracts without terminating the host or a plugin.

## Key definitions and abbreviations

- Production code. Non-test code under `host/`, `internal/`, `plugins/`, and `sdk/`. Generated files, experiments, test fixtures, and `*_test.go` files are excluded.
- Panic path. A code path that invokes `panic` directly or calls an API that panics when an expected value or invariant is absent.
- Process boundary. The running Glyph host or plugin process that must remain operational after an operation failure.

## Problem Statement

Glyph production code contains:

- 20 direct `panic` calls in 14 functions.
- 16 calls to `mo.Option.MustGet()`.
- 3 calls to `regexp.MustCompile()`.

A panic can terminate the Glyph host or a plugin. Operation failures, invalid arguments, missing dependencies, and violated internal invariants must not terminate a process.

## Target Picture

Production code contains no intentional panic paths.

When an operation cannot continue, the responsible function returns an error through its existing error contract. The error preserves the complete original cause. The host or plugin reports the failure and remains operational when its surrounding contract permits further operations.

## Scenarios

### SCN-01: Invalid public API argument

- Actor: Host, plugin, or SDK consumer.
- Pre-condition: A production API receives an empty code, nil error, nil service, or another invalid argument.
- Trigger: The API validates the argument.
- Required behavior: The API returns an error without panicking or terminating the process.
- Example input and expected output: Calling an SDK failure constructor with an empty code produces an error that describes the invalid code.

### SCN-02: Internal invariant violation

- Actor: Glyph production component.
- Pre-condition: An expected binding, option value, tree state, or operation dependency is absent or inconsistent.
- Trigger: Production code attempts to use the missing value.
- Required behavior: The failure propagates as an error with its original cause. The process does not panic.
- Example input and expected output: An absent `mo.Option` value produces an error instead of invoking `MustGet()`.

### SCN-03: Valid execution

- Actor: Glyph host or plugin.
- Pre-condition: All required arguments and state are valid.
- Trigger: Existing production behavior executes.
- Required behavior: Observable successful behavior remains unchanged.
- Example input and expected output: A valid operation failure object retains its code and cause.

## Scope

In scope:

- Remove all direct `panic` calls from production code.
- Remove production calls to APIs that intentionally panic when input or state is invalid.
- Propagate failures through explicit error contracts.
- Preserve complete error text and original causes across every affected layer.
- Update callers and tests affected by changed error contracts.
- Add regression coverage for previously panicking conditions.

Out of scope:

- Panics used only in tests and test fixtures.
- Generated code.
- Experimental code under `experiments/`.
- Recovery from arbitrary runtime faults such as memory exhaustion.
- Unrelated error-handling refactoring.

## Dependencies and Preconditions

- Existing callers of affected functions must be updated together with their contracts.
- No backward compatibility is required.
- Production code does not need to compile at the start of the work, but all required project checks must pass before completion.

## Requirements

### Goals

- Prevent operation and validation failures from terminating the Glyph host or plugins.
- Make every identified failure observable through an explicit error contract.
- Preserve existing successful behavior.

### Functional Requirements

- [X] FRQ-01: Production code shall not call `panic` directly.
  - Origin: source. The user explicitly prohibited panic in production code.
  - Goal: Prevent host and plugin termination.
  - Goal achievement: Full. This removes all identified explicit panic paths.

- [X] FRQ-02: Production code shall not use `mo.Option.MustGet()` or equivalent panic-based value extraction.
  - Origin: source. These calls can produce the prohibited process-terminating behavior.
  - Goal: Prevent invariant violations from becoming panics.
  - Goal achievement: Full. Missing values become explicit failures.

- [X] FRQ-03: Production code shall not use `regexp.MustCompile()` or another `Must*` API whose failure behavior is panic.
  - Origin: source. The prohibition applies to every intentional panic path.
  - Goal: Remove panic-capable initialization paths.
  - Goal achievement: Full. Initialization failures use explicit error handling.

- [X] FRQ-04: Every previously panicking validation or invariant failure shall return an error through the affected operation's contract.
  - Origin: source. A service or plugin must not terminate because an operation failed.
  - Goal: Keep process-level failure separate from operation-level failure.
  - Goal achievement: Full. Callers receive the failure without process termination.

- [X] FRQ-05: Returned errors shall preserve the complete original error text and cause.
  - Origin: source. `AGENTS.md` error-handling rule.
  - Goal: Keep failures diagnosable after replacing panic paths.
  - Goal achievement: Full. Error propagation does not lose failure information.

- [X] FRQ-06: The implementation shall not replace panic calls with `recover` wrappers.
  - Origin: formulated. Recovery would retain panic-based control flow instead of eliminating the defect.
  - Goal: Remove panic paths rather than conceal them.
  - Goal achievement: Full. Failures use normal error control flow.

### Non-Functional Requirements

- [X] NRQ-01: Each previously panicking reachable condition shall have a behavioral test that verifies error return and process continuity.
  - Origin: source. Project TDD and testing rules.
  - Goal: Prevent regression of the corrected behavior.
  - Goal achievement: Full. Tests exercise actual failure behavior.

- [X] NRQ-02: `task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, and `task test-coverage` shall pass.
  - Origin: source. Required by `AGENTS.md`.
  - Goal: Verify repository-wide correctness.
  - Goal achievement: Full. All mandatory project checks provide observable completion evidence.

## Overengineering and Overspecification Considerations

The ticket covers only intentional panic paths identified in production code. It does not require global panic recovery, defensive handling of arbitrary Go runtime faults, or unrelated redesign.

## Constraints and Risks

- Changing constructors or helpers to return errors will require coordinated caller updates.
- Errors introduced at initialization boundaries must reach the existing host or plugin startup error path.
- Guarded `MustGet()` calls still violate the requirement because their API contract remains panic-based.

## Assumptions

- Test-only panics remain allowed because they cannot terminate a production process.
- Generated code is controlled by its generator and is outside this ticket.
- Existing successful behavior can be preserved while failure contracts change because the project requires no backward compatibility.

## Open Questions

None.

## Technical Supplement

The known direct panic sources are:

- `sdk/plugins/extension/v1.Connection.Fail`
- `sdk/plugins/extension/v1.Reject`
- `sdk/plugins/extension/v1.Fail`
- `sdk/plugins/ui/v1.Reject`
- `sdk/plugins/ui/v1.Fail`
- `sdk/plugins/ui/v1.newServer`
- `session.Tree.Clone`
- `sessions.newReplayState`
- `sessiontree.Service.BindModels`
- `sessions.Service.BindPricingCatalog`
- `operation.newTracker`
- `operation.newWriter`
- `operation.Failed`
- `operation.NewOwner`

Panic-based value extraction also exists in:

- `host/internal/controller/programmatic/delivery.go`
- `host/internal/infra/persistence/sessions/codec.go`
- `host/internal/usecase/host/contextcompaction/orchestration.go`
- `host/internal/usecase/host/programmatic/prepared.go`
- `host/internal/usecase/host/sessions/entry_history.go`
- `host/internal/usecase/host/sessions/history.go`
- `host/internal/usecase/host/ui/prepared_operations.go`

## References

- `AGENTS.md` - Project error-handling, testing, and compatibility rules.
- `sdk/plugins/extension/v1/` - Extension SDK panic sources.
- `sdk/plugins/ui/v1/` - UI SDK panic sources.
- `internal/operation/` - Shared operation panic sources.
- `host/internal/` - Host panic and panic-based extraction sources.
