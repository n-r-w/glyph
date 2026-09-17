# PHS-07 technical debt

Status: Open

## Accepted phase closure

PHS-07 is marked Completed by user decision, with the verification gap below accepted as technical debt. Phase closure does not mean that the unexecuted PTY scenario passed. The implementation and Linux verification evidence remain in the [technical solution](solution.md#implementation-evidence).

## Standard TUI PTY verification

`TestLifecycleObserverThroughStandardTUI` in [the Host integration tests](../../../../../../host/internal/app/lifecycle_observer_modes_integration_test.go) requires Darwin arm64. It was skipped on the Linux verification host.

The remaining debt is the real standard TUI PTY evidence for [ACC-01 and ACC-05](ticket.md#acceptance-criteria). Headless, UI application assembly, and Programmatic Control evidence do not replace this terminal check. No implementation defect is inferred from the missing run.

## Closure criteria

Run the scenario on Darwin arm64 from the repository root:

```bash
go test -race -count=1 -tags=integration -p 1 -parallel 1 \
  ./host/internal/app -run '^TestLifecycleObserverThroughStandardTUI$' -v
```

Close this debt only after the scenario passes without a skip. Record the tested revision, platform, command, and result in this document, then update its entry in the [issue index](../../../../issues/issues.md). A failed run leaves the debt open until its cause is resolved and the scenario passes.
