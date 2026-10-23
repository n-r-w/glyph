# PHS-06 correction decisions

All decisions below stay within the [confirmed authority](target-picture.md#decision-authority-and-boundaries). Their technical rationale is in the linked solution sections.

| ID | Decision | Basis and effect |
| --- | --- | --- |
| D1 | Use refusal-only Codex SDK continuation with request-local delivery-failure observation. | [Codex design](technical-solution.md#codex-refusal-only-continuation) preserves existing successful and delivery-failure exits while acquiring the later cause. |
| D2 | Dispatch enabled overflow retry handlers before enforcing the absent-recovery repeat guard. | [Retry design](technical-solution.md#retry-handlers-before-ownerless-overflow-admission) preserves handler authority and prohibits unchanged overflow repeats. |
| D3 | Combine bundled public result-rejection assertions with coordinator no-Commit mock expectations. | [Bundled observations](technical-solution.md#bundled-result-rejection-observations) distinguish durable no-mutation from no attempted mutation without a public test API. |
| D4 | Strengthen the existing repeated-history restart fixture using saved-branch and second-receipt expected data. | [Restoration design](technical-solution.md#ordered-restored-suffix) avoids assuming the first marker's retained content survives the second. |
| D5 | Establish installed retry-wait cancellation with synctest and retain existing public lifecycle evidence separately. | [Cancellation design](technical-solution.md#deterministic-installed-wait-cancellation) avoids timing sleeps and production timer interfaces. |
| D6 | Distinguish behavior-correction RED/GREEN from evidence-only assertion-sensitivity controls. | [Verification design](technical-solution.md#verification-and-evidence-ownership) preserves original chronology and adds no test-first exception. |

### D7: Delivery-owned lint correction and outside-task reporting

The supplementary non-integration lint configuration exposed three diagnostics in UNT-02's added test. The separately reviewed follow-up commit `5375876f770dc3eb4a7b4010bea472ff55a31539` fixes those three test-style issues within delivery authority. The remaining 18 diagnostics name six files unchanged from delivery baseline `7238582309a8abd2750898ac0c0d8bbc07e78a18`. Their disposition is report-only under the outside-task rule and general delivery boundaries. The [full deviation description](unt-06-evidence.md#outside-task-deviation-18-supplementary-diagnostics) retains developer impact, unestablished runtime effect, options and cleanup ownership. This decision grants no specific user acceptance of those diagnostics, new product debt, second scanner exception or cleanup authority.

### D8: Preserve and correct the assembled build recorder

The first assembled build recorder expanded an empty BUILD_ROOT argument. The build exited 0 and wrote four ignored checkout binaries. Within verification authority, the collector retained that record and ran a second build using a literal temporary BUILD_ROOT. That build also exited 0 and produced four verified binaries. The [recorder correction](unt-06-evidence.md#recorder-correction) distinguishes the two invocations and bounds the canonical manifest comparison to the authoritative second record. The author preserves the raw records and their failed recording intent without claiming a failed product build or reconstructed earlier provenance.

## Open questions

No unresolved autonomous design decision remains. Required runtime outcomes remain verification obligations.
