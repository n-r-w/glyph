# Constant regular-expression replacement evidence

## Status and baseline

UNT06 implementation is complete; final production issue verification and closure remain pending under UNT12. Baseline revision: `7bdd7dcd96b3961467beebab972946b8db47b328`. The starting worktree was clean. No reachable invalid-pattern failure exists for the fixed valid baseline expressions. This record establishes source elimination and observable behavior preservation, with no claimed failing baseline or RED run.

## Source and equivalence

The three `regexp.MustCompile` initializers and their imports are removed from `host/internal/usecase/host/sessions/service.go`, `host/internal/usecase/host/programmatic/stored_session.go`, and `host/internal/usecase/host/ui/stored_session.go`.

`Service.SetActiveName` in the sessions file and `StoredSession.publicItem` in each preview file now use `strings.FieldsFunc`, a CR/LF-only predicate, `strings.Join` with one ASCII space, and the retained `strings.TrimSpace`. Package-local predicates are `sessions.isLineBreak` in `sessions/line_break.go` and `isListLineBreak` in each preview file. Reference inspection also found `LoadedSession.firstUserText` in `sessions/list.go` using the removed global; that caller uses the same replacement and still returns exact stored text after checking for an empty normalized preview.

For example, normalization maps `"\r\na\r\n\n\rb\n"` to `"a b"` and `" \ta  \tb\r\n \tc \t"` to `"a  \tb  \tc"`. The second example preserves internal spaces and tabs while replacing the CR/LF run with one space.

Equivalence follows from segmentation: each interior maximal CR/LF run separates two original source spans, so Join inserts the same single space as the regex. FieldsFunc discards edge delimiter runs; the old replacement spaces at those edges were removed by TrimSpace. FieldsFunc retains source byte spans, including invalid UTF-8, internal spaces, tabs, and non-CR/LF Unicode whitespace. The final trimming is unchanged. Join cannot enlarge this input because each inserted byte replaces at least one delimiter byte. Allocation and arbitrary runtime faults are outside this proof.

The preview mapping retains optional absence and present-empty values. The name mutation retains `session.ErrInvalidName` for empty normalized names. No error plumbing, recovery, shared normalization package, or dependency change was introduced.

## Executed preservation checks

Exact command, run once against baseline production plus the new tests and once against corrected production:

```sh
go test ./host/internal/usecase/host/{sessions,programmatic,ui} -run 'TestSetActiveNameNormalization|TestStoredSessionPreviewNormalization' -count=1
```

Both runs exited 0 with all three packages reporting `ok`. The tests are `host/internal/usecase/host/sessions/name_normalization_test.go`, `TestSetActiveNameNormalization`; `host/internal/usecase/host/programmatic/stored_session_normalization_test.go`, `TestStoredSessionPreviewNormalization`; and `host/internal/usecase/host/ui/stored_session_normalization_test.go`, `TestStoredSessionPreviewNormalization`. They assert public name results and list-preview bytes for maximal CR/LF runs, internal spaces/tabs, Unicode whitespace, invalid UTF-8, empty and all-delimiter inputs. Preview tests include absent and present-empty options. Name tests assert the invalid-name error and use generated repository/clock mocks to check the persisted name. These are logic tests, without real storage or combined production adapters.

## Checks

The following commands exited 0 on the corrected worktree:

- `task fmt`
- `task fix_dry_run`: no output or proposed changes; no fixes required.
- `task lint`: `0 issues.`, `ifaceguard: no errors found`, `No vulnerabilities found.`
- `task test`
- `task itest`
- `task test-coverage`: `Coverage: 84.3%, required: 80.0%`
- `task build`

Full logs and current source snapshots are retained in the collaboration report. Baseline-versus-corrected targeted runs establish preservation for the explicit cases; the segmentation argument covers other supported string inputs. This record does not claim final independent acceptance or process-continuity testing for an unreachable constant-pattern error.

## Scoped source scan

The full non-test Go source under `host`, `internal`, `plugins`, and `sdk` was searched with:

```sh
rg -n --glob '*.go' --glob '!**/*_test.go' --glob '!**/*_mock.go' --glob '!**/*.pb.go' '\bpanic\s*\(|\bMust\w*\s*\(|\.(MustGet|MustCompile)\b|log\.(Fatal|Panic)|slog\.(Fatal|Panic)' host internal plugins sdk
rg -n -i --glob '*.go' --glob '!**/*_test.go' 'panic|must|fatal|assert|\.unwrap\(' host internal plugins sdk
```

The first search returned no matches, exit 1. Review of the broader search found no additional intentional-panic API. Its fatal calls are test-support methods: `internal/testsupport/tui/tui.go:OutputObserver.WaitNext` at line 95 and `OutputObserver.WaitForOutputAfter` at line 114, and `sdk/plugins/ui/v1/sdk.go:TestClient` at lines 145, 155, and 159. They call `testing.T.Fatalf`, which terminates a test rather than intentionally panicking in production request handling. Remaining matches are ordinary error unwrapping, validation text, comments, and type-assertion explanations. Generated source and test-only calls are outside the production inventory. This lexical scan does not establish freedom from arbitrary runtime panics or panics inside dependencies.
