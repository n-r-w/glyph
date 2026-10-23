# UNT-04 restored-suffix evidence

## Scope and review state

This uncommitted test/evidence diff addresses [FRQ-04](requirements.md#functional-requirements), A4 and D4 through `TestStandardPreparedHistoryPublic`. Production source remains at HEAD `e18ee60b5c229ebf24c48834adae010ebfc1b24e`. Repository review and unit completion remain pending. The delivery plan and phase statuses remain unchanged.

Changed paths:

- `host/internal/app/standard_compaction_history_public_integration_test.go`
- `host/internal/app/standard_compaction_restored_suffix_integration_test.go` (new)
- This evidence document (new)

The eight pre-existing untracked acceptance-corrections documents remain byte-identical. No production, dependency, toolchain, API, journal or architecture change was made. No commit or push was made. F17 and optional F18 were excluded from investigation and changes.

## Positive comparison and independent expected data

After the second committed receipt, the test saves the detailed active branch before closing the application owner. After recreating the owner and resuming the session, the test obtains the next provider request through the existing generated HTTP mock.

`assertPreparedRestoredSuffix` finds the second receipt's `FirstKeptEntryId` in the saved branch. It requires the saved branch to end at the second receipt, then reads every entry from that boundary up to the receipt. The focused oracle accepts one plain-text user item or one STOP model response containing one finalized text item. It fails explicitly on other retained variants. It derives expected roles and text from those saved entries, prepends the second receipt's summary and appends the new request.

`decodePreparedRestoredInput` decodes the complete provider `input` array. It validates the message type, role and known content shape, rejecting unknown message/content members and unsupported roles.

`assertPreparedRestoredSuffix` requires the first item's text to contain the accepted receipt summary. It replaces only that item's text with the summary before comparing the complete role/text sequence in order, because production prompt wrapper wording is mutable. The entire retained suffix and newly submitted request remain subject to full equality against the saved-entry oracle. Expected suffix data uses neither production projection helpers nor the first receipt's suffix.

The summarized-span retained-token fixture changes from 1 to 20. The recent fixture message and next request each gain 100 characters. Default-installed still uses the installed bundled executable and its default retention setting, with the existing 80,000-character additions. The retained-span override remains 100. The original first-boundary assertions (`recent-source` or `calls`), six-message/two-message source-selection assertions, original-entry preservation, exclusion assertions, tool correspondence, interrupted-call checks and navigation checks remain in place and pass.

Both repeated-compaction cases now require at least two distinct provider-visible retained items. The retained-span case keeps its existing earlier return and is preserved as separate coverage.

### Observed second-receipt derivation

An isolated observation copy added diagnostic output to the test helper, then passed all three cases at 2026-10-23 04:16:50Z through 04:16:55Z, exit 0. The diagnostics were removed before the sensitivity runs. The final helper contains no diagnostic instrumentation.

| Case | Second receipt ID | FirstKeptEntryId | Following retained model entry |
| --- | --- | --- | --- |
| summarized-span | `580454800f36c241962f800ccf4f4532` | `e0a2c0fd0661c750a685533dffe5dc94` | `d911ef511852dc14c8fbe0c55643aaf1` |
| default-installed | `d4cf943142b00605f5cef96e0917ebc4` | `ee929bb9974da03d1cf0627bf23d5610` | `0216cd07176a454ceac1c162697fd6c3` |

Entry IDs are runtime observations, not fixed expected IDs. In both cases the entire decoded expected sequence has four items:

1. User: accepted second-receipt summary, observed as `prepared-summary-2`.
2. User: saved next request. In summarized-span this is `manualNextText` plus 100 `n` characters. In default-installed it also includes the existing 80,000 `n` characters.
3. Assistant: saved finalized response, observed as `Request complete.`.
4. User: newly submitted `after restoration`.

| Item | Text bytes | Text SHA256 |
| --- | ---: | --- |
| Summary, both cases | 18 | `a7bd53243e903d5d5c6e3a732b7abe9193469c8624c4b1138747eee790f3fe60` |
| Retained user, summarized-span | 119 | `3685a2b78f30852470f37ba7dd83a5bffa1e8b34baddbd769126fbe0e83e76a8` |
| Retained user, default-installed | 80119 | `507587c0ea60b6bf9eccf1bf284e8c7a08d6c6b9f3604f0c318c734ec07ad3fe` |
| Retained assistant, both cases | 17 | `a7a78960cb6a97f11604b7f23620a57b64a7d6d75447f9836bdf30912e4728a1` |
| New request, both cases | 17 | `2223fbb8c8aff402fe018ea520ab3241f73a2a3d2bb8d386c8de564c87523bad` |

## Source identities

Environment: Linux amd64, Go `go1.27.2`, Task `3.54.0`. SHA256 values below identify final test source used by all retained control runs and final checks:

| Path | SHA256 |
| --- | --- |
| `host/internal/app/standard_compaction_history_public_integration_test.go` | `9a3bd455419af5949f6c682e5d7fad0265d95f95c39962b23ecb9a9ee3ad1b26` |
| `host/internal/app/standard_compaction_restored_suffix_integration_test.go` | `b50aac453467c5e91da22cefe0e0a7f70e03a68fb04a925c8ac89100c20e6364` |
| `host/internal/usecase/host/sessions/entry_history.go`, unchanged production source | `ffda1f1183e0787fe68b3fd18d12c4a34706dfe17572190b27800f5a42a0d7e5` |

The complete source manifest uses `git ls-files --cached --others --exclude-standard -z`, SHA256 for each existing file, and path-sorted records. It includes the new test and all pre-existing untracked task documents: 1,391 files. Its SHA256 is `0f373599be32d192e500ff475bf55520887c702fa80e6814f838845e8704d476`. This manifest precedes this evidence document. Corrected manifests before and after the final uncached checks are byte-identical and match the independently recorded control baseline manifest.

## Assertion-sensitivity controls

These controls test assertion sensitivity. They are not behavior-correction RED evidence. The new comparisons first compiled and passed with unchanged production. Every retained control invocation used:

```sh
TMPDIR=/var/tmp go test -count=1 -tags=integration -p 1 -parallel 1 ./host/internal/app -run '^TestStandardPreparedHistoryPublic$' -v
```

The focused checkout run and the baseline copy each passed summarized-span, retained-span and default-installed before either mutation. The copy held all tracked and nonignored untracked files, excluding Git administration and build output. Both controls changed only the temporary copy of `host/internal/usecase/host/sessions/entry_history.go`, in `compactedHistoryFromEntries`, immediately before its final `return history`.

Omission insertion:

```go
if compaction.Summary == "prepared-summary-2" && len(history) >= 3 {
    history = append(history[:1], history[2:]...)
}
```

Reordering insertion, applied after restoring the original source:

```go
if compaction.Summary == "prepared-summary-2" && len(history) >= 3 {
    history[1], history[2] = history[2], history[1]
}
```

The summary-specific condition isolates the second-summary projection. The first compaction and its source-selection assertions remain executable without mutation. Each control preserves summary placement and the new request so the original substring exclusions do not mask the new observation.

| Run | UTC start, 2026-10-23 | UTC end | Exit | Outcome |
| --- | --- | --- | ---: | --- |
| Baseline copy | 04:16:01Z | 04:16:19Z | 0 | All three cases pass |
| Focused checkout | 04:16:39Z | 04:16:42Z | 0 | All three cases pass |
| Omission | 04:17:02Z | 04:17:09Z | 1 | Both repeated cases fail at new equality |
| Omission restored | 04:17:09Z | 04:17:12Z | 0 | All three cases pass |
| Reordering | 04:17:19Z | 04:17:26Z | 1 | Both repeated cases fail at new equality |
| Reordering restored | 04:17:26Z | 04:17:29Z | 0 | All three cases pass |

Both failures compile and reach `assertPreparedRestoredSuffix` at line 80, called by the public test at line 159. The diagnostic assertion is:

```text
ordered restored suffix must match the second receipt's saved branch
```

The summarized-span omission diagnostic shows expected length 4 and actual length 3. The missing item is the retained user request. The reordering diagnostic shows actual order summary, retained assistant, retained user, new request. Expected order remains summary, retained user, retained assistant, new request. Retained-span passes under both controls. Default-installed reaches the same equality and fails in both controls; testify's large-value rendering reports `cannot display message: bufio.Scanner: token too long`, so its full value diff is unavailable. Its assertion location and failure label remain recorded.

| Source state | Production file SHA256 | Complete source manifest SHA256 |
| --- | --- | --- |
| Before and both restorations | `ffda1f1183e0787fe68b3fd18d12c4a34706dfe17572190b27800f5a42a0d7e5` | `0f373599be32d192e500ff475bf55520887c702fa80e6814f838845e8704d476` |
| Omission | `cc9d5596ebcdee7810417d16cf5d9c33bb292ff91f5edac5fcabea0f5898144e` | `a1c444d66789dedb49e5cd17ad56b34b00d61b42d888e60693966b008c6025b7` |
| Reordering | `10abbe064d1fed5ecef1596aa3a8f4411bbc4c24dd34c3365f39d5a27db36554` | `23dcef7a1508efaf045ad65772954f0188b70b72bd188405b7ba20dc2056e4a9` |

The observation-only instrumented manifest SHA256 is `926f30e29f7d9d6622cdce2db620dec7ffe48a0a8ff53a51ad9fc6e055c249fe`. The control manifests hold the uninstrumented final test helper.

### Control output hashes

| Output | SHA256 |
| --- | --- |
| Baseline | `01b514d0c57719870927c2e0857c2865fb87d1fa6436db74af9f19aaa83b96a9` |
| Focused checkout | `9039f4916fcdc01665ed6ada4b9e1cb84849bcae93213598cd53b2f8d7cb0ac2` |
| Observed derivation | `16a8bf6fef7a3fb8c1c726d852f88f4cdf6cf05f9825283bec818f68b17c961b` |
| Omission | `7012c9db5dcfc6cd95e3ecf4e9b5ef1fb5358d36561f2c16fac9aa11aeee0115` |
| Omission restored | `475efb3988b8bacb698b89408c557b4f7030039b44ceff49c5e944a84b6b9489` |
| Reordering | `eeada8f3add76b70a9d6ee4b96b74599976bfaf01ae1ba6775bd8e0a259db8b5` |
| Reordering restored | `6e3b4d4cb84cfa9ebde99bc8ac4b80922c3400f822527395893b46c3160f07ed` |

## Required final checks

All commands ran after the final source changes with child `TMPDIR=/var/tmp` directly. Working directory was the repository root except external-plugin commands, which ran in `testdata/external-plugins`.

| Command | UTC start, 2026-10-23 | UTC end | Actual exit | Material output |
| --- | --- | --- | ---: | --- |
| `TMPDIR=/var/tmp task fmt` | 04:16:26Z | 04:16:32Z | 0 | No output or further changes |
| `TMPDIR=/var/tmp task fix_dry_run` | 04:16:32Z | 04:16:32Z | 0 | Empty proposal; no fixes to apply |
| `TMPDIR=/var/tmp task lint` | 04:16:32Z | 04:16:39Z | 201 | `0 issues.`, `ifaceguard: no errors found`; scanner reports only GO-2026-6809 and exits 3 |
| `TMPDIR=/var/tmp task test` | 04:15:12.332128889Z | 04:15:13.709424700Z | 0 | Root and external-plugin unit suites pass, cached results present |
| `TMPDIR=/var/tmp task itest` | 04:15:13.713319274Z | 04:20:55.569638636Z | 0 | Root and external integration suites pass, cached results present |
| `TMPDIR=/var/tmp task test-coverage` | 04:20:55.573717712Z | 04:25:14.243888326Z | 0 | `Coverage: 84.2%, required: 80.0%` |
| `TMPDIR=/var/tmp task build BUILD_ROOT=<temporary-build-root>` | 04:25:35.154447994Z | 04:25:37.471843245Z | 0 | Host, tools, compaction and TUI executables built |
| External lint command below | 04:25:17.005994315Z | 04:25:17.399573036Z | 0 | `0 issues.` |

`BUILD_ROOT=<temporary-build-root>` is an explicit abbreviation for the fresh temporary output directory used on the larger filesystem. To reproduce the build, choose a fresh temporary output directory on that filesystem and substitute it for the placeholder. An earlier temporary-root build also exited 0 at 04:25:14.247829911Z through 04:25:17.002241512Z. The authoritative build above records the later run's times, exit and output hash.

External lint was run separately because main lint stopped at the scanner:

```sh
# ROOT denotes the repository root.
LINT_TOOL=$(cd "$ROOT" && go tool -n golangci-lint)
cd "$ROOT/testdata/external-plugins"
TMPDIR=/var/tmp "$LINT_TOOL" run --config ../../.golangci.yml --build-tags=integration ./...
```

The scanner exception is solely GO-2026-6809, `crypto/x509@go1.27.2`, fixed in Go 1.27.3. No suppression or version change was applied. The report does not describe `task lint` as a zero-exit pass.

The repeated fmt/fix/lint observations overlap the delegated test run. They changed no source; the final source hashes match the baseline, restored-control and corrected check manifests. The instrumented observation and mutated controls have the distinct identities recorded above. Initial successful recorder outputs for fmt/fix/lint were deleted by the recorder error described below, so the retained repeated observations are authoritative.

### Check output hashes

| Output | SHA256 |
| --- | --- |
| fmt and fix_dry_run, each empty | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| lint | `1b69288b32d3a618014146aeb5ac1cdc449bed2201ffba9e01caa25a2e416399` |
| test | `1c10243a44d2adfec09ee098a45208713bd797bd97b2890dd09a89bfe5c635d4` |
| itest | `57c9056cb08a61d93a4ee4fccd2e21a5f755fe87afc37ec3f037ded3e0eccf38` |
| test-coverage | `407a916bdb2ce99a155ef36db04d469bb393b524d60047ed416f5e819e2b4b49` |
| authoritative build | `d19065a703c0624af7fdb8af459020ff40fc606705794816a292722d7a524935` |
| external lint | `16c497704e298bf3741c816833610cdc574754388642cf5d96a39daae61a9781` |

Task test/itest permit Go result caching. Coverage executes unit and integration tests with `-count=1`, but its successful output hides per-test events. To obtain an executed skip inventory, these additional commands ran with `-count=1`:

```sh
TMPDIR=/var/tmp go test -count=1 -json -race -tags=integration -p 1 -parallel 1 ./...
```

| Working directory | UTC start, 2026-10-23 | UTC end | Exit | Output SHA256 |
| --- | --- | --- | ---: | --- |
| Root | 04:25:37.475531206Z | 04:32:56.468129377Z | 0 | `58384a9d371dbf47005ff2b641697ca0ec0997305c885f8755eca3e2e6c637ad` |
| External plugins | 04:32:56.473392326Z | 04:33:03.542316797Z | 0 | `602df53595c18e6aeca0616a1faafbf1b5680f78c9a14a51a8335ff49178bd45` |

## Skips and evidence limits

The uncached JSON inventories contain 64 skip events: 57 package-level events for packages without matching tests and seven named tests:

| Package | Named skipped test | Reported prerequisite |
| --- | --- | --- |
| `host/internal/app` | `TestRunWithPathsHeadlessPersistenceFailurePreservesContext` | Darwin directory permission enforcement |
| `host/internal/app` | `TestClientAppSuites/Programmatic/TestResumeRecoveryPersistenceFailureProcessPath` | Darwin `chflags` |
| `host/internal/app` | `TestLifecycleObserverThroughStandardTUI` | Darwin arm64 real PTY acceptance |
| `host/internal/app` | `TestStandardTUIHostSmoke` | Darwin arm64 real PTY acceptance |
| `host/internal/app` | `TestStandardTUIHostRuntimePersistenceFailures` | Darwin arm64 real PTY acceptance |
| `internal/testsupport/tui` | `TestEarlyFailureCleanup` | Darwin arm64 real PTY cleanup |
| `plugins/ui/tui/internal/app` | `TestStandardTUIPTY` | Darwin arm64 real PTY acceptance |

These scenarios were skipped, not executed successfully. The strengthened history cases executed and passed. Sensitivity runs establish detection of the targeted omission and first-two-item swap, rather than every possible history corruption. Summary wrapper wording remains deliberately outside the equality contract. No independent repository review, generation-stability conclusion, whole-phase acceptance reassessment or unit-completion claim is supplied here.

### Recorder deviations

The check recorder initially removed its shared temporary evidence directory. This deleted earlier focused logs and an in-progress observation copy. A subsequent shell could not write its completion files because that copy directory had disappeared. Earlier tool-returned observations are historical; they are not used as retained evidence. The retained baseline, focused, observation and both controls were recreated in a separate directory. Baseline and focused passes precede all retained mutations.

The initial recorder also used invalid `git ls-files -cached`. Both initial `manifest-before.sha256` and `manifest-after.sha256` files are empty and unusable. Corrected complete manifests include the new test and match the separately recorded baseline manifest before and after checks. These replacement records support source correspondence; they do not recreate an uninterrupted original pre-run provenance record. The initial temporary build location was replaced by another passing build on the larger temporary filesystem.

The focused checkout pass retains output, timestamps, exit and log digest, but its per-run `.command` and `.complete-source.sha256` files are absent. That run alone does not establish exact command/source provenance. The separately recorded baseline, controls, restorations and uncached checkout inventory independently support the required restored-suffix observations.

The historical aggregate checksum index `evidence-sha256-complete.txt` contains a stale entry for the descriptive `recorder-failures.txt`: recorded SHA256 `c20ad1dfd1fa418a5be38e4a9c643183e4c45a1bd72e93958e66e6112d08f63b`, actual SHA256 `8ea3e64b47aaf27484f8f06b5147154f702ec9fb56457ed880c85054c4aadd9c`. The whole aggregate cannot be described as clean. The reviewed check/control output hashes and source identities match their retained records; no required runtime observation depends on historical bytes of that descriptive file. Original artifacts and indexes remain unchanged.

These are recording failures within the current work. Their consequence is MEDIUM because missing identity/output could invalidate later acceptance reasoning. The observed filesystem loss and invalid Git invocation establish the failure directly. The options were to leave evidence missing or rerun and reconstruct it. The latter was chosen within the approved evidence scope: isolated artifact ownership and corrected manifest enumeration address the recording causes without changing product source or acceptance criteria. The remaining provenance gaps and stale aggregate entry are current recorder limits, not a new product debt exception. No new product limitation or user decision was needed.

No production defect was found in the scoped scenarios. Existing accepted scanner debt and platform prerequisites remain. Review and unit completion remain for the delivery owner; no status file was advanced.
