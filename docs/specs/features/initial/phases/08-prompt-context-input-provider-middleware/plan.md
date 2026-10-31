# Delivery plan: PHS-08

Deliver the complete [ticket](ticket.md) through independently verifiable behavior slices, followed by author documentation. The [technical solution](solution.md) defines the mechanisms; this plan defines delivery and checks.

## Key definitions and abbreviations

Use the [domain glossary](../../../../../terms.md). A unit is ready only after its behavior evidence, mandatory project checks, temporary-code removal and quality review are complete.

## Delivery strategy

Use the [technical solution](solution.md), especially Data ownership and public operations, Effective-context port and prepared provider binding, and Provider-neutral wire callbacks. Deliver sequentially in the visible working copy. Replace obsolete internal/public paths in the same behavior slice; add no compatibility path, feature flag or storage migration.

## Main changes

- Public extension registration, SDK dispatch and Host middleware orchestration.
- Shared effective-context preparation, text/image input handling and finalized-message replacement.
- Safe provider-request customization and response observation.
- Public examples, author documentation and criterion-to-evidence records.

## Entities and invariants

See the technical solution's Data ownership and public operations, Confidential projection and restoration, and Error categories and operation completion sections. [Decisions](decisions.md) record successful context revision recomposition and confidential serialized replacement.

## New folders and components

See the technical solution's Package and import boundaries and Reference extensions and verification sections. Shared infrastructure is introduced only with the first behavior that consumes it.

## Backward compatibility

No backward compatibility.

## Plan units

| Unit | Outcome | Performer | Status |
| --- | --- | --- | --- |
| UNT-01 | Safe provider-wire middleware through ordinary extension operations | SubAgentDeveloperComplex | Ready |
| UNT-02 | Ordered effective instructions/context after preparation | SubAgentDeveloperComplex | Ready |
| UNT-03 | Public text/image input transformation and full handling | SubAgentDeveloperComplex | Ready |
| UNT-04 | Role-preserving finalized messages used consistently | SubAgentDeveloperComplex | Ready |
| UNT-05 | Public author guide and integrated phase records | SubAgentDeveloperRegular | Ready |

### Unit tree

```text
UNT-01 -> UNT-02 -> UNT-03 -> UNT-04 -> UNT-05
```

The sequence avoids concurrent changes to shared public envelopes, SDK predicates, startup, middleware registry, application assembly and integration fixtures.

### Decomposition justification

Each slice starts at an actual public client or extension invocation and ends at an observable request, result, persisted entry or public failure. Tests use existing application/extension/HTTP infrastructure. No helper API or temporary implementation exists solely to verify a unit. UNT-01 leads because private replay restoration and SDK failure precedence are the highest-risk implementation assumptions. UNT-02 establishes the effective-context port before input/finalization integrations consume the completed model boundary. Each unit includes all layers required by its behavior.

## Overengineering and overspecification considerations

The five units follow the distinct invocation/result boundaries already selected in the solution. Shared contracts are delivered with their first user, not as an unobservable schema-only unit. No future provider registration, recursion framework, generic patch language, alternative executor, compatibility branch or unrelated cleanup is planned. Exact declarations, local helpers and fixture layout remain implementation details.

## UNT-01: Slice, safe provider-wire middleware

### Goal

An ordinary extension can transform public headers and safe serialized requests and observe response metadata through the shared raw-provider boundary.

### Behavior

A client starts a model request with ordered registered wire handlers. The final request reaches the provider with the selected transformations and preserved or deliberately discarded private replay. The client receives complete categorized failure if request/response middleware fails, and Host starts no recovery attempt for that failure.

### Work

Implement the solution's Data ownership and public operations, Provider-neutral wire callbacks, Confidential projection and restoration, Error categories and operation completion, and Package and import boundaries as needed for wire operations. Extend the public contract, generated bindings, SDK, startup registration, middleware registry, runtime invocation, Host execution callbacks and both built-in provider families. Add the public reference-extension command with wire behavior and its public tests. Keep source failure classification, SDK retries disabled and Codex acquired diagnostics intact.

Relevant areas are `api/plugins/extension/v1`, `pkg/plugins/extension/v1`, `sdk/plugins/extension/v1`, `host/internal/usecase/host/{middleware,startup,extensionruntime,modelexecution}`, `host/internal/infra/plugins/extension/runtime`, `host/internal/infra/providers/openai`, `host/internal/app`, and `testdata/external-plugins`. D8-02 and D8-07 are mandatory. Update related author/reference documentation and architecture placement for introduced components.

### Verification

Own verification through real client/extension/provider integration fixtures. Highest available level is an in-process call through the assembled public Programmatic or UI boundary with real internal components and extension processes; the remote provider is an HTTP fixture. Exercise Codex, compatible Chat and compatible Responses. Also exercise configured requests and a generated raw-provider substitute through the same callback protocol. Do not claim a production PHS-12 extension-provider process implementation.

### Exit criteria

- Focused tests have recorded RED before behavior exists and GREEN after implementation.
- Two handlers demonstrate original/current preserve and replacement; request-local markers do not enter session entries.
- Replay retention/removal and restoration from immutable original work without exposing private data; header-only changes preserve body bytes.
- Pre-dispatch failure, invalid replacement and response-observer failure expose complete `EXTENSION_FAILED` causes, stop later handlers and suppress retry even with retry-like causes or concurrent cancellation.
- Successful observers preserve ordinary provider classification/retry and existing response diagnostics.
- After the final correction, `task fmt`, analyzed `task fix_dry_run`, `task lint`, `task test`, `task itest`, `task test-coverage` and `task build` pass. Remove temporary code.
- Separate SubAgentAnalyst tasks check code quality, text quality, behavior and technical-solution conformance under the active workflow. The parent decides on their findings and accepts their reviewed result before a local unit commit.

### Relation

Independent of later units. Introduces the shared infrastructure they consume. No other code-changing unit runs concurrently.

### Risks

HIGH: confidential replay exposure or lost handler causes at the SDK boundary would violate the public contract. Real adapter probes and cancellation/provider-retry cases check those boundaries directly. Preserve original non-secret causes and never classify extension failure by a provider-like descendant alone.

## UNT-02: Slice, effective instructions and request-local context

### Goal

Extensions change effective instructions and prepared context without mutating canonical entries or losing changes during initial/overflow preparation.

### Behavior

Core requests effective context through its consumer-owned port. Ordered handlers transform the prepared original/current values. The provider receives the effective value; canonical history remains unchanged by these transformations. Configured callers use the applicable shared path.

### Work

Implement the solution's Effective-context port and prepared provider binding and typed confidential history projection. Integrate instruction/context handler kinds with the existing UNT-01 registry and public envelopes. Preserve the selected raw provider, conversation owner and registrations through execution. Remove replaced implicit preparation paths. Extend the reference extension and its author documentation.

Relevant areas add `host/internal/usecase/agent/run` and existing captured preparation/overflow boundaries to the UNT-01 contract/runtime/model-execution/app areas. D8-01 and D8-06 are mandatory. Compaction policy remains extension-owned; configured requests retain their ownerless preparation semantics.

### Verification

Own verification through assembled Programmatic/UI requests with real sessions, extension process and provider fixture. Isolated execution tests use generated mocks. Check configured generation and branch-summary callers through their existing public fixtures. Highest available level is the assembled public boundary with real internal components.

### Exit criteria

- Focused RED/GREEN evidence covers both instruction and context transformations, original/current replacement, and unchanged durable entries.
- Automatic preparation cannot overwrite middleware results. Transient retry reuses the effective revision. Committed overflow creates a correctly transformed new revision with frozen selection/owner.
- Configured requests share applicable composition without automatic conversation compaction; handler failure dispatches no provider request and causes no chain retry.
- Core depends only on its consumer-owned effective-context interface and receives provider-neutral final values. No generic hooks dependency or concrete-provider import is introduced.
- All mandatory project checks and build pass after final correction; temporary paths are removed.
- Separate SubAgentAnalyst tasks check code quality, text quality, behavior and technical-solution conformance. The parent decides on findings and accepts the reviewed result before a local unit commit.

### Relation

Depends on ready UNT-01. Shares registration, transport, SDK, middleware and assembly files, so runs after UNT-01 reviews and corrections.

### Risks

HIGH: preparation could overwrite context or repeated attempts could bind another provider/owner. Public compaction/retry fixtures compare outbound history and capture identity. MEDIUM: nested configured generation could accidentally recurse through automatic preparation; explicit configured caller coverage checks its existing ownerless behavior.

## UNT-03: Slice, user text/image transformation and full handling

### Goal

All supported user-submit paths reach one asynchronous input pipeline before Core, with final-model validation and successful fully-handled completion.

### Behavior

A client submits ordered text/image input. Extensions preserve, replace or fully handle it. Host either invokes Core with compatible final input, completes a handled operation without a run, or reports final-input failure before Core/provider work.

### Work

Implement Public and internal user input and associated Error categories and operation completion. Replace the text-only public submit representation, adapt SDK/controller/Host command mappings and shared runcontrol request data, and retain current TUI/CLI text UX. Reuse the established middleware registry and typed message mapping. Expose `INPUT_INVALID` with complete text in both public client contracts. Extend reference-extension input behavior and relevant documentation.

Relevant areas include public Programmatic/UI protobuf and generated bindings, UI SDK, client controllers, Host Programmatic/UI/runcontrol, Core user append, standard TUI Host mapping, assemblies and public fixtures. D8-04, D8-05 and D8-06 apply. Do not add image UI, CLI flags, legacy fallback or provider calls for handled input.

### Verification

Own verification through real Programmatic/UI operations, extension processes, sessions and provider fixtures. Highest available level is the assembled public boundary. One-shot exercises the same coordinator and current text command.

### Exit criteria

- RED/GREEN evidence covers text/image replacement, ordered original/current values, fully handled input and final modality validation.
- An image removed by handlers succeeds for a text-only model; a retained final image fails before agent-start/provider dispatch; a supporting model accepts it.
- Handled input completes successfully without history append or synthetic Core settlement. Admission and UI availability recover for a subsequent request.
- Ordinary and invalid-action failures expose complete `EXTENSION_FAILED` causes, skip later handlers and dispatch no provider.
- Public mappings compile without the replaced string-only path, and existing text submissions remain behaviorally consistent.
- All mandatory project checks and build pass after final correction. Separate SubAgentAnalyst code-quality, text-quality, behavior and technical-solution-conformance checks complete, followed by parent acceptance of the reviewed result and a local unit commit.

### Relation

Depends on ready UNT-02 and its stable model-context boundary. Shares middleware, public envelopes, assembly and fixtures with earlier units. Runs sequentially. SubAgentDeveloperComplex performs it because completion of an accepted operation without Core execution is a mechanism that no existing path shows.

### Risks

HIGH: checking modalities before the final handler or invoking Core for handled input would change public behavior. Provider and agent-start counters plus session-state inspection check this. MEDIUM: a reserved operation could remain busy after input failure; next-request recovery tests check release.

## UNT-04: Slice, role-preserving finalized messages

### Goal

The final replaced message is the single value used by durable history, completion events and subsequent execution.

### Behavior

An extension preserves or replaces finalized user/model/tool-result content without changing role or Host-owned identity/accounting. Successful replacements become canonical values. A failed chain stops the operation without append of its candidate or a provider retry to recover from its failure.

### Work

Implement Finalized messages and durable history with the established typed confidential mapping. User finalization belongs before final input validation. Core's specific finalization port covers terminal model/tool values; configured completion uses the same Host owner without conversation persistence. Keep middleware errors outside persistence-only wrapping and prevent double finalization or recursive failed-chain invocation. Extend the reference extension and author examples.

Relevant areas are the existing middleware/public/runtime/SDK path, runcontrol input processor, Core terminal/error/tool paths, model-execution configured completion, Host failure mapping and public fixtures. D8-03 applies. Completed tool side effects and independent prior commits remain retained.

### Verification

Own verification through assembled public clients with real sessions and deterministic provider/tool fixtures. Highest available level is the assembled public boundary. Check resulting persisted entries, completion payloads and the next provider request, not only mapper output.

### Exit criteria

- RED/GREEN evidence covers user/model/tool-result replacement and configured terminal results.
- Replacements preserve role, accounting and private replay; invalid role/reference/action stops the chain.
- Finalized-user image introduction is validated before Core. Model/tool replacements are identical at persistence, events and subsequent consumers.
- A handler failure remains `EXTENSION_FAILED`, never persistence failure; later handlers and recovery retries do not run. Completed tool actions and prior committed entries remain retained.
- Canceled terminal bookkeeping does not reenter a failed/canceled chain.
- All mandatory project checks and build pass after final correction. Separate SubAgentAnalyst tasks check code quality, text quality, behavior and technical-solution conformance. Parent acceptance of the reviewed result and a local unit commit follow.

### Relation

Depends on ready UNT-03. Consumes its pre-Core final-validation boundary and earlier context/wire infrastructure. Runs sequentially.

### Risks

HIGH: silently replacing only stored data would leave events or tool execution inconsistent. Public event/next-request/session comparisons check the whole path. HIGH: replacement failure could be mislabeled as persistence failure or repeat the failing chain; explicit categorized failure probes check both.

## UNT-05: Document, public middleware author guide and phase records

### Goal

An extension author can build and use the supported middleware through documented public contracts and examples, and phase status points to executed evidence.

### Behavior

An extension author reads the guide, builds the public reference extension and understands registration order, actions, safe confidential-data references, errors and the resulting model-facing behavior.

### Work

Complete the author guide under `docs/guidelines`, reference-extension instructions, canonical phase `solution.md` link, architecture placement and roadmap/delivery-plan/issue status references. Link to existing requirements/solution rather than duplicating them. Incorporate executed evidence from ready UNT-01 through UNT-04. Do not mark user acceptance complete before final user review.

### Verification

Documentation verification against actual public contracts and executed example commands. SubAgentAnalyst reviews text quality as a separate task and checks public-description conformance. Parent applies documentation/terms/overengineering gates. No new feature code belongs in this unit.

### Exit criteria

All public names, commands and outcomes match actual implementation. Each ticket criterion has a source of executed evidence or a clearly identified actual verification limit for the final verification stage. Status says implemented and awaiting final verification/acceptance when appropriate. No temporary evidence paths or unsupported claims appear in permanent documents. Create a local documentation commit after review.

### Relation

Depends on ready UNT-04 and the completed behavior of every earlier unit.

### Risks

MEDIUM: status or prose could claim acceptance before evidence/user review. Keep implemented, verified and user-accepted states distinct and link stable evidence records.

## Test strategy

Generated mocks isolate capability-owner composition, validation and error decisions. Integration-tagged tests combine real production components and public SDK/process/network/filesystem boundaries. Public client probes verify handler order, source preservation, private-data exclusion, session/request separation, modality timing, handled completion, retry suppression and recovery for a subsequent request. Existing provider failure and compaction/retry tests remain part of full checks.

Each slice records focused RED/GREEN commands and output before parent acceptance. Run the mandatory checks in project order, analyze `fix_dry_run` output before fixing, and retain exact failure output when a check blocks completion. Use all available public fixtures; describe unavailable production PHS-12 process-provider behavior as a scope boundary rather than inventing it.

Within a unit, checks run in this order:

1. While the unit performer works and while review corrections continue, the performer runs the tests that cover the changed files, `task fmt`, `task lint` and `task test`.
2. After the reviews of the unit end with no accepted findings, the parent starts a separate agent that runs the full mandatory set of the project rules and `task build`.
3. If the full run passes, the parent creates the local unit commit. If the full run fails, or `task fmt` or `task fix_dry_run` changes files, the parent assigns the correction to the performer. The performer runs the checks of step 1, the reviewers review the correction, and the separate agent runs the full set again.

The full set runs once per passing unit because one run is slow; see [TD-02](../../../../../tech-debt.md#td-02-integration-tests-are-slow). The code changes of a unit are complete at its local commit, so this order still runs the mandatory project checks before code changes are complete.

## Dependencies and resourcing

Use the existing declared Go toolchain, SDK, protobuf generators, mockgen and test infrastructure. No dependency changes are planned. One developer works on project files at a time. Code quality, text quality, behavior and technical-solution conformance are distinct read-only SubAgentAnalyst assignments. The parent does not inspect unit changes itself, as required by the active implementation stage. Reviewers receive complete unit scope, not only earlier findings.

## Project definition of done

All five units are ready, every ticket criterion is verified within explicitly recorded actual limits, all mandatory project checks and build pass after final changes, and final verification is independently reviewed. Phase completion still requires final user acceptance under `final_only`.

## Assumptions

None about implemented behavior. Proposed results remain unverified until their unit evidence is recorded.

## Open questions

None. Each unit has known entry points, affected areas, consumers, decisions, complete verification and a named performer. All units are accepted for execution under the autonomous planning authority.

## Standards deviations

None.

## References

- [Ticket](ticket.md), [requirements](requirements.md), [technical solution](solution.md), [decisions](decisions.md).
- [Facts](facts.md), [project state](project-state.md), [design review](design-review.md).
