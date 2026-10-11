# Request-handler integration evidence

The [migration plan](migration-plan.md#sequential-implementation-units) owns implementation and acceptance status. The [target solution](solution.md#policy-invocation-and-participating-handlers) owns the system contract. The [manual integration evidence](manual-integration-evidence.md) describes the accepted manual foundation and its retained evidence limits.

## Implemented path

`extensioncontext.Service` captures request participants with the manual operation. The policy input contains each participant's extension identifier, runtime generation, handler identifier, and position in the captured request sequence.

The public SDK exposes `CompactionPolicyContext.StartRequestHandler` and `CompactionRequestOperation.Wait`. The Extension controller admits the asynchronous Host request under the policy lifetime. Its consumer-owned command and response types preserve the dependency direction described in the target solution.

The operation owner checks policy authority and captured membership. It issues an ordinary session context for the selected generation and creates a separate participant lifetime. The runtime manager invokes exactly one request handler through its registered-generation mechanism. The runtime adapter returns the handler's typed action to the policy extension.

`extensioncontext.PolicyRuntime.InvokeCompactionRequest` uses participant, invocation, action, and nested request/result types owned by `extensioncontext`. The runtime manager converts these values at its dispatch boundary. It reuses captured-generation resolution, operation accounting, and runtime invocation. The retained `contextcompaction` automatic and overflow consumer uses `HandleCompactionRequest` with its own contracts. Both entries dispatch through the runtime's request-handler mechanism.

The participant receives ordinary session and configured-model access. Its lifetime identifier authorizes neither replacement nor participant invocation. This distinction applies when the participant and policy share a runtime.

The owner applies the [Host participant duration](../../../../../guidelines/configuration.md#compaction-participant-lifetime) to each admitted invocation and its nested work. Caller cancellation and earlier deadlines take precedence. The owner captures execution failure before normal teardown cancels the lifetime, so successful teardown preserves the action. The owner cancels and joins participant children before releasing their lifetime. It retains handled participant-child causes for a later terminal failure. Runtime cancellation waits for the dispatched SDK operation to settle. Host receive loops and parent state locks remain available during nested waits.

## Reference composition

The public-only executable in `testdata/external-plugins/cmd/compaction-policy` registers five request handlers in deliberate registration order. The reference policy establishes detached original and current request values, then invokes one captured participant per Host call.

The first handler replaces the request and supplies a result. The second derives a request replacement from the original and preserves the result. The third preserves the request and replaces the result. The fourth can preserve, clear, cancel, fail, or return an invalid action. The fifth checks the final state after successful composition.

The SDK synchronously clones the complete participant invocation before enqueue. The reference policy passes its selected original, current request, and current result directly to that ownership boundary. It keeps the initial separation between original and current requests.

The policy validates each action before installing its independent request and result values. After cancellation, failure, or an invalid action, the policy stops composition and requests no replacement. A cleared result produces an explicit reference-policy failure in this unit. Generator invocation belongs to UNT-06.

Host returns the action and retains operation ownership. The reference extension owns the composition loop and the original values.

## Permanent public evidence

- `TestProgrammaticManualRequestHandlers` exercises real application, runtime, SDK, and session wiring. It checks registration order, same-runtime and separate-runtime generations, detached originals, request-only and result-only replacement, result clearing, terminal actions, preserved history, and the next actual provider request.
- Participant input artifacts use exclusive creation. The tests compare each handler's original request with the first captured original. They check the independent current values and the absence of a later participant after terminal actions.
- The Programmatic, UI, and Extension manual terminal-state suites include request-handler cancellation, failure, invalid action, and a handled participant-child preparation rejection followed by a later handler failure. They check the acquired composition state, both preparation and handler causes, and absence of a replacement.
- `TestProgrammaticManualCancellationStopsNestedModel` includes participant-owned configured-model work. Public parent cancellation stops that provider work before terminal return and leaves the original history intact.
- `TestProgrammaticManualParticipantTimeout` configures a two-second participant duration through YAML and enters through Programmatic manual compaction without caller cancellation. The real participant handles a model-selection rejection, then starts nested configured-model work. The test checks timeout text, the acquired selection cause, stopped provider work before terminal return, and retained original history.

These tests replace only the external provider HTTP transport. UI evidence enters through the established public UI protocol fixture. It is not an interactive terminal proof.

## Layer evidence

`TestPolicyRequestInvocation` checks selection of one captured participant, authority and membership rejection, independent typed action return, shared-runtime lifetime restrictions, unavailable captured generations, and handled nested-cause retention with generated mocks.

`TestCompactionRequestValues` checks detached preceding-marker details and persisted accounting through request mapping. The shared marker and public-value mappers retain their automatic, overflow, and manual consumers.

`TestPolicyRequestDeadline` uses a fake clock to check participant expiry, an earlier caller deadline with its cause, caller cancellation with its cause, joined delayed child cleanup, and released lifetime state. The existing successful request-action tests check normal teardown. `TestSettingsSuite` checks the five-minute default, a configured positive duration, and rejection of zero, negative, and malformed durations through the YAML loader.

Layer tests establish the corresponding logic under their mock or pure-mapping contracts. The public cases above establish runtime and session wiring.

## Causal checks and evidence limits

Fresh uncached public tests first failed because the manual policy supplied its earlier direct replacement without request composition. The request-cancellation test also failed because the policy completed before participant-owned model work started. Their corresponding executions pass with the request path.

Compiling missing-behavior runs reproduced absent dispatch, accepted invalid authority or membership, missing handled-child causes, and missing captured-generation rejection. The corresponding operation-owner tests pass after restoration of the implementation.

Capture-discrimination runs omitted participant metadata while retaining compiling schemas and SDK declarations. The expanded public request cases and all three terminal-state routes failed. Their corresponding executions pass with captured metadata restored.

A separate public cause-discrimination run omitted the transfer of handled participant-child causes to the policy parent. The final failure retained the handler cause but lost the preparation cause. The same permanent test passes with cause transfer restored. The reference policy keeps error text outside candidate details.

Marker-mapping tests failed when request encoding omitted preceding accounting or entry-marker details. They pass with the complete shared marker projection.

The unit handoff records exact command results and source identities. The initial command records have reserved logs and results but no per-command source manifest. Later records include before-and-after source manifests. Counterfactual runs are identified separately from the initial behavior RED runs. No broader historical test-first claim is made.

### Participant-deadline correction

Fresh compiling ownership RED failed because an invocation exceeded its configured duration without a deadline cause. Configuration RED failed on an omitted default, a rejected positive duration, and missing positive-duration validation. Public RED canceled the still-pending operation after its assertion deadline and failed because Host had not exposed a participant timeout. The corresponding corrected tests pass. Caller-deadline and caller-cancellation cases are characterization checks of preserved behavior.

Checkpoint `84446d6` is intermediate evidence. The deadline correction and the behavior-preserving redundant-result-guard cleanup require source-matched final verification and unit reviews before UNT-05 acceptance. The retained orchestration helper unpacks a supplied result once, returns detached data, and bypasses generation. Generation failure classification remains unchanged.

## Migration boundary

UNT-06 through UNT-11 retain their approved scope. This unit adds no generator or result-handler invocation API, bundled sizing algorithm, general extension-to-extension service, or Host composition loop. Automatic preparation and overflow still use their unmigrated mechanisms.

The accepted temporary Context debt remains limited to `compactionOperation.context`. Participant lifetimes use that owner with its synchronous cause guards and explicit cancel-and-join teardown. The accepted fixture environment access, fixed dependency manifests, UI initialization repair, and executable fixture protection remain in place.
