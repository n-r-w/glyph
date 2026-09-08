# Issues

- [ ] Agent run failure semantics: `docs/specs/issues/agent-run-failure-semantics`
  - [X] Shared `Failed.code` transport and closed failure-category sets: `docs/specs/issues/blocking-contract-operation-processing/solution.md`
  - [X] Source-backed history-persistence failure distinction for both client contracts, verified in PHS-07.1 U7: `docs/specs/features/initial/phases/07.1-architecture-audit-and-correction/solution.md`
    - PHS-07.1 remains blocked after review of `ded1549` found FND-15 through FND-17. The approved TUI causality correction has implementation and main-agent verification evidence. Runtime completion has implementation and main-agent verification evidence. Review of `cb3a8f6` found FND-18 through FND-20; O74-1 adds FND-21. Their producer correction has implementation and main-agent verification evidence. The separate FND-22 process-cancellation correction has implementation and main-agent verification evidence. The O78-1 FND-23 dispatch-confirmation correction has implementation and main-agent verification evidence. The U8 smoke-fixture correction has implementation and main-agent verification evidence. Fresh independent review and explicit final acceptance remain pending. See `docs/specs/features/initial/phases/07.1-architecture-audit-and-correction/u8-evidence.md#smoke-fixture-correction`.
  - [ ] Provider-neutral terminal model-execution categories and retry ownership: `docs/specs/features/initial/phases/06-context-compaction-retry-control/ticket.md`
  - [ ] Provider source failure classification: `docs/specs/features/initial/phases/12-extension-defined-providers/ticket.md`
  - [ ] Final public failure-code closure and Programmatic/UI contract tests after PHS-12: `docs/specs/issues/agent-run-failure-semantics/problem.md`
- [X] Blocking contract operation processing: `docs/specs/issues/blocking-contract-operation-processing`
- [X] Reliable branch summarization: `docs/specs/issues/reliable-branch-summarization`
- [X] Unclear model operation naming: `docs/specs/issues/unclear-model-operation-naming`
