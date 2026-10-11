# Request-handler integration evidence

The [migration plan](migration-plan.md#sequential-implementation-units) records implementation and acceptance status. The [target solution](solution.md#policy-invocation-and-participating-handlers) describes the system contract. The [manual integration evidence](manual-integration-evidence.md) describes the accepted manual foundation and its retained evidence limits.

## Implemented path

`extensioncontext.Service` captures request participants with the manual operation. The policy input contains each participant's extension identifier, runtime generation, handler identifier, and position in the captured request sequence.

The public SDK exposes `CompactionPolicyContext.StartRequestHandler` and `CompactionRequestOperation.Wait`. The Extension controller admits the asynchronous Host request under the policy lifetime. Its consumer-owned command and response types preserve the dependency direction described in the target solution.

The operation owner checks policy authority and captured membership. It issues an ordinary session context for the selected generation and creates a separate participant lifetime. The runtime manager invokes exactly one request handler through its registered-generation mechanism. The runtime adapter returns the handler's typed action to the policy extension.

`extensioncontext.PolicyRuntime.InvokeCompactionRequest` uses participant, invocation, action, and nested request/result types owned by `extensioncontext`. The runtime manager converts these values at its dispatch boundary. It reuses captured-generation resolution, operation accounting, and runtime invocation. The retained `contextcompaction` automatic and overflow consumer uses `HandleCompactionRequest` with its own contracts. The runtime manager dispatches both `InvokeCompactionRequest` and `HandleCompactionRequest` through the runtime's request-handler mechanism.

The participant receives ordinary session and configured-model access. Its lifetime identifier authorizes neither replacement nor participant invocation. This distinction applies when the participant and policy share a runtime.

The owner applies the [Host participant duration](../../../../../guidelines/configuration.md#compaction-participant-lifetime) to each admitted invocation and its nested work. Caller cancellation and earlier deadlines take precedence. The owner captures execution failure before normal teardown cancels the lifetime, so successful teardown preserves the action. The owner cancels and joins participant children before releasing their lifetime. It retains handled participant-child causes for a later terminal failure. During runtime cancellation, the adapter `Runtime` in `host/internal/infra/plugins/extension/runtime` initiates SDK cancellation and waits for the cancellation operation to settle dispatched work. Host receive loops and parent state locks remain available during nested waits.

## Reference composition

The public-only executable in `testdata/external-plugins/cmd/compaction-policy` registers five request handlers in deliberate registration order. The reference policy establishes detached original and current request values, then invokes one captured participant per Host call.

The first handler replaces the request and supplies a result. The second derives a request replacement from the original and preserves the result. The third preserves the request and replaces the result. The fourth can preserve, clear, cancel, fail, or return an invalid action. The fifth checks the final state after successful composition.

The SDK synchronously clones the complete participant invocation before enqueue. The reference policy passes its selected original, current request, and current result directly to that ownership boundary. It keeps the initial separation between original and current requests.

The policy validates each action before installing its independent request and result values. After cancellation, failure, or an invalid action, the policy stops composition and requests no replacement. A cleared result produces an explicit reference-policy failure in this unit. Generator invocation belongs to UNT-06.

Host returns the action and retains operation ownership. The reference extension owns the composition loop and the original values.

## Permanent public evidence

- `TestProgrammaticManualRequestHandlers` exercises real application, runtime, SDK, and session wiring. It checks registration order, same-runtime and separate-runtime generations, detached originals, request-only and result-only replacement, result clearing, terminal actions, preserved history, and the next actual provider request.
- The reference request handlers create participant input artifacts with exclusive creation. The tests compare each handler's original request with the first captured original. They check the independent current values and the absence of a later participant after terminal actions.
- The Programmatic, UI, and Extension manual terminal-state suites include request-handler cancellation, failure, invalid action, and a handled participant-child preparation rejection followed by a later handler failure. They check the acquired composition state, both preparation and handler causes, and absence of a replacement.
- `TestProgrammaticManualCancellationStopsNestedModel` includes participant-owned configured-model work. When the public parent operation is canceled, Host stops that provider work before terminal return. The original history remains intact.
- `TestProgrammaticManualParticipantTimeout` configures a two-second participant duration through YAML and enters through Programmatic manual compaction without caller cancellation. The real participant handles a model-selection rejection, then starts nested configured-model work. The provider transport returns cancellation joined with `nested provider cleanup source`. The test checks timeout text, both acquired causes, stopped provider work before terminal return, and retained original history.

- `TestProgrammaticManualProviderFailures` exercises nested configured-model failures before and during participant expiry. Its cancellation-overlap cases return an HTTP rejection, fail cleanup after reading a complete rejection body, or fail both response-body reading and closing after receiving bytes. Its active-caller cases check a full SDK rejection envelope, structured context overflow with failed cleanup, and HTTP-success stream read and cleanup failures. It checks received diagnostics and acquired causes in the public operation failure, including the configured-model `CONTEXT_LIMIT` category for structured overflow. Every case checks the earlier model-selection cause, joined provider work, and retained original history.

These tests replace only the external provider HTTP transport. UI evidence enters through the established public UI protocol fixture. It is not an interactive terminal proof.

## Layer evidence

`TestPolicyRequestInvocation` checks selection of one captured participant, authority and membership rejection, independent typed action return, shared-runtime lifetime restrictions, unavailable captured generations, and handled nested-cause retention with generated mocks.

`TestCompactionRequestValues` checks detached preceding-marker details and persisted accounting through request mapping. The shared marker and public-value mappers retain their automatic, overflow, and manual consumers.

`TestPolicyRequestDeadline` uses a fake clock to check participant expiry, an earlier caller deadline with its cause, caller cancellation with its cause, joined delayed child cleanup, and released lifetime state. The existing successful request-action tests check normal teardown. `TestSettingsSuite` checks the five-minute default, a configured positive duration, and rejection of zero, negative, and malformed durations through the YAML loader.

`TestDriverStreamCancellationRetainsHTTPRejection` checks the complete received body, including malformed text, and HTTP status in the canceled provider error and terminal response. It checks retry classification and provider delay in the returned error. `TestErrorCaptureTransportRetainsReadAndCloseCauses` and `TestDriverStreamRetainsReadAndCloseCauses` check both acquired body-failure identities through capture and model execution. Their close-only cases check the received rejection status and complete body alongside the acquired cleanup cause. `TestErrorCaptureTransportKeepsExistingCause` includes an SDK-supplied HTTP rejection and checks SDK error identity and the absence of duplicate diagnostics.

`TestRequestProducerFailureRetainsPrecedingState` checks detached successful result details and unchanged failure-observer state after a later producer mismatch in retained threshold orchestration. The test was applied to both implementations of `applyValidatedRequestAction`: the helper from commit `904d754` supplied through an overlay, and the helper from commit `823ec09`. Both executions passed. The second implementation validates the producer before acquiring the action's detached next request and result values. This comparison did not test an unmodified checkout of `904d754`, and the test did not exist in that commit.

The layer tests check individual component behavior. The settings tests load YAML from real files. The public cases above establish runtime and session wiring.

## Causal checks and evidence limits

Fresh uncached public tests first failed because the manual policy supplied its earlier direct replacement without request composition. The request-cancellation test also failed because the policy completed before participant-owned model work started. Their corresponding executions pass with the request path.

Compiling missing-behavior runs reproduced absent dispatch, accepted invalid authority or membership, missing handled-child causes, and missing captured-generation rejection. The corresponding operation-owner tests pass after restoration of the implementation.

Test runs with participant metadata omitted retained schemas and SDK declarations that compiled. The expanded public request cases and all three terminal-state routes failed. Their corresponding executions pass with captured metadata restored.

A separate public test run used an implementation without the transfer of handled participant-child causes to the policy parent. The final failure retained the handler cause but lost the preparation cause. The same permanent test passes with cause transfer restored. The reference policy keeps error text outside candidate details.

Marker-mapping tests failed when request encoding omitted preceding accounting or entry-marker details. They pass with the complete shared marker projection.

Historical exact command records and available source manifests are retained in the development workspace outside the repository. The permanent tests listed here are the repository's reproducible evidence. Readers cannot audit the historical records from this document alone. The initial command records have reserved logs and results but no per-command source manifest. Later records include before-and-after source manifests. Counterfactual runs are identified separately from the initial behavior RED runs. No broader historical test-first claim is made.

### Participant-deadline correction

A fresh ownership test run compiled but failed because an invocation exceeded its configured duration without a deadline cause. Configuration tests failed because the default was omitted, a positive duration was rejected, and positive-duration validation was missing. After the public test's assertion deadline, the test canceled the still-pending operation. The test failed because Host had not exposed a participant timeout. The corresponding corrected tests pass. Caller-deadline and caller-cancellation cases are characterization checks of preserved behavior.

Checkpoint `84446d6` is intermediate evidence. The deadline correction and the behavior-preserving redundant-result-guard cleanup passed source-matched final verification and unit reviews. The retained orchestration helper unpacks a supplied result once, returns detached data, and bypasses generation. Generation failure classification remains unchanged.

### Codex transport-cause preservation

Fresh compiling RED runs reproduced the loss of a transport cause returned after request cancellation. `TestDriverStreamCancellationRetainsTransportFailure` failed on missing error identity at the Codex boundary. `TestProgrammaticManualParticipantTimeout` failed on the missing transport marker in the public failure. Both tests pass after the correction in the existing Codex capture transport. These executions reconstruct the behavior; the earlier temporary probe has no retained source snapshot or recorded intermediate SDK error values.

`TestErrorCaptureTransportJoinsMissingCause` failed when the combining method omitted the acquired cause, then passed after restoration. `TestErrorCaptureTransportKeepsExistingCause` characterizes preservation of the full SDK error when it already contains the captured cause. The provider suite also checks pure cancellation, stream-finalization failures, and terminal-delivery failures. The adapter combines acquired transport errors before cancellation classification and assembler finalization. Dependencies, public schemas, retry policy, and participant ownership remain unchanged.

### Received rejection and body-cleanup corrections

Fresh runs of the permanent regression tests compiled but failed because cancellation hid a received HTTP rejection and a response-body read failure hid its acquired close cause. The failures occurred at capture, provider execution, and Programmatic manual-operation boundaries. The corresponding regressions pass after correction.

A further close-only regression failed when cleanup discarded a completely read rejection. Its capture, provider, and public executions pass after the transport retains that rejection before returning the cleanup failure.

The capture transport retains received rejection status, headers and every acquired body byte, including partial reception. It combines acquired read and close causes. A read cause indicates incomplete reception. When capture succeeds, the capture transport replays the body for the SDK. The capture transport owns the original rejection body and closes it once. When the SDK exposes only the inner error object of the received envelope, the provider preserves the full received envelope. In this case, the provider also retains the SDK error identity without repeating the inner diagnostic. The provider supplies a usable received structured code to the shared classifier, with status fallback and received retry metadata. The driver settles SSE cleanup before terminal publication and combines acquired execution, handler and cleanup causes. When the caller cancels the request, the driver retains the aborted outcome alongside acquired failure text. `TestDriverReceivedDiagnostics` and `TestDriverStreamCleanup` check these provider boundaries. `TestProgrammaticManualProviderFailures` checks these public diagnostics, the structured overflow category, cancellation-overlap causes and retained history. Pure cancellation and ordinary retry behavior remain covered by their established tests.

The later rejection-envelope, overflow-classification, partial-capture, and cancellation-with-cleanup regressions have recorded corrective RED/GREEN cycles. Their RED runs compiled the Codex files from `823ec09` through overlays and failed on the expected assertions. Subsequent GREEN runs used the restored implementation with identical test source. The historical evidence limits above remain.

## Migration boundary

UNT-06 through UNT-11 retain their approved scope. This unit adds no generator or result-handler invocation API, bundled sizing algorithm, general extension-to-extension service, or Host composition loop. Automatic preparation and overflow still use their unmigrated mechanisms.

The accepted temporary Context debt remains limited to `compactionOperation.context`. `extensioncontext.Service` and `compactionOperation` implement Host participant lifetimes with synchronous cancellation checks and explicit cancel-and-join teardown. The accepted fixture environment access, fixed dependency manifests, UI initialization repair, and executable fixture protection remain in place.
