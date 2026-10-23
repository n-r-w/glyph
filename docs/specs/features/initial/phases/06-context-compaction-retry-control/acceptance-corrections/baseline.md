# PHS-06 correction baseline

## Source and method

Checks ran against an isolated Git archive of revision `7238582309a8abd2750898ac0c0d8bbc07e78a18`, using Go `go1.27.2` on Linux amd64. The working checkout remained unchanged apart from this delivery's untracked documentation directory. The extractor retained command output, exit codes, metadata, and source manifests. Revision identity was rechecked directly against recorded metadata after correcting transcription errors in the extractor's first report.

## Results

| Command | Exit code | Result |
| --- | --- | --- |
| `task fmt` | 0 | No source manifest changes, including external-plugin formatting. |
| `task fix_dry_run` | 0 | No proposals. No task fix execution. |
| `task lint` | 201 | Root checks reached govulncheck, which reported the accepted GO-2026-6809 exception. External-plugin lint was not reached. |
| `task test` | 0 | Root and external-plugin race unit tests passed. |
| Root-resolved golangci-lint binary in testdata/external-plugins, with integration tags | 0 | External-plugin lint reported zero issues. |
| `TMPDIR=/var/tmp task itest` | 0 | Root and external-plugin race integration tests completed. The root application package completed in 312.660 seconds. |
| `TMPDIR=/var/tmp task test-coverage` | 0 | Combined coverage was 84.2%, required 80.0%. |
| `task build` with BUILD_ROOT pointing to temporary output | 0 | Host, tools, compaction, and TUI binaries built. |
| `task generate`, twice | 0, 0 | Both runs preserved the source manifest. |

TMPDIR assignments applied to individual child commands only. No source, dependency, session environment, or HOME modification was made. These commands establish the original revision's baseline, not verification of future corrections. Ordinary Go package output does not inventory individual platform test skips; final acceptance will record them separately.

## Environment and recorder failures

The first integration and coverage runs timed out after 10 minutes in TestUIManualCancellationStopsObserverModel/TestScenario/observe-cancellation. They used a nested temporary root. The resulting socket paths were 132 and 133 bytes; direct OS bind probes returned AF_UNIX path too long. The first shorter rerun still constructed 109-byte paths and also timed out.

A focused unchanged-source execution with `TMPDIR=/var/tmp go test -count=1 -race -tags=integration -timeout=90s ./host/internal/app -run TestUIManualCancellationStopsObserverModel` exited 0 in 4.040 seconds. Both full tasks then passed with the same short TMPDIR. No product fix or F17 investigation was needed to obtain those results.

The separate external-plugin lint recorder first attempted an internal Task directly, which Task rejected. Its next attempt looked up a root-module tool from the external module and failed with go: no such tool. Those commands are recorder errors, not product failures, test skips, or missing declared dependencies. The corrected invocation resolves the tool in the root module and executes it in the external module, as the Taskfile prescribes. It exited 0 and reported zero issues.

## Baseline deviation disposition

Consequences were MEDIUM for verification: integration and coverage evidence was initially incomplete. The timeout occurred with socket paths exceeding the OS capacity. Direct bind probes, unchanged-source focused execution, and passing full tasks with the short root support the environment diagnosis. The correction concerns the temporary command environment only.

The smallest response was a shorter temporary root with sufficient disk space. Alternatives were a product/test edit or accepting incomplete checks; neither was needed. The delivery owner selected the environment-only response within verification authority. The deviation and fix-strategy gates permit this direct correction of the execution setup. F17 remains excluded, and no new product limitation or scanner exception was accepted.

## Open questions

No unresolved baseline check remains under the accepted GO-2026-6809 exception. Checks and coverage for future corrections remain implementation and final-verification obligations; the baseline is not a substitute.
