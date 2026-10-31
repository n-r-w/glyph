# Technical debt

## TD-01: Extension runtime stores a context

- Status: Open.
- Location: `host/internal/infra/plugins/extension/runtime/runtime.go`, `Runtime.context`.
- Problem: `Runtime` stores `context.Context` in a struct field, contrary to the Go guideline to pass contexts explicitly to operations that need them.
- Verification: The field declaration was confirmed during the UNT-01 review and rechecked when recording this item. Context lifetime behavior and runtime consequences remain uninvestigated.
- Scope: Recorded outside UNT-01; no implementation correction has been made.

## TD-02: Integration tests are slow

- Status: Resolved.
- Location: integration tests of `host/internal/app`; `Taskfile.yml`, task `check`.
- Problem: `task itest` ran packages and tests one at a time under the race detector and took about 9 minutes 25 seconds; `host/internal/app` took about 440 seconds of this time. `task test-coverage` ran the same tests a second time.
- Cause: 137 of 170 top-level integration tests of `host/internal/app` call `t.Setenv` to pass settings to plugin processes, so Go runs them serially inside one test process even without `-p 1 -parallel 1`. `TestPublicProviderRetryAfter` waited 70 seconds for provider-requested retry delays. Builds of test binaries took 0.05 to 0.5 seconds each and were not a cause.
- Resolution: `task check` replaced `task itest` and `task test-coverage`. It runs every test once with the race detector and coverage collection, and runs the integration tests of `host/internal/app` in one process per CPU core. `TestPublicProviderRetryAfter` waits for a 1-second delay and checks the 60-second delay as a scheduled retry that it cancels.
- Verification: On a 24-core machine, five consecutive runs of `task check` passed in 60 to 67 seconds each, with coverage 85.1% to 85.2%.
