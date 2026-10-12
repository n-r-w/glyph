# Generator integration evidence

This document describes UNT-06 behavior and verification. The [migration plan](migration-plan.md#sequential-implementation-units) records unit acceptance and subsequent work.

The [target solution](solution.md#policy-invocation-and-participating-handlers) defines ownership. The [request-handler evidence](request-integration-evidence.md) describes the accepted foundation and its historical limits.

## Implemented path

Host captures request handlers and generators from one admitted handler snapshot. The public policy input supplies extension identifiers, runtime generations, handler identifiers and registration positions. `CompactionParticipant` is the generated public identity. Separate consumer-owned Host contracts retain the controller and integration boundaries. Current request callers use the generalized identity directly.

The SDK exposes `CompactionPolicyContext.StartGenerator` and `CompactionGeneratorOperation.Wait`. The controller admits the nested operation under the policy lifetime. The integration owner validates policy authority, captured membership, registration position, runtime generation and session identity before dispatch. Host routes one selected generator and returns its result.

Request and generator callers share session-binding validation and participant lifetime ownership. Their dispatch and result mapping remain separate. A generator receives ordinary session-bound access and a bounded lifetime without policy authority. This restriction also applies inside the policy runtime. The runtime adapter forwards the participant lifetime into generator dispatch. For legacy generation, `contextcompaction.Service` continues to leave `ParentCompactionID` absent.

`extensioncontext.Service` captures execution errors before `child.finish` cancels and joins the lifetime during normal teardown. The service cancels and joins nested work before release. Expiry remains a deadline failure, including a late successful generator return. Failure results retain invocation, execution, caller and acquired child causes.

The generator controller preserves the SDK canceled event for cancellation-only errors. Its cancellation check excludes deadlines and preserves classified failure precedence. A classified generator error retains its category in diagnostic text alongside the Host operation category. Successful results retain summary, retained boundary, producer or model source, normalized usage and optional details. Host rejects an extension producer that differs from the selected generator. Candidate and session validation stay at their established boundaries.

## Reference policy

The public-only fixture in `testdata/external-plugins/cmd/compaction-policy` preserves the accepted request composition. When a result exists after request composition, the policy returns before generator lookup. Otherwise, the policy requires exactly one captured generator and invokes it with the immutable original and final current request. Zero or multiple generators produce an explicit policy failure.

When the nested generator operation is canceled while the caller remains active, the policy returns its explicit canceled result and requests no replacement. The fixture also supplies independent and classified failure scenarios and a forged producer scenario.

The generator can request a distinct configured model with ordered text/image/text user input and assistant text blocks. It receives ordinary retry progress and reports the model selection and normalized usage in its result. The conversation model, reasoning choice and original history remain separate from that configured request. A blocked-model fixture exercises public caller cancellation, participant expiry and classified provider failure.

## Permanent tests

`TestProgrammaticManualRequestHandlers` includes these generator scenarios:

- `TestSameRuntimeGenerator` and `TestSeparateGenerator`: selected generation, captured runtime identity, replacement and preserved original history.
- `TestClearResult`, `TestMultipleGenerators`, `TestComposition` and `TestCompleteResultBypassesMultipleGenerators`: generation after clearing, explicit cardinality failures and complete-result bypass.
- `TestGeneratorCancellation`, `TestGeneratorMixedCancellation`, `TestGeneratorClassifiedCancellation` and `TestGeneratorProducerRejection`: terminal outcome distinctions and no replacement on cancellation or failure.
- `TestGeneratorConfiguredImages`: ordered image bytes, explicit configured selection, normalized usage, present-empty details, conversation reasoning isolation and the next provider input.
- `TestGeneratorRetryProgress`: identical retried configured input, the ordinary four-attempt allowance, one-second first delay and complete failed-provider text through SDK progress.
- `TestRejectedGeneratorPreservesComposedState`: an out-of-branch generated boundary, complete rejection text, no commit, unchanged original entry projections and failure-observer state with the preceding requests and absent result.
- `TestGeneratorCallerCancellation`, `TestGeneratorParticipantTimeout` and `TestGeneratorProviderFailure`: provider completion before public terminal return, retained handled-child causes, deadline precedence, provider category and cleanup text, and unchanged original history.

`TestPolicyGeneratorInvocation` checks the integration owner's contract with generated mocks. It covers exact policy selection among two generators, non-authorizing lifetimes, normal teardown with joined children, result metadata, authority and captured-membership rejection, unavailable or replaced generations, changed session identity or incarnation, expiry, late success and earlier caller termination. Its timing scenarios use a fake clock.

Public tests use real application, runtime, SDK and session wiring. They replace the external provider HTTP transport. Layer tests establish the integration owner's behavior under its consumed contracts.

## Collected corrections

The reference policy installs a generated candidate after its existing projection succeeds. It performs one projection before replacement. If the session owner rejects the generated candidate, the policy reports the preceding original/current requests and absent result to failure observers. Successful direct-result, request-result and generated-result replacement retain their established callers.

The selected-generator join fixture signals completion of tracked child work before its final lifetime release. It retains the fatal join assertion through testify. A deterministic overlay holds the test goroutine after release: the original ordering failed its join assertion, and the corrected ordering passed. No sleep, test retry or production join change was added.

The new public rejected-candidate regression has fresh compiling assertion RED and corresponding GREEN. Approved wording corrections preserve ownership, caller limits and test sections.

## Final verification

All 33 final command records for code checkpoint `87699e9` have exit code 0 and identical before-and-after source maps. The commands include generation twice, formatting, fix dry-run analysis, lint, unit and integration tests, coverage, build, package loading, external-module checks, and repeated race regressions. The fix proposal is empty. Coverage is 83.8%, above the required 80.0%.

Each listed public generator scenario passed 20 repetitions with race detection. Selected generator authority, binding and lifetime tests passed 200 executions across `-cpu=1,2`. Seven standard-suite skips remain recorded; no pass is claimed for those skipped behaviors.

## Evidence limits

Fresh compiling public RED runs preceded the core generator path, configured-image fixture, separate-runtime fixture, blocked-model fixture and retry-progress fixture. Their corresponding targeted GREEN runs exercise the assembled path. Complete-result bypass is a characterization of accepted behavior.

Supplementary operation-owner and normalized-usage checks also have compiling counterfactual RED runs and ordinary GREEN runs. Those RED runs use preserved overlays. They establish sensitivity to missing generator invocation or altered accounting. They do not establish first-ever edit order. One earlier overlay run ended with a fake-clock deadlock after an assertion; a later assertion-only run replaced it as qualifying evidence. Failed compilation, fixture setup, lint and check records remain retained.

The initial Git inspection and an early diff inspection lacked reserved log/result pairs. Later verification commands reserve exclusive logs and result files and record their commands, working directories, source manifests and outcomes. Historical command records and overlay files remain outside the repository. This document contains no temporary evidence links. The permanent tests provide reproducible checks; readers cannot audit all historical records from this document alone.

Preparation used targeted checks before the final verification. The integration-tagged lint scope follows the project task. The retained supplementary non-integration package lint run failed with established-unit findings and some new-line findings that were later corrected. A separate historical `golangci-lint` run used a changed-line scope and reported no findings in added or changed source. That restricted result is not a passing unrestricted package lint run. Unchanged-source findings remain outside this slice.

## Migration boundary

Automatic preparation and overflow continue through `contextcompaction`. Result handlers, observations, automatic preparation, overflow migration, final cleanup and the bundled algorithm retain their allocated units. The five-minute positive participant duration and the accepted `compactionOperation.context` debt remain in place. Dependency manifests, project rules, Codex diagnostics and protected shutdown fixtures remain unchanged.
