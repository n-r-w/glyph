# Technical Solution: PHS-06 acceptance corrections

## Problem Statement

The [problem statement](problem.md) and [requirements](requirements.md) define the five correction subjects. Existing ownership remains in the [phase solution](../solution.md#target-ownership).

## Proposed Solution

### Existing data and ownership

The correction uses existing request-local semantic responses, typed provider refusals, joined causes, retry decisions, compaction state, saved session entries, and committed receipts. Provider diagnostics remain in the concrete adapter. Retry composition and repeat admission remain in Host model execution. Compaction coordination stays in the bundled extension, and session persistence and projection retain their existing owner. No public contract, persisted representation, dependency version, shared mutable state, or new operation lifecycle is introduced.

### Codex refusal-only continuation

For FRQ-01, extend SDK iteration only after acquiring a structured provider refusal and successfully delivering its semantic content. Retain the acquired terminal response and all refusal/conversion causes while the existing SDK obtains its later stream error. Further SDK events do not enter the semantic assembler or reach callers. The existing cleanup defer remains responsible for body Close and owning cancellation causes.

`Driver.executeRequest` in `host/internal/infra/providers/openai/codex/provider.go` already owns the SDK loop and cleanup. A request-local delivery-failure observation distinguishes a refusal with failed semantic delivery from a refusal whose delivery succeeded. `Driver.Stream` retains its existing exact delivery-error ownership. Typed refusal discovery through joined errors admits refusal plus conversion/merge failure, while successful terminals and assembler-only failures retain immediate exit. Refusal plus ContentEnd callback failure also retains immediate exit.

Continuing every terminal stream would add network reads after successful output and delivery failure. Restricting continuation to any nonnil error would also include assembler-only failures. Restricting it to a direct refusal type assertion would omit joined refusal causes. The selected typed-refusal and successful-delivery boundary is the smallest change that preserves these existing exits.

The original context-bound SDK request owns continued reading. No cancellation-free drain, raw-body wrapper, new timeout, or dependency capability is added. The SDK exposes its first subsequent error; the design makes no promise to discover further errors after the SDK itself terminates on a decoding failure.

Dry run: a context-length refusal followed by unexpected EOF retains both causes and context-overflow classification. A clean end retains the refusal alone. Refusal plus conversion error retains that cause and the later SDK error. Delivery failure returns immediately with acquired refusal/delivery/close causes. Owning cancellation interrupts the context-bound read and remains available to Host's existing concurrent-failure mapping. Successful completed/length output retains its immediate terminal behavior.

### Retry handlers before ownerless-overflow admission

For FRQ-02, separate enabled handler composition from overflow-repeat admission in `host/internal/usecase/host/modelexecution/retry.go`, `Service.execute`. Disabled retry retains its existing early termination. Enabled ownerless overflow receives the existing built-in nonretryable decision and traverses the existing ordered handler loop. Its terminal actions and action-validation failures retain their existing categories and complete causes.

After successful handler composition and decision validation, Host enforces absence of an overflow recovery owner before admitting a repeat. The guard precedes recovery invocation, retry progress, and scheduling. Thus a valid replacement decision cannot invoke nil recovery or repeat unchanged overflowing input. Ordinary requests with a captured owner retain their existing allowance and recovery path.

Moving all overflow policy into handlers would require handlers to duplicate Host's recovery-admission invariant. A new retry/recovery state model would add a contract change for behavior already expressible by the current decision and optional owner. The selected change adjusts the placement of the existing checks without introducing either mechanism.

Dry run: ownerless Cancel returns RETRY_CANCELED; handler error or invalid action returns EXTENSION_FAILED; Preserve returns CONTEXT_LIMIT. Later handlers stop for terminal outcomes. A valid override requesting a repeat still returns CONTEXT_LIMIT without another attempt when recovery is absent. Disabled retry invokes no handler. Captured-owner recovery remains charged to the current logical attempt allowance.

### Bundled result-rejection observations

For FRQ-03, use existing reference participants under bundled policy ownership. Preserve the existing reference-policy suite. Public assertions compare complete pre/post session entries, absent receipts, last accepted requests/result, acquired cause text, and the missing later-handler artifact. Include invalid action shape, rejected boundary, ordinary handler failure, and a cancellation-containing error with a live owning context separately from explicit Cancel.

Complement the public path with bundled coordinator tests using existing generated Host mocks. Unexpected later participant or Commit calls fail those tests, so an attempted commit that the real session owner would reject is observable. Candidate rejection preserves the previously accepted state. This checks the same public outcome at the application level and the otherwise-unobservable no-commit-attempt invariant at the coordinator level; the tests have different verification responsibilities.

No production change is presumed for this evidence subject. Shared fixture helpers remain test-only. A new public commit counter or test-only production interface is unnecessary.

### Ordered restored suffix

For FRQ-04, strengthen the existing repeated-compaction restart scenario. Obtain the saved active branch after the second receipt, select its retained portion from that receipt's boundary, and derive an independent expected provider-visible sequence for the fixture's retained messages. Compare decoded resumed provider input positively, including latest summary placement and the newly submitted request. Include enough retained messages for a reorder to be observable.

Expected data comes from stored entries and the receipt, rather than the first marker's retained prefix or production projection helpers. The assertion does not require content retained by the first marker to survive a later summary. Existing exclusion assertions and navigation observations remain in place. Test-only decoding can use the known fixture variants and fail explicitly on an unexpected retained variant.

A new restoration fixture would duplicate persistence, owner recreation, and provider capture. Reusing this existing scenario exercises the actual assembly and makes omission/reordering detectable without adding a production projection abstraction.

### Deterministic installed-wait cancellation

For FRQ-05, use testing/synctest around the existing mocked-port model-execution scenario. Run the logical request in a bubble goroutine. Progress only signals availability and returns; cancellation comes from the controlling test after synctest establishes that the worker is durably blocked. With synchronous mocks and no additional test-owned worker wait, the production backoff timer select is the pending wait. Fake-clock assertions establish that no retry delay elapsed before cancellation, and provider expectations prohibit a repeat.

Retain complete cancellation/provider cause and empty-response assertions. Existing public abort scenarios continue to demonstrate terminal ordering, idle, and retained-connection recovery. The report distinguishes deterministic in-process timer evidence from public progress-boundary lifecycle evidence. This combination is permitted by target A5 and adds no claim that the public test itself acknowledges timer installation.

A sleep would depend on scheduler timing. A public timer acknowledgement or injected production timer abstraction would change contracts only to expose a test event. Synctest uses the declared standard library and existing synchronous test ports, so neither mechanism is needed.

### Verification and evidence ownership

For NRQ-01, obtain compiling RED/GREEN runs for the two behavior corrections before acceptance. Evidence-only additions exercise existing behavior; use controlled assertion-sensitivity mutations in isolated copies to show that the new assertions detect the named regressions, without reporting those controls as original test-first chronology.

Run all mandatory project commands, inspect fix proposals before applying any correction, build the executables, check generation stability, and inspect actual skips. Use independent code-quality and behavior/conformance review. Reassess the existing 40-clause inventory and preserve historical evidence boundaries. Phase status changes depend on the corrected revision's results.

Use a sufficiently short temporary root for Unix-socket acceptance tests and enough disk space for builds. A source-free environment correction does not authorize skipping a failing test, accepting a new product limitation, or investigating F17. Raw verification and recorder failures remain distinguishable from product results.

## Overengineering and Overspecification Considerations

Every production change serves FRQ-01 or FRQ-02 at an existing owner. FRQ-03 through FRQ-05 add evidence at existing test seams and require no production architecture. NRQ-01 requires the whole-project checks and independent acceptance inventory. No new abstraction, compatibility path, fallback, benchmark, setting, dependency, or test framework is introduced.

The implementation may choose private helper names and split test-only fixture decoding into a focused file. It may reuse existing test helpers when their assertions remain independent of production output. Those choices do not change the continuation boundary, recovery invariant, or evidence obligations above.

## Open Questions

No unresolved implementation choice requires user input. Actual compiling RED/GREEN results, sensitivity controls, full checks, and platform skips belong to implementation verification. The [baseline](baseline.md) records passing unchanged-source integration and coverage runs with a short temporary root and the accepted vulnerability exception.

## References

- [Requirements](requirements.md): local correction requirements.
- [Fact collection](facts.md): source paths and existing assertion boundaries.
- [Target picture](target-picture.md): authority and exclusions.
- [Phase solution](../solution.md): existing component ownership.
