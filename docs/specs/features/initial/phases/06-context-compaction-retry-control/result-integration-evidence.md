# Result-handler integration evidence

This document describes UNT-07 behavior and verification. The [migration plan](migration-plan.md#sequential-implementation-units) records unit acceptance and subsequent work.

## Public operation path

During manual compaction, `extensioncontext.Service` captures result participants from the admitted handler set. The policy input contains their extension, runtime generation, handler, and registration position. The public SDK exposes `CompactionPolicyContext.StartResultHandler` and `CompactionResultOperation.Wait`.

The Extension controller decodes four required values: original request, final current request, original result, and current result. Host checks policy authority, membership in the captured result list, and session/runtime binding. The operation owner applies the configured participant duration and issues a separate nested-work lifetime. The selected handler receives ordinary session-bound model access. Only the policy receives participant-invocation and replacement authority.

The runtime manager reserves the captured generation and dispatches one result handler through the runtime adapter. The adapter forwards the participant lifetime into the SDK. The retained automatic/overflow caller supplies an absent parent lifetime.

For a replacement attributed to an extension, Host requires the selected result handler's extension identity. For example, a handler from extension B can preserve extension A's result, but a replacement attributed to extension A fails. Model-attributed replacements retain their historical selection and optional normalized usage. The session owner validates that accounting without requiring the conversation model.

## Policy-owned acceptance and composition

The reference policy retains the original request and final current request. At result-chain entry, it clones the accepted result as the immutable original result. It invokes captured result participants in registration order and supplies the last accepted result to each participant.

The policy checks every action before changing its state. For PRESERVE, the policy retains the prior result and source. REPLACE requires a candidate. For exclusive CANCEL, the policy ends composition before mutation. CLEAR, missing replacements, preservation with a candidate, unknown actions, and mixed cancellation fail before another handler runs.

The policy requests session projection at each new-result acceptance site:

- A request handler's replacement result is projected before either value in a combined request/result update is installed.
- A generated result is projected before installation.
- A direct policy result is projected before installation.
- A result handler's replacement is projected before installation or delivery to another result handler.

Preservation and clearing introduce no result candidate. The policy enters result handling with an accepted result and commits the final accepted result without repeating projection at chain entry or finalization. Host and session owners retain live identity, active-leaf, accounting, tool-pair, cancellation, and runtime guards at durable commit.

After rejection or a participant failure, the policy returns its last accepted state with the acquired failure text. Host cancels and joins participant children before releasing their lifetime. Host retains acquired child causes and the durable receipt independently of diagnostic state. The session owner retains original entries and atomically appends one marker. Host records the receipt before publication.

## Permanent public evidence

`TestProgrammaticManualResultHandlers` uses the production Programmatic entry, application assembly, runtime, SDK, and session services. Only the external provider transport is replaced.

- The ordered-composition test checks deliberately nonlexical registration order, original and current requests and results, participant-local mutation isolation, and the next provider request. Stored history contains the original entries and one final marker.
- The separate-runtime test checks selected participants outside the policy runtime and preservation of their result source.
- The original-derived replacement test checks a later transformation from the immutable original result.
- Request-supplied and generated-result cases check the corresponding entry into result composition.
- The configured-model test checks ordered text/image input, complete normalized usage, reasoning selection, details presence, and conversation isolation.
- The historical-model test checks a historical selection with absent usage independently of configured conversation selection.
- The terminal-action tests check explicit and invocation cancellation, mixed cancellation, CLEAR, missing replacement, preservation with a candidate, unknown disposition, producer mismatch, participant failure, classified failure, and out-of-branch rejection. Each case checks unchanged durable entries, the preceding accepted observer result, and absence of the later handler.
- The combined request/result and direct-policy rejection tests check that the rejected candidate remains absent from observer state. The combined-update test also checks that the policy retains the preceding request and stops later request invocation.
- The caller-cancellation, participant-timeout, and provider-failure tests check joined nested model work, acquired provider/selection/cleanup causes, failure precedence, and unchanged durable history.

The existing request, generator, manual-entry, outcome, receipt, guard, and session-projection suites remain tests of behavior that this unit must preserve. In particular, `TestRejectedGeneratorPreservesComposedState` checks that a rejected generator leaves the observer result absent, as it was before generation.

## Layer evidence

`TestPolicyResultInvocation` checks one selected result dispatch, captured order, wrong-kind membership, foreign authority, runtime identity, session identity/incarnation, non-authorizing lifetime, normal cleanup, and acquired invocation/child cause identities. Its fake-clock scenarios check participant expiry, late success, earlier caller deadline, caller cancellation, delayed cleanup, and complete release.

The controller and runtime use their existing request/result value mappings and captured-generation accounting. The protocol and mock generators update the public protocol output and consumer mocks. Existing SDK fixtures explicitly initialize the added completion and participant fields.

## Final verification

All 35 final post-review commands finished with exit 0 on identical before-and-after source maps. The commands include generation twice, formatting, fix dry-run analysis, lint, unit and integration tests, coverage, build, package loading, external-module checks, and repeated race regressions. The fix proposal is empty. Coverage is 83.8%, above the required 80.0%.

Each of the 23 public result scenarios passed 20 repetitions in both the broad manual suite and the dedicated result suite. Each of the 17 result-layer scenarios passed 200 executions across `-cpu=1,2`. Suite and intermediate parent events are excluded from these scenario counts. Seven standard-suite skips remain evidence gaps for their named behavior.

The independent analyst approved the implementation behavior. After the final checks, only the roadmap, migration progress and this evidence document changed to record acceptance. All other 1,214 files match the checked versions.

## Causal checks and evidence limits

Fresh uncached public assertion failures first reproduced the missing result route and request/direct candidates installed before projection. Additional missing-behavior runs temporarily disabled result composition or selected-result dispatch while keeping the tests compiling. Restored implementations passed the corresponding targeted checks. The later disabled-implementation runs check the missing behavior; they do not establish that every test preceded implementation.

The first public test attempt failed while building the new fixture and is retained as a compile failure. The first recorder launch failed before Git inspection because the interpreter name was unavailable. The first launch's log/result pair was reconstructed after failure rather than reserved beforehand. Those attempts are excluded from assertion RED evidence. Historical command records, failure records, and source maps are retained outside the repository.

### Child-join test observation correction

The post-review result-layer repetition reported 199 passes and one failure in `TestPolicyResultInvocation/TestSelectedAction`. The test released its tracked child before closing the completion signal, so Host could finish its tracked join before the test observed that signal. The corrected worker closes the completion signal before deferred release, following the accepted generator test's ordering. The production join code and assertion remain unchanged.

The failed post-review matrix and original/corrected deterministic observations are retained outside the repository. After the test correction, all 35 commands were restarted on the final code. The final verification above describes that passing run.

This document identifies reproducible repository tests. Later migration units retain their approved scope and order.
