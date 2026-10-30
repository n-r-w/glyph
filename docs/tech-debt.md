# Technical debt

## TD-01: Extension runtime stores a context

- Status: Open.
- Location: `host/internal/infra/plugins/extension/runtime/runtime.go`, `Runtime.context`.
- Problem: `Runtime` stores `context.Context` in a struct field, contrary to the Go guideline to pass contexts explicitly to operations that need them.
- Verification: The field declaration was confirmed during the UNT-01 review and rechecked when recording this item. Context lifetime behavior and runtime consequences remain uninvestigated.
- Scope: Recorded outside UNT-01; no implementation correction has been made.

## TD-02: Integration tests are slow

- Status: Open.
- Location: integration tests of `host/internal/app`; `Taskfile.yml`, tasks `itest` and `test-coverage`.
- Problem: On commit `1fef01e`, `task itest` took about 9 minutes 40 seconds. Of this time, `host/internal/app` took 400.6 seconds and `host/internal/infra/plugins/extension/runtime` took 48.8 seconds; each other package took 1 to 5 seconds. `task test-coverage` runs all integration tests a second time without the race detector and took about 5 minutes 40 seconds. One run of all mandatory checks and `task build` took about 16 minutes 30 seconds.
- Suspected contributors: `task itest` runs packages and tests one at a time under the race detector (`-p 1 -parallel 1`); the `Taskfile.yml` comment states that this serial order is intentional because the tests start real plugin, terminal and shell processes. `host/internal/app` has 140 integration-tagged test files. Some of its tests build binaries during the run with `go build` or `go test -c`, for example `buildWireFixture` in `host/internal/app/wire_integration_test.go`.
- Verification: Package times come from the `task itest` output on commit `1fef01e`. Task durations come from the completion times of the logs of the same run. The file count comes from a count of `//go:build integration` files on the same commit. Per-test times were not measured, so the slowest tests and the share of time spent on in-test builds remain unknown. No speed-up has been tried.
- Scope: Recorded during PHS-08. The [PHS-08 plan](specs/features/initial/phases/08-prompt-context-input-provider-middleware/plan.md#test-strategy) runs the full check set once per passing unit to limit this cost.
