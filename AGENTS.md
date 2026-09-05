# Project Rules

1. NO BACKWARDS COMPATIBILITY AT ALL (code, proto, etc.). This is new project
2. NO OVERENGINEERING: REMEMBER, we're not building a "spaceship", just a local developer tool. ALWAYS CRITICALLY EVALUATE need to address edge cases based on their REALISM.

## Goal

Glyph is a local, extensible coding agent with a thin provider-neutral Agent Core and Host-managed plugins.

## Language
1. All documentation and code comments MUST be written in English.

## Documentation
1. `docs/terms.md`: domain glossary
2. `docs/artefacts/`: various artefacts (external research, etc.)
3. `docs/roadmap.md`: project roadmap. MUST keep up to date.
4. `docs/specs/features/`: features
5. `docs/specs/issues/`: issues. `docs/specs/issues/issues.md` contains list of all issues with status, keep up to date.
6. `docs/guidelines/`: user and developer guidelines

For regular features and issues:
1. `docs/specs/{features or issues}/{feature/issue name}/problem.md`: problem statement and user story
2. `docs/specs/{features or issues}/{feature/issue name}/terms.md`: feature/issue-specific terminology
3. `docs/specs/{features or issues}/{feature/issue name}/prd.md`: feature/issue-specific requirements
4. `docs/specs/{features or issues}/{feature/issue name}/solution.md`: technical solution

For complex features:
1. `docs/specs/features/{feature name}/problem.md`: problem statement and user story
2. `docs/specs/features/{feature name}/terms.md`: feature-specific terminology
3. `docs/specs/features/{feature name}/prd.md`: feature-specific requirements
4. `docs/specs/features/{feature name}/delivery-plan.md`: phase order and dependencies
5. `docs/specs/features/{feature name}/phases/<phase>/ticket.md`: phase requirements and completion criteria
6. `docs/specs/features/{feature name}/phases/<phase>/solution.md`: phase technical solution

MUST NOT duplicate information. Instead, provide links to existing documents.

## Tech stack
1. go 1.27
2. `github.com/stretchr/testify` for tests
3. `github.com/caarlos0/env/v11` for loading configuration from environment variables
4. `log/slog` for logging (must use structured logging with context). Use global logger with context, instead of passing logger instances around. E.g. `slog.DebugContext`.
5. `github.com/cenkalti/backoff/v7` for retry strategies
6. `github.com/samber/lo` for slices/maps/strings/channels/functions (if no standard library functions available)
7. `github.com/samber/mo` and `mo.Option` for optional fields instead of pointers or empty values.

## Architecture Decisions
1. NO PARANOID SAFETY: don't hide errors from user, etc. User is ONLY owner of this tool.
2. Errors MUST preserve and expose complete error text, including original cause, across every layer and public contract. Machine-readable codes MUST supplement, never replace, that text. Only secrets MAY be redacted.
3. All operations that may take a noticeable amount of time must be asynchronous. Always prefer async over sync APIs.

## Coding rules
1. MUST run before completing changes in code:
    1) `task fmt`
    2) `task fix_dry_run` -> analyze proposal -> `task fix` or manually fix issues
    3) `task lint`
    4) `task test`, `task itest`
    5) `task test-coverage`
2. Use pi code ONLY as a source of ideas, but NOT AS a source of algorithms, since this project has a COMPLETELY DIFFERENT ARCHITECTURE.
3. Empty structs MAY use `T{}`. If any field is set, struct literal MUST initialize every field explicitly. MUST NOT assign fields after `T{}` to bypass `exhaustruct_v5`.
4. Suppressing `//nolint:exhaustruct_v5` is prohibited except when partial struct initialization is intentional, such as in Protobuf `oneof` builders that set only active field.
5. Use `mo.Option[T]` directly for required JSON fields, but use `*T` with `omitempty` when `Some` zero values must remain distinguishable from `None`.
6. MUST NOT suppress ifaceguard warnings in production code. If they appear, it means dependency direction is incorrect.
7. Define named constants for strings that represent stable UI text, commands, domain or protocol values, and formatting templates, even when used once. Keep incidental implementation text, including one-off error messages, inline.
8. Unused parameter such as `_ name` is allowed only in these cases:
    1) Current stage is an intermediate compile state and the parameter will be used by a later approved stage
    2) Function implements a shared interface whose parameter is used by another implementation

## Protobuf rules
1. Proto `edition 2023`
2. Use `int64` for all protobuf numeric fields representing indices, positions, sizes, counters, or calculations. `int32` is forbidden for such fields; remove int32 range checks, related errors, and narrowing conversions. Protobuf enums are exempt and must remain enums.
3. NO BACKWARDS COMPATIBILITY: no reserved fields, etc.

## Code structure
1. Avoid large files. More than 500 lines of code is a reason to consider splitting.
2. Separate logically related functionality into separate files of reasonable size.

## Code comments
1. MUST explain your code with concise and clear comments. Don't make developers guess what's going on!
2. Following objects MUST contain a comment describing their purpose (exported and non-exported):
    1) Functions
    2) Structures and ALL their fields
    3) Enums and contants (not just group, but ALL elements)
    4) Variables involved in business logic
3. Generated code MUST be ignored.

## Testing rules
1. Use `t.Context()` instead of `context.Background()`
2. Use `github.com/stretchr/testify` and `testify/suite`
3. Use `t.Parallel()` if possible
4. Each test MUST have a function comment describing scenario and descriptive `Arrange`, `Act`, and `Assert` comments, e.g.: `// Arrange test dependencies`.
5. Any test that combines production components or exercises a production adapter against a real filesystem, network, process, or terminal MUST use `//go:build integration` and run through `task itest`.
6. ALL non integration tests MUST use `//go:build !integration` and run through `task test`.
7. MUST NEVER test mutable content (like prompts), ONLY logic.
8. MUST NEVER test logs.
9. MUST investigate causes of flaky tests.

### Mocking
1. Use `go.uber.org/mock` and `//go:generate go tool mockgen ...` to generate.
2. MUST NOT create custom mocks, ONLY mockgen-generated.
3. MUST NOT create interfaces or production abstractions solely for tests or mocking. Mocks MUST target interfaces consumed by PRODUCTION CODE.

## Pi Documentation (use for feature comparison ONLY, NOT for architecture extraction)
1. /opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/README.md
2. /opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/
3. /opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/examples/README.md