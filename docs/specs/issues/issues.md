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
- [ ] Compaction source differs from conversation context: completion-plan A4 implements the approved [history-source solution](compaction-history-projection/solution.md) with original source-kind metadata. The [evidence record](../features/initial/phases/06-context-compaction-retry-control/a4-prepared-history-evidence.md) records verification and the two scoped acceptance exceptions. A4 is accepted after code-quality, text-quality, and independent unit review. The [whole-phase review](../features/initial/phases/06-context-compaction-retry-control/acceptance-report.md) retains restored-suffix evidence finding F20; full PHS-06 acceptance remains blocked.
- [ ] Terminal provider refusal lost after stream truncation: completion-plan A3 remains accepted for OpenAI-compatible Responses under [D3](../features/initial/phases/06-context-compaction-retry-control/solution.md#d3-terminal-refusal-and-subsequent-stream-failure). The [A3 evidence record](../features/initial/phases/06-context-compaction-retry-control/a3-terminal-failure-evidence.md) retains its regressions and verification limits. The [whole-phase review](../features/initial/phases/06-context-compaction-retry-control/acceptance-report.md#findings) records the remaining Codex path as Major finding F23.
- [ ] Retry handlers skipped on configured-model or ownerless conversation context overflow: the [whole-phase review](../features/initial/phases/06-context-compaction-retry-control/acceptance-report.md#findings) records Major finding F24. Correction requires separate implementation authorization.
- [X] Run completion panics after prepared history shortens: for completion-plan unit A4.1, Agent Core accounts for appended history independently of prepared-history length. A4.1 is accepted after code-quality, text-quality, and independent unit review. The [evidence record](../features/initial/phases/06-context-compaction-retry-control/a4.1-run-history-evidence.md) contains the public regression, lifecycle coverage, and verification limits. Full PHS-06 acceptance remains pending.
- [ ] Production panic elimination: `docs/specs/issues/production-panic-elimination/ticket.md`
- [X] Unclear model operation naming: `docs/specs/issues/unclear-model-operation-naming`
