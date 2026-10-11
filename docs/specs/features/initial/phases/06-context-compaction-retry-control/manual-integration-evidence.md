# Manual compaction integration evidence

Status: UNT-04 implementation and executor checks are complete. The integration coordinator owns the remaining fresh CodeQuality and independent analysis passes. Acceptance remains pending those passes.

The [migration plan](migration-plan.md#sequential-implementation-units) owns unit order and acceptance. The [target solution](solution.md#manual-automatic-and-overflow-integration) owns outcome notification requirements.

## Implemented paths

`modelexecution.Service` implements the UI, Programmatic, and session-bound Extension manual adapters. `extensioncontext.Service` captures one operation snapshot and invokes the registered policy generation. The runtime and SDK expose asynchronous candidate projection and replacement operations. `sessions.Service` validates the captured branch, persists one replacement, and records its receipt before publication.

The policy invocation receives replacement authority. Nested operations receive a separate lifetime identifier. Host cancels and joins nested work before releasing operation state. The session owner retains the original entries and supplies the replacement summary plus retained suffix to the next model request.

## Outcome state and observers

The extension supplies acquired original and last valid composed values through `CompactionPolicyResult.state`. The policy can return acquired failure text with that state. `CompactionOutcome.state` is absent when Host did not receive policy diagnostic values. Host records the durable receipt independently of diagnostic state availability.

Host captures success and failure observer generations before policy invocation. Host delivers outcomes in registration order and stops at the first observer failure. Each observer receives an ordinary session context and a non-authorizing child lifetime. Host joins observer-created nested work before terminal return. A terminal failure includes handled policy-child and earlier observer-child causes.

Automatic and overflow callers use the same outcome contract. Until their migration units, those callers report the composition state they already own.

## Public-entry evidence

The reference executable in `testdata/external-plugins/cmd/compaction-policy` uses the public Extension SDK.

- `TestProgrammaticManualCompactionUsesRegisteredPolicy` and `TestUIManualCompactionUsesRegisteredPolicy` check replacement and the next actual provider request.
- `TestProgrammaticManualCompactionTerminalStates`, `TestUIManualCompactionTerminalStates`, and `TestExtensionManualCompactionTerminalStates` check canceled policy outcomes, changed-branch rejection, preserved originals, and post-commit failures with a durable receipt and original cause.
- The terminal-state tests also check actual observer failure after commit. The observer performs a configured-model request through the public receive loop and checks that its context has no replacement authority.
- For policy result loss, separate policy, observer, and caller processes use the public SDK. The policy exits after receiving its durable replacement receipt. The surviving observer receives absent diagnostic state, the committed entry, and the acquired transport failure. All three callers receive the receipt and the full transport and observer causes. Stored originals and the next actual provider input remain correct.
- `TestProgrammaticManualCompactionRejectsAbsentPolicy`, the UI terminal-state test, and the Extension terminal-state test check explicit owner absence. `TestProgrammaticManualCompactionRejectsAmbiguousOwners` checks multiple registered owners.
- `TestProgrammaticManualCancellationStopsNestedModel` cancels actual nested configured-model work through the public receive loop. The test checks child completion and absence of a replacement. When the operation owner reports failure, the test checks the retained cancellation and model-execution causes.

The Extension caller retains an ordinary session context from an AgentSettled observer. A Unix-socket signal starts its manual request after conversation settlement.

## RED/GREEN evidence

Surviving implementation reports describe the earlier test-first results below. Some original initial snapshots and logs are unavailable, so those reports cannot establish every historical run's ordering. The explicit kind matrices were added after structural edits and provide post-edit regression evidence. The admission and caller-coordination RED/GREEN logs remain available.

Uncached compiling tests failed on missing policy registration, the UI SDK's rejection of a committed compaction entry, a handled projection rejection that failed its parent, expected child cancellation that failed a committed parent, and policy completion before nested model work started. The corresponding uncached tests pass after their changes.

Before outcome notification integration, the public terminal-state tests failed because the outcome artifact was absent and the observer failure was missing from the public result. The same tests pass after integration.

`TestManualObserverFailureRetainsHandledPolicyCauses` first failed because a later observer failure omitted a handled policy-child cause. An extended test then failed because a later observer failure omitted an earlier observer-child cause. The test now checks both causes, the durable receipt, and joined observer-child teardown. Twenty uncached race-enabled runs pass.

Real process-loss tests initially required EOF text. Repeated checks exposed both EOF and connection reset by peer. The tests check runtime unavailability and preservation of the complete acquired transport text. Twenty uncached race-enabled runs of the public observer-failure and process-loss cases pass.

The caller cancellation test initially required an exact Canceled terminal event. Repeated runs showed that the Programmatic owner can report Failed when cancellation acquires additional runtime or model causes. The test follows the owner's cause-preserving contract and checks cancellation or failure before mutation. Twenty uncached race-enabled runs pass.

## Local review corrections

When an observer context cannot be issued, `extensioncontext.Service` retains the initiating cause, handled policy causes, and earlier observer causes. `TestManualUnavailableObserverRetainsCauses` checks cause identity. The Programmatic and UI terminal-state tests check a real shared policy/observer process exit after commit. Both callers receive the independent receipt, the original RPC failure, and the observer-context failure.

The runtime adapter acquires policy failure text before it decodes diagnostic state. When the policy supplies incomplete state, Host retains both errors and reports absent acquired state. All three public terminal-state tests check this SDK completion after commit.

Outcome observers inherit the manual owner's cancellation. `TestProgrammaticManualCancellationStopsObserverModel`, `TestUIManualCancellationStopsObserverModel`, and `TestExtensionManualCancellationStopsObserverModel` check actual pending observer provider work after commit. Each caller cancels through its public route. The tests check stopped provider work before terminal return, the retained receipt, and cancellation diagnostics. Explicit policy cancellation with a live owning context continues to deliver its failure observation.

The new manual-compaction tests use independent `testify/suite` instances and each scenario's test context. Parallel runners own separate suite instances. Environment-dependent application scenarios remain serialized.

Fresh compiling RED runs reproduced each missing cause and each detached observer cancellation route. The corresponding uncached GREEN runs pass. Twenty uncached race-enabled repetitions pass for the three observer-cancellation routes, invalid-state and unavailable-observer public cases, handled-child teardown, and the policy-phase cancellation case.

## Retained review corrections

`sessions.Service.ProjectCompaction` returns history and its structural error. Both automatic callers preserve that cause. `TestThresholdCompactionPreservesSplitBoundaryCause` uses a public conversation request, actual tool-call/result history, a smaller selected model, and a real registered compaction handler. The test checks the split-boundary cause, supplemental failure code, unchanged provider-call count, and absence of a replacement.

The Extension controller retains scalar lifetime identity and release ownership. At Run, the parent owner links the explicit child context to parent cancellation. The parent registry remains available until every admitted child releases. `TestManualCommitGuardRejectsCanceledParentWithLiveChild` checks the synchronous parent cause at the persistence guard while the child context remains live. The test checks no append, no receipt, and child release.

The reference caller starts once from AgentSettled Run with explicit detached context and ordinary binding arguments. Its collection context remains live after cancellation of the operation owner. The public UI and Extension cancellation tests exercise both pre-commit policy work and post-commit observer work.

The UI cancellation fixture retains and awaits the SDK cancellation operation. Its owner receives acquired start, terminal, and context causes. `TestUICancellationFixtureRetainsRequestFailure` checks a cancellation request failure through the real SDK connection. Expected listener shutdown remains separate from cancellation failure.

The accepted temporary Context debt covers only the parent lifetime field in [manual_operation.go](../../../../../../host/internal/usecase/host/extensioncontext/manual_operation.go). The field comment states its synchronous-check and join purpose and the removal condition. Controller and fixture Context fields are removed. The fixture's `os.Getenv` exception remains local to its configuration inputs. The external-plugin module declares `github.com/samber/mo v1.17.0` indirectly for the SDK's optional prepared fields. Its checksum file contains the two corresponding checksums. The other dependency manifests retain their versions.

The integration coordinator dropped the observer-only snapshot proposal. `extensioncontext.Service` reuses the ordered aggregate snapshot with its automatic and overflow consumers. The remaining accepted corrections simplify receipt routing, registration collection, descriptor ownership, shared content-kind mapping, test observations, fixture artifact writes, stable values, and SDK lookup naming. The public lookup is `FindCompactionPolicy`; no compatibility alias remains.

The real session/history capture scenario runs under the integration tag. Its original data, snapshot-isolation assertions, and fixture dependencies are preserved.

## Admission and dispatch corrections

`BeginCompactionChild` returns release ownership and an admission error. `LinkCompactionChild` supplies the explicit execution context. The controller prepares one typed executable action and retains common gate, linkage, and release ownership.

When nested selection preparation fails after child admission, the controller records the acquired error before scope cleanup. `TestSelectionPreparationRetainsAdmittedChildCause` checks returned error identity, the public rejection category, and once-only child/runtime settlement. Its ordinary-selection control checks the uncorrelated path.

The Programmatic, UI, and Extension terminal-state suites include a policy-handled nested selection rejection followed by durable replacement. Each suite checks observer failure with both the underlying selection and observer causes, and observer success with an empty terminal error. The tests retain receipt, original-history, and next-provider-input checks. The policy keeps candidate details empty and handles the rejection without copying its text into diagnostics. Compiling RED runs reproduced the lost cause on all three public routes; the corrected runs pass.

`TestManualChildAdmissionRetainsDistinctParentCause` checks cancellation-cause identity before child admission. `TestManualChildRejectionCarriesParentCauseThroughSDK` checks the acquired rejection through the real controller, runtime adapter, and SDK peer before parent settlement. Both tests failed when admission omitted the distinct cause and pass with the cause-preserving join.

The SDK defines request completion, validation, failure, and progress rules in request-kind descriptors. A bounded predicate table identifies handler payloads. `TestHostRequestContracts` and `TestHandlerPayloadContracts` check request and handler matching, absent payloads, and unknown kinds. Policy authority and parent lifetime use separate optional prepared values.

The Extension caller's cancellation worker retains unexpected accept, connection-close, and listener-close causes. Unexpected accept failure cancels pending caller work with its original cause. The caller joins coordination before returning its report and collects the terminal receipt with its live collection context. `TestCallerCancellationRetainsFailures` uses real Unix sockets to check retained failure after a committed outcome, unexpected accept cancellation, expected owner shutdown, and incoming cancellation signals.

Startup owns universal handler identity and kind validation before it partitions compaction registrations. The compaction registrar converts that admitted partition in registration order. Projection and capture tests arrange only active history; the receipt test supplies its intended publisher once.

The controller represents optional parent lifetime and child release with `mo.Option`. Admission validates the compaction dependency, and SDK dispatch supplies progress reporters. Execution retains progress-delivery failures.

## Checks

`task fmt`, `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage`, `task build`, `go list ./...`, and `git diff --check` pass. Fix dry-run proposals were analyzed before correction. Coverage is 83.5%; the required minimum is 80.0%. The checks include the external-plugin module.

Two consecutive protobuf and affected mock generation commands produced files with identical SHA-256 manifests.
