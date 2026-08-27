# Project Rules

NO BACKWARDS COMPATIBILITY AT ALL. This is new project

NO OVERENGINEERING: REMEMBER, we're not building a "spaceship," just a local developer tool. ALWAYS CRITICALLY EVALUATE need to address edge cases based on their REALISM.

## Language
1. All documentation and code comments MUST be written in English.

## Documentation
1. `docs/terms.md`: domain glossary
1. `docs/artefacts/`: various artefacts
2. `docs/specs/features/`: features
3. `docs/specs/issues/`: issues

## Tech stack
1. go 1.26
2. `github.com/stretchr/testify` for tests
3. `github.com/caarlos0/env/v11` for loading configuration from environment variables
4. `log/slog` for logging (must use structured logging with context). Use global logger with context, instead of passing logger instances around. E.g. `slog.DebugContext`.
5. `github.com/cenkalti/backoff/v7` for retry strategies
6. `github.com/samber/lo` for slices/maps/strings/channels/functions (if no standard library functions available)
7. `github.com/samber/mo` and `mo.Option` for optional fields instead of pointers or empty values.

## Coding rules
1. MUST run before completing changes:
    1) `go fix -diff ./...` -> analyze proposal -> `go fix ./...`
    2) `task lint`
    3) `task test`
2. Use pi code ONLY as a source of ideas, but NOT AS a source of algorithms, since this project has a COMPLETELY DIFFERENT ARCHITECTURE.
3. Empty structs MAY use `T{}`. If any field is set, struct literal MUST initialize every field explicitly. MUST NOT assign fields after `T{}` to bypass `exhaustruct`.
4. Suppressing `//nolint:exhaustruct` is prohibited except when partial struct initialization is intentional, such as in Protobuf `oneof` builders that set only the active field.

## Testing rules
1. Use `t.Context()` instead of `context.Background()`
2. Use `go.uber.org/mock` for mocks. Custom mocks are FORBIDDEN. Use `//go:generate go tool mockgen ...` to generate mocks
3. Use `github.com/stretchr/testify` and `testify/suite`
4. Use `t.Parallel()` if possible
5. Each test MUST have a function comment describing scenario and descriptive `Arrange`, `Act`, and `Assert` comments, e.g.: `// Arrange test dependencies`.

## Pi Documentation (use for feature comparison ONLY, NOT for architecture extraction)
1. /opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/README.md
2. /opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/
3. /opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/examples/README.md