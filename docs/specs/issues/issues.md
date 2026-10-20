# Issues

- [ ] Align implemented extension chains with the [extension failure rule](../features/initial/prd.md#extension-failure-rule). Requirements are updated; implementation and verification remain pending. See the [roadmap](../../roadmap.md#extension-failures-stop-the-invoking-operation).

- [ ] PHS-07 standard TUI PTY verification: [Accepted technical debt](../features/initial/phases/07-extension-context-lifecycle/technical-debt.md)

- [X] TUI hyperlinks: `docs/specs/issues/tui-hyperlinks`

- [ ] Agent run failure semantics: `docs/specs/issues/agent-run-failure-semantics`
  - [X] Shared `Failed.code` transport and closed failure-category sets: `docs/specs/issues/blocking-contract-operation-processing/solution.md`
  - [X] Source-backed history-persistence failure distinction for both client contracts, verified in PHS-07.1 U7: `docs/specs/features/initial/phases/07.1-architecture-audit-and-correction/solution.md`
    - PHS-07.1 is complete and accepted. See `docs/specs/features/initial/phases/07.1-architecture-audit-and-correction/solution.md#status-and-scope` for final status and evidence.
  - [ ] Provider-neutral terminal model-execution categories and retry ownership: `docs/specs/features/initial/phases/06-context-compaction-retry-control/ticket.md`
  - [ ] Provider source failure classification: `docs/specs/features/initial/phases/12-extension-defined-providers/ticket.md`
  - [ ] Final public failure-code closure and Programmatic/UI contract tests after PHS-12: `docs/specs/issues/agent-run-failure-semantics/problem.md`
- [X] Blocking contract operation processing: `docs/specs/issues/blocking-contract-operation-processing`
- [X] Reliable branch summarization: `docs/specs/issues/reliable-branch-summarization`
- [ ] Compaction source differs from conversation context: [problem statement](compaction-history-projection/problem.md) and [requirements](compaction-history-projection/prd.md) approved for PHS-06 completion; the [history-source technical solution](compaction-history-projection/solution.md) remains approved. Implementation and independent acceptance are pending.
- [ ] Terminal provider refusal lost after stream truncation: reproduced through public Programmatic Control during PHS-06.4. The [PHS-06 ticket](../features/initial/phases/06-context-compaction-retry-control/ticket.md#problem-statement) owns the problem, FRQ-04.8.1, and ACC-04.5 acceptance. Requirements and the [D3 technical solution](../features/initial/phases/06-context-compaction-retry-control/solution.md#d3-terminal-refusal-and-subsequent-stream-failure) are approved; implementation and verification pending.
- [ ] Production panic elimination: `docs/specs/issues/production-panic-elimination/ticket.md`
- [X] Unclear model operation naming: `docs/specs/issues/unclear-model-operation-naming`
